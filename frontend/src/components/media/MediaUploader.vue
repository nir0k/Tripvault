<script setup lang="ts">
import { onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { useUploadsStore, type UploadHandle } from '@/stores/uploads'

// Adding photographs: a button that opens a window where they are dropped or
// chosen, with the two choices about how they go up. A page therefore carries
// one button rather than a panel, however many days and places it lists. The
// files are handed to the application's upload queue, whose window in the
// corner (UploadPanel) follows them on their way up, so this window closes as
// soon as it has them.
const props = withDefaults(defineProps<{
  tripId: string
  /** A small button, for a day or a place among many others. */
  small?: boolean
}>(), { small: false })

const emit = defineEmits<{
  uploaded: [media: Media[]]
}>()

const { t } = useI18n()
const uploads = useUploadsStore()

const field = useTemplateRef<HTMLInputElement>('field')
const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const dragging = ref(false)
const isPrivate = ref(false)
const shrink = ref(readShrinkPreference())
// The batches this panel started, which stop reporting to it once it is gone:
// the files still go up, and the page they were added from is no longer there
// to show them.
const handles: UploadHandle[] = []
onBeforeUnmount(() => {
  for (const handle of handles) {
    handle.detach()
  }
})

/** SHRINK_KEY remembers, per browser, whether large pictures are reduced. */
const SHRINK_KEY = 'tripvault.media_shrink'

// readShrinkPreference reads the remembered choice; reducing is the default,
// because a full-size photograph is rarely what a report needs.
function readShrinkPreference(): boolean {
  try {
    return localStorage.getItem(SHRINK_KEY) !== 'false'
  } catch {
    return true
  }
}

// rememberShrink keeps the choice for the next upload from this browser.
function rememberShrink(value: boolean): void {
  try {
    localStorage.setItem(SHRINK_KEY, String(value))
  } catch {
    // Without storage the choice simply lasts as long as the page does.
  }
}

// send hands the chosen files to the upload queue; the page hears of the
// stored ones once they are all through.
function send(files: File[]): void {
  if (files.length === 0) {
    return
  }
  handles.push(uploads.enqueue(props.tripId, files, { private: isPrivate.value, shrink: shrink.value },
    (media) => emit('uploaded', media)))
  // The window in the corner takes over from here.
  dialog.value?.close()
}

// open shows the window, with nothing dragged over it yet.
function open(): void {
  dragging.value = false
  dialog.value?.showModal()
}

// onPick takes the files from the field and empties it, so the same file can be
// chosen again after it was removed.
function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  send(Array.from(input.files ?? []))
  input.value = ''
}

// onDrop takes the files dropped anywhere on the window.
function onDrop(event: DragEvent): void {
  dragging.value = false
  send(Array.from(event.dataTransfer?.files ?? []))
}

// onShrink stores the choice as it is made.
function onShrink(event: Event): void {
  shrink.value = (event.target as HTMLInputElement).checked
  rememberShrink(shrink.value)
}
</script>

<template>
  <button
    type="button"
    class="btn btn-hover-outline"
    :class="{ 'btn-sm': small }"
    @click="open"
  >
    <AppIcon name="upload" />
    {{ t('media.add') }}
  </button>

  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <div
      class="modal-box flex flex-col gap-4"
      @dragover.prevent="dragging = true"
      @dragleave.self="dragging = false"
      @drop.prevent="onDrop"
    >
      <h2 class="text-lg font-bold">{{ t('media.add') }}</h2>

      <div
        class="flex flex-col items-center gap-3 rounded-box border-2 border-dashed px-4 py-10 text-center transition-colors"
        :class="dragging ? 'border-primary bg-primary/5' : 'border-base-300'"
      >
        <span class="text-primary"><AppIcon name="upload" /></span>
        <p class="text-sm text-base-content/70">{{ t('media.dropHint') }}</p>
        <input
          ref="field"
          type="file"
          accept="image/jpeg,image/png,image/webp"
          multiple
          class="hidden"
          @change="onPick"
        />
        <button type="button" class="btn btn-primary btn-sm" @click="field?.click()">
          {{ t('media.choose') }}
        </button>
      </div>

      <div class="flex flex-wrap items-center justify-center gap-4">
        <label class="label cursor-pointer gap-2 text-sm">
          <input type="checkbox" class="toggle toggle-sm" :checked="shrink" @change="onShrink" />
          <span>{{ t('media.shrink') }}</span>
        </label>
        <label class="label cursor-pointer gap-2 text-sm">
          <input v-model="isPrivate" type="checkbox" class="toggle toggle-sm" />
          <span>{{ t('media.uploadPrivate') }}</span>
        </label>
      </div>

      <div class="modal-action mt-0">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.cancel') }}</button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
