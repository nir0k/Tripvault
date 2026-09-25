<script setup lang="ts">
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import * as mediaApi from '@/api/media'
import { MEDIA_FAVORITE_LIMIT } from '@/api/media'
import { updateTrip } from '@/api/trips'
import type { CoverCrop, Media, MediaTarget, PlanDay, PlanItem, TripDocument } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CoverCropDialog from '@/components/media/CoverCropDialog.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import MediaLinkDialog from '@/components/media/MediaLinkDialog.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import MediaViewer from '@/components/media/MediaViewer.vue'
import { useDropdownGroup } from '@/composables/useDropdown'
import { useProgressive } from '@/composables/useProgressive'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { formatDayDate } from '@/utils/format'
import { mediaLinkUpdates, type MediaLinkChange } from '@/utils/mediaLinks'
import { isVisit, itemIcon } from '@/utils/plan'

// Every picture of a trip in one place, laid out the way the trip's document
// files them: a section for each day, the day's own pictures first and then one
// group per place, and the pictures that hang nowhere at the end - which is the
// only way to find one after it was taken out of a gallery. A picture filed in
// two places is shown in both.
//
// A report shows a handful of favourites for each day and place, and the mark
// belongs to the link rather than to the file, so a star here means "a
// favourite of this day" or "of this place". A picture that hangs nowhere has
// no place to be a favourite of yet, and asking for it opens the choice of
// where it belongs. A plan has no favourites at all.
//
// A read-only link opens this page too, with nothing on it that changes the
// trip. A link reads no list of the trip's files, only its document, so there
// the page shows what the document files - which is all a link may see anyway.
//
// A trip of a few thousand photographs is laid out a batch at a time as the
// page is scrolled, and the viewer still walks all of them.

type Filter = 'all' | 'private' | 'unlinked'

const FILTERS: readonly Filter[] = ['all', 'private', 'unlinked']

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()
const { close: closeMenu } = useDropdownGroup()

