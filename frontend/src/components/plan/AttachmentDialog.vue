<script setup lang="ts">
import { reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Attachment } from '@/api/types'
import { formatFileSize } from '@/utils/format'

// The window a file passes through on its way to a place: each chosen file with
// a line saying what it is - "Return tickets, both of us" - counted as it is
// typed against the 150 characters a description may take. The same window
// changes the line of a file already attached.
const emit = defineEmits<{
  /** The chosen files with their descriptions, in the order they were chosen. */
  attach: [entries: { file: File; description: string }[]]
  /** The new description of an attached file. */
  describe: [attachment: Attachment, description: string]
}>()

/** MAX_DESCRIPTION is how many characters the server keeps of a description. */
const MAX_DESCRIPTION = 150

const { t, locale } = useI18n()
const dialog = useTemplateRef<HTMLDialogElement>('dialog')
// entries are the files being attached; editing is the file whose line changes.
const entries = reactive<{ file: File; description: string }[]>([])
const editing = ref<Attachment | null>(null)
const description = ref('')

// length counts characters as the server does, so a letter outside the basic
// plane counts once, as it is seen.
function length(text: string): number {
  return [...text].length
}

/** openFiles asks for the descriptions of files just chosen. */
function openFiles(files: File[]): void {
  editing.value = null
  entries.splice(0, entries.length, ...files.map((file) => ({ file, description: '' })))
  dialog.value?.showModal()
}

/** openAttachment asks for a new description of a file already attached. */
function openAttachment(attachment: Attachment): void {
  editing.value = attachment
  entries.splice(0)
  description.value = attachment.description
  dialog.value?.showModal()
}

/** close hides the window. */
function close(): void {
  dialog.value?.close()
}

// submit hands what was written over and closes the window.
function submit(): void {
  if (editing.value) {
    emit('describe', editing.value, description.value.trim())
  } else {
    emit('attach', entries.map((entry) => ({ file: entry.file, description: entry.description.trim() })))
  }
  close()
}

defineExpose({ openFiles, openAttachment })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('attachment.describeTitle') : t('attachment.attachTitle') }}</h2>

      <template v-if="editing">
        <p class="truncate text-sm text-base-content/70">{{ editing.original_name }}</p>
        <label class="flex flex-col gap-1">
          <span class="sr-only">{{ t('attachment.description') }}</span>
          <input
            v-model="description"
            type="text"
            :maxlength="MAX_DESCRIPTION"
            class="input w-full"
            :placeholder="t('attachment.descriptionPlaceholder')"
          />
          <span class="self-end text-xs text-base-content/60 tabular-nums" aria-live="polite">
            {{ t('attachment.counter', { count: length(description), max: MAX_DESCRIPTION }) }}
          </span>
        </label>
      </template>
      <template v-else>
        <div v-for="(entry, index) in entries" :key="index" class="flex flex-col gap-1">
          <p class="flex min-w-0 items-baseline gap-2 text-sm">
            <span class="truncate font-medium">{{ entry.file.name }}</span>
            <span class="shrink-0 text-base-content/60">{{ formatFileSize(entry.file.size, locale) }}</span>
          </p>
          <label class="flex flex-col gap-1">
            <span class="sr-only">{{ t('attachment.descriptionOf', { name: entry.file.name }) }}</span>
            <input
              v-model="entry.description"
              type="text"
              :maxlength="MAX_DESCRIPTION"
              class="input w-full"
              :placeholder="t('attachment.descriptionPlaceholder')"
            />
            <span class="self-end text-xs text-base-content/60 tabular-nums" aria-live="polite">
              {{ t('attachment.counter', { count: length(entry.description), max: MAX_DESCRIPTION }) }}
            </span>
          </label>
        </div>
      </template>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ editing ? t('common.save') : t('attachment.add') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
