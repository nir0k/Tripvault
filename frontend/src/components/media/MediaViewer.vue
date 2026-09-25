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

// How far a finger has to carry the picture before letting go turns the page;
// a shorter drag springs back.
const SWIPE_THRESHOLD = 60

// A swipe is followed with the picture under the finger. The gesture belongs to
// one pointer: a second finger means a pinch, which the browser zooms instead.
const dragX = ref(0)
const dragging = ref(false)
let swipePointer: number | null = null
let swipeStartX = 0
let swipeStartY = 0
// A swipe that ends over one of the edge zones would have its release taken for
// a tap and turn the page a second time, so clicks are ignored for a moment.
let swallowClicksUntil = 0

// onPointerDown starts following a finger; a mouse clicks the zones instead.
function onPointerDown(event: PointerEvent): void {
  if (event.pointerType === 'mouse' || props.items.length < 2) {
    return
  }
  if (swipePointer !== null) {
    cancelSwipe()
    return
  }
  swipePointer = event.pointerId
  swipeStartX = event.clientX
  swipeStartY = event.clientY
}

// onPointerMove carries the picture once the finger moves more sideways than
// up or down; a vertical move is left to the page.
function onPointerMove(event: PointerEvent): void {
  if (event.pointerId !== swipePointer) {
    return
  }
  const dx = event.clientX - swipeStartX
  const dy = event.clientY - swipeStartY
  if (!dragging.value) {
    if (Math.abs(dx) < 10 || Math.abs(dx) <= Math.abs(dy)) {
      return
    }
    dragging.value = true
  }
  dragX.value = dx
}

// onPointerUp turns the page when the picture was carried far enough.
function onPointerUp(event: PointerEvent): void {
  if (event.pointerId !== swipePointer) {
    return
  }
  const dx = dragX.value
  const swiped = dragging.value
  cancelSwipe()
  if (swiped) {
    swallowClicksUntil = performance.now() + 400
    if (Math.abs(dx) >= SWIPE_THRESHOLD) {
      step(dx < 0 ? 1 : -1)
    }
  }
}

// cancelSwipe lets go of the gesture and puts the picture back.
function cancelSwipe(): void {
  swipePointer = null
  dragging.value = false
  dragX.value = 0
}

// tap turns the page from an edge zone, unless the tap is the end of a swipe
// that has already done so.
function tap(by: number): void {
  if (performance.now() < swallowClicksUntil) {
    return
  }
  step(by)
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

      <!-- A finger swipes the picture sideways; up and down, and a pinch, stay
           the browser's. -->
      <div
        ref="stage"
        class="relative flex min-h-0 flex-1 touch-pan-y touch-pinch-zoom items-center justify-center overflow-hidden bg-base-200 select-none"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointercancel="cancelSwipe"
      >
        <img
          v-if="url && current"
          :src="url"
          :alt="current.original_name"
          class="max-h-full max-w-full object-contain"
          :class="{ 'transition-transform duration-200': !dragging }"
          :style="{ transform: dragX ? `translateX(${dragX}px)` : undefined }"
          draggable="false"
        />
        <span v-else-if="loading" class="loading loading-spinner loading-lg opacity-70"></span>
        <!-- Each outer third of the stage turns the page, so a tap anywhere near
             an edge does it rather than only on the small round button there. -->
        <template v-if="items.length > 1">
          <button
            type="button"
            class="group absolute inset-y-0 start-0 flex w-1/3 items-center justify-start ps-2"
            :aria-label="t('media.previous')"
            @click="tap(-1)"
          >
            <span class="btn btn-circle btn-sm pointer-events-none opacity-70 group-hover:opacity-100">
              <AppIcon name="chevronLeft" />
            </span>
          </button>
          <button
            type="button"
            class="group absolute inset-y-0 end-0 flex w-1/3 items-center justify-end pe-2"
            :aria-label="t('media.next')"
            @click="tap(1)"
          >
            <span class="btn btn-circle btn-sm pointer-events-none opacity-70 group-hover:opacity-100">
              <AppIcon name="chevronRight" />
            </span>
          </button>
        </template>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