const items = ref<Media[]>([])
// The trip's one document, the plan or the report: its pictures hang on its own
// days and places, and a report's photographs never mix with a plan's.
const documents = ref<TripDocument[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
// Filing a whole afternoon under one day picture by picture is no way to spend
// an evening, so every tile carries a circle to pick it out with. There is no
// mode to switch on: while nothing is picked a tile opens, and once something
// is, a tile joins the selection instead. As in Google Photos, the circle beside
// a day or a place picks all of its pictures, and Shift picks every tile between
// the one picked last and the one clicked.
const selected = ref(new Set<string>())

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const linkDialog = useTemplateRef<InstanceType<typeof MediaLinkDialog>>('linkDialog')
const viewer = useTemplateRef<InstanceType<typeof MediaViewer>>('viewer')
const coverCrop = useTemplateRef<InstanceType<typeof CoverCropDialog>>('coverCrop')
const more = useTemplateRef<HTMLElement>('more')

const trip = computed(() => store.trip)
const canEdit = computed(() => trip.value?.role === 'owner' || trip.value?.role === 'editor')
// Only a report has favourites: they are the pictures it shows for a day or a place.
const isReport = computed(() => trip.value?.kind === 'report')
const document = computed(() => documents.value[0] ?? null)

// The filter lives in the address, as the other choices of the application do.
// A link sees neither private files nor the ones filed nowhere, so it has none.
const filter = computed<Filter>(() => {
  if (store.shared) {
    return 'all'
  }
  const requested = route.query.show
  return FILTERS.find((each) => each === requested) ?? 'all'
})

// shown holds the files a document puts somewhere: on a day or on a place. What
// is not here is reachable from this page alone.
const shown = computed(() => {
  const ids = new Set<string>()
  for (const each of documents.value) {
    for (const day of each.days) {
      day.media.forEach((picture) => ids.add(picture.id))
      for (const item of day.items) {
        item.media.forEach((picture) => ids.add(picture.id))
      }
    }
    for (const item of each.unassigned) {
      item.media.forEach((picture) => ids.add(picture.id))
    }
  }
  return ids
})

/** Gallery is a day or a place of the document, as a link names it. */
interface Gallery {
  target: Extract<MediaTarget, 'day' | 'item'>
  id: string
}

/** Tile is one picture as a group shows it. */
interface Tile {
  /** The picture as the trip lists it, private flag included. */
  picture: Media
  /** A favourite of this group's day or place; never in a plan or without one. */
  favorite: boolean
  /** Its position in the viewer's reading order. */
  index: number
}

/** Group is the pictures of one day, of one place, or of nowhere. */
interface Group {
  key: string
  label: string
  icon: IconName
  /** The day or place the pictures hang on; null for those that hang nowhere. */
  gallery: Gallery | null
  tiles: Tile[]
}

/** Section is a day with its own pictures and its places', or the pictures of nowhere. */
interface Section {
  key: string
  label: string
  groups: Group[]
}

// byId finds the trip's own record of a picture: a document's galleries carry
// the link's favourite mark, the trip's list the picture's own flags.
const byId = computed(() => new Map(items.value.map((picture) => [picture.id, picture])))

// visible applies the filter to one picture; "unlinked" is handled by leaving
// out every section but the last.
function visible(picture: Media): boolean {
  return filter.value !== 'private' || picture.is_private
}

// dayLabel names a day the way the plan and the report do.
function dayLabel(day: PlanDay): string {
  const parts = [t('plan.dayNumber', { n: day.position + 1 })]
  if (day.date) {
    parts.push(formatDayDate(day.date, locale.value, true))
  }
  if (day.title) {
    parts.push(day.title)
  }
  return parts.join(' · ')
}

// sections lay the pictures out by where the document files them. Tiles are
// numbered as they are placed, so the viewer walks the page in its own order.
const sections = computed<Section[]>(() => {
  let index = 0
  const tilesOf = (gallery: Media[]): Tile[] => gallery.flatMap((linked) => {
    const picture = byId.value.get(linked.id)
    if (!picture || !visible(picture)) {
      return []
    }
    return [{ picture, favorite: isReport.value && linked.is_favorite, index: index++ }]
  })
  const placeGroup = (item: PlanItem): Group => ({
    key: `item:${item.id}`,
    label: item.name || t('media.linkUnnamedPlace'),
    icon: itemIcon(item),
    gallery: { target: 'item', id: item.id },
    tiles: tilesOf(item.media),
  })

  const result: Section[] = []
  if (filter.value !== 'unlinked') {
    for (const day of document.value?.days ?? []) {
      const own: Group = {
        key: `day:${day.id}`, label: t('media.dayOwn'), icon: 'calendar', gallery: { target: 'day', id: day.id },
        tiles: tilesOf(day.media),
      }
      const groups = [own, ...day.items.filter(isVisit).map(placeGroup)]
        .filter((group) => group.tiles.length > 0)
      if (groups.length > 0) {
        result.push({ key: day.id, label: dayLabel(day), groups })
      }
    }
    const ideas = (document.value?.unassigned ?? []).map(placeGroup).filter((group) => group.tiles.length > 0)
    if (ideas.length > 0) {
      result.push({ key: 'unassigned', label: t('plan.unassigned'), groups: ideas })
    }
  }

  const loose = items.value.filter((picture) => !shown.value.has(picture.id) && visible(picture))
  if (loose.length > 0) {
    result.push({
      key: 'unlinked',
      label: t('media.notLinked'),
      groups: [{
        key: 'unlinked', label: '', icon: 'image', gallery: null,
        tiles: loose.map((picture) => ({ picture, favorite: false, index: index++ })),
      }],
    })
  }
  return result
})

// order is the reading order of the viewer: the same the page lays out, a
// picture filed twice appearing twice.
const order = computed(() => sections.value
  .flatMap((section) => section.groups)
  .flatMap((group) => group.tiles.map((tile) => tile.picture)))

// load reads the trip's files and the documents that say where they are shown.
// quiet reads them again without the loading flag, which would take the gallery
// off the screen and leave the reader at the top of the page, far from the
// picture they were working on; only the first read shows it.
async function load(quiet = false): Promise<void> {
  const current = trip.value
  if (!current) {
    return
  }
  loading.value = !quiet
  error.value = ''
  try {
    const ids = [current.plan_id, current.report_id].filter((id): id is string => id !== null)
    documents.value = await Promise.all(ids.map((id) => store.readDocument(id)))
    items.value = store.shared ? filedMedia(documents.value) : await mediaApi.listMedia(current.id)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }
}

watch(() => trip.value?.id, () => void load())
onMounted(() => void load())

// choose switches the filter, keeping the choice in the address.
function choose(next: Filter): void {
  void router.replace({ query: { ...route.query, show: next === 'all' ? undefined : next } })
}

// apply runs a change and reads the files again, quietly, so the page and the
// server agree without the reader having to reload it. A change that already
// put its own result on the page passes reload false and is not read back.
async function apply(change: () => Promise<unknown>, reload = true): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    await change()
    if (reload) {
      await load(true)
    }
    return true
  } catch (err) {
    error.value = errorMessage(err, t, te)
    return false
  } finally {
    busy.value = false
  }
}

