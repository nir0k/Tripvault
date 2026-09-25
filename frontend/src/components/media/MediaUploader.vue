<script setup lang="ts">
import { onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { useUploadsStore, type UploadHandle } from '@/stores/uploads'

// Adding photographs to a day or a place: pick them, or drop them on the panel.
// The files are handed to the application's upload queue, whose window in the
// corner (UploadPanel) follows them on their way up, so this panel stays free
// for the next ones and the page keeps its shape.
const props = withDefaults(defineProps<{
  tripId: string
  /** Start as a button and open on a click, for a place among many others. */
  compact?: boolean
}>(), { compact: false })

const emit = defineEmits<{
  uploaded: [media: Media[]]
}>()

const { t } = useI18n()
const uploads = useUploadsStore()

const field = useTemplateRef<HTMLInputElement>('field')
// A compact uploader is a button until it is asked for: a report with a dozen
// places would otherwise be a column of identical panels.
const open = ref(!props.compact)
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
  // A compact panel folds away at once: the window in the corner takes over.
  open.value = !props.compact
}

// onPick takes the files from the field and empties it, so the same file can be
// chosen again after it was removed.
function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  send(Array.from(input.files ?? []))
  input.value = ''
}

// onDrop takes the files dropped on the panel.
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
  <div class="space-y-2">
    <button
      v-if="compact && !open"
      type="button"
      class="btn btn-hover-outline btn-sm"
      @click="open = true"
    >
      <AppIcon name="upload" />
      {{ t('media.add') }}
    </button>

    <div
      v-if="open"
      class="flex flex-col items-center gap-2 rounded-box border border-dashed px-4 py-6 text-center"
      :class="dragging ? 'border-primary bg-primary/5' : 'border-base-300'"
      @dragover.prevent="dragging = true"
      @dragleave="dragging = false"
      @drop.prevent="onDrop"
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
      <button type="button" class="btn btn-sm btn-hover-outline" @click="field?.click()">
        {{ t('media.choose') }}
      </button>

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
    </div>
  </div>
</template>
