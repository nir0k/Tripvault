<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const wordInput = useTemplateRef<HTMLInputElement>('wordInput')
const question = ref('')
const details = ref<string[]>([])
const danger = ref(false)
// When set, the action goes through only once this word is typed, which keeps a
// deletion that cannot be undone from being one careless click away.
const word = ref('')
const typed = ref('')
let settle: ((confirmed: boolean) => void) | null = null

// The word is compared ignoring case and surrounding spaces: the point is to
// make the reader stop and read, not to test their typing.
const ready = computed(() => word.value === '' || typed.value.trim().toLowerCase() === word.value.toLowerCase())

/**
 * ask shows a question, with an optional list of what the action affects, and
 * resolves to whether the person confirmed. A confirmWord has to be typed
 * before the action can be confirmed.
 */
function ask(
  text: string,
  options: { details?: string[]; danger?: boolean; confirmWord?: string } = {},
): Promise<boolean> {
  question.value = text
  details.value = options.details ?? []
  danger.value = options.danger ?? false
  word.value = options.confirmWord ?? ''
  typed.value = ''
  dialog.value?.showModal()
  if (word.value !== '') {
    void nextTick(() => wordInput.value?.focus())
  }
  return new Promise((resolve) => {
    settle = resolve
  })
}

// answer closes the dialog and resolves the pending question.
function answer(confirmed: boolean): void {
  if (confirmed && !ready.value) {
    return
  }
  const resolve = settle
  settle = null
  dialog.value?.close()
  resolve?.(confirmed)
}

defineExpose({ ask })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle" @close="answer(false)">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="answer(true)">
      <h2 class="text-lg font-bold">{{ t('users.confirmTitle') }}</h2>
      <p>{{ question }}</p>
      <ul v-if="details.length > 0" class="list-inside list-disc text-sm text-base-content/80">
        <li v-for="line in details" :key="line">{{ line }}</li>
      </ul>
      <label v-if="word" class="flex flex-col gap-1">
        <span class="label">{{ t('common.typeToConfirm', { word }) }}</span>
        <input ref="wordInput" v-model="typed" type="text" autocomplete="off" class="input w-full" :placeholder="word" />
      </label>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="answer(false)">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn" :class="danger ? 'btn-error' : 'btn-primary'" :disabled="!ready">
          {{ t('common.confirm') }}
        </button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
