<script setup lang="ts">
import { reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { uploadMedia } from '@/api/media'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { errorMessage } from '@/utils/errors'
import { shrinkPicture } from '@/utils/picture'

// Adding photographs to a day or a place: pick them, or drop them on the panel.
// Each file goes up in a request of its own, so each has a bar of its own, and
// large ones are shrunk here first - a phone's photograph is far larger than
// anything a report shows, and the service would only store the difference.
const props = withDefaults(defineProps<{
  tripId: string
  /** Start as a button and open on a click, for a place among many others. */
  compact?: boolean
}>(), { compact: false })

const emit = defineEmits<{
  uploaded: [media: Media[]]
}>()

const { t, te } = useI18n()

/** A file on its way up, with what has been sent of it so far. */
interface Upload {
  name: string
  share: number
  error: string
}

const field = useTemplateRef<HTMLInputElement>('field')
// A compact uploader is a button until it is asked for: a report with a dozen
// places would otherwise be a column of identical panels.
const open = ref(!props.compact)
const uploads = reactive<Upload[]>([])
const busy = ref(false)
const dragging = ref(false)
const isPrivate = ref(false)
const shrink = ref(readShrinkPreference())

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

// send uploads the chosen files one after another, reporting each one's
// progress, and hands the stored files to the page that asked for them.
async function send(files: File[]): Promise<void> {
  if (files.length === 0 || busy.value) {
    return
  }
  busy.value = true
  uploads.splice(0, uploads.length, ...files.map((file) => ({ name: file.name, share: 0, error: '' })))
  const stored: Media[] = []

  for (const [index, file] of files.entries()) {
    const upload = uploads[index]
    if (!upload) {
      continue
    }
    try {
      const prepared = shrink.value ? await shrinkPicture(file) : file
      stored.push(await uploadMedia(props.tripId, prepared, {
        private: isPrivate.value,
        onProgress: (share) => {
          upload.share = share
        },
      }))
      upload.share = 1
    } catch (err) {
      upload.error = errorMessage(err, t, te)
    }
  }

  busy.value = false
  if (stored.length > 0) {
    emit('uploaded', stored)
    // The panel folds away again once it has done its work.
    open.value = !props.compact
  }
  // The finished bars stay only while something failed, so the reason can be read.
  if (!uploads.some((upload) => upload.error)) {
    uploads.splice(0, uploads.length)
  }
}

// onPick takes the files from the field and empties it, so the same file can be
// chosen again after it was removed.
function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  void send(Array.from(input.files ?? []))
  input.value = ''
}

// onDrop takes the files dropped on the panel.
function onDrop(event: DragEvent): void {
  dragging.value = false
  void send(Array.from(event.dataTransfer?.files ?? []))
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
      <button type="button" class="btn btn-sm btn-hover-outline" :disabled="busy" @click="field?.click()">
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

    <ul v-if="uploads.length > 0" class="space-y-1">
      <li v-for="upload in uploads" :key="upload.name" class="flex items-center gap-2 text-sm">
        <span class="min-w-0 flex-1 truncate">{{ upload.name }}</span>
        <progress
          v-if="!upload.error"
          class="progress progress-primary w-32"
          :value="Math.round(upload.share * 100)"
          max="100"
        ></progress>
        <span v-else class="text-error">{{ upload.error }}</span>
      </li>
    </ul>
  </div>
</template>
