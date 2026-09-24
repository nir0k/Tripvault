<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Night, Stay, TripDocument } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import { formatDayDate, formatMoney } from '@/utils/format'
import { stayColor } from '@/utils/plan'

// The stays of the document: the strip of nights, the list by check-in and the
// totals.
const props = defineProps<{
  document: TripDocument
  currency: string
  canEdit: boolean
}>()

const emit = defineEmits<{
  add: []
  edit: [stay: Stay]
  remove: [stay: Stay]
}>()

const { t, locale } = useI18n()

const colors = computed(() => new Map(props.document.stays.map((stay, index) => [stay.id, stayColor(index)])))
const names = computed(() => new Map(props.document.stays.map((stay) => [stay.id, stay.name])))
const overlaps = computed(() => props.document.nights.filter((night) => night.stay_ids.length > 1).length)
const missing = computed(() => props.document.nights.filter((night) => night.missing).length)

// nightClass colours a night by its stay, and marks gaps and overlaps.
function nightClass(night: Night): string {
  if (night.stay_ids.length > 0) {
    const overlap = night.stay_ids.length > 1 ? ' ring-2 ring-error ring-offset-1' : ''
    return (colors.value.get(night.stay_ids[0] ?? '') ?? 'bg-primary') + overlap
  }
  return night.no_overnight ? 'bg-base-300' : 'bg-error/30 border border-dashed border-error'
}

// nightLabel describes a night for its tooltip and screen readers.
function nightLabel(night: Night): string {
  const date = formatDayDate(night.date, locale.value, true)
  if (night.stay_ids.length > 0) {
    return `${date}: ${night.stay_ids.map((id) => names.value.get(id) ?? '').join(', ')}`
  }
  return `${date}: ${night.no_overnight ? t('stay.noOvernight') : t('stay.missing')}`
}

// checkText shows a check-in or check-out date with its time.
function checkText(date: string, time: string | null): string {
  const day = formatDayDate(date, locale.value, true)
  return time ? `${day}, ${time}` : day
}
</script>

<template>
  <section class="space-y-4" :aria-label="t('stay.title')">
    <header class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-xl font-bold">{{ t('stay.title') }}</h2>
      <button v-if="canEdit" type="button" class="btn btn-primary btn-sm" @click="emit('add')">
        <AppIcon name="plus" />
        {{ t('stay.add') }}
      </button>
    </header>

    <div v-if="document.nights.length > 0" class="space-y-2">
      <ol class="flex gap-1" :aria-label="t('stay.nights')">
        <li
          v-for="night in document.nights"
          :key="night.date"
          class="h-6 min-w-2 flex-1 rounded-sm"
          :class="nightClass(night)"
          :title="nightLabel(night)"
          :aria-label="nightLabel(night)"
        ></li>
      </ol>
      <p v-if="missing > 0"><span class="badge badge-warning h-auto py-0.5">{{ t('stay.missingNights', missing) }}</span></p>
      <p v-if="overlaps > 0" class="text-sm text-error">{{ t('stay.overlaps', overlaps) }}</p>
    </div>
    <p v-else class="text-sm text-base-content/70">{{ t('stay.noDates') }}</p>

    <ul class="space-y-2">
      <li v-for="stay in document.stays" :key="stay.id" class="flex items-start gap-3 rounded-box border border-base-300 p-3">
        <span class="mt-1.5 size-3 shrink-0 rounded-full" :class="colors.get(stay.id)" aria-hidden="true"></span>
        <div class="min-w-0 flex-1 space-y-1">
          <p class="flex flex-wrap items-center gap-2">
            <span class="font-medium">{{ stay.name }}</span>
            <span class="badge badge-ghost badge-sm">{{ t(`stayKinds.${stay.kind}`) }}</span>
          </p>
          <p class="text-sm text-base-content/70">
            {{ checkText(stay.check_in_date, stay.check_in_time) }} → {{ checkText(stay.check_out_date, stay.check_out_time) }}
            · {{ t('stay.nightsCount', stay.nights) }}
          </p>
          <p v-if="stay.planned_cost_amount" class="text-sm">
            {{ formatMoney(stay.planned_cost_amount, currency, locale) }}
            <span v-if="stay.price_per_night" class="text-base-content/70">
              · {{ t('stay.perNight', { amount: formatMoney(stay.price_per_night, currency, locale) }) }}
            </span>
          </p>
          <p v-if="stay.address" class="text-sm text-base-content/70">{{ stay.address }}</p>
          <p v-if="stay.booking_ref || stay.url" class="flex flex-wrap gap-2 text-sm">
            <span v-if="stay.booking_ref">{{ t('stay.bookingRef') }}: {{ stay.booking_ref }}</span>
            <a v-if="stay.url" :href="stay.url" target="_blank" rel="noopener noreferrer nofollow" class="link">{{ t('stay.link') }}</a>
          </p>
          <p v-if="stay.contacts" class="text-sm whitespace-pre-line text-base-content/70">{{ stay.contacts }}</p>
          <MarkdownText v-if="stay.notes_md" :source="stay.notes_md" />
        </div>
        <div v-if="canEdit" class="flex gap-1">
          <button type="button" class="btn btn-ghost btn-sm" @click="emit('edit', stay)">{{ t('plan.edit') }}</button>
          <button type="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('plan.delete')" @click="emit('remove', stay)">
            <AppIcon name="trash" />
          </button>
        </div>
      </li>
    </ul>
    <p v-if="document.stays.length === 0" class="rounded-box border border-dashed border-base-300 p-4 text-center text-sm text-base-content/60">
      {{ t('stay.empty') }}
    </p>

    <footer v-if="document.stays.length > 0" class="flex flex-wrap gap-x-4 gap-y-1 rounded-box bg-base-200 px-4 py-3 text-sm">
      <span>{{ t('stay.nightsCount', document.stay_summary.nights) }}</span>
      <span>{{ t('stay.total', { amount: formatMoney(document.stay_summary.cost, currency, locale) }) }}</span>
      <span v-if="document.stay_summary.average_per_night">
        {{ t('stay.average', { amount: formatMoney(document.stay_summary.average_per_night, currency, locale) }) }}
      </span>
    </footer>
  </section>
</template>
