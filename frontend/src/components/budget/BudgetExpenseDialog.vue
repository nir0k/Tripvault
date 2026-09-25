<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ExpenseFields } from '@/api/documents'
import type { BudgetDayRow, CostCategory, Expense } from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import IconSelect from '@/components/IconSelect.vue'
import { formatDayDate, normalizeAmount } from '@/utils/format'
import { costCategoryOptions } from '@/utils/plan'

// The form for a cost that belongs to no place, stay or leg. Without a day it
// belongs to the whole trip.
const props = defineProps<{
  days: BudgetDayRow[]
}>()

const emit = defineEmits<{
  save: [fields: ExpenseFields, expense: Expense | null]
}>()

const { t, locale } = useI18n()

const categories = computed(() => costCategoryOptions(t))

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<Expense | null>(null)
const error = ref('')
const form = reactive({ dayId: '', category: 'other' as CostCategory, amount: '', note: '' })

/** dayLabel names a day by its number and, when the trip has dates, its date. */
function dayLabel(day: BudgetDayRow): string {
  const number = t('plan.dayNumber', { n: day.position + 1 })
  const date = formatDayDate(day.date, locale.value)
  return date ? `${number} · ${date}` : number
}

/** open shows the form for an existing expense, or for a new one on a day. */
function open(expense: Expense | null, dayId: string | null = null): void {
  editing.value = expense
  error.value = ''
  Object.assign(form, {
    dayId: expense ? expense.day_id ?? '' : dayId ?? '',
    category: expense?.category ?? 'other',
    amount: expense?.planned_amount ?? '',
    note: expense?.note ?? '',
  })
  dialog.value?.showModal()
}

/** close hides the form. */
function close(): void {
  dialog.value?.close()
}

/** fail shows why saving did not work, keeping the form open. */
function fail(message: string): void {
  error.value = message
}

// submit collects the fields and hands them to the page.
function submit(): void {
  emit('save', {
    day_id: form.dayId || null,
    category: form.category,
    planned_amount: normalizeAmount(form.amount),
    note: form.note,
  }, editing.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('budget.editExpense') : t('budget.newExpense') }}</h2>

      <label class="floating-label">
        <span>{{ t('budget.expenseNote') }}</span>
        <input
          v-model="form.note"
          type="text"
          maxlength="500"
          class="input w-full"
          :placeholder="t('budget.expenseNotePlaceholder')"
        />
      </label>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('budget.amount') }}</span>
          <AmountInput
            v-model="form.amount"
            required
            class="input w-full"
            :placeholder="t('budget.amount')"
          />
        </label>
        <label class="flex flex-col gap-1">
          <span class="label">{{ t('place.costCategory') }}</span>
          <IconSelect v-model="form.category" :options="categories" :label="t('place.costCategory')" block />
        </label>
      </div>

      <label class="select w-full">
        <span class="label">{{ t('budget.expenseDay') }}</span>
        <select v-model="form.dayId">
          <option value="">{{ t('budget.wholeTrip') }}</option>
          <option v-for="day in props.days" :key="day.day_id" :value="day.day_id">{{ dayLabel(day) }}</option>
        </select>
      </label>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