// replaceMedia puts pictures the server answered with in place of the page's own
// copies, which is all a change of privacy needs.
function replaceMedia(updated: Media[]): void {
  const byUpdatedId = new Map(updated.map((picture) => [picture.id, picture]))
  items.value = items.value.map((picture) => byUpdatedId.get(picture.id) ?? picture)
}

// setPrivacy marks a picture private, or public again.
function setPrivacy(picture: Media, isPrivate: boolean): void {
  void apply(async () => replaceMedia([await mediaApi.updateMedia(picture.id, { is_private: isPrivate })]), false)
}

// setCover makes a picture the one the trip is shown by, framed as it was
// framed in the window that asked which part of it to show.
function setCover(mediaId: string, crop: CoverCrop): void {
  const current = trip.value
  if (!current) {
    return
  }
  void apply(async () => {
    store.set(await updateTrip(current.id, { cover_media_id: mediaId, cover_crop: crop }))
  })
}

// remove deletes a picture from the trip, and so from every gallery and cover
// that showed it. There is no undo, so it is confirmed first.
async function remove(picture: Media): Promise<void> {
  const name = picture.original_name || t('media.picture')
  if (!(await confirmDialog.value?.ask(t('media.confirmDelete', { name }), { danger: true }))) {
    return
  }
  await apply(() => mediaApi.deleteMedia(picture.id))
}

// filedMedia lists each picture the documents file somewhere once, which is
// what a link reads instead of the trip's own list of files.
function filedMedia(each: TripDocument[]): Media[] {
  const found = new Map<string, Media>()
  const add = (pictures: Media[]): void => pictures.forEach((picture) => found.set(picture.id, picture))
  for (const current of each) {
    for (const day of current.days) {
      add(day.media)
      day.items.forEach((item) => add(item.media))
    }
    current.unassigned.forEach((item) => add(item.media))
  }
  return [...found.values()]
}

// limit is how many tiles are laid out so far; the rest follow as the page is
// scrolled towards them.
const limit = useProgressive(more, () => order.value.length)

// laidOut is what of the sections is on the page: the tiles numbered below the
// limit, and the groups and sections they fill.
const laidOut = computed<Section[]>(() => sections.value.flatMap((section) => {
  const groups = section.groups
    .map((group) => ({ ...group, tiles: group.tiles.filter((tile) => tile.index < limit.value) }))
    .filter((group) => group.tiles.length > 0)
  return groups.length > 0 ? [{ ...section, groups }] : []
}))

// open shows a picture full size, counting from the whole page rather than from
// its group, so the arrows walk the trip in order.
function open(tile: Tile): void {
  viewer.value?.open(tile.index)
}

// selectedMedia are the picked pictures in the order the page lays them out,
// which is the order they join a gallery in; a picture filed twice counts once.
const selectedMedia = computed(() => [...new Map(order.value
  .filter((picture) => selected.value.has(picture.id))
  .map((picture) => [picture.id, picture])).values()])

// anchor is the tile picked last, where a range picked with Shift starts.
const anchor = ref<number | null>(null)

// pick puts a picture into the selection, or takes it out again. With Shift
// held, as in Google Photos, it picks every tile from the one picked last to
// this one instead, in the order the page lays them out.
function pick(tile: Tile, event?: MouseEvent): void {
  const from = anchor.value
  anchor.value = tile.index
  if (event?.shiftKey && from !== null) {
    const [first, last] = from < tile.index ? [from, tile.index] : [tile.index, from]
    order.value.slice(first, last + 1).forEach((picture) => selected.value.add(picture.id))
    return
  }
  if (selected.value.has(tile.picture.id)) {
    selected.value.delete(tile.picture.id)
  } else {
    selected.value.add(tile.picture.id)
  }
}

