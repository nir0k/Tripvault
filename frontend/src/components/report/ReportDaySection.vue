<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ItemStatus, Leg, Media, PlanDay, PlanItem, Stay } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaGallery from '@/components/media/MediaGallery.vue'
import MediaHints from '@/components/media/MediaHints.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import ReportLegLine from '@/components/report/ReportLegLine.vue'
import ReportPlaceCard from '@/components/report/ReportPlaceCard.vue'
import { useReportText } from '@/composables/useContentLanguage'
import { formatDayDate, formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'
import type { MediaHint } from '@/utils/mediaHints'
import { isVisit } from '@/utils/plan'

// One day of a report: what it was called, what happened, the places of the day
// and what it cost. Stay marks are left out here - where the night was spent is
// part of the plan's schedule, and the report reads as prose - but the journey
// to each place is told in a line above it: how, how long and how far.
//
// While a translation is written the day keeps only its words editable - its
// title, its notes and the stories of its places - and adding, removing and
// arranging wait for the original.
const props = defineProps<{
  day: PlanDay
  currency: string
  editing: boolean
  /** Leave out the places that were not reached. */
  hideSkipped: boolean
  /** The trip the day belongs to, which is what pictures are uploaded against. */
  tripId?: string
  /** Dragging pictures into order needs a pointer and room. */
  draggable?: boolean
  /** What the pictures just uploaded here say about where they belong. */
  hints?: MediaHint[]
  /** The document's stays, which name the journey that starts at one. */
  stays?: Stay[]
  /** The elements of every day, which name the journey in from the day before. */
  documentItems?: PlanItem[]
  /** Whether the day may be deleted; a document keeps at least one day. */
  removable?: boolean
}>()

const emit = defineEmits<{
  title: [title: string]
  notes: [notes: string]
  status: [item: PlanItem, status: ItemStatus]
  rate: [item: PlanItem, rating: number | null]
  story: [item: PlanItem, story: string]
  edit: [item: PlanItem]
  remove: [item: PlanItem]
  add: [kind: 'place' | 'activity']
  uploaded: [media: Media[]]
  reorderMedia: [mediaIds: string[]]
  cover: [media: Media | null]
  privacy: [media: Media, isPrivate: boolean]
  favoriteMedia: [media: Media, isFavorite: boolean]
  unlinkMedia: [media: Media]
  removeMedia: [media: Media]
  uploadedToPlace: [item: PlanItem, media: Media[]]
  reorderPlaceMedia: [item: PlanItem, mediaIds: string[]]
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
}>()

const { t, locale } = useI18n()
const text = useReportText()

// structural is editing what every language shares.
const structural = computed(() => props.editing && !text.value.translating)

// legEditable says whether a journey's line opens a form: its own while the
// original is written, and the translation of its note - when it has one -
// while a translation is.
function legEditable(leg: Leg): boolean {
  if (!props.editing) {
    return false
  }
  return !text.value.translating || !!text.value.original(leg.id, 'note')
}

const places = computed(() => props.day.items.filter(isVisit))
const shown = computed(() => (props.hideSkipped ? places.value.filter((item) => item.status !== 'skipped') : places.value))
const hidden = computed(() => places.value.length - shown.value.length)

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

const date = computed(() => formatDayDate(props.day.date, locale.value, true))
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
          v-if="removable && structural"
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

    <div v-if="shown.length > 0" class="space-y-3">
      <template v-for="item in shown" :key="item.id">
        <ReportLegLine
          v-if="legTo.get(item.id)"
          :leg="legTo.get(item.id)!"
          :from="legFrom(legTo.get(item.id)!)"
          :editing="legEditable(legTo.get(item.id)!)"
          @edit="emit('editLeg', legTo.get(item.id)!)"
        />
        <ReportPlaceCard
          :item="item"
          :currency="currency"
          :editing="editing"
          :trip-id="tripId"
          :draggable="draggable"
          @status="(status) => emit('status', item, status)"
          @rate="(rating) => emit('rate', item, rating)"
          @story="(story) => emit('story', item, story)"
          @edit="emit('edit', item)"
          @remove="emit('remove', item)"
          @uploaded="(media) => emit('uploadedToPlace', item, media)"
          @reorder-media="(ids) => emit('reorderPlaceMedia', item, ids)"
          @cover="(media) => emit('placeCover', item, media)"
          @privacy="(media, isPrivate) => emit('privacy', media, isPrivate)"
          @favorite-media="(media, isFavorite) => emit('favoritePlaceMedia', item, media, isFavorite)"
          @unlink-media="(media) => emit('unlinkPlaceMedia', item, media)"
          @remove-media="(media) => emit('removeMedia', media)"
          @import-track="(file) => emit('importPlaceTrack', item, file)"
          @remove-track="emit('removePlaceTrack', item)"
        />
      </template>
    </div>

    <p v-if="hidden > 0" class="text-sm text-base-content/60">{{ t('report.hiddenSkipped', hidden) }}</p>

    <!-- The places are what a day of a report is made of, so adding one comes
         right after them and before the pictures, as two tiles that are hard
         to miss. -->
    <div v-if="structural" class="grid gap-3 sm:grid-cols-2">
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
      :can-edit="structural"
      :cover-id="day.cover_media_id"
      :draggable="draggable"
      :trip-id="tripId"
      prefer-favorites
      :can-favorite="structural"
      @reorder="(ids) => emit('reorderMedia', ids)"
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
    <MediaUploader v-if="structural && tripId" :trip-id="tripId" @uploaded="(media) => emit('uploaded', media)" />
  </section>
</template>
