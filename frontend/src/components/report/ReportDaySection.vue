<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { VueDraggable, type DraggableEvent } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import type { ItemStatus, Leg, Media, PlanDay, PlanItem, Stay, Transfer } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaGallery from '@/components/media/MediaGallery.vue'
import MediaHints from '@/components/media/MediaHints.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import TransferRow from '@/components/plan/TransferRow.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import ReportLegLine from '@/components/report/ReportLegLine.vue'
import ReportPlaceCard from '@/components/report/ReportPlaceCard.vue'
import ReportStayCard from '@/components/report/ReportStayCard.vue'
import { useReportText } from '@/composables/useContentLanguage'
import { formatDayDate, formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'
import type { MediaHint } from '@/utils/mediaHints'
import { isVisit } from '@/utils/plan'

// One day of a report: what it was called, what happened, the places of the day
// and what it cost, ending where the night was spent, with its story. The
// journey to each place and to the night is told in a line above it: how, how
// long and how far.
//
// While a translation is written the day's words - its title, its notes and
// the stories of its places - are written in that language, and everything
// every language shares is edited as ever.
const props = defineProps<{
  day: PlanDay
  currency: string
  editing: boolean
  /** Leave out the places that were not reached. */
  hideSkipped: boolean
  /** The trip the day belongs to, which is what pictures are uploaded against. */
  tripId?: string
  /** What the pictures just uploaded here say about where they belong. */
  hints?: MediaHint[]
  /** The document's stays, which name the journey that starts at one. */
  stays?: Stay[]
  /** The document's transfers; those leaving or arriving on the day's date are shown. */
  transfers?: Transfer[]
  /** The elements of every day, which name the journey in from the day before. */
  documentItems?: PlanItem[]
  /** Whether the day may be deleted; a document keeps at least one day. */
  removable?: boolean
  /** Whether places are dragged: on a wide screen with a pointer, as in the plan. */
  draggable?: boolean
  /** The speed on the flat a line without times is estimated at. */
  trackSpeed?: number | null
}>()

const emit = defineEmits<{
  title: [title: string]
  notes: [notes: string]
  /** The moment the day is remembered by, in a line. */
  highlight: [highlight: string]
  status: [item: PlanItem, status: ItemStatus]
  rate: [item: PlanItem, rating: number | null]
  story: [item: PlanItem, story: string]
  edit: [item: PlanItem]
  translate: [item: PlanItem]
  remove: [item: PlanItem]
  /** A place goes to a position of a day, as in the plan. */
  move: [itemId: string, dayId: string, position: number]
  pickTarget: [item: PlanItem, mode: 'move' | 'copy']
  add: [kind: 'place' | 'activity']
  uploaded: [media: Media[]]
  cover: [media: Media | null]
  privacy: [media: Media, isPrivate: boolean]
  favoriteMedia: [media: Media, isFavorite: boolean]
  unlinkMedia: [media: Media]
  removeMedia: [media: Media]
  uploadedToPlace: [item: PlanItem, media: Media[]]
  placeCover: [item: PlanItem, media: Media | null]
  favoritePlaceMedia: [item: PlanItem, media: Media, isFavorite: boolean]
  unlinkPlaceMedia: [item: PlanItem, media: Media]
  importPlaceTrack: [item: PlanItem, file: File]
  removePlaceTrack: [item: PlanItem]
  moveMediaToDay: [day: PlanDay, media: Media[]]
  attachMediaToPlace: [item: PlanItem, media: Media[]]
  dismissHints: []
  removeDay: []
  editLeg: [leg: Leg]
  translateLeg: [leg: Leg]
  /** The story of the night the day ends with, told on its evening stay mark. */
  night: [night: PlanItem, story: string]
  translateTransfer: [transfer: Transfer]
}>()

const { t, locale } = useI18n()
const text = useReportText()

// legTranslatable says whether a journey's note is offered for translation:
// while a translation is written, and when there is a note to translate.
function legTranslatable(leg: Leg): boolean {
  return props.editing && text.value.translating && !!text.value.original(leg.id, 'note')
}

const places = computed(() => props.day.items.filter(isVisit))
// placeNumbers are the places' numbers in the day, as the map numbers its pins,
// counting the places hidden from the list too.
const placeNumbers = computed(() => new Map(places.value.map((item, index) => [item.id, index + 1])))

const shown = computed(() => (props.hideSkipped ? places.value.filter((item) => item.status !== 'skipped') : places.value))
const hidden = computed(() => places.value.length - shown.value.length)

// The places are dragged as in the plan - within the day, from and into other
// days - while every place is shown, so a position means the same to the page
// and to the server. The drag library reorders this copy while dragging; it is
// replaced whenever the document changes.
const dragging = computed(() => !!props.draggable && props.editing && !props.hideSkipped)
const local = ref<PlanItem[]>([])
watch(shown, (next) => {
  local.value = [...next]
}, { immediate: true })

// onDrop reports a place dropped into this day or moved within it.
function onDrop(event: DraggableEvent<PlanItem>): void {
  const item = event.data
  if (item) {
    emit('move', item.id, props.day.id, event.newDraggableIndex ?? event.newIndex ?? 0)
  }
}

// step moves a place one position up or down among the day's places.
function step(item: PlanItem, by: number): void {
  emit('move', item.id, props.day.id, places.value.findIndex((place) => place.id === item.id) + by)
}

// legTo finds the journey that ends at a place: the day keeps one between every
// pair of neighbouring elements, stay marks included.
const legTo = computed(() => new Map<string, Leg>(props.day.legs.map((leg) => [leg.to_item_id, leg])))

// legFrom names where a journey started when that is not the card right above
// it: a stay, or the last place of an earlier day for the journey that opens
// this one.
function legFrom(leg: Leg): string | undefined {
  const own = props.day.items.find((item) => item.id === leg.from_item_id)
  const from = own ?? props.documentItems?.find((item) => item.id === leg.from_item_id)
  if (!from) {
    return undefined
  }
  if (from.kind === 'stay_anchor') {
    return props.stays?.find((stay) => stay.id === from.stay_id)?.name || undefined
  }
  return own ? undefined : from.name || undefined
}

// night is the day's evening stay mark and the stay it names, when the day
// has a stay for the night.
const night = computed(() => {
  const mark = props.day.items.find((item) => item.kind === 'stay_anchor' && item.anchor === 'evening')
  const stay = mark && props.stays?.find((each) => each.id === mark.stay_id)
  return mark && stay ? { mark, stay } : null
})

// dayTransfers are the flights and trains that leave or arrive on the day's
// date; a day without a date has none.
const dayTransfers = computed(() => (props.transfers ?? []).filter((transfer) =>
  props.day.date !== null
  && (transfer.departure_date === props.day.date || (transfer.arrival_date ?? transfer.departure_date) === props.day.date)))

const date = computed(() => formatDayDate(props.day.date, locale.value, true))

/** MAX_HIGHLIGHT is how many characters the server keeps of a day's highlight. */
const MAX_HIGHLIGHT = 200

// highlight is the line being written, reset whenever the day or the language
// being written changes under it.
const highlight = ref('')
watch(
  () => text.value.source(props.day.id, 'highlight', props.day.highlight),
  (value) => {
    highlight.value = value
  },
  { immediate: true },
)
const spent = computed(() => formatMoney(props.day.summary.actual_cost, props.currency, locale.value))
</script>

<template>
  <section :id="`day-${day.position + 1}`" class="space-y-4 scroll-mt-20">
    <header class="space-y-1">
      <p class="text-sm text-base-content/60">
        {{ t('plan.dayNumber', { n: day.position + 1 }) }}<span v-if="date"> · {{ date }}</span>
      </p>
      <div v-if="editing" class="flex items-center gap-2">
        <input
          class="input input-ghost w-full text-xl font-bold"
          :value="text.source(day.id, 'title', day.title)"
          maxlength="200"
          :placeholder="text.original(day.id, 'title') || t('plan.dayTitlePlaceholder')"
          :aria-label="t('plan.dayTitle')"
          @change="emit('title', ($event.target as HTMLInputElement).value)"
        />
        <button
          v-if="removable && editing"
          type="button"
          class="btn btn-ghost btn-sm btn-square text-error"
          :aria-label="t('plan.deleteDay')"
          :title="t('plan.deleteDay')"
          @click="emit('removeDay')"
        >
          <AppIcon name="trash" />
        </button>
      </div>
      <h3 v-else-if="day.title" class="text-xl font-bold break-words">{{ day.title }}</h3>

      <p class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
        <span v-if="day.summary.distance_m > 0">{{ formatDistance(day.summary.distance_m, locale, activeUnits) }}</span>
        <span v-if="spent && Number(day.summary.actual_cost) !== 0">{{ spent }}</span>
      </p>
    </header>

    <EditableMarkdown
      :source="text.source(day.id, 'notes_md', day.notes_md)"
      :original="text.original(day.id, 'notes_md')"
      :editing="editing"
      :placeholder="t('report.dayStoryPlaceholder')"
      :rows="6"
      @save="(notes) => emit('notes', notes)"
    />

    <!-- The moment the day is remembered by: one line, set apart in the
         report's PDF. It is written here, and read as a quote. -->
    <label v-if="editing" class="flex flex-col gap-1">
      <span class="text-xs font-semibold tracking-wide text-primary uppercase">{{ t('report.highlight') }}</span>
      <input
        v-model="highlight"
        type="text"
        :maxlength="MAX_HIGHLIGHT"
        class="input w-full"
        :placeholder="text.original(day.id, 'highlight') || t('report.highlightPlaceholder')"
        @change="emit('highlight', highlight.trim())"
      />
      <span class="self-end text-xs text-base-content/60 tabular-nums" aria-live="polite">
        {{ t('attachment.counter', { count: [...highlight].length, max: MAX_HIGHLIGHT }) }}
      </span>
    </label>
    <blockquote
      v-else-if="day.highlight"
      class="rounded-box border-s-4 border-primary bg-base-200 px-4 py-3"
    >
      <p class="text-xs font-semibold tracking-wide text-primary uppercase">{{ t('report.highlight') }}</p>
      <p class="font-semibold break-words">{{ text.source(day.id, 'highlight', day.highlight) }}</p>
    </blockquote>

    <ul v-if="dayTransfers.length > 0" class="space-y-2" :aria-label="t('transfer.title')">
      <li
        v-for="transfer in dayTransfers"
        :key="transfer.id"
        class="flex items-start gap-2 rounded-box border border-base-300 p-3"
      >
        <TransferRow :transfer="transfer" :currency="currency" :date="day.date" class="flex-1" />
        <button
          v-if="editing && text.translating"
          type="button"
          class="btn btn-ghost btn-xs"
          @click="emit('translateTransfer', transfer)"
        >
          {{ t('report.translate') }}
        </button>
      </li>
    </ul>

    <VueDraggable
      v-model="local"
      :group="{ name: 'places', pull: true, put: true }"
      :disabled="!dragging"
      handle=".drag-handle"
      :animation="150"
      ghost-class="opacity-40"
      class="flex flex-col gap-3"
      :class="{ 'min-h-12': dragging }"
      @add="onDrop"
      @update="onDrop"
    >
      <div v-for="(item, index) in local" :key="item.id" class="flex flex-col gap-3">
        <div v-if="legTo.get(item.id)" class="flex items-center gap-2">
          <ReportLegLine
            class="min-w-0 flex-1"
            :leg="legTo.get(item.id)!"
            :from="legFrom(legTo.get(item.id)!)"
            :editing="editing"
            @edit="emit('editLeg', legTo.get(item.id)!)"
          />
          <button
            v-if="legTranslatable(legTo.get(item.id)!)"
            type="button"
            class="btn btn-ghost btn-xs"
            @click="emit('translateLeg', legTo.get(item.id)!)"
          >
            {{ t('report.translate') }}
          </button>
        </div>
        <ReportPlaceCard
          :item="item"
          :currency="currency"
          :editing="editing"
          :trip-id="tripId"
          :first="index === 0"
          :last="index === local.length - 1"
          :draggable="dragging"
          :track-speed="trackSpeed"
          :number="placeNumbers.get(item.id)"
          @status="(status) => emit('status', item, status)"
          @rate="(rating) => emit('rate', item, rating)"
          @story="(story) => emit('story', item, story)"
          @edit="emit('edit', item)"
          @translate="emit('translate', item)"
          @remove="emit('remove', item)"
          @up="step(item, -1)"
          @down="step(item, 1)"
          @pick-target="(mode) => emit('pickTarget', item, mode)"
          @uploaded="(media) => emit('uploadedToPlace', item, media)"
          @cover="(media) => emit('placeCover', item, media)"
          @privacy="(media, isPrivate) => emit('privacy', media, isPrivate)"
          @favorite-media="(media, isFavorite) => emit('favoritePlaceMedia', item, media, isFavorite)"
          @unlink-media="(media) => emit('unlinkPlaceMedia', item, media)"
          @remove-media="(media) => emit('removeMedia', media)"
          @import-track="(file) => emit('importPlaceTrack', item, file)"
          @remove-track="emit('removePlaceTrack', item)"
        />
      </div>
    </VueDraggable>

    <p v-if="hidden > 0" class="text-sm text-base-content/60">{{ t('report.hiddenSkipped', hidden) }}</p>

    <div v-if="night" class="flex flex-col gap-3">
      <div v-if="legTo.get(night.mark.id)" class="flex items-center gap-2">
        <ReportLegLine
          class="min-w-0 flex-1"
          :leg="legTo.get(night.mark.id)!"
          :editing="editing"
          @edit="emit('editLeg', legTo.get(night.mark.id)!)"
        />
        <button
          v-if="legTranslatable(legTo.get(night.mark.id)!)"
          type="button"
          class="btn btn-ghost btn-xs"
          @click="emit('translateLeg', legTo.get(night.mark.id)!)"
        >
          {{ t('report.translate') }}
        </button>
      </div>
      <ReportStayCard
        :night="night.mark"
        :stay="night.stay"
        :currency="currency"
        :editing="editing"
        :trip-id="tripId"
        @story="(story) => emit('night', night!.mark, story)"
        @uploaded="(media) => emit('uploadedToPlace', night!.mark, media)"
        @privacy="(media, isPrivate) => emit('privacy', media, isPrivate)"
        @favorite-media="(media, isFavorite) => emit('favoritePlaceMedia', night!.mark, media, isFavorite)"
        @unlink-media="(media) => emit('unlinkPlaceMedia', night!.mark, media)"
        @remove-media="(media) => emit('removeMedia', media)"
      />
    </div>

    <!-- The places are what a day of a report is made of, so adding one comes
         right after them and before the pictures, as two tiles that are hard
         to miss. -->
    <div v-if="editing" class="grid gap-3 sm:grid-cols-2">
      <button
        type="button"
        class="flex min-h-16 items-center justify-center gap-3 rounded-box border-2 border-dashed border-primary/30 bg-primary/5 px-4 py-3 font-medium text-primary transition-colors hover:border-primary/60 hover:bg-primary/10"
        @click="emit('add', 'place')"
      >
        <AppIcon name="plus" />
        {{ t('report.addPlace') }}
      </button>
      <button
        type="button"
        class="flex min-h-16 items-center justify-center gap-3 rounded-box border-2 border-dashed border-primary/30 bg-primary/5 px-4 py-3 font-medium text-primary transition-colors hover:border-primary/60 hover:bg-primary/10"
        @click="emit('add', 'activity')"
      >
        <AppIcon name="plus" />
        {{ t('report.addActivity') }}
      </button>
    </div>

    <MediaGallery
      v-if="day.media.length > 0"
      :items="day.media"
      :can-edit="editing"
      :cover-id="day.cover_media_id"
      :trip-id="tripId"
      prefer-favorites
      :can-favorite="editing"
      @cover="(media) => emit('cover', media)"
      @privacy="(media, isPrivate) => emit('privacy', media, isPrivate)"
      @favorite="(media, isFavorite) => emit('favoriteMedia', media, isFavorite)"
      @unlink="(media) => emit('unlinkMedia', media)"
      @remove="(media) => emit('removeMedia', media)"
    />

    <MediaHints
      v-if="hints && hints.length > 0"
      :hints="hints"
      @move-to-day="(target, media) => emit('moveMediaToDay', target, media)"
      @attach-to-place="(item, media) => emit('attachMediaToPlace', item, media)"
      @dismiss="emit('dismissHints')"
    />
    <MediaUploader v-if="editing && tripId" :trip-id="tripId" small @uploaded="(media) => emit('uploaded', media)" />
  </section>
</template>