// onTileClick opens a picture while nothing is picked, and joins it to the
// selection once something is - or at once, when Shift is held.
function onTileClick(tile: Tile, event: MouseEvent): void {
  if (selected.value.size > 0 || event.shiftKey) {
    pick(tile, event)
  } else {
    open(tile)
  }
}

// allTiles lists the pictures of a section or of one of its groups: every one
// of them, not only those laid out so far.
function allTiles(key: string): Tile[] {
  for (const section of sections.value) {
    if (section.key === key) {
      return section.groups.flatMap((group) => group.tiles)
    }
    const group = section.groups.find((each) => each.key === key)
    if (group) {
      return group.tiles
    }
  }
  return []
}

// coverage says how much of a section or a group is picked, for the circle
// beside its heading.
function coverage(key: string): 'none' | 'some' | 'all' {
  const tiles = allTiles(key)
  const picked = tiles.filter((tile) => selected.value.has(tile.picture.id)).length
  if (picked === 0) {
    return 'none'
  }
  return picked === tiles.length ? 'all' : 'some'
}

// pickAll picks every picture of a section or a group, as the circle beside a
// date does in Google Photos; when all of them are picked already it takes
// them out again.
function pickAll(key: string): void {
  const tiles = allTiles(key)
  const everything = coverage(key) === 'all'
  for (const tile of tiles) {
    if (everything) {
      selected.value.delete(tile.picture.id)
    } else {
      selected.value.add(tile.picture.id)
    }
  }
  anchor.value = null
}

// pickEverything picks every picture the page shows.
function pickEverything(): void {
  order.value.forEach((picture) => selected.value.add(picture.id))
}

// clearSelection lets go of every picked picture.
function clearSelection(): void {
  selected.value.clear()
  anchor.value = null
}

// coverageClass paints the circle beside a heading: filled when its whole
// section is picked, outlined when part of it is.
function coverageClass(state: 'none' | 'some' | 'all'): string {
  if (state === 'all') {
    return 'border-primary bg-primary text-primary-content'
  }
  return state === 'some' ? 'border-primary text-primary' : 'border-base-content/40 text-base-content/60'
}

// tooManyForReport says whether more pictures are picked than a day or a place
// shows in the report, which is when marking them as favourites is off.
const tooManyForReport = computed(() => selected.value.size > MEDIA_FAVORITE_LIMIT)

// galleryOf finds the pictures a day or a place of the document holds.
function galleryOf(gallery: Gallery): Media[] {
  for (const day of document.value?.days ?? []) {
    if (gallery.target === 'day' && day.id === gallery.id) {
      return day.media
    }
    const item = day.items.find((each) => each.id === gallery.id)
    if (gallery.target === 'item' && item) {
      return item.media
    }
  }
  return document.value?.unassigned.find((each) => each.id === gallery.id)?.media ?? []
}

/**
 * setGalleryFavorite marks a picture as a favourite of one day or place, or
 * takes the mark away there, leaving it as it is everywhere else. The gallery
 * is sent whole with its favourites, which is how the API takes them.
 *
 * Arguments:
 *   - gallery: the day or place the picture is a favourite of.
 *   - picture: the picture.
 *   - favorite: true to mark it, false to take the mark away.
 */
function setGalleryFavorite(gallery: Gallery, picture: Media, favorite: boolean): void {
  const current = galleryOf(gallery)
  const favorites = current.filter((each) => each.is_favorite && each.id !== picture.id).map((each) => each.id)
  if (favorite) {
    if (favorites.length >= MEDIA_FAVORITE_LIMIT) {
      error.value = t('media.favoriteFull')
      return
    }
    favorites.push(picture.id)
  }
  // The mark is put on the page's own copy of the gallery once the server took
  // it, rather than reading the whole trip back for one star.
  void apply(async () => {
    await mediaApi.setMediaLinks(gallery.target, gallery.id, current.map((each) => each.id), favorites)
    for (const each of current) {
      each.is_favorite = favorites.includes(each.id)
    }
  }, false)
}

