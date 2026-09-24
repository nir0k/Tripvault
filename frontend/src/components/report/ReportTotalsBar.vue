<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ReportTotals } from '@/api/types'
import { formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// The figures a report opens with: how long it lasted, how far it went by which
// means, how many of its places were reached and what it all cost.
const props = defineProps<{
  totals: ReportTotals
  currency: string
}>()

const { t, locale } = useI18n()

const spent = computed(() => formatMoney(props.totals.actual_cost, props.currency, locale.value))
const planned = computed(() => formatMoney(props.totals.planned_cost, props.currency, locale.value))
// A trip that cost more than the plan said is worth pointing out; one that cost
// less is not, so only the overrun is coloured.
const overspent = computed(() => Number(props.totals.difference) > 0)

const modes = computed(() => props.totals.by_mode.filter((mode) => mode.distance_m > 0))
</script>

<template>
  <section class="space-y-3" :aria-label="t('report.totals')">
    <div class="stats stats-vertical w-full border border-base-300 sm:stats-horizontal">
      <div class="stat">
        <div class="stat-title">{{ t('report.daysTitle') }}</div>
        <div class="stat-value text-2xl">{{ totals.days }}</div>
        <div class="stat-desc">{{ t('stay.nightsCount', totals.nights) }}</div>
      </div>
      <div class="stat">
        <div class="stat-title">{{ t('report.visited') }}</div>
        <div class="stat-value text-2xl">{{ totals.places.visited }}</div>
        <div class="stat-desc">{{ t('report.ofPlaces', totals.places.total) }}</div>
      </div>
      <div class="stat">
        <div class="stat-title">{{ t('report.distance') }}</div>
        <div class="stat-value text-2xl">{{ formatDistance(totals.distance_m, locale, activeUnits) }}</div>
        <div class="stat-desc truncate">
          {{ modes.map((mode) => `${t(`modes.${mode.mode}`)} ${formatDistance(mode.distance_m, locale, activeUnits)}`).join(' · ') }}
        </div>
      </div>
      <div class="stat">
        <div class="stat-title">{{ t('report.spent') }}</div>
        <div class="stat-value text-2xl" :class="{ 'text-error': overspent }">{{ spent }}</div>
        <div class="stat-desc">{{ t('report.againstPlan', { amount: planned }) }}</div>
      </div>
    </div>

    <p v-if="totals.average_rating !== null" class="text-sm text-base-content/70">
      {{ t('report.averageRating', { rating: totals.average_rating, n: totals.rated }) }}
    </p>
    <p v-if="totals.places.skipped > 0 || totals.places.unplanned > 0" class="flex flex-wrap gap-2 text-sm">
      <span v-if="totals.places.skipped > 0" class="badge badge-ghost h-auto py-0.5">
        {{ t('report.skippedCount', totals.places.skipped) }}
      </span>
      <span v-if="totals.places.unplanned > 0" class="badge badge-ghost h-auto py-0.5">
        {{ t('report.unplannedCount', totals.places.unplanned) }}
      </span>
    </p>
  </section>
</template>
