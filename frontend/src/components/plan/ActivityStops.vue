<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanItem, Stop } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { STOP_KIND_ICONS } from '@/components/icons'
import MarkdownText from '@/components/MarkdownText.vue'
import { useStopEditing } from '@/composables/useStopEditor'
import { useTripStore } from '@/stores/trip'
import { formatDistance, formatMoney, formatTimeOfDay } from '@/utils/format'
import { formatDuration, stopLabel, stopName } from '@/utils/plan'
import { stopSeconds } from '@/utils/trackTime'
import { activeUnits } from '@/utils/units'

// The stops along the line of an activity, in the order they come along it:
// a café halfway up a hike, a rest by a lake. Each row tells how far along the
// line the stop is, what it is and, in a plan, how long the way to it takes at
// the line's speed, or in a report when it was reached, and what it costs, with
// its note below. Somebody who may change the trip adds, changes and removes
// stops through the page's forms (useStopEditing).
const props = defineProps<{
  item: PlanItem
  editing: boolean
  /** Whether the activity belongs to a report. */
  report: boolean
  /** The activity's number in its day, which labels its stops as the map does. */
  number?: number
}>()

const { t, locale } = useI18n()
const store = useTripStore()
const editor = useStopEditing()

const stops = computed(() => props.item.track?.stops ?? [])
const currency = computed(() => store.trip?.currency ?? 'EUR')
// speed is what the way to a stop is timed at in a plan: the line's own speed
// or the plan's.
const speed = computed(() => props.item.track?.speed_kmh ?? store.trip?.track_speed_kmh ?? null)

/** when is how long the way to a stop takes in a plan, or when it was reached in a report. */
function when(stop: Stop): string {
  if (props.report) {
    return formatTimeOfDay(stop.actual_time)
  }
  if (speed.value === null) {
    return ''
  }
  // A stop at the start is reached at once, which needs no saying.
  const minutes = Math.round(stopSeconds(stop, speed.value) / 60)
  return minutes > 0 ? `≈ ${formatDuration(minutes, t)}` : ''
}

/** cost is what a stop costs: in a report what was spent once it is known. */
function cost(stop: Stop): string {
  const value = props.report && stop.actual_cost_amount !== null ? stop.actual_cost_amount : stop.planned_cost_amount
  const amount = formatMoney(value, currency.value, locale.value)
  return amount && stop.cost_per_person ? `${amount} ${t('place.perPersonShort')}` : amount
}
</script>

<template>
  <div v-if="item.track && (stops.length > 0 || (editing && editor))" class="flex flex-col gap-1">
    <ol v-if="stops.length > 0" class="flex flex-col gap-1" :aria-label="t('stop.title')">
      <li v-for="(stop, index) in stops" :key="stop.id" class="rounded-box bg-base-200/60 px-3 py-1.5 text-sm">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <span class="badge badge-ghost badge-sm font-semibold tabular-nums">{{ stopLabel(number, index) }}</span>
          <AppIcon :name="STOP_KIND_ICONS[stop.kind]" class="size-4! opacity-70" />
          <span class="font-medium break-words">{{ stopName(stop, t) }}</span>
          <span class="text-base-content/60 tabular-nums">
            {{ formatDistance(stop.distance_m, locale, activeUnits, true) }}
          </span>
          <span v-if="when(stop)" class="text-base-content/60 tabular-nums" :title="report ? t('stop.reachedAt') : t('stop.fromStart')">
            · {{ when(stop) }}
          </span>
          <span v-if="cost(stop)" class="text-base-content/60">· {{ cost(stop) }}</span>
          <span class="min-w-0 flex-1"></span>
          <template v-if="editing && editor">
            <button
              type="button"
              class="btn btn-ghost btn-xs btn-square"
              :aria-label="t('stop.edit', { name: stopName(stop, t) })"
              :title="t('stop.edit', { name: stopName(stop, t) })"
              @click="editor.edit(item, stop)"
            >
              <AppIcon name="pencil" class="size-3.5!" />
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-xs btn-square"
              :aria-label="t('stop.cost', { name: stopName(stop, t) })"
              :title="t('stop.cost', { name: stopName(stop, t) })"
              @click="editor.editCost(item, stop)"
            >
              <AppIcon name="dollar" class="size-3.5!" />
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-xs btn-square text-error"
              :aria-label="t('stop.remove', { name: stopName(stop, t) })"
              :title="t('stop.remove', { name: stopName(stop, t) })"
              @click="editor.remove(item, stop)"
            >
              <AppIcon name="trash" class="size-3.5!" />
            </button>
          </template>
        </div>
        <MarkdownText v-if="stop.note_md" :source="stop.note_md" class="mt-0.5 text-base-content/80" />
      </li>
    </ol>
    <button
      v-if="editing && editor"
      type="button"
      class="btn btn-ghost btn-xs self-start"
      @click="editor.add(item)"
    >
      <AppIcon name="plus" class="size-4!" />
      {{ t('stop.add') }}
    </button>
  </div>
</template>