// favoriteLoose asks where pictures that hang nowhere belong, and marks them as
// favourites there once that is chosen.
function favoriteLoose(pictures: Media[]): void {
  if (pictures.length > 0 && document.value) {
    linkDialog.value?.open(pictures, { favorite: true })
  }
}

// setSelectedFavorite marks the picked pictures as favourites of every day and
// place they hang on. The ones that hang nowhere have no place to be one of, so
// the choice of where they belong is opened for them.
function setSelectedFavorite(): void {
  const pictures = selectedMedia.value
  const linked = pictures.filter((picture) => shown.value.has(picture.id))
  const loose = pictures.filter((picture) => !shown.value.has(picture.id))
  void apply(async () => {
    if (linked.length > 0) {
      await mediaApi.setMediaFavorites(linked.map((picture) => picture.id), true)
    }
    clearSelection()
  }).then((ok) => {
    if (ok) {
      favoriteLoose(loose)
    }
  })
}

// setSelectedPrivacy marks every picked picture private, or public again.
function setSelectedPrivacy(isPrivate: boolean): void {
  const pictures = selectedMedia.value
  if (pictures.length === 0) {
    return
  }
  void apply(async () => {
    const updated: Media[] = []
    for (const picture of pictures) {
      updated.push(await mediaApi.updateMedia(picture.id, { is_private: isPrivate }))
    }
    replaceMedia(updated)
    clearSelection()
  }, false)
}

// removeSelected deletes every picked picture, which takes them out of every
// gallery they hang in. There is no undo, so it is confirmed first.
async function removeSelected(): Promise<void> {
  const pictures = selectedMedia.value
  if (pictures.length === 0) {
    return
  }
  const count = pictures.length
  if (!(await confirmDialog.value?.ask(t('media.confirmDeleteSelected', { count }, { plural: count }),
    { danger: true }))) {
    return
  }
  await apply(async () => {
    for (const picture of pictures) {
      await mediaApi.deleteMedia(picture.id)
    }
    clearSelection()
  })
}

// openLinks asks where pictures should be shown. A trip with neither a plan nor
// a report has no days to hang them on, which is why the entry offering this is
// disabled.
function openLinks(pictures: Media[]): void {
  if (pictures.length > 0 && documents.value.length > 0) {
    linkDialog.value?.open(pictures)
  }
}

// applyLinks puts the pictures into the galleries that were ticked and takes
// them out of the ones that were cleared. A gallery is replaced whole, one
// request each, so only the galleries that really change are sent. Pictures
// asked to be favourites are marked as such in every gallery they join, in the
// same request.
function applyLinks(pictures: Media[], changes: MediaLinkChange[], favorite: boolean): void {
  const updates = mediaLinkUpdates(documents.value, pictures, changes)
  if (updates.length === 0) {
    return
  }
  const ids = pictures.map((picture) => picture.id)
  void apply(async () => {
    for (const update of updates) {
      const attached = changes.some((change) => change.attach && change.targetId === update.targetId)
      const favorites = favorite && attached
        ? [...galleryOf({ target: update.target, id: update.targetId })
            .filter((each) => each.is_favorite && !ids.includes(each.id)).map((each) => each.id), ...ids]
        : undefined
      await mediaApi.setMediaLinks(update.target, update.targetId, update.mediaIds, favorites)
    }
    clearSelection()
  })
}
</script>

