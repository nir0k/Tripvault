<script setup lang="ts">
import { reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TranslationEntry, TranslationTarget } from '@/api/types'
import MarkdownText from '@/components/MarkdownText.vue'

// The form a translator writes an element of a report in: its words alone,
// each beside the original it is translated from. Times, ratings, costs and
// pictures are the same in every language, so they are not here - they are
// changed in the original. A field emptied here goes back to the original.

/** TranslateField is one field of the form. */
export interface TranslateField {
  target: TranslationTarget
  id: string
  field: string
  label: string
  original: string
  /** The translation as it stands; '' when there is none. */
  value: string
  multiline?: boolean
  maxlength: number
}

const emit = defineEmits<{
  /** Only the fields that changed are sent. */
  save: [entries: TranslationEntry[]]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const heading = ref('')
const fields = ref<TranslateField[]>([])
const drafts = reactive<Record<string, string>>({})
const error = ref('')

// keyOf names a field of the form.
function keyOf(field: TranslateField): string {
  return `${field.id}.${field.field}`
}

/** open shows the form for the given fields, under a heading. */
function open(title: string, entries: TranslateField[]): void {
  heading.value = title
  fields.value = entries
  error.value = ''
  for (const key of Object.keys(drafts)) {
    delete drafts[key]
  }
  for (const entry of entries) {
    drafts[keyOf(entry)] = entry.value
  }
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

// submit sends what changed, or just closes when nothing did.
function submit(): void {
  const changed = fields.value
    .filter((entry) => (drafts[keyOf(entry)] ?? '').trim() !== entry.value.trim())
    .map((entry) => ({
      target_type: entry.target, target_id: entry.id, field: entry.field, value: drafts[keyOf(entry)] ?? '',
    }))
  if (changed.length === 0) {
    close()
    return
  }
  emit('save', changed)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-4 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold break-words">{{ heading }}</h2>
      <p class="text-sm text-base-content/70">{{ t('report.translateHint') }}</p>

      <div v-for="entry in fields" :key="keyOf(entry)" class="flex flex-col gap-1">
        <span class="label">{{ entry.label }}</span>
        <div class="rounded-box bg-base-200 px-3 py-2 text-sm text-base-content/80">
          <span class="text-xs font-medium text-base-content/60">{{ t('report.original') }}</span>
          <MarkdownText v-if="entry.multiline" :source="entry.original" />
          <p v-else class="break-words">{{ entry.original }}</p>
        </div>
        <textarea
          v-if="entry.multiline"
          v-model="drafts[keyOf(entry)]"
          class="textarea w-full"
          rows="5"
          :maxlength="entry.maxlength"
          :placeholder="t('report.notTranslated')"
          :aria-label="entry.label"
        ></textarea>
        <input
          v-else
          v-model="drafts[keyOf(entry)]"
          type="text"
          class="input w-full"
          :maxlength="entry.maxlength"
          :placeholder="t('report.notTranslated')"
          :aria-label="entry.label"
        />
      </div>

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
