<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Leg, TravelMode } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IconSelect from '@/components/IconSelect.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import { formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'
import { formatDuration, travelModeOptions } from '@/utils/plan'

// The journey to the element below it: mode, distance, time and how they were
// obtained.
const props = defineProps<{
  leg: Leg
  currency: string
  canEdit: boolean
}>()

const emit = defineEmits<{
  mode: [mode: TravelMode]
  edit: []
  retry: []
}>()

const { t, locale } = useI18n()

const modes = computed(() => travelModeOptions(t))

const distance = computed(() => (props.leg.distance_m === null ? '' : formatDistance(props.leg.distance_m, locale.value, activeUnits.value)))
const duration = computed(() =>
  props.leg.duration_s === null ? '' : formatDuration(Math.round(props.leg.duration_s / 60), t),
)
const cost = computed(() => formatMoney(props.leg.planned_cost_amount, props.currency, locale.value))

// A flight's time is an estimate until somebody types the scheduled one.
const flightEstimate = computed(() => props.leg.mode === 'flight' && !props.leg.manual_duration && props.leg.duration_s !== null)
const manual = computed(() => props.leg.manual_distance || props.leg.manual_duration)
// Without a provider a retry cannot help, and the page already explains why.
const retryable = computed(() => props.leg.source === 'estimate' && props.leg.error !== 'provider_disabled')
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-2 gap-y-1 py-1 pl-4 text-sm text-base-content/70 sm:pl-16" :data-leg-id="leg.id">
    <span class="h-4 border-l-2 border-dashed border-base-300" :class="{ 'border-solid': leg.source === 'provider' }" aria-hidden="true"></span>
    <IconSelect
      v-if="canEdit"
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
    <span v-if="leg.mode === 'transit' && leg.source === 'provider'" class="badge badge-ghost badge-xs">{{ t('leg.transitEstimate') }}</span>
    <span v-if="leg.note" class="italic">{{ leg.note }}</span>

    <template v-if="canEdit">
      <button v-if="retryable" type="button" class="btn btn-ghost btn-xs" @click="emit('retry')">{{ t('leg.retry') }}</button>
      <button type="button" class="btn btn-ghost btn-xs" @click="emit('edit')">{{ t('plan.edit') }}</button>
    </template>
    <p v-if="retryable && leg.error" class="basis-full pl-3 text-xs">{{ t(`leg.errors.${leg.error}`) }}</p>
  </div>
</template>