<template>
  <div class="space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-xl font-bold">{{ t('media.tripGallery') }}</h1>
      <div class="flex flex-wrap items-center gap-3">
        <div v-if="!store.shared" role="tablist" class="tabs tabs-border tabs-sm w-fit">
          <button
            v-for="each in FILTERS"
            :key="each"
            type="button"
            role="tab"
            class="tab"
            :class="{ 'tab-active': filter === each }"
            :aria-selected="filter === each"
            @click="choose(each)"
          >
            {{ t(`media.filters.${each}`) }}
          </button>
        </div>
      </div>
    </header>

    <p v-if="error" role="alert" class="alert alert-error">{{ error }}</p>
    <div v-if="loading" class="flex justify-center py-8"><span class="loading loading-spinner"></span></div>

    <template v-else>
      <MediaUploader v-if="canEdit && trip" :trip-id="trip.id" @uploaded="() => void load()" />

      <p v-if="sections.length === 0" class="py-6 text-sm text-base-content/70">
        {{ items.length === 0 ? t('media.tripEmpty') : t('media.noMatches') }}
      </p>

      <section v-for="section in laidOut" :key="section.key" class="space-y-3">
        <h2 class="flex items-center gap-2 border-b border-base-300 pb-1 text-base font-semibold">
          <button
            v-if="canEdit"
            type="button"
            class="flex size-5 shrink-0 items-center justify-center rounded-full border"
            :class="coverageClass(coverage(section.key))"
            :aria-label="t('media.selectSection', { name: section.label })"
            :aria-pressed="coverage(section.key) === 'all'"
            @click="pickAll(section.key)"
          >
            <AppIcon v-if="coverage(section.key) !== 'none'" name="check" class="size-3.5!" />
          </button>
          {{ section.label }}
        </h2>
        <div v-for="group in section.groups" :key="group.key" class="space-y-2">
          <h3 v-if="group.label" class="flex items-center gap-1 text-sm font-medium text-base-content/70">
            <button
              v-if="canEdit"
              type="button"
              class="me-1 flex size-4 shrink-0 items-center justify-center rounded-full border"
              :class="coverageClass(coverage(group.key))"
              :aria-label="t('media.selectSection', { name: group.label })"
              :aria-pressed="coverage(group.key) === 'all'"
              @click="pickAll(group.key)"
            >
              <AppIcon v-if="coverage(group.key) !== 'none'" name="check" class="size-3!" />
            </button>
            <AppIcon :name="group.icon" class="size-4!" />
            {{ group.label }}
          </h3>
          <!-- Shift picks a range of tiles, which must not also select their text. -->
          <ul class="grid grid-cols-4 gap-2 select-none sm:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10 2xl:grid-cols-12">
            <li v-for="tile in group.tiles" :key="tile.picture.id" class="group relative">
              <button
                type="button"
                class="block w-full overflow-hidden rounded-box border border-base-300"
                :class="selected.has(tile.picture.id) ? 'ring-2 ring-primary' : ''"
                :aria-label="tile.picture.original_name"
                @click="onTileClick(tile, $event)"
              >
                <MediaImage :id="tile.picture.id" :alt="tile.picture.original_name" :size="320" square />
              </button>

              <!-- The circle appears under the pointer, and stays out where there
                   is no pointer to hover with or something is already picked. -->
              <button
                v-if="canEdit"
                type="button"
                class="absolute start-1 top-1 flex size-6 items-center justify-center rounded-full border border-base-300 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
                :class="selected.has(tile.picture.id)
                  ? 'bg-primary text-primary-content opacity-100!'
                  : 'bg-base-100/80 text-base-content/60'"
                :aria-label="t('media.selectPicture')"
                :aria-pressed="selected.has(tile.picture.id)"
                @click="pick(tile, $event)"
              >
                <AppIcon name="check" class="size-4!" />
              </button>

              <span class="pointer-events-none absolute bottom-1 start-1 flex items-center gap-1">
                <span v-if="tile.picture.id === trip?.cover_media_id" class="badge badge-primary badge-xs">
                  {{ t('media.cover') }}
                </span>
                <AppIcon
                  v-if="tile.favorite"
                  name="starFill"
                  class="size-4! text-amber-400 drop-shadow-[0_1px_1px_rgb(0_0_0/0.7)]"
                  role="img"
                  :aria-label="t('media.favorite')"
                />
                <AppIcon
                  v-if="tile.picture.is_private"
                  name="lockFill"
                  class="size-4! text-amber-400 drop-shadow-[0_1px_1px_rgb(0_0_0/0.7)]"
                  role="img"
                  :aria-label="t('media.private')"
                />
              </span>

              <details v-if="canEdit" class="dropdown dropdown-end absolute end-1 top-1">
                <summary class="btn btn-square btn-xs" :aria-label="t('media.actions')">
                  <AppIcon name="dots" />
                </summary>
                <ul
                  class="menu dropdown-content z-20 w-60 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg"
                  @click="closeMenu"
                >
                  <li>
                    <button
                      type="button"
                      :disabled="busy || !document"
                      :title="document ? undefined : t('media.linkNoDocuments')"
                      @click="openLinks([tile.picture])"
                    >
                      <AppIcon name="calendar" />
                      {{ t('media.linkAction') }}
                    </button>
                  </li>
                  <li>
                    <button type="button" :disabled="busy" @click="coverCrop?.open(tile.picture.id)">
                      <AppIcon name="image" />
                      {{ t('media.setTripCover') }}
                    </button>
                  </li>
                  <li v-if="isReport && group.gallery">
                    <button
                      type="button"
                      :disabled="busy"
                      @click="setGalleryFavorite(group.gallery, tile.picture, !tile.favorite)"
                    >
                      <AppIcon :name="tile.favorite ? 'star' : 'starFill'" />
                      {{ tile.favorite ? t('media.unsetFavoriteHere') : t('media.setFavoriteHere') }}
                    </button>
                  </li>
                  <li v-else-if="isReport">
                    <button type="button" :disabled="busy || !document" @click="favoriteLoose([tile.picture])">
                      <AppIcon name="starFill" />
                      {{ t('media.setFavorite') }}
                    </button>
                  </li>
                  <li>
                    <button type="button" :disabled="busy" @click="setPrivacy(tile.picture, !tile.picture.is_private)">
                      <AppIcon :name="tile.picture.is_private ? 'eye' : 'eyeSlash'" />
                      {{ tile.picture.is_private ? t('media.makePublic') : t('media.makePrivate') }}
                    </button>
                  </li>
                  <li>
                    <button type="button" class="text-error" :disabled="busy" @click="remove(tile.picture)">
                      <AppIcon name="trash" />
                      {{ t('media.remove') }}
                    </button>
                  </li>
                </ul>
              </details>
            </li>
          </ul>
        </div>
      </section>

      <!-- The end of what is laid out; coming near it lays out the next batch. -->
      <div v-if="limit < order.length" ref="more" class="flex justify-center py-4" aria-hidden="true">
        <span class="loading loading-spinner loading-sm opacity-60"></span>
      </div>

      <div
        v-if="canEdit && selected.size > 0"
        class="sticky bottom-2 z-30 flex flex-wrap items-center gap-2 rounded-box border border-base-300 bg-base-100 p-3 shadow-lg"
      >
        <span class="me-1 text-sm font-medium">
          {{ t('media.selectedCount', { count: selected.size }, { plural: selected.size }) }}
        </span>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="busy || !document"
          :title="document ? undefined : t('media.linkNoDocuments')"
          @click="openLinks(selectedMedia)"
        >
          <AppIcon name="calendar" />
          {{ t('media.linkAction') }}
        </button>
        <button
          v-if="isReport"
          type="button"
          class="btn btn-sm"
          :disabled="busy || tooManyForReport"
          :title="tooManyForReport ? t('media.favoriteTooMany') : undefined"
          @click="setSelectedFavorite"
        >
          <AppIcon name="starFill" />
          {{ t('media.setFavorite') }}
        </button>
        <button type="button" class="btn btn-sm" :disabled="busy" @click="setSelectedPrivacy(true)">
          <AppIcon name="eyeSlash" />
          {{ t('media.makePrivate') }}
        </button>
        <button type="button" class="btn btn-sm" :disabled="busy" @click="setSelectedPrivacy(false)">
          <AppIcon name="eye" />
          {{ t('media.makePublic') }}
        </button>
        <button type="button" class="btn btn-sm btn-error" :disabled="busy" @click="removeSelected">
          <AppIcon name="trash" />
          {{ t('media.remove') }}
        </button>
        <button type="button" class="btn btn-ghost btn-sm" :disabled="selected.size === order.length" @click="pickEverything">
          {{ t('media.selectAll') }}
        </button>
        <button type="button" class="btn btn-ghost btn-sm" @click="clearSelection">
          {{ t('media.clearSelection') }}
        </button>
      </div>

      <MediaViewer ref="viewer" :items="order" />
      <MediaLinkDialog ref="linkDialog" :documents="documents" @save="applyLinks" />
      <ConfirmDialog ref="confirmDialog" />
      <CoverCropDialog ref="coverCrop" @save="setCover" />
    </template>
  </div>
</template>
