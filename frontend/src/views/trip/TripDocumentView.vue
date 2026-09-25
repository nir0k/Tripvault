<script setup lang="ts">
import { computed, onMounted, provide, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import * as documentsApi from '@/api/documents'
import * as mediaApi from '@/api/media'
import { getClientConfig } from '@/api/config'
import { downloadSharedReportPDF } from '@/api/shared'
import type {
  ClientConfig, ItemStatus, Leg, Media, PlanDay, PlanItem, Transfer, TranslationEntry, TranslationTarget,
  TripDocument,
} from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { LANGUAGE_NAMES } from '@/i18n'
import PlanLegDialog from '@/components/plan/PlanLegDialog.vue'
import PlanMap from '@/components/plan/PlanMap.vue'
import ContentLanguageSwitch from '@/components/report/ContentLanguageSwitch.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import ReportDayNav from '@/components/report/ReportDayNav.vue'
import ReportDaySection from '@/components/report/ReportDaySection.vue'
import ReportPlaceDialog from '@/components/report/ReportPlaceDialog.vue'
import ReportFloatingActions from '@/components/report/ReportFloatingActions.vue'
import ReportTotalsBar from '@/components/report/ReportTotalsBar.vue'
import ReportTranslateDialog, { type TranslateField } from '@/components/report/ReportTranslateDialog.vue'
import { reportTextKey, useContentLanguage, type ReportTextEditing } from '@/composables/useContentLanguage'
import { useLegCalculation } from '@/composables/useLegCalculation'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { mediaHint, type MediaHint } from '@/utils/mediaHints'
import { tripRoute } from '@/utils/tripRoutes'
import { isVisit } from '@/utils/plan'
import { revealElement } from '@/utils/reveal'
import { activeUnits } from '@/utils/units'
import {
  translatableTexts, translateDocument, translationOf, translationProgress,
} from '@/utils/translate'

// The report: a trip as it actually went, read as prose day by day and edited in
// the same layout rather than in a separate editor, so what you write is what
// you will read.
//
// Both modes live in the address: ?mode=edit opens the editable one, and
// ?skipped=hide leaves out the places that were not reached.
//
// A report is a trip of its own, so this page reads the one document its trip
// holds; a report made from a plan points back at it for whoever may open it.
//
// A report may be written in several languages. The page shows its words in
// the language chosen above it - the reader's own when the report has it - and
// editing in a language other than the original writes a translation: only the
// words can be changed then, each beside the original it is made from.

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const document = ref<TripDocument | null>(null)
const clientConfig = ref<ClientConfig | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
// The day a new place is added to.
const placeDay = ref<string | null>(null)
// What the pictures of the last upload suggest about where they belong, and the
// day they were uploaded under, which is where the offer is shown.
const hints = ref<MediaHint[]>([])
const hintsDayId = ref<string | null>(null)

// Exporting a report of a few hundred photographs takes a moment, so the button
// says so rather than looking as if nothing happened.
const exporting = ref(false)

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const placeDialog = useTemplateRef<InstanceType<typeof ReportPlaceDialog>>('placeDialog')
const legDialog = useTemplateRef<InstanceType<typeof PlanLegDialog>>('legDialog')
const translateDialog = useTemplateRef<InstanceType<typeof ReportTranslateDialog>>('translateDialog')

const trip = computed(() => store.trip)
const canEdit = computed(() => trip.value?.role === 'owner' || trip.value?.role === 'editor')
const currency = computed(() => trip.value?.currency ?? 'EUR')
const documentId = computed(() => trip.value?.report_id ?? null)
// The plan this report was copied from, when the reader may open it.
const sourcePlan = computed(() => {
  const source = trip.value?.source_trip_id
  return source ? tripRoute({ id: source, kind: 'plan' }) : null
})

// Dragging pictures into order needs a pointer and room; a phone uses the menu.
const wideEnoughToDrag = useMediaQuery('(min-width: 1024px) and (pointer: fine)')
// With room for it the trip column holds the days, as it does for the plan.
const wideNav = useMediaQuery('(min-width: 1024px)')

const editing = computed(() => canEdit.value && route.query.mode === 'edit')
// documentItems are the elements of every day, which the journey that opens a
// day starts from.
const documentItems = computed(() => document.value?.days.flatMap((day) => day.items) ?? [])

const languages = computed(() => trip.value?.languages ?? [])
const { lang: contentLang, original: originalLang, choose: chooseLanguage } = useContentLanguage(() => languages.value)
// translating is editing in a language other than the original.
const translating = computed(() => editing.value && contentLang.value !== originalLang.value)
// shown is the document in the language chosen, the original standing in for
// every field nobody translated.
const shown = computed(() => (document.value ? translateDocument(document.value, contentLang.value) : null))
const shownItems = computed(() => shown.value?.days.flatMap((day) => day.items) ?? [])
// originals are the texts a translation is made from, by element and field.
const originals = computed(() => new Map(
  (document.value ? translatableTexts(document.value) : []).map((text) => [`${text.id}.${text.field}`, text.original]),
))
const progress = computed(() => {
  const current = document.value
  return current
    ? Object.fromEntries(languages.value.slice(1).map((code) => [code, translationProgress(current, code)]))
    : {}
})

// The editors of the page learn from here which text they open with.
const textEditing = computed<ReportTextEditing>(() => translating.value
  ? {
      translating: true,
      source: (id, field) => translationOf(document.value?.translations, contentLang.value, id, field),
      original: (id, field) => originals.value.get(`${id}.${field}`) ?? '',
    }
  : { translating: false, source: (_id, _field, text) => text, original: () => undefined })
provide(reportTextKey, textEditing)
const hideSkipped = computed(() => route.query.skipped === 'hide')

// searchFocus is where place searches look first: the middle of the report's
// known positions.
const searchFocus = computed(() => {
  const placed = (document.value?.days ?? [])
    .flatMap((day) => day.items)
    .filter((item) => item.lat !== null && item.lng !== null)
  if (placed.length === 0) {
    return null
  }
  return {
    lat: placed.reduce((sum, item) => sum + (item.lat ?? 0), 0) / placed.length,
    lng: placed.reduce((sum, item) => sum + (item.lng ?? 0), 0) / placed.length,
  }
})

onMounted(async () => {
  try {
    clientConfig.value = await getClientConfig()
  } catch {
    // Without the configuration the report still reads, just without a map.
  }
})

// load reads the document of the open trip.
async function load(): Promise<void> {
  const id = documentId.value
  if (!id) {
    document.value = null
    return
  }
  loading.value = true
  error.value = ''
  try {
    document.value = await store.readDocument(id)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }
}

watch(documentId, () => void load(), { immediate: true })

// The journeys between places are calculated like a plan's, so a report
// written from scratch tells how long and how far as well. What comes back is
// kept with the legs, and only while editing is anything asked for: reading a
// report never waits on the routing provider.
const { retryEstimates } = useLegCalculation({
  document,
  enabled: () => editing.value,
  busy,
  onError: (err) => { error.value = errorMessage(err, t, te) },
})
watch(editing, forgetHints)

// apply runs a change and shows the document the server returns.
async function apply(change: () => Promise<TripDocument>): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    document.value = await change()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

/**
 * saveText stores one field of the report: in the original through the change
 * given, or as a translation while one is written.
 *
 * Arguments:
 *   - target: the kind of element the field belongs to.
 *   - id: the element.
 *   - field: the field.
 *   - value: the text typed.
 *   - original: the change that writes the original.
 */
function saveText(target: TranslationTarget, id: string, field: string, value: string,
  original: () => Promise<TripDocument>): void {
  const current = document.value
  if (translating.value && current) {
    void apply(() => documentsApi.saveTranslations(current.id, contentLang.value, [
      { target_type: target, target_id: id, field, value },
    ]))
    return
  }
  void apply(original)
}

// saveDocumentText stores the words before or after the days.
function saveDocumentText(field: 'intro_md' | 'summary_md', value: string): void {
  const id = document.value?.id
  if (id) {
    saveText('document', id, field, value, () => documentsApi.updateDocument(id, { [field]: value }))
  }
}

// saveDayText stores a day's title or notes.
function saveDayText(day: PlanDay, field: 'title' | 'notes_md', value: string): void {
  saveText('day', day.id, field, value, () => documentsApi.updateDay(day.id, { [field]: value }))
}

// saveStory stores what happened at a place.
function saveStory(item: PlanItem, story: string): void {
  saveText('item', item.id, 'story_md', story, () => documentsApi.updatePlace(item.id, { story_md: story }))
}

// saveTranslation stores what the translation form sends.
async function saveTranslation(entries: TranslationEntry[]): Promise<void> {
  const current = document.value
  if (!current) {
    return
  }
  busy.value = true
  try {
    document.value = await documentsApi.saveTranslations(current.id, contentLang.value, entries)
    translateDialog.value?.close()
  } catch (err) {
    translateDialog.value?.fail(errorMessage(err, t, te))
  } finally {
    busy.value = false
  }
}

// translateField describes one field of the translation form, or nothing when
// its original is empty and there is nothing to translate.
function translateField(target: TranslationTarget, id: string, field: string, label: string,
  maxlength: number, multiline = false): TranslateField[] {
  const original = originals.value.get(`${id}.${field}`) ?? ''
  if (original === '') {
    return []
  }
  const value = translationOf(document.value?.translations, contentLang.value, id, field)
  return [{ target, id, field, label, original, value, maxlength, multiline }]
}

// translatePlace opens the translation of a place's name and description; its
// story is translated in place, like the other prose of the report.
function translatePlace(item: PlanItem): void {
  const original = documentItems.value.find((each) => each.id === item.id) ?? item
  translateDialog.value?.open(original.name, [
    ...translateField('item', item.id, 'name', t('place.name'), 200),
    ...translateField('item', item.id, 'description_md', t('place.description'), 20000, true),
  ])
}

// translateLeg opens the translation of a journey's note.
function translateLeg(leg: Leg): void {
  translateDialog.value?.open(t('report.translateLeg'), translateField('leg', leg.id, 'note', t('leg.note'), 500))
}

// translateTransfer opens the translation of a transfer's two ends and notes.
function translateTransfer(transfer: Transfer): void {
  const original = document.value?.transfers.find((each) => each.id === transfer.id) ?? transfer
  translateDialog.value?.open(`${original.from_name} → ${original.to_name}`, [
    ...translateField('transfer', transfer.id, 'from_name', t('transfer.fromName'), 200),
    ...translateField('transfer', transfer.id, 'to_name', t('transfer.toName'), 200),
    ...translateField('transfer', transfer.id, 'notes_md', t('transfer.notes'), 20000, true),
  ])
}

// editPlace opens the form a place is changed through: its own, or the
// translation of its words while one is written.
function editPlace(dayId: string, item: PlanItem): void {
  if (translating.value) {
    translatePlace(item)
    return
  }
  const original = documentItems.value.find((each) => each.id === item.id) ?? item
  openPlace(dayId, original)
}

// editLeg opens the form a journey is changed through, or the translation of
// its note while one is written.
function editLeg(leg: Leg): void {
  if (translating.value) {
    translateLeg(leg)
    return
  }
  const original = document.value?.days.flatMap((day) => day.legs).find((each) => each.id === leg.id) ?? leg
  legDialog.value?.open(original)
}

// removeDay deletes a day of the report after saying what goes with it: its
// places and its track are deleted - a report has no unassigned list to keep
// them in - while its photographs stay in the gallery. The days after it move
// one day earlier, so the trip is read again for its new end date. The question
// asked here is the confirmation, so the server is told the answer at once.
async function removeDay(day: PlanDay): Promise<void> {
  const places = day.items.filter(isVisit).length
  const details: string[] = []
  if (places > 0) {
    details.push(t('report.deleteDayPlaces', places))
  }
  if (day.media.length > 0) {
    details.push(t('report.deleteDayPhotos'))
  }
  details.push(t('report.deleteDayShift'))
  const question = t('report.confirmDeleteDay', { n: day.position + 1 })
  if (!(await confirmDialog.value?.ask(question, { details, danger: true }))) {
    return
  }
  await apply(() => documentsApi.deleteDay(day.id, true))
  if (hintsDayId.value === day.id) {
    forgetHints()
  }
  if (trip.value) {
    void store.load(trip.value.id)
  }
}

// setMode switches between reading and editing, keeping the choice in the address.
function setMode(mode: 'read' | 'edit'): void {
  void router.replace({ query: { ...route.query, mode: mode === 'edit' ? 'edit' : undefined } })
}

// toggleSkipped shows or hides the places that were not reached.
function toggleSkipped(): void {
  void router.replace({ query: { ...route.query, skipped: hideSkipped.value ? undefined : 'hide' } })
}

// exportPDF saves the report as a file. Photographs are asked for unless the
// reader turned them off, which is the difference between a small file and a
// large one. A link has no account behind it for the server to take the
// language and the units from, so those travel with its request.
async function exportPDF(photos: boolean): Promise<void> {
  const tripId = trip.value?.id
  if (!tripId || exporting.value) {
    return
  }
  exporting.value = true
  error.value = ''
  try {
    if (store.shared) {
      await downloadSharedReportPDF(locale.value, activeUnits.value, photos, contentLang.value || undefined)
    } else {
      await documentsApi.downloadReportPDF(tripId, photos, contentLang.value || undefined)
    }
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    exporting.value = false
  }
}

// openPlace opens the form for a new place or activity of a day, or for an
// existing one.
function openPlace(dayId: string | null, item: PlanItem | null = null, kind: 'place' | 'activity' = 'place'): void {
  placeDay.value = dayId
  placeDialog.value?.open(item, kind)
}

// savePlace creates or changes a place from the form, then imports the
// recording chosen there, which gives the place the times it has none of.
//
// A new place is found in the answer as the one element that was not there
// before. Should the recording be refused after the place was stored, the form
// stays open on that place, so a second try changes it instead of adding
// another.
async function savePlace(fields: documentsApi.PlaceFields, item: PlanItem | null, track: File | null): Promise<void> {
  const current = document.value
  if (!current) {
    return
  }
  const known = new Set(documentItems.value.map((each) => each.id))
  busy.value = true
  let saved: PlanItem | null = item
  try {
    document.value = item
      ? await documentsApi.updatePlace(item.id, fields)
      : await documentsApi.createPlace(current.id, placeDay.value, fields)
    saved = item ?? documentItems.value.find((each) => !known.has(each.id)) ?? null
    if (track && saved) {
      document.value = await documentsApi.importItemTrack(saved.id, track)
    }
    placeDialog.value?.close()
  } catch (err) {
    if (saved && saved !== item) {
      placeDialog.value?.open(saved)
    }
    placeDialog.value?.fail(errorMessage(err, t, te))
  } finally {
    busy.value = false
  }
}

// removePlace deletes a place after confirmation.
async function removePlace(item: PlanItem): Promise<void> {
  if (!(await confirmDialog.value?.ask(t('report.confirmDeletePlace', { name: item.name }), { danger: true }))) {
    return
  }
  await apply(() => documentsApi.deletePlace(item.id))
}

// Pictures live beside the document rather than in it: linking one, changing
// its privacy or removing it answers with nothing, so the document is read
// again afterwards and the galleries come back in step with it.
async function applyMedia(change: () => Promise<void>): Promise<void> {
  const id = documentId.value
  if (!id) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    await change()
    // The document is read back without the page's loading flag: raising it
    // would take the report off the screen for a moment and the reader would
    // find themselves at the top of the page, far from the picture they were
    // working on.
    document.value = await store.readDocument(id)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// importPlaceTrack makes a GPX or KML export the line of a place or an
// activity, such as the hike that started there.
function importPlaceTrack(item: PlanItem, file: File): void {
  void apply(() => documentsApi.importItemTrack(item.id, file))
}

// removePlaceTrack takes the recording of a place or an activity away.
function removePlaceTrack(item: PlanItem): void {
  void apply(() => documentsApi.deleteItemTrack(item.id))
}

// addDayMedia hangs freshly uploaded pictures at the end of a day's gallery and
// then reads what they say about where they belong.
async function addDayMedia(day: PlanDay, media: Media[]): Promise<void> {
  const ids = [...day.media.map((item) => item.id), ...media.map((item) => item.id)]
  await applyMedia(() => mediaApi.setMediaLinks('day', day.id, ids))
  readHints(media, day.id, null)
}

/**
 * readHints collects what the pictures just uploaded suggest: the day they were
 * taken on and the place they were taken next to, when either differs from
 * where they now hang. An offer is shown under the day they were added to.
 *
 * Arguments:
 *   - media: the pictures that were just stored.
 *   - dayId: the day they are linked to, or null.
 *   - itemId: the place they are linked to, or null.
 */
function readHints(media: Media[], dayId: string | null, itemId: string | null): void {
  const current = document.value
  const timezone = trip.value?.timezone ?? 'UTC'
  if (!current) {
    return
  }
  hints.value = media
    .map((picture) => mediaHint(picture, current, timezone, dayId, itemId))
    .filter((hint): hint is MediaHint => hint !== null)
  hintsDayId.value = dayId
}

/** forgetHints takes the offer away, which every answer to it does too. */
function forgetHints(): void {
  hints.value = []
  hintsDayId.value = null
}

// moveMediaToDay accepts the offer of a day: the pictures leave the day they
// were put in and join the one they were taken on.
async function moveMediaToDay(target: PlanDay, media: Media[]): Promise<void> {
  const from = hintsDayId.value
  const moved = new Set(media.map((picture) => picture.id))
  const source = document.value?.days.find((day) => day.id === from) ?? null
  await applyMedia(async () => {
    if (source) {
      const left = source.media.filter((picture) => !moved.has(picture.id)).map((picture) => picture.id)
      await mediaApi.setMediaLinks('day', source.id, left)
    }
    const joined = [...target.media.map((picture) => picture.id), ...moved]
    await mediaApi.setMediaLinks('day', target.id, [...new Set(joined)])
  })
  forgetHints()
}

// attachMediaToPlace accepts the offer of a place, leaving the pictures in the
// day as well: a photograph belongs to the day it was taken and to what it shows.
async function attachMediaToPlace(item: PlanItem, media: Media[]): Promise<void> {
  const ids = [...item.media.map((picture) => picture.id), ...media.map((picture) => picture.id)]
  await applyMedia(() => mediaApi.setMediaLinks('item', item.id, [...new Set(ids)]))
  forgetHints()
}

// setDayMediaOrder stores the order the pictures were dragged into.
function setDayMediaOrder(day: PlanDay, mediaIds: string[]): void {
  void applyMedia(() => mediaApi.setMediaLinks('day', day.id, mediaIds))
}

// unlinkDayMedia takes a picture out of a day, leaving the file itself alone.
function unlinkDayMedia(day: PlanDay, media: Media): void {
  const ids = day.media.filter((item) => item.id !== media.id).map((item) => item.id)
  void applyMedia(() => mediaApi.setMediaLinks('day', day.id, ids))
}

// setDayMediaFavorite chooses whether the report shows a picture on this day.
// The whole gallery is sent with the favourites named, which is how the API
// takes them; the server refuses one picture too many and the message says so.
function setDayMediaFavorite(day: PlanDay, media: Media, isFavorite: boolean): void {
  void applyMedia(() => mediaApi.setMediaLinks('day', day.id,
    day.media.map((picture) => picture.id), favoriteIDs(day.media, media, isFavorite)))
}

// favoriteIDs works out the favourites of a gallery after one picture was
// marked or unmarked, keeping the order the gallery holds them in.
function favoriteIDs(gallery: Media[], media: Media, isFavorite: boolean): string[] {
  return gallery
    .filter((picture) => (picture.id === media.id ? isFavorite : picture.is_favorite))
    .map((picture) => picture.id)
}

// setDayCover picks the picture a day is shown by, or takes it away.
function setDayCover(day: PlanDay, media: Media | null): void {
  void apply(() => documentsApi.updateDay(day.id, { cover_media_id: media?.id ?? null }))
}

// addPlaceMedia hangs freshly uploaded pictures at the end of a place's gallery
// and then reads what they say about where they belong.
async function addPlaceMedia(item: PlanItem, media: Media[]): Promise<void> {
  const ids = [...item.media.map((picture) => picture.id), ...media.map((picture) => picture.id)]
  await applyMedia(() => mediaApi.setMediaLinks('item', item.id, ids))
  readHints(media, item.day_id, item.id)
}

// setPlaceMediaOrder stores the order the pictures of a place were dragged into.
function setPlaceMediaOrder(item: PlanItem, mediaIds: string[]): void {
  void applyMedia(() => mediaApi.setMediaLinks('item', item.id, mediaIds))
}

// unlinkPlaceMedia takes a picture off a place, leaving the file itself alone.
function unlinkPlaceMedia(item: PlanItem, media: Media): void {
  const ids = item.media.filter((picture) => picture.id !== media.id).map((picture) => picture.id)
  void applyMedia(() => mediaApi.setMediaLinks('item', item.id, ids))
}

// setPlaceMediaFavorite chooses whether the report shows a picture of a place.
function setPlaceMediaFavorite(item: PlanItem, media: Media, isFavorite: boolean): void {
  void applyMedia(() => mediaApi.setMediaLinks('item', item.id,
    item.media.map((picture) => picture.id), favoriteIDs(item.media, media, isFavorite)))
}

// setPlaceCover picks the picture a place is shown by, or takes it away.
function setPlaceCover(item: PlanItem, media: Media | null): void {
  void apply(() => documentsApi.updatePlace(item.id, { cover_media_id: media?.id ?? null }))
}

// setMediaPrivacy marks a picture private, or public again.
function setMediaPrivacy(media: Media, isPrivate: boolean): void {
  void applyMedia(async () => {
    await mediaApi.updateMedia(media.id, { is_private: isPrivate })
  })
}

// removeMedia deletes a picture from the trip, and so from every gallery and
// cover that showed it. There is no undo, so it is confirmed first.
async function removeMedia(media: Media): Promise<void> {
  const name = media.original_name || t('media.picture')
  if (!(await confirmDialog.value?.ask(t('media.confirmDelete', { name }), { danger: true }))) {
    return
  }
  await applyMedia(() => mediaApi.deleteMedia(media.id))
}

// saveLeg stores what the leg form says: the means, typed figures and costs.
async function saveLeg(leg: Leg, changes: documentsApi.LegChanges): Promise<void> {
  busy.value = true
  try {
    document.value = await documentsApi.updateLeg(leg.id, changes)
    legDialog.value?.close()
  } catch (err) {
    legDialog.value?.fail(errorMessage(err, t, te))
  } finally {
    busy.value = false
  }
}

// recalculateLeg asks the provider about one leg again, past the route cache,
// and shows the form with what came back.
async function recalculateLeg(leg: Leg): Promise<void> {
  busy.value = true
  try {
    document.value = await documentsApi.recalculateLeg(leg.id)
    const updated = document.value.days.flatMap((day) => day.legs).find((each) => each.id === leg.id)
    legDialog.value?.close()
    if (updated) {
      legDialog.value?.open(updated)
    }
  } catch (err) {
    legDialog.value?.fail(errorMessage(err, t, te))
  } finally {
    busy.value = false
  }
}

// focusItem answers a pin's "to the description": the page scrolls to the
// card of the place. A stay mark has no card, and a place the reader hid is
// not on the page, so for those the page goes to the day instead.
function focusItem(itemId: string, dayIndex: number | null): void {
  if (!revealElement(`item-${itemId}`) && dayIndex !== null) {
    revealElement(`day-${dayIndex + 1}`, false)
  }
}

// setStatus marks a place visited, skipped or unmarked again.
function setStatus(item: PlanItem, status: ItemStatus): void {
  void apply(() => documentsApi.updatePlace(item.id, { status }))
}
</script>

<template>
  <div class="max-w-5xl space-y-8">
    <p v-if="error" role="alert" class="alert alert-error">{{ error }}</p>
    <div v-if="loading" class="flex justify-center py-8"><span class="loading loading-spinner"></span></div>

    <div
      v-else-if="!document"
      class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-6 py-16 text-center"
    >
      <span class="text-primary"><AppIcon name="report" /></span>
      <p class="max-w-md text-base-content/70">{{ t('trip.reportMissing') }}</p>
    </div>

    <template v-else-if="shown">
      <!-- With room for it the trip column holds the days; on a narrow screen
           they stay here, as a row of chips above the report. -->
      <Teleport v-if="shown.days.length > 1" defer to="#trip-sidebar-days" :disabled="!wideNav">
        <ReportDayNav :days="shown.days" />
      </Teleport>

      <div class="flex flex-wrap items-center justify-between gap-2">
        <ContentLanguageSwitch
          v-if="languages.length > 1"
          :languages="languages"
          :model-value="contentLang"
          :label="editing ? t('report.writingLanguage') : t('report.contentLanguage')"
          :progress="editing ? progress : undefined"
          @update:model-value="chooseLanguage"
        />
        <span v-else></span>
        <div class="flex flex-wrap items-center gap-3">
          <RouterLink v-if="sourcePlan" :to="sourcePlan" class="btn btn-ghost btn-sm">
            <AppIcon name="plan" />
            {{ t('report.openPlan') }}
          </RouterLink>

          <div class="dropdown dropdown-end">
            <div tabindex="0" role="button" class="btn btn-sm" :class="{ 'btn-disabled': exporting }">
              <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
              <AppIcon v-else name="download" />
              {{ t('report.exportPdf') }}
            </div>
            <ul tabindex="0" class="menu dropdown-content z-10 w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
              <li><button type="button" @click="exportPDF(true)">{{ t('report.exportWithPhotos') }}</button></li>
              <li><button type="button" @click="exportPDF(false)">{{ t('report.exportWithoutPhotos') }}</button></li>
            </ul>
          </div>
        </div>
      </div>

      <p v-if="translating" role="status" class="alert alert-info text-sm">
        {{ t('report.translatingHint', { language: LANGUAGE_NAMES[contentLang] ?? contentLang }) }}
      </p>

      <p v-if="editing && clientConfig && !clientConfig.routing_enabled" role="status" class="alert alert-info text-sm">
        {{ t('leg.routingDisabled') }}
      </p>
      <div
        v-else-if="editing && document.estimated_legs > 0"
        role="status"
        class="alert alert-info flex-wrap gap-2 text-sm"
      >
        <span>{{ t('leg.estimatesPending', document.estimated_legs) }}</span>
        <button type="button" class="btn btn-sm" :disabled="busy" @click="retryEstimates">
          <span v-if="busy" class="loading loading-spinner loading-xs"></span>
          {{ t('leg.retryEstimates') }}
        </button>
      </div>

      <ReportTotalsBar v-if="document.totals" :totals="document.totals" :currency="currency" />

      <EditableMarkdown
        :source="textEditing.source(shown.id, 'intro_md', shown.intro_md)"
        :original="textEditing.original(shown.id, 'intro_md')"
        :editing="editing"
        :placeholder="t('report.introPlaceholder')"
        :rows="6"
        @save="(intro) => saveDocumentText('intro_md', intro)"
      />

      <div v-if="clientConfig" class="h-[60dvh]">
        <PlanMap
          :document="shown"
          :selected-day="null"
          :tile-url="clientConfig.map_tile_url"
          :attribution="clientConfig.map_attribution"
          @focus="focusItem"
        />
      </div>

      <div class="space-y-10">
        <ReportDaySection
          v-for="day in shown.days"
          :key="day.id"
          :day="day"
          :stays="shown.stays"
          :transfers="shown.transfers"
          :document-items="shownItems"
          :currency="currency"
          :editing="editing"
          :hide-skipped="hideSkipped"
          :trip-id="trip?.id"
          :draggable="wideEnoughToDrag"
          :hints="hintsDayId === day.id ? hints : []"
          :removable="shown.days.length > 1"
          @title="(title) => saveDayText(day, 'title', title)"
          @notes="(notes) => saveDayText(day, 'notes_md', notes)"
          @status="setStatus"
          @rate="(item, rating) => apply(() => documentsApi.updatePlace(item.id, { rating }))"
          @story="saveStory"
          @edit="(item) => editPlace(day.id, item)"
          @remove="removePlace"
          @add="(kind) => openPlace(day.id, null, kind)"
          @uploaded="(media) => addDayMedia(day, media)"
          @reorder-media="(ids) => setDayMediaOrder(day, ids)"
          @cover="(media) => setDayCover(day, media)"
          @privacy="(media, isPrivate) => setMediaPrivacy(media, isPrivate)"
          @favorite-media="(media, isFavorite) => setDayMediaFavorite(day, media, isFavorite)"
          @favorite-place-media="(item, media, isFavorite) => setPlaceMediaFavorite(item, media, isFavorite)"
          @unlink-media="(media) => unlinkDayMedia(day, media)"
          @remove-media="(media) => removeMedia(media)"
          @uploaded-to-place="(item, media) => addPlaceMedia(item, media)"
          @reorder-place-media="(item, ids) => setPlaceMediaOrder(item, ids)"
          @place-cover="(item, media) => setPlaceCover(item, media)"
          @unlink-place-media="(item, media) => unlinkPlaceMedia(item, media)"
          @import-place-track="importPlaceTrack"
          @remove-place-track="removePlaceTrack"
          @move-media-to-day="(target, media) => moveMediaToDay(target, media)"
          @attach-media-to-place="(item, media) => attachMediaToPlace(item, media)"
          @dismiss-hints="forgetHints"
          @remove-day="removeDay(day)"
          @edit-leg="editLeg"
          @translate-transfer="translateTransfer"
        />
      </div>

      <EditableMarkdown
        :source="textEditing.source(shown.id, 'summary_md', shown.summary_md)"
        :original="textEditing.original(shown.id, 'summary_md')"
        :editing="editing"
        :placeholder="t('report.summaryPlaceholder')"
        :rows="6"
        @save="(summary) => saveDocumentText('summary_md', summary)"
      />

      <ReportPlaceDialog ref="placeDialog" :focus="searchFocus" @save="savePlace" />
      <PlanLegDialog ref="legDialog" report :busy="busy" @save="saveLeg" @recalculate="recalculateLeg" />
      <ReportTranslateDialog ref="translateDialog" @save="saveTranslation" />
      <ConfirmDialog ref="confirmDialog" />
      <ReportFloatingActions
        :editing="editing"
        :can-edit="canEdit"
        :hide-skipped="hideSkipped"
        @toggle-edit="setMode(editing ? 'read' : 'edit')"
        @toggle-skipped="toggleSkipped"
      />
    </template>
  </div>
</template>
