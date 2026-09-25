<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AmountInput from '@/components/AmountInput.vue'
import { normalizeAmount } from '@/utils/format'

// The form for the trip's overall budget. An empty amount clears it, which is
// how a trip goes back to having no budget at all.
const emit = defineEmits<{
  save: [amount: string | null]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const draft = ref('')
const error = ref('')

/** open shows the form with the amount the trip carries now. */
function open(current: string | null): void {
  draft.value = current ?? ''
  error.value = ''
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

// submit hands the typed amount to the page, normalised the way the API wants it.
function submit(): void {
  emit('save', normalizeAmount(draft.value))
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('budget.setBudget') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('budget.budgetHint') }}</p>

      <label class="floating-label">
        <span>{{ t('trip.budget') }}</span>
        <AmountInput
          v-model="draft"
          class="input w-full"
          :placeholder="t('trip.budget')"
        />
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
