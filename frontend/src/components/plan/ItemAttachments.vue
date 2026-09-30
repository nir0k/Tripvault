<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { attachItemFile, deleteAttachment, describeAttachment, downloadAttachment } from '@/api/documents'
import type { Attachment } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import AttachmentDialog from '@/components/plan/AttachmentDialog.vue'
import { useDocumentChange } from '@/composables/useDocumentChange'
import { errorMessage } from '@/utils/errors'
import { formatFileSize } from '@/utils/format'

// The files a place or an activity carries besides its pictures: a ticket, a
// booking, a timetable. Each is one row - its name with the line describing it,
// its size, a download and, for somebody who may change the trip, a pencil for
// the line and a way to remove it - and a file is always downloaded, never
// shown inside the page. Chosen files pass through a window where each is given
// its line before it goes up. Only documents and pictures
// are taken; the server decides from the bytes, the picker only suggests.
//
// The component uploads and removes files itself and hands the document the
// server answered with to the page (useDocumentChange), so the card it sits in
// only says where "Attach" is: its own button, or pick() from the card's row of
// actions.
// The card's classes go to the list, not to the wrapper, which draws no box of
// its own: the picker and the window beside the list take no room on the card.
defineOptions({ inheritAttrs: false })

const props = defineProps<{
  itemId: string
  attachments: Attachment[]
  editing: boolean
  /** Show a button of its own for attaching a file. */
  addButton?: boolean
}>()

const { t, te, locale } = useI18n()
const takeDocument = useDocumentChange()

// ACCEPT lists the endings the server takes, so the picker offers those first.
const ACCEPT = [
  '.pdf', '.txt', '.md', '.markdown', '.csv', '.ics', '.eml', '.html', '.htm', '.rtf',
  '.doc', '.docx', '.xls', '.xlsx', '.ppt', '.pptx', '.odt', '.ods', '.odp',
  '.jpg', '.jpeg', '.png', '.webp', '.avif', '.bmp',
].join(',')

const field = useTemplateRef<HTMLInputElement>('field')
const describer = useTemplateRef<InstanceType<typeof AttachmentDialog>>('describer')
// uploading names the file on its way up and how much of it is sent, 0 to 1.
const uploading = ref<{ name: string; share: number } | null>(null)
// busy is the attachment being downloaded or removed.
const busy = ref('')
const error = ref('')

/** pick opens the file picker, for a card that keeps "Attach" in a row of its own. */
function pick(): void {
  field.value?.click()
}

defineExpose({ pick })

// onPick asks for the descriptions of the chosen files and empties the field,
// so the same file can be chosen again after it was removed.
function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length > 0) {
    describer.value?.openFiles(files)
  }
}

// attach sends the described files one after another, stopping at the first
// the server refuses.
async function attach(entries: { file: File; description: string }[]): Promise<void> {
  error.value = ''
  for (const { file, description } of entries) {
    uploading.value = { name: file.name, share: 0 }
    try {
      const document = await attachItemFile(props.itemId, file, description, (share) => {
        if (uploading.value) {
          uploading.value.share = share
        }
      })
      takeDocument?.(document)
    } catch (err) {
      error.value = `${file.name}: ${errorMessage(err, t, te)}`
      break
    } finally {
      uploading.value = null
    }
  }
}

// download saves one attached file.
async function download(attachment: Attachment): Promise<void> {
  busy.value = attachment.id
  error.value = ''
  try {
    await downloadAttachment(attachment)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = ''
  }
}

// describe stores a new line for an attached file.
async function describe(attachment: Attachment, description: string): Promise<void> {
  busy.value = attachment.id
  error.value = ''
  try {
    takeDocument?.(await describeAttachment(attachment.id, description))
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = ''
  }
}

// remove takes a file off the place.
async function remove(attachment: Attachment): Promise<void> {
  busy.value = attachment.id
  error.value = ''
  try {
    takeDocument?.(await deleteAttachment(attachment.id))
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="contents">
    <div
      v-if="attachments.length > 0 || uploading || error || (editing && addButton)"
      v-bind="$attrs"
      class="flex flex-col gap-1 text-sm"
    >
      <ul v-if="attachments.length > 0" class="flex flex-col gap-1" :aria-label="t('attachment.list')">
        <li
          v-for="attachment in attachments"
          :key="attachment.id"
          class="flex items-center gap-2 rounded-box bg-base-200 px-3 py-1"
        >
          <AppIcon name="paperclip" class="size-4! shrink-0 opacity-70" />
          <button
            type="button"
            class="flex min-w-0 flex-1 flex-col text-start"
            :title="t('attachment.download', { name: attachment.original_name })"
            :disabled="busy === attachment.id"
            @click="download(attachment)"
          >
            <span class="truncate hover:underline">{{ attachment.original_name }}</span>
            <span v-if="attachment.description" class="text-xs break-words text-base-content/60">{{ attachment.description }}</span>
          </button>
          <span class="shrink-0 text-base-content/60 tabular-nums">{{ formatFileSize(attachment.size, locale) }}</span>
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-square"
            :disabled="busy === attachment.id"
            :aria-label="t('attachment.download', { name: attachment.original_name })"
            :title="t('attachment.download', { name: attachment.original_name })"
            @click="download(attachment)"
          >
            <span v-if="busy === attachment.id" class="loading loading-spinner loading-xs"></span>
            <AppIcon v-else name="download" class="size-4!" />
          </button>
          <button
            v-if="editing"
            type="button"
            class="btn btn-ghost btn-xs btn-square"
            :disabled="busy === attachment.id"
            :aria-label="t('attachment.describe', { name: attachment.original_name })"
            :title="t('attachment.describe', { name: attachment.original_name })"
            @click="describer?.openAttachment(attachment)"
          >
            <AppIcon name="pencil" class="size-4!" />
          </button>
          <button
            v-if="editing"
            type="button"
            class="btn btn-ghost btn-xs btn-square text-error"
            :disabled="busy === attachment.id"
            :aria-label="t('attachment.remove', { name: attachment.original_name })"
            :title="t('attachment.remove', { name: attachment.original_name })"
            @click="remove(attachment)"
          >
            <AppIcon name="trash" class="size-4!" />
          </button>
        </li>
      </ul>
      <div v-if="uploading" class="flex items-center gap-2 px-3" role="status">
        <span class="min-w-0 flex-1 truncate text-base-content/70">{{ t('attachment.uploading', { name: uploading.name }) }}</span>
        <progress class="progress progress-primary w-24" :value="uploading.share" max="1"></progress>
      </div>
      <p v-if="error" role="alert" class="px-3 text-error">{{ error }}</p>
      <div v-if="editing && addButton">
        <button type="button" class="btn btn-ghost btn-xs" :disabled="!!uploading" @click="pick">
          <AppIcon name="paperclip" class="size-4!" />
          {{ t('attachment.add') }}
        </button>
      </div>
    </div>
    <template v-if="editing">
      <input ref="field" type="file" multiple :accept="ACCEPT" class="hidden" @change="onPick" />
      <AttachmentDialog ref="describer" @attach="attach" @describe="describe" />
    </template>
  </div>
</template>
