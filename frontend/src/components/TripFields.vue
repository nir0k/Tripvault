<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import CurrencySelect from '@/components/CurrencySelect.vue'

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

const { t } = useI18n()
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
      <label class="floating-label">
        <span>{{ t('tripForm.startDate') }} <span class="text-error" :aria-label="t('tripForm.required')">*</span></span>
        <input v-model="model.startDate" type="date" required class="input w-full" :max="model.endDate || undefined" />
      </label>
      <label class="floating-label">
        <span>{{ t('tripForm.endDate') }} <span class="text-error" :aria-label="t('tripForm.required')">*</span></span>
        <input v-model="model.endDate" type="date" required class="input w-full" :min="model.startDate || undefined" />
      </label>
    </div>

    <div class="grid gap-3 sm:grid-cols-2">
      <CurrencySelect v-model="model.currency" />
      <label class="floating-label">
        <span>{{ t('tripForm.travelers') }}</span>
        <input v-model.number="model.travelers" type="number" required min="1" max="100" class="input w-full" :placeholder="t('tripForm.travelers')" />
      </label>
    </div>
    <label v-if="!extended" class="floating-label">
      <span>{{ t('tripForm.budget') }}</span>
      <input v-model="model.budget" type="text" inputmode="decimal" class="input w-full" :placeholder="t('tripForm.budget')" />
    </label>
  </div>
</template>
