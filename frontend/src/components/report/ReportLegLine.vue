<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Leg } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import { formatDistance } from '@/utils/format'
import { formatDuration } from '@/utils/plan'
import { activeUnits } from '@/utils/units'

// How the report got to a place: the means, the time and the distance, as one
// quiet line above the place's card. The figures are calculated like a plan's
// and may be typed over; while editing, the line opens the form that does it.
const props = defineProps<{
  leg: Leg
  /** Where the journey started, when it was not the place above - the stay. */
  from?: string
  editing?: boolean
}>()

const emit = defineEmits<{
  edit: []
}>()

const { t, locale } = useI18n()

const distance = computed(() =>
  props.leg.distance_m === null ? '' : formatDistance(props.leg.distance_m, locale.value, activeUnits.value))
const duration = computed(() =>
  props.leg.duration_s === null ? '' : formatDuration(Math.round(props.leg.duration_s / 60), t))
// A figure drawn as a straight line or guessed is said to be one.
const estimate = computed(() => props.leg.source === 'estimate' || props.leg.source === 'missing_coordinates')
const pending = computed(() => props.leg.source === 'pending' && !duration.value && !distance.value)
// Why a road was not found, told only to the person who can move the pin.
const noRoute = computed(() => props.editing && props.leg.source === 'estimate' && props.leg.error === 'no_route')
</script>

<template>
  <component
    :is="editing ? 'button' : 'p'"
    :type="editing ? 'button' : undefined"
    class="flex flex-wrap items-center gap-x-2 gap-y-1 ps-4 text-start text-sm text-base-content/60"
    :class="{ 'cursor-pointer rounded-box pe-2 hover:bg-base-200': editing }"
    :title="editing ? t('leg.editTitle') : undefined"
    :data-leg-id="leg.id"
    @click="editing && emit('edit')"
  >
    <span class="h-4 border-l-2 border-dashed border-base-300" aria-hidden="true"></span>
    <span class="flex items-center gap-1">
      <AppIcon :name="TRAVEL_MODE_ICONS[leg.mode]" class="size-4!" />
      {{ t(`modes.${leg.mode}`) }}
    </span>
    <span v-if="duration">· {{ duration }}</span>
    <span v-if="distance">· {{ distance }}</span>
    <span v-if="pending">· {{ t('leg.pending') }}</span>
    <span v-if="from" class="italic">· {{ t('report.legFrom', { name: from }) }}</span>
    <span v-if="estimate && (duration || distance)" class="badge badge-ghost badge-xs">{{ t('leg.estimate') }}</span>
    <span v-if="leg.note" class="italic">· {{ leg.note }}</span>
    <AppIcon v-if="editing" name="pencil" class="size-3.5! opacity-60" />
    <span v-if="noRoute" class="basis-full text-xs">{{ t('leg.errors.no_route') }} {{ t('leg.noRouteHint') }}</span>
  </component>
</template>
