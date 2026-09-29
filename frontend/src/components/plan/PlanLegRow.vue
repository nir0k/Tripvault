<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Leg, TravelMode } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IconSelect from '@/components/IconSelect.vue'
import LegChain from '@/components/plan/LegChain.vue'
import PlanInsertButton from '@/components/plan/PlanInsertButton.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import { formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'
import { formatDuration, travelModeOptions } from '@/utils/plan'

// The journey to the element below it: mode, distance, time and how they were
// obtained. A journey with changes shows its parts in a row instead of one
// mode, and its mode is changed part by part in its form, not here. The gap before it is where a new element goes between the two it
// joins, so it carries the "+" that puts one there.
const props = defineProps<{
  leg: Leg
  currency: string
  canEdit: boolean
  /** Offer to put a new place or activity where the leg is. */
  insertable?: boolean
}>()

const emit = defineEmits<{
  mode: [mode: TravelMode]
  edit: []
  retry: []
  insert: [kind: 'place' | 'activity']
}>()

const { t, locale } = useI18n()

const modes = computed(() => travelModeOptions(t))

const distance = computed(() => (props.leg.distance_m === null ? '' : formatDistance(props.leg.distance_m, locale.value, activeUnits.value)))
const duration = computed(() =>
  props.leg.duration_s === null ? '' : formatDuration(Math.round(props.leg.duration_s / 60), t),
)
const cost = computed(() => formatMoney(props.leg.planned_cost_amount, props.currency, locale.value))

// A journey drawn as a straight line - a flight, a cable car, a train, a metro,
// a ferry - has an estimated time until somebody types the real one.
const STRAIGHT_TIMED: TravelMode[] = ['flight', 'cable_car', 'train', 'tram', 'ferry']
const composite = computed(() => props.leg.segments.length > 0)
const flightEstimate = computed(() => !composite.value && STRAIGHT_TIMED.includes(props.leg.mode)
  && !props.leg.manual_duration && props.leg.duration_s !== null)
const manual = computed(() => props.leg.manual_distance || props.leg.manual_duration)
// Without a provider a retry cannot help, and the page already explains why.
const retryable = computed(() => props.leg.source === 'estimate' && props.leg.error !== 'provider_disabled')
// A road the provider did not find is usually a pin too far from one, which
// only the person editing can move.
const noRoute = computed(() => props.leg.source === 'estimate' && props.leg.error === 'no_route')
</script>

<template>
  <div
    class="group relative flex flex-wrap items-center gap-x-2 gap-y-1 py-1 text-sm text-base-content/70 sm:pl-16"
    :class="insertable ? 'pl-10' : 'pl-4'"
    :data-leg-id="leg.id"
  >
    <PlanInsertButton v-if="insertable" class="absolute top-1 left-2 sm:left-8" @pick="(kind) => emit('insert', kind)" />
    <span class="h-4 border-l-2 border-dashed border-base-300" :class="{ 'border-solid': leg.source === 'provider' }" aria-hidden="true"></span>
    <LegChain v-if="composite" :segments="leg.segments" />
    <IconSelect
      v-else-if="canEdit"
      :model-value="leg.mode"
      :options="modes"
      :label="t('leg.mode')"
      compact
      @update:model-value="(mode) => emit('mode', mode as TravelMode)"
    />
    <span v-else class="flex items-center gap-1">
      <AppIcon :name="TRAVEL_MODE_ICONS[leg.mode]" class="size-4!" />
      {{ t(`modes.${leg.mode}`) }}
    </span>

    <span v-if="leg.source === 'pending'" class="flex items-center gap-1">
      <span class="loading loading-spinner loading-xs"></span>{{ t('leg.pending') }}
    </span>
    <span v-else-if="leg.source === 'missing_coordinates' && !manual">{{ t('leg.missingCoordinates') }}</span>
    <template v-else>
      <span v-if="distance">{{ distance }}</span>
      <span v-if="duration">· {{ duration }}</span>
      <span v-else-if="leg.mode === 'other'">· {{ t('leg.noTime') }}</span>
    </template>
    <span v-if="cost">· {{ cost }}</span>

    <span v-if="manual" class="badge badge-ghost badge-xs">{{ t('leg.manual') }}</span>
    <span v-if="leg.source === 'estimate'" class="badge badge-warning badge-xs" :title="leg.error ? t(`leg.errors.${leg.error}`) : ''">
      {{ t('leg.estimate') }}
    </span>
    <span v-if="flightEstimate" class="badge badge-ghost badge-xs">{{ t('leg.flightEstimate') }}</span>
    <span
      v-if="!composite && (leg.mode === 'transit' || leg.mode === 'bus') && leg.source === 'provider'"
      class="badge badge-ghost badge-xs"
    >{{ t('leg.transitEstimate') }}</span>
    <span v-if="leg.route_preference === 'shortest'" class="badge badge-ghost badge-xs">{{ t('leg.routeShortest') }}</span>
    <span v-if="leg.route_pinned" class="badge badge-ghost badge-xs">{{ t('leg.pinned') }}</span>
    <span v-if="leg.via.length > 0" class="badge badge-ghost badge-xs">{{ t('leg.via') }}</span>
    <span v-if="leg.note" class="italic">{{ leg.note }}</span>

    <template v-if="canEdit">
      <button v-if="retryable" type="button" class="btn btn-ghost btn-xs" @click="emit('retry')">{{ t('leg.retry') }}</button>
      <button type="button" class="btn btn-ghost btn-xs" @click="emit('edit')">{{ t('plan.edit') }}</button>
    </template>
    <p v-if="retryable && leg.error" class="basis-full pl-3 text-xs">{{ t(`leg.errors.${leg.error}`) }}</p>
    <p v-if="canEdit && noRoute" class="basis-full pl-3 text-xs">{{ t('leg.noRouteHint') }}</p>
  </div>
</template>
