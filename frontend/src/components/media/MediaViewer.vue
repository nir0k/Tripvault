<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MEDIA_SIZES, mediaThumbnailPath, type MediaSize } from '@/api/media'
import type { Media } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { prefetchMedia, useMediaBase, useMediaUrl } from '@/composables/useMediaUrl'
import { formatDateTime } from '@/utils/format'

// A photograph across nearly the whole window, with the rest of its gallery a
// key away. The picture is fetched as a preview rather than the original file:
// what a screen shows is a preview, and the file itself can be tens of
// megabytes of camera output nobody asked to download. The preview is the
// narrowest one that still fills the screen it is shown on.
const props = defineProps<{ items: Media[] }>()

const { t, locale } = useI18n()
const base = useMediaBase()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
// The frame rather than the dialog goes full screen: a browser refuses a
// <dialog> itself, and the frame is what holds the picture anyway.
const frame = useTemplateRef<HTMLElement>('frame')
// The stage keeps its size whatever picture it holds, so the window does not
// shrink while the next one loads nor follow each picture's proportions.
const stage = useTemplateRef<HTMLElement>('stage')
const index = ref(0)
const fullscreen = ref(false)
const stageWidth = ref(0)
const stageHeight = ref(0)

const current = computed<Media | null>(() => props.items[index.value] ?? null)
const taken = computed(() => (current.value?.taken_at
  ? formatDateTime(current.value.taken_at, locale.value)
  : ''))

// previewSize picks the narrowest preview that covers the pixels a picture
// takes on the stage, counting the screen's density, and never a wider one
// than the photograph itself has. Nothing is chosen while the stage is hidden,
// so a closed viewer fetches nothing.
function previewSize(item: Media): MediaSize | null {
  if (stageWidth.value <= 0 || stageHeight.value <= 0) {
    return null
  }
  let shown = stageWidth.value
  if (item.width > 0 && item.height > 0) {
    shown = Math.min(shown, stageHeight.value * item.width / item.height)
  }
  let wanted = shown * (window.devicePixelRatio || 1)
  if (item.width > 0) {
    wanted = Math.min(wanted, item.width)
  }
  return MEDIA_SIZES.find((size) => size >= wanted) ?? 1920
}

// previewPath is the address of the preview of an item on the current stage.
function previewPath(item: Media | undefined | null): string | null {
  const size = item ? previewSize(item) : null
  return item && size ? mediaThumbnailPath(base, item.id, size) : null
}

// The picture the reader opened goes ahead of every tile still loading behind
// the dialog.
const { url, loading } = useMediaUrl(computed(() => previewPath(current.value)), { priority: 'high' })

// The pictures either side of the one shown are fetched once it has arrived,
// at the back of the loading queue, so a step through the gallery finds the
// next one in the browser's cache rather than waiting for it. Each address is
// asked for once per viewer.
const prefetched = new Set<string>()
watch(url, (shown) => {
  const count = props.items.length
  if (!shown || count < 2) {
    return
  }
  for (const by of [1, -1]) {
    const path = previewPath(props.items[(index.value + by + count) % count])
    if (path && !prefetched.has(path)) {
      prefetched.add(path)
      prefetchMedia(path)
    }
  }
})

/** open shows the gallery starting at one of its pictures. */
function open(at: number): void {
  index.value = Math.min(Math.max(at, 0), Math.max(props.items.length - 1, 0))
  dialog.value?.showModal()
}

/** close hides the viewer, leaving full screen behind if it was entered. */
function close(): void {
  if (window.document.fullscreenElement) {
    void window.document.exitFullscreen().catch(() => {})
  }
  dialog.value?.close()
}

// toggleFullscreen hands the whole screen to the picture, and takes it back.
// A photograph is worth the room, and a modal dialog is the element the browser
// can be asked to enlarge.
async function toggleFullscreen(): Promise<void> {
  try {
    if (window.document.fullscreenElement) {
      await window.document.exitFullscreen()
    } else {
      await frame.value?.requestFullscreen()
    }
  } catch {
    // A browser that refuses full screen simply keeps the dialog as it is.
  }
}

// onFullscreenChange follows the state, including a reader leaving full screen
// with Escape or the browser's own control.
function onFullscreenChange(): void {
  fullscreen.value = window.document.fullscreenElement === frame.value
}

// The stage is measured rather than assumed, because its size depends on the
// window, on full screen and on the header, and it is zero while the dialog is
// closed.
const resizes = new ResizeObserver(([entry]) => {
  stageWidth.value = entry?.contentRect.width ?? 0
  stageHeight.value = entry?.contentRect.height ?? 0
})

onMounted(() => {
  window.document.addEventListener('fullscreenchange', onFullscreenChange)
  if (stage.value) {
    resizes.observe(stage.value)
  }
})
onBeforeUnmount(() => {
  window.document.removeEventListener('fullscreenchange', onFullscreenChange)
  resizes.disconnect()
})

// step walks the gallery, wrapping at both ends so the keys never dead-end.
function step(by: number): void {
  const count = props.items.length
  if (count > 0) {
    index.value = (index.value + by + count) % count
  }
}

defineExpose({ open, close })
</script>

<template>
  <dialog
    ref="dialog"
    class="modal backdrop-blur-md"
    @keydown.left.prevent="step(-1)"
    @keydown.right.prevent="step(1)"
  >
    <div
      ref="frame"
      class="modal-box flex max-h-none max-w-none flex-col bg-base-100 p-0"
      :class="fullscreen ? 'h-dvh w-screen rounded-none' : 'h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)]'"
    >
      <header class="flex items-center justify-between gap-2 border-b border-base-300 px-4 py-2">
        <p class="min-w-0 truncate text-sm">
          <span v-if="taken" class="opacity-70">{{ taken }}</span>
          <AppIcon
            v-if="current?.is_private"
            name="lockFill"
            class="ms-2 inline size-4! align-text-bottom text-amber-400"
            role="img"
            :aria-label="t('media.private')"
          />
        </p>
        <div class="flex items-center gap-1">
          <span class="text-sm opacity-70">{{ index + 1 }} / {{ items.length }}</span>
          <button
            type="button"
            class="btn btn-ghost btn-sm btn-square"
            :aria-label="fullscreen ? t('media.exitFullscreen') : t('media.fullscreen')"
            :title="fullscreen ? t('media.exitFullscreen') : t('media.fullscreen')"
            @click="toggleFullscreen"
          >
            <AppIcon :name="fullscreen ? 'collapse' : 'expand'" />
          </button>
          <button type="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('common.close')" @click="close">
            <AppIcon name="close" />
          </button>
        </div>
      </header>

      <div ref="stage" class="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden bg-base-200">
        <img
          v-if="url && current"
          :src="url"
          :alt="current.original_name"
          class="max-h-full max-w-full object-contain"
        />
        <span v-else-if="loading" class="loading loading-spinner loading-lg opacity-70"></span>
        <template v-if="items.length > 1">
          <button
            type="button"
            class="btn btn-circle btn-sm absolute start-2 top-1/2 -translate-y-1/2"
            :aria-label="t('media.previous')"
            @click="step(-1)"
          >
            <AppIcon name="chevronLeft" />
          </button>
          <button
            type="button"
            class="btn btn-circle btn-sm absolute end-2 top-1/2 -translate-y-1/2"
            :aria-label="t('media.next')"
            @click="step(1)"
          >
            <AppIcon name="chevronRight" />
          </button>
        </template>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
