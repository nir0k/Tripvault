<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { mediaThumbnailPath } from '@/api/media'
import type { CoverCrop } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { useMediaBase, useMediaUrl } from '@/composables/useMediaUrl'

// Choosing which part of a photograph a trip is shown by. A cover is a wide
// strip and a photograph is not, so the strip is dragged and scaled over the
// picture here rather than always cut from its middle.
//
// Nothing is cut: what comes out is a frame, as fractions of the picture, and
// the whole photograph stays in the gallery. The frame has the proportions of
// the cover on a trip's card (aspect-[5/2] there), so what is chosen here is
// exactly what the card shows.

/** COVER_ASPECT is the width of a cover over its height. */
const COVER_ASPECT = 5 / 2

/** ZOOM_MAX is how far in the picture can be pushed. */
const ZOOM_MAX = 4

/** WHEEL_STEP is how much one notch of a mouse wheel zooms. */
const WHEEL_STEP = 0.0015

const emit = defineEmits<{
  save: [mediaId: string, crop: CoverCrop]
}>()

const { t } = useI18n()
const base = useMediaBase()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const frame = useTemplateRef<HTMLElement>('frame')

const mediaId = ref<string | null>(null)
// A wide preview is enough to choose by, and it is already upright, which is
// what the frame is measured on.
const path = computed(() => (mediaId.value ? mediaThumbnailPath(base, mediaId.value, 1280) : null))
const { url, loading } = useMediaUrl(path, { priority: 'high' })

/** The picture's own size, known once it has loaded. */
const natural = ref({ width: 0, height: 0 })
/** The frame's width on the screen, following the window. */
const frameWidth = ref(0)
const frameHeight = computed(() => frameWidth.value / COVER_ASPECT)

// The state is kept in the picture's own terms - the point under the middle of
// the frame, as fractions, and a zoom - so it survives the window being resized.
const zoom = ref(1)
const centre = ref({ x: 0.5, y: 0.5 })
// A frame to start from, applied once the picture's size is known.
let pending: CoverCrop | null = null

// baseScale fits the picture so that it just covers the frame: at a zoom of one
// the frame is as large as the picture allows.
const baseScale = computed(() => {
  const { width, height } = natural.value
  if (width <= 0 || height <= 0 || frameWidth.value <= 0) {
    return 0
  }
  return Math.max(frameWidth.value / width, frameHeight.value / height)
})
const scale = computed(() => baseScale.value * zoom.value)

// span is how much of the picture the frame covers, as fractions of its sides.
const span = computed(() => {
  const { width, height } = natural.value
  if (scale.value <= 0) {
    return { w: 1, h: 1 }
  }
  return {
    w: Math.min(1, frameWidth.value / (width * scale.value)),
    h: Math.min(1, frameHeight.value / (height * scale.value)),
  }
})

// pictureStyle places the picture under the frame so that the chosen point is
// in the middle of it.
const pictureStyle = computed(() => {
  const width = natural.value.width * scale.value
  const height = natural.value.height * scale.value
  return {
    width: `${width}px`,
    height: `${height}px`,
    left: `${frameWidth.value / 2 - centre.value.x * width}px`,
    top: `${frameHeight.value / 2 - centre.value.y * height}px`,
  }
})

// clampCentre keeps the frame inside the picture: a cover with a band of empty
// canvas along one edge is a mistake, not a choice.
function clampCentre(point: { x: number; y: number }): { x: number; y: number } {
  const halfW = span.value.w / 2
  const halfH = span.value.h / 2
  return {
    x: Math.min(Math.max(point.x, halfW), 1 - halfW),
    y: Math.min(Math.max(point.y, halfH), 1 - halfH),
  }
}

// applyPending turns a frame chosen before into a zoom and a centre, once both
// the picture and the frame on the screen have a size.
function applyPending(): void {
  const crop = pending
  if (!crop || baseScale.value <= 0) {
    return
  }
  pending = null
  const wanted = frameWidth.value / (crop.w * natural.value.width)
  zoom.value = Math.min(Math.max(wanted / baseScale.value, 1), ZOOM_MAX)
  centre.value = clampCentre({ x: crop.x + crop.w / 2, y: crop.y + crop.h / 2 })
}

// onLoad records the picture's size, which every measure above waits for.
function onLoad(event: Event): void {
  const image = event.target as HTMLImageElement
  natural.value = { width: image.naturalWidth, height: image.naturalHeight }
  applyPending()
  centre.value = clampCentre(centre.value)
}

