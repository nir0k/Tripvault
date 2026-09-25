<script setup lang="ts">
import { useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { invalidAmount, isFormula, resolveAmount } from '@/utils/amount'

// A text field for an amount of money that also takes a formula such as
// "=120*3+45". The formula is computed when the field is left; one that does
// not compute keeps the form from being sent, as a required field would.
const model = defineModel<string>({ required: true })

const { t } = useI18n()
const input = useTemplateRef<HTMLInputElement>('input')

// The field is invalid while it holds a formula that does not compute. It is
// checked on every change, including a form filled anew for another record.
watch([model, input], ([value, element]) => {
  element?.setCustomValidity(invalidAmount(value) ? t('amount.invalidFormula') : '')
}, { immediate: true, flush: 'post' })

// compute replaces a formula with its result once the person leaves the field.
function compute(): void {
  if (isFormula(model.value) && !invalidAmount(model.value)) {
    model.value = resolveAmount(model.value)
  }
}
</script>

<template>
  <input
    ref="input"
    v-model="model"
    type="text"
    inputmode="decimal"
    autocomplete="off"
    :title="t('amount.formulaHint')"
    @blur="compute"
  />
</template>
