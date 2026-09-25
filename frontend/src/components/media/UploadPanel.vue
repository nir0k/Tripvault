<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useUploadsStore, type Upload } from '@/stores/uploads'
import { errorMessage } from '@/utils/errors'

// The window in the corner that follows the photographs on their way up, the
// way a photo library does: a line saying how many of how many are through, a
// bar for the whole queue, and a row for each file that can be retried or
// stopped. It lives in the layout, so it stays while its reader moves on.
//
// It is a manual popover, which puts it in the top layer: a picker dialog that
// uploads a picture would otherwise cover the window reporting on it. It is
// raised again whenever files are added, so it sits above the dialog they were
// added from.

const { t, te } = useI18n()
const uploads = useUploadsStore()

const panel = useTemplateRef<HTMLElement>('panel')

// The files are shown by thumbnails of themselves, read from the reader's own
// disk; each address owns memory until it is let go.
const thumbnails = reactive(new Map<number, string>())

watch(() => uploads.items.map((item) => item.id), (ids) => {
  const wanted = new Set(ids)
  for (const [id, url] of thumbnails) {
    if (!wanted.has(id)) {
      URL.revokeObjectURL(url)
      thumbnails.delete(id)
    }
  }
  for (const item of uploads.items) {
    if (!thumbnails.has(item.id)) {
      thumbnails.set(item.id, URL.createObjectURL(item.file))
    }
  }
})

onBeforeUnmount(() => {
  for (const url of thumbnails.values()) {
    URL.revokeObjectURL(url)
  }
  thumbnails.clear()
})

// The panel is shown and raised whenever the queue grows, and put away with it.
watch(() => [uploads.visible, uploads.items.length] as const, async ([visible]) => {
  await nextTick()
  const element = panel.value
  if (!element) {
    return
  }
  if (element.matches(':popover-open')) {
    element.hidePopover()
  }
  if (visible) {
    element.showPopover()
  }
}, { flush: 'post' })

// title sums the queue up in one line.
const title = computed(() => {
  if (uploads.busy) {
    return t('media.uploadProgress', { done: uploads.done, total: uploads.total })
  }
  if (uploads.failed > 0) {
    return t('media.uploadFailedCount', { done: uploads.done, failed: uploads.failed })
  }
  return t('media.uploadDone', { count: uploads.done }, { plural: uploads.done })
})

const shown = computed(() => uploads.items.filter((item) => item.state !== 'cancelled'))

// reason words why a file was refused.
function reason(upload: Upload): string {
  return errorMessage(upload.failure, t, te)
}
</script>

<template>
  <section
    ref="panel"
    popover="manual"
    class="upload-panel inset-auto right-0 bottom-16 left-0 m-0 w-full overflow-hidden rounded-t-box border border-base-300 bg-base-100 p-0 text-base-content shadow-xl sm:right-4 sm:left-auto sm:w-96 sm:rounded-box lg:bottom-4"
    :aria-label="t('media.uploadTitle')"
  >
    <header class="flex items-center gap-2 border-b border-base-300 px-4 py-3">
      <span v-if="uploads.busy" class="loading loading-spinner loading-sm text-primary"></span>
      <AppIcon v-else-if="uploads.failed > 0" name="cross" class="text-error" />
      <AppIcon v-else name="check" class="text-success" />
      <h2 class="min-w-0 flex-1 truncate text-sm font-semibold" aria-live="polite">{{ title }}</h2>
      <button
        type="button"
        class="btn btn-ghost btn-square btn-sm"
        :aria-label="uploads.collapsed ? t('media.uploadExpand') : t('media.uploadCollapse')"
        :aria-expanded="!uploads.collapsed"
        @click="uploads.collapsed = !uploads.collapsed"
      >
        <AppIcon :name="uploads.collapsed ? 'chevronUp' : 'chevronDown'" />
      </button>
      <button
        v-if="!uploads.busy"
        type="button"
        class="btn btn-ghost btn-square btn-sm"
        :aria-label="t('common.close')"
        @click="uploads.close()"
      >
        <AppIcon name="close" />
      </button>
    </header>

    <progress
      v-if="uploads.busy"
      class="progress progress-primary block h-1 w-full rounded-none"
      :value="Math.round(uploads.progress * 100)"
      max="100"
    ></progress>

    <template v-if="!uploads.collapsed">
      <ul class="max-h-72 divide-y divide-base-300 overflow-y-auto">
        <li v-for="upload in shown" :key="upload.id" class="flex items-center gap-3 px-4 py-2">
          <img
            :src="thumbnails.get(upload.id)"
            alt=""
            class="size-10 shrink-0 rounded object-cover"
            :class="{ 'opacity-50': upload.state === 'waiting' }"
            loading="lazy"
            decoding="async"
          />
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm">{{ upload.file.name }}</p>
            <progress
              v-if="upload.state === 'uploading'"
              class="progress progress-primary h-1 w-full"
              :value="Math.round(upload.share * 100)"
              max="100"
            ></progress>
            <p v-else-if="upload.state === 'failed'" class="text-xs text-error">{{ reason(upload) }}</p>
            <p v-else-if="upload.state === 'waiting'" class="text-xs text-base-content/60">{{ t('media.uploadWaiting') }}</p>
          </div>
          <AppIcon v-if="upload.state === 'done'" name="check" class="shrink-0 text-success" />
          <button
            v-else-if="upload.state === 'failed'"
            type="button"
            class="btn btn-ghost btn-xs shrink-0"
            @click="uploads.retry(upload)"
          >
            {{ t('media.uploadRetry') }}
          </button>
          <button
            v-else
            type="button"
            class="btn btn-ghost btn-square btn-xs shrink-0"
            :aria-label="t('media.uploadCancelOne', { name: upload.file.name })"
            @click="uploads.cancel(upload)"
          >
            <AppIcon name="close" class="size-4!" />
          </button>
        </li>
      </ul>
      <footer v-if="uploads.busy" class="flex justify-end border-t border-base-300 px-4 py-2">
        <button type="button" class="btn btn-ghost btn-xs" @click="uploads.cancelAll()">
          {{ t('media.uploadCancelAll') }}
        </button>
      </footer>
    </template>
  </section>
</template>