// The frame follows the width of the window, so it is measured while it shows.
let observer: ResizeObserver | null = null
watch(frame, (element) => {
  observer?.disconnect()
  observer = null
  if (!element) {
    return
  }
  observer = new ResizeObserver(() => {
    frameWidth.value = element.clientWidth
    applyPending()
    centre.value = clampCentre(centre.value)
  })
  observer.observe(element)
})
onBeforeUnmount(() => observer?.disconnect())

/**
 * open shows a picture with the frame it is shown by, or its middle when it has
 * none yet.
 */
function open(id: string, crop: CoverCrop | null = null): void {
  if (mediaId.value !== id) {
    natural.value = { width: 0, height: 0 }
  }
  mediaId.value = id
  zoom.value = 1
  centre.value = { x: 0.5, y: 0.5 }
  pending = crop
  applyPending()
  dialog.value?.showModal()
}

// close puts the window away.
function close(): void {
  dialog.value?.close()
}

// setZoom scales the picture around the middle of the frame, keeping the frame
// inside it.
function setZoom(value: number): void {
  zoom.value = Math.min(Math.max(value, 1), ZOOM_MAX)
  centre.value = clampCentre(centre.value)
}

// onWheel zooms with a mouse wheel or a touchpad's pinch.
function onWheel(event: WheelEvent): void {
  setZoom(zoom.value * (1 - event.deltaY * WHEEL_STEP))
}

// onPointerDown drags the picture under the frame, following the pointer until
// it is released anywhere on the page.
function onPointerDown(event: PointerEvent): void {
  if (scale.value <= 0) {
    return
  }
  const start = { pointerX: event.clientX, pointerY: event.clientY, x: centre.value.x, y: centre.value.y }
  const width = natural.value.width * scale.value
  const height = natural.value.height * scale.value
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture(event.pointerId)

  const move = (moved: PointerEvent): void => {
    // Dragging the picture right shows more of its left, so the centre moves
    // against the pointer.
    centre.value = clampCentre({
      x: start.x - (moved.clientX - start.pointerX) / width,
      y: start.y - (moved.clientY - start.pointerY) / height,
    })
  }
  const up = (): void => {
    target.removeEventListener('pointermove', move)
    target.removeEventListener('pointerup', up)
    target.removeEventListener('pointercancel', up)
  }
  target.addEventListener('pointermove', move)
  target.addEventListener('pointerup', up)
  target.addEventListener('pointercancel', up)
}

// save hands the frame over as fractions of the picture and closes.
function save(): void {
  if (!mediaId.value || scale.value <= 0) {
    return
  }
  const { w, h } = span.value
  const x = Math.min(Math.max(centre.value.x - w / 2, 0), 1 - w)
  const y = Math.min(Math.max(centre.value.y - h / 2, 0), 1 - h)
  emit('save', mediaId.value, { x, y, w, h })
  close()
}

defineExpose({ open, close })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <div class="modal-box flex w-[min(96vw,44rem)] max-w-none flex-col gap-3">
      <h2 class="text-lg font-bold">{{ t('media.cropTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('media.cropHint') }}</p>

      <div
        ref="frame"
        class="relative aspect-[5/2] w-full cursor-grab touch-none overflow-hidden rounded-box border border-base-300 bg-base-200 active:cursor-grabbing"
        @pointerdown="onPointerDown"
        @wheel.prevent="onWheel"
      >
        <img
          v-if="url"
          :src="url"
          alt=""
          class="pointer-events-none absolute max-w-none select-none"
          :class="{ invisible: scale <= 0 }"
          :style="pictureStyle"
          draggable="false"
          @load="onLoad"
        />
        <span v-if="loading || scale <= 0" class="absolute inset-0 flex items-center justify-center">
          <span class="loading loading-spinner loading-sm opacity-60"></span>
        </span>
      </div>

      <label class="flex items-center gap-3 text-sm">
        <AppIcon name="search" class="size-4! opacity-70" />
        <input
          :value="zoom"
          type="range"
          min="1"
          :max="ZOOM_MAX"
          step="0.01"
          class="range range-sm flex-1"
          :aria-label="t('media.cropZoom')"
          @input="setZoom(Number(($event.target as HTMLInputElement).value))"
        />
      </label>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="scale <= 0" @click="save">
          {{ t('common.save') }}
        </button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
