<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ItemStatus, Media, PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaGallery from '@/components/media/MediaGallery.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import ReportTrackLine from '@/components/report/ReportTrackLine.vue'
import { useReportText } from '@/composables/useContentLanguage'
import { formatMoney, formatTimeOfDay } from '@/utils/format'
import { itemIcon, itemKindLabel } from '@/utils/plan'

// One place of a report: how it turned out, what it cost and what happened
// there. A place was visited unless the switch on its card says otherwise,
// which is one tap on a phone during the trip rather than a form afterwards.
//
// While a translation is written only the words of the card can be changed:
// the story in place, the name and description through the form "Translate"
// opens. Everything else is shared by every language.
const props = defineProps<{
  item: PlanItem
  currency: string
  editing: boolean
  /** The trip the place belongs to, which is what pictures are uploaded against. */
  tripId?: string
  /** Dragging pictures into order needs a pointer and room. */
  draggable?: boolean
}>()

const emit = defineEmits<{
  status: [status: ItemStatus]
  rate: [rating: number | null]
  story: [story: string]
  edit: []
  remove: []
  uploaded: [media: Media[]]
  reorderMedia: [mediaIds: string[]]
  cover: [media: Media | null]
  privacy: [media: Media, isPrivate: boolean]
  favoriteMedia: [media: Media, isFavorite: boolean]
  unlinkMedia: [media: Media]
  removeMedia: [media: Media]
  importTrack: [file: File]
  removeTrack: []
}>()

const { t, locale } = useI18n()
const text = useReportText()

// structural is editing what every language shares; translating is writing the
// words of one language.
const structural = computed(() => props.editing && !text.value.translating)
const translating = computed(() => props.editing && text.value.translating)

const planned = computed(() => amountOf(props.item.planned_cost_amount))
// period is when the place was reached and, when it is known, when it was left.
const period = computed(() => {
  const from = formatTimeOfDay(props.item.actual_time)
  const to = formatTimeOfDay(props.item.actual_end_time)
  if (!from) {
    return to ? `– ${to}` : ''
  }
  return to ? `${from}–${to}` : from
})
const actual = computed(() => amountOf(props.item.actual_cost_amount))

// amountOf shows an amount, marking a per-person one as such.
function amountOf(value: string | null): string {
  const amount = formatMoney(value, props.currency, locale.value)
  return amount && props.item.cost_per_person ? `${amount} ${t('place.perPersonShort')}` : amount
}

// STATUS_BADGES mark the places that were not simply visited: a visited place
// is what a report is made of and needs no badge.
const STATUS_BADGES: Partial<Record<ItemStatus, string>> = {
  skipped: 'badge-warning',
  unplanned: 'badge-info',
}

/**
 * setVisited turns the switch: on is visited, off is skipped. A place added in
 * the report has no switch, because unplanned is where it came from rather than
 * a judgement to be changed.
 */
function setVisited(event: Event): void {
  emit('status', (event.target as HTMLInputElement).checked ? 'visited' : 'skipped')
}

// rate sets a rating, or clears it when the same star is pressed again.
function rate(stars: number): void {
  emit('rate', props.item.rating === stars ? null : stars)
}
</script>

<template>
  <article
    :id="`item-${item.id}`"
    class="scroll-mt-20 space-y-2 rounded-box border border-base-300 bg-base-100 p-3"
    :class="{ 'opacity-70': item.status === 'skipped' }"
  >
    <header class="flex flex-wrap items-start justify-between gap-2">
      <div class="min-w-0 space-y-1">
        <h4 class="font-medium break-words" :class="{ 'line-through': item.status === 'skipped' }">
          {{ item.name }}
        </h4>
        <p class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
          <span v-if="item.kind === 'activity'" class="flex items-center gap-1">
            <AppIcon :name="itemIcon(item)" class="size-4!" />
            {{ itemKindLabel(item, t) }}
          </span>
          <span v-if="STATUS_BADGES[item.status]" class="badge badge-sm" :class="STATUS_BADGES[item.status]">
            {{ t(`report.statuses.${item.status}`) }}
          </span>
          <span v-if="period" class="tabular-nums">{{ period }}</span>
          <span
            v-if="item.kind === 'activity' && item.difficulty"
            class="badge badge-sm difficulty-soft"
            :class="`difficulty-${item.difficulty}`"
          >
            {{ t('place.difficultyOf', { level: t(`place.difficulties.${item.difficulty}`) }) }}
          </span>
          <span v-if="actual">{{ actual }}</span>
          <span v-else-if="planned" class="text-base-content/50">{{ t('report.plannedAmount', { amount: planned }) }}</span>
        </p>
      </div>

      <button v-if="translating" type="button" class="btn btn-ghost btn-sm" @click="emit('edit')">
        <AppIcon name="globe" />
        {{ t('report.translate') }}
      </button>
      <div v-else-if="structural" class="flex items-center gap-1">
        <label v-if="item.status !== 'unplanned'" class="label me-2 cursor-pointer gap-2 text-sm">
          <input
            type="checkbox"
            class="toggle toggle-success toggle-sm"
            :checked="item.status !== 'skipped'"
            :aria-label="t('report.markVisited', { name: item.name })"
            @change="setVisited"
          />
          {{ t('report.statuses.visited') }}
        </label>
        <button type="button" class="btn btn-ghost btn-sm" @click="emit('edit')">{{ t('plan.edit') }}</button>
        <button
          type="button"
          class="btn btn-ghost btn-sm btn-square text-error"
          :aria-label="t('plan.delete')"
          @click="emit('remove')"
        >
          <AppIcon name="trash" />
        </button>
      </div>
    </header>

    <div v-if="structural || item.rating !== null" class="flex items-center gap-0.5">
      <template v-if="structural">
        <button
          v-for="stars in 5"
          :key="stars"
          type="button"
          class="btn btn-ghost btn-xs px-0.5"
          :class="item.rating !== null && stars <= item.rating ? 'text-warning' : 'text-base-content/30'"
          :aria-label="t('report.rate', { n: stars, name: item.name })"
          :aria-pressed="item.rating === stars"
          @click="rate(stars)"
        >
          <AppIcon :name="item.rating !== null && stars <= item.rating ? 'starFill' : 'star'" />
        </button>
      </template>
      <span v-else class="flex items-center gap-0.5 text-warning" :aria-label="t('report.rated', { n: item.rating })">
        <AppIcon v-for="stars in item.rating ?? 0" :key="stars" name="starFill" />
      </span>
    </div>

    <EditableMarkdown
      :source="text.source(item.id, 'story_md', item.story_md)"
      :original="text.original(item.id, 'story_md')"
      :editing="editing"
      :placeholder="t('report.storyPlaceholder')"
      :rows="3"
      @save="(story) => emit('story', story)"
    />

    <ReportTrackLine
      :track="item.track"
      :editing="structural"
      :hint="t(item.kind === 'activity' ? 'track.activityHint' : 'track.placeHint')"
      @import="(file) => emit('importTrack', file)"
      @remove="emit('removeTrack')"
    />

    <MediaGallery
      v-if="item.media.length > 0"
      :items="item.media"
      :can-edit="structural"
      :cover-id="item.cover_media_id"
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
    <MediaUploader v-if="structural && tripId" :trip-id="tripId" compact @uploaded="(media) => emit('uploaded', media)" />
  </article>
</template>
