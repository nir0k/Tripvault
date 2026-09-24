<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Media, PlanDay, TripDocument } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { formatDayDate } from '@/utils/format'
import type { MediaLinkChange, MediaLinkPlace } from '@/utils/mediaLinks'
import { isVisit, itemIcon } from '@/utils/plan'

// Where photographs are shown: the days of the trip's documents and the places
// of those days, as boxes to tick. A document's own galleries are filled from
// the day they belong to, one day at a time; this is meant for the other
// direction - an afternoon's worth of pictures, filed at once under the day
// they were taken on, without walking the document day by day.
//
// Both the plan and the report are offered, because a trip still being planned
// has no report and its pictures have to hang somewhere too.
const props = defineProps<{ documents: TripDocument[] }>()

const emit = defineEmits<{
  /** favorite asks for the pictures to be marked favourites wherever they are put. */
  save: [media: Media[], changes: MediaLinkChange[], favorite: boolean]
}>()

const { t, locale } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const media = ref<Media[]>([])
// Set when the pictures were asked to be favourites before they hung anywhere:
// the dialog then says so, and the save marks them where they are put.
const favorite = ref(false)
// What the reader has ticked or cleared, by the key of the gallery. A gallery
// missing here keeps whatever the report says about it.
const decided = ref(new Map<string, boolean>())

/** How much of the batch a gallery already holds. */
type Held = 'all' | 'some' | 'none'

/** Row is one gallery offered in the list. */
interface Row {
  key: string
  target: MediaLinkPlace
  id: string
  label: string
  /** The places of a day follow it, indented under its own row. */
  places: Row[]
  held: Held
  icon: IconName
}

/** Section is one document of the trip with the days it offers. */
interface Section {
  key: string
  label: string
  rows: Row[]
}

// sections list the documents of the trip with their days and the places of
// those days, each carrying how much of the batch it already holds.
const sections = computed<Section[]>(() => props.documents.map((document) => ({
  key: document.id,
  label: t(`trip.tabs.${document.kind}`),
  rows: document.days.map((day) => ({
    key: `day:${day.id}`,
    target: 'day' as MediaLinkPlace,
    id: day.id,
    label: dayLabel(day),
    icon: 'calendar' as IconName,
    held: held(day.media),
    places: day.items
      .filter(isVisit)
      .map((item) => ({
        key: `item:${item.id}`,
        target: 'item' as MediaLinkPlace,
        id: item.id,
        label: item.name || t('media.linkUnnamedPlace'),
        icon: itemIcon(item),
        held: held(item.media),
        places: [],
      })),
  })),
})))

// flat walks the days and their places as one list, which is what a choice is
// read back from.
const flat = computed<Row[]>(() => sections.value
  .flatMap((section) => section.rows)
  .flatMap((row) => [row, ...row.places]))

// changed counts the galleries the reader has touched, so the button can say
// there is nothing to save.
const changed = computed(() => flat.value.filter((row) => decided.value.has(row.key)).length)

// held says how much of the batch a gallery holds already: all of it, some of
// it, or none - which is what a tick, a dash and an empty box stand for.
function held(gallery: Media[]): Held {
  const ids = new Set(gallery.map((picture) => picture.id))
  const count = media.value.filter((picture) => ids.has(picture.id)).length
  if (count === 0) {
    return 'none'
  }
  return count === media.value.length ? 'all' : 'some'
}

// dayLabel names a day the way the report's own lists of days do.
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

/**
 * open asks where these pictures should be shown, starting from where they hang
 * now.
 *
 * Arguments:
 *   - next: the pictures being filed; one from a tile's menu, or a selection.
 *   - options.favorite: the pictures were asked to be favourites but hang
 *     nowhere yet, so the reader is told to choose where first.
 */
function open(next: Media[], options: { favorite?: boolean } = {}): void {
  media.value = next
  favorite.value = options.favorite === true
  decided.value = new Map()
  dialog.value?.showModal()
}

// checked says whether a box shows as ticked: what the reader chose, or else
// what the report says.
function checked(row: Row): boolean {
  return decided.value.get(row.key) ?? row.held === 'all'
}

// partial marks a box nobody has touched whose gallery holds only part of the
// batch: ticking it adds the rest, clearing it takes them all out.
function partial(row: Row): boolean {
  return !decided.value.has(row.key) && row.held === 'some'
}

// toggle records a choice, forgetting it again when it comes back round to what
// the report already says.
function toggle(row: Row, value: boolean): void {
  if ((value && row.held === 'all') || (!value && row.held === 'none')) {
    decided.value.delete(row.key)
  } else {
    decided.value.set(row.key, value)
  }
}

// submit reports the galleries that were ticked or cleared and closes.
function submit(): void {
  const changes: MediaLinkChange[] = flat.value
    .filter((row) => decided.value.has(row.key))
    .map((row) => ({ target: row.target, targetId: row.id, attach: decided.value.get(row.key) === true }))
  emit('save', media.value, changes, favorite.value)
  dialog.value?.close()
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[85dvh] flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('media.linkTitle') }}</h2>
      <p class="text-sm text-base-content/70">
        {{ t('media.linkCount', { count: media.length }, { plural: media.length }) }}
      </p>
      <p v-if="favorite" role="status" class="alert alert-warning alert-soft text-sm">
        <AppIcon name="starFill" class="text-amber-400" />
        {{ t('media.favoriteNeedsPlace', { count: media.length }, { plural: media.length }) }}
      </p>

      <p v-if="flat.length === 0" class="py-2 text-sm text-base-content/70">{{ t('media.linkNoDays') }}</p>
      <div v-else class="min-h-0 flex-1 space-y-3 overflow-y-auto">
        <section v-for="section in sections" :key="section.key" class="space-y-1">
          <h3 v-if="sections.length > 1" class="text-sm font-semibold text-base-content/70">{{ section.label }}</h3>
          <ul class="space-y-1">
            <li v-for="row in section.rows" :key="row.key">
              <label class="label cursor-pointer justify-start gap-2 py-1">
                <input
                  type="checkbox"
                  class="checkbox checkbox-sm"
                  :checked="checked(row)"
                  :indeterminate="partial(row)"
                  @change="toggle(row, ($event.target as HTMLInputElement).checked)"
                />
                <AppIcon :name="row.icon" class="size-4! opacity-70" />
                <span class="font-medium">{{ row.label }}</span>
              </label>
              <ul v-if="row.places.length > 0" class="ms-6 space-y-1">
                <li v-for="place in row.places" :key="place.key">
                  <label class="label cursor-pointer justify-start gap-2 py-1">
                    <input
                      type="checkbox"
                      class="checkbox checkbox-sm"
                      :checked="checked(place)"
                      :indeterminate="partial(place)"
                      @change="toggle(place, ($event.target as HTMLInputElement).checked)"
                    />
                    <AppIcon :name="place.icon" class="size-4! opacity-70" />
                    <span class="truncate">{{ place.label }}</span>
                  </label>
                </li>
              </ul>
            </li>
          </ul>
        </section>
      </div>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="changed === 0">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
