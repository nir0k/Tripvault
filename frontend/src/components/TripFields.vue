<script setup lang="ts">
import { ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AmountInput from '@/components/AmountInput.vue'
import AppIcon from '@/components/AppIcon.vue'
import CurrencySelect from '@/components/CurrencySelect.vue'
import DateRangePicker from '@/components/DateRangePicker.vue'
import { formatDayDate } from '@/utils/format'

/** TripFormModel is a trip as its form edits it: every value as typed. */
export interface TripFormModel {
  title: string
  summary: string
  startDate: string
  endDate: string
  timezone: string
  currency: string
  travelers: number
  budget: string
}

// extended marks the settings page, which shows the description but not the
// budget: once the trip exists, the budget is set on the budget screen, next to
// the costs it is compared with.
withDefaults(defineProps<{ extended?: boolean }>(), { extended: false })
const model = defineModel<TripFormModel>({ required: true })

const { t, locale } = useI18n()

// The two dates, each a field that opens the calendar to pick it.
const DATE_FIELDS = [
  { phase: 'start', key: 'startDate', label: 'tripForm.startDate' },
  { phase: 'end', key: 'endDate', label: 'tripForm.endDate' },
] as const

// fieldDate writes a date in a field: its day and month in the reader's order,
// and the year, "10 Sep 2026".
function fieldDate(value: string): string {
  return `${formatDayDate(value, locale.value)} ${value.slice(0, 4)}`
}

// picking is the date the open calendar picks, or null while it is folded.
const picking = ref<'start' | 'end' | null>(null)

const guard = useTemplateRef<HTMLInputElement>('guard')
watch([() => model.value.startDate, () => model.value.endDate, guard], ([startDate, endDate, element]) => {
  element?.setCustomValidity(startDate && endDate ? '' : t('dateRange.required'))
}, { immediate: true, flush: 'post' })
</script>

<template>
  <div class="flex flex-col gap-3">
    <label class="floating-label">
      <span>{{ t('tripForm.title') }} <span class="text-error" :aria-label="t('tripForm.required')">*</span></span>
      <input v-model="model.title" type="text" required maxlength="200" class="input w-full" :placeholder="t('tripForm.title')" />
    </label>

    <label v-if="extended" class="floating-label">
      <span>{{ t('tripForm.summary') }}</span>
      <textarea v-model="model.summary" maxlength="2000" rows="3" class="textarea w-full" :placeholder="t('tripForm.summary')"></textarea>
    </label>

    <div class="grid gap-3 sm:grid-cols-2">
      <label v-for="field in DATE_FIELDS" :key="field.phase" class="floating-label">
        <span>{{ t(field.label) }} <span class="text-error" :aria-label="t('tripForm.required')">*</span></span>
        <button
          type="button"
          class="input w-full justify-between"
          :class="{ 'input-primary': picking === field.phase }"
          :aria-expanded="picking === field.phase"
          @click="picking = picking === field.phase ? null : field.phase"
        >
          <span :class="{ 'text-base-content/50': !model[field.key] }">
            {{ model[field.key] ? fieldDate(model[field.key]) : t(field.label) }}
          </span>
          <AppIcon name="calendar" class="size-4! opacity-60" />
        </button>
      </label>
    </div>
    <DateRangePicker
      v-if="picking"
      v-model:start="model.startDate"
      v-model:end="model.endDate"
      :phase="picking"
      @done="picking = null"
    />
    <!-- The form is refused until both dates are chosen, which the browser
         says at this field: invisible, but not hidden. -->
    <input ref="guard" class="sr-only" tabindex="-1" aria-hidden="true" />

    <div class="grid gap-3 sm:grid-cols-2">
      <CurrencySelect v-model="model.currency" />
      <label class="floating-label">
        <span>{{ t('tripForm.travelers') }}</span>
        <input v-model.number="model.travelers" type="number" required min="1" max="100" class="input w-full" :placeholder="t('tripForm.travelers')" />
      </label>
    </div>
    <label v-if="!extended" class="floating-label">
      <span>{{ t('tripForm.budget') }}</span>
      <AmountInput v-model="model.budget" class="input w-full" :placeholder="t('tripForm.budget')" />
    </label>
  </div>
</template>
