<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Media, PlanDay, PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import type { MediaHint } from '@/utils/mediaHints'
import { formatDayDate, formatDistance } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// What the pictures just uploaded say about where they belong. A camera knows
// the day and the place better than the person filing photographs in the
// evening, so the offer is made here - once, quietly, and only while it is
// still about the upload that has just happened.
const props = defineProps<{ hints: MediaHint[] }>()

const emit = defineEmits<{
  moveToDay: [day: PlanDay, media: Media[]]
  attachToPlace: [item: PlanItem, media: Media[]]
  dismiss: []
}>()

const { t, locale } = useI18n()

/** A suggestion several pictures share, so a batch is one line rather than ten. */
interface Group<T> {
  target: T
  media: Media[]
  distanceM: number
}

// dayGroups collect the pictures taken on a day other than the one they hang in.
const dayGroups = computed<Group<PlanDay>[]>(() => group(props.hints
  .filter((hint): hint is MediaHint & { day: PlanDay } => hint.day !== null)
  .map((hint) => ({ target: hint.day, media: [hint.media], distanceM: 0 }))))

// placeGroups collect the pictures taken next to a place they are not tied to.
const placeGroups = computed<Group<PlanItem>[]>(() => group(props.hints
  .filter((hint): hint is MediaHint & { item: PlanItem } => hint.item !== null)
  .map((hint) => ({ target: hint.item, media: [hint.media], distanceM: hint.distanceM }))))

// group merges suggestions pointing at the same day or place, keeping the
// closest distance of the batch.
function group<T extends { id: string }>(entries: Group<T>[]): Group<T>[] {
  const merged = new Map<string, Group<T>>()
  for (const entry of entries) {
    const found = merged.get(entry.target.id)
    if (found) {
      found.media.push(...entry.media)
      found.distanceM = Math.min(found.distanceM, entry.distanceM)
    } else {
      merged.set(entry.target.id, { ...entry, media: [...entry.media] })
    }
  }
  return [...merged.values()]
}

// dayLabel names a day the way the report's contents do.
function dayLabel(day: PlanDay): string {
  return day.title || formatDayDate(day.date, locale.value) || t('plan.dayNumber', { n: day.position + 1 })
}
</script>

<template>
  <div v-if="dayGroups.length > 0 || placeGroups.length > 0" class="rounded-box bg-base-200 p-3 text-sm">
    <div class="flex items-start justify-between gap-2">
      <ul class="min-w-0 flex-1 space-y-2">
        <li v-for="entry in dayGroups" :key="`day-${entry.target.id}`" class="flex flex-wrap items-center gap-2">
          <AppIcon name="calendar" class="size-4! opacity-70" />
          <span>
            {{ t('media.hintDay', { date: formatDayDate(entry.target.date, locale) }, { plural: entry.media.length }) }}
          </span>
          <button type="button" class="btn btn-xs btn-hover-outline" @click="emit('moveToDay', entry.target, entry.media)">
            {{ t('media.moveToDay', { day: dayLabel(entry.target) }) }}
          </button>
        </li>
        <li v-for="entry in placeGroups" :key="`place-${entry.target.id}`" class="flex flex-wrap items-center gap-2">
          <AppIcon name="other" class="size-4! opacity-70" />
          <span>
            {{ t('media.hintPlace', {
              name: entry.target.name,
              distance: formatDistance(entry.distanceM, locale, activeUnits),
            }, { plural: entry.media.length }) }}
          </span>
          <button
            type="button"
            class="btn btn-xs btn-hover-outline"
            @click="emit('attachToPlace', entry.target, entry.media)"
          >
            {{ t('media.attachToPlace') }}
          </button>
        </li>
      </ul>
      <button type="button" class="btn btn-ghost btn-xs btn-square" :aria-label="t('media.dismiss')" @click="emit('dismiss')">
        <AppIcon name="close" />
      </button>
    </div>
  </div>
</template>
