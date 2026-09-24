<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'

// Choosing the picture an account is shown by, and choosing what of it is seen:
// an avatar is a circle, and a photograph is not, so the part that lands inside
// the circle is dragged and scaled here rather than guessed by the service.
//
// The crop happens in the browser, on a canvas, and what is sent is the square
// the reader chose - not the whole photograph with instructions. The server
// renders that square once more to the size it keeps, so nothing arrives with a
// camera's metadata still attached.

/** VIEWPORT is the side of the circle the picture is fitted into, in pixels. */
const VIEWPORT = 256

/** EXPORT_SIZE is the side of the square that is sent, in pixels. */
const EXPORT_SIZE = 512

/** JPEG_QUALITY is how hard the square is compressed on the way out. */
const JPEG_QUALITY = 0.9

/** ZOOM_MAX is how far in the picture can be pushed. */
const ZOOM_MAX = 4

const emit = defineEmits<{
  save: [picture: Blob]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')

const source = ref<HTMLImageElement | null>(null)
const zoom = ref(1)
const offset = ref({ x: 0, y: 0 })
const busy = ref(false)
const error = ref('')

// baseScale fits the shorter side of the picture to the circle, which is where
// every crop starts: the whole of that side is in view, and zooming goes in.
const baseScale = computed(() => {
  const image = source.value
  if (!image) {
    return 1
  }
  return VIEWPORT / Math.min(image.naturalWidth, image.naturalHeight)
})

const scale = computed(() => baseScale.value * zoom.value)

// drawn is the size the picture is shown at, which is what the offsets are
// bounded by: the circle must not run off the edge of the photograph.
const drawn = computed(() => {
  const image = source.value
  return {
    width: (image?.naturalWidth ?? 0) * scale.value,
    height: (image?.naturalHeight ?? 0) * scale.value,
  }
})

// The address the chosen file is shown through. It owns memory until it is
// released, and the preview reads it, so it is let go only when another file is
// chosen or the window is done with.
let objectUrl: string | null = null

// release frees the memory the preview holds.
function release(): void {
  if (objectUrl) {
    URL.revokeObjectURL(objectUrl)
    objectUrl = null
  }
}

/** open asks for a picture, starting from nothing chosen. */
function open(): void {
  release()
  source.value = null
  zoom.value = 1
  offset.value = { x: 0, y: 0 }
  error.value = ''
  dialog.value?.showModal()
}

// close puts the window away and frees the picture it was showing.
function close(): void {
  dialog.value?.close()
  release()
  source.value = null
}

// onFile reads the chosen file into a picture the canvas can draw, and says so
// when the file turns out not to be one.
function onFile(event: Event): void {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) {
    return
  }
  error.value = ''
  release()
  objectUrl = URL.createObjectURL(file)
  const image = new Image()
  image.onload = () => {
    source.value = image
    zoom.value = 1
    offset.value = { x: 0, y: 0 }
  }
  image.onerror = () => {
    release()
    source.value = null
    error.value = t('profile.avatarNotAPicture')
  }
  image.src = objectUrl
}

// clamp keeps the circle inside the picture: an avatar with a corner of empty
// canvas in it is a mistake, not a choice.
function clamp(value: number, size: number): number {
  const room = Math.max((size - VIEWPORT) / 2, 0)
  return Math.min(Math.max(value, -room), room)
}

// onZoom re-clamps the offsets after a scale change, because zooming out leaves
// less room to move in.
function onZoom(): void {
  offset.value = {
    x: clamp(offset.value.x, drawn.value.width),
    y: clamp(offset.value.y, drawn.value.height),
  }
}

// onPointerDown drags the picture under the circle, following the pointer until
// it is released anywhere on the page.
function onPointerDown(event: PointerEvent): void {
  if (!source.value) {
    return
  }
  const start = {
    pointerX: event.clientX,
    pointerY: event.clientY,
    x: offset.value.x,
    y: offset.value.y,
  }
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture(event.pointerId)

  const move = (moved: PointerEvent): void => {
    offset.value = {
      x: clamp(start.x + (moved.clientX - start.pointerX), drawn.value.width),
      y: clamp(start.y + (moved.clientY - start.pointerY), drawn.value.height),
    }
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

// save renders what the circle shows onto a square canvas and hands it over.
async function save(): Promise<void> {
  const image = source.value
  if (!image) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    const canvas = document.createElement('canvas')
    canvas.width = EXPORT_SIZE
    canvas.height = EXPORT_SIZE
    const context = canvas.getContext('2d')
    if (!context) {
      throw new Error('no canvas')
    }
    // The part of the photograph the circle stands over, in its own pixels.
    const side = VIEWPORT / scale.value
    const left = (image.naturalWidth - side) / 2 - offset.value.x / scale.value
    const top = (image.naturalHeight - side) / 2 - offset.value.y / scale.value
    context.drawImage(image, left, top, side, side, 0, 0, EXPORT_SIZE, EXPORT_SIZE)

    const picture = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob(resolve, 'image/jpeg', JPEG_QUALITY)
    })
    if (!picture) {
      throw new Error('no picture')
    }
    emit('save', picture)
    close()
  } catch {
    error.value = t('profile.avatarFailed')
  } finally {
    busy.value = false
  }
}

onBeforeUnmount(release)

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <div class="modal-box flex flex-col gap-3">
      <h2 class="text-lg font-bold">{{ t('profile.avatarTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('profile.avatarHint') }}</p>

      <input
        type="file"
        accept="image/jpeg,image/png,image/webp"
        class="file-input file-input-sm w-full"
        :aria-label="t('profile.avatarChoose')"
        @change="onFile"
      />

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

      <template v-if="source">
        <div class="flex justify-center">
          <div
            class="relative touch-none overflow-hidden rounded-full border border-base-300 bg-base-200"
            :style="{ width: `${VIEWPORT}px`, height: `${VIEWPORT}px` }"
            @pointerdown="onPointerDown"
          >
            <img
              :src="source.src"
              alt=""
              class="pointer-events-none absolute top-1/2 left-1/2 max-w-none select-none"
              :style="{
                width: `${drawn.width}px`,
                height: `${drawn.height}px`,
                transform: `translate(calc(-50% + ${offset.x}px), calc(-50% + ${offset.y}px))`,
              }"
            />
          </div>
        </div>
        <label class="flex items-center gap-3 text-sm">
          <AppIcon name="search" class="size-4! opacity-70" />
          <input
            v-model.number="zoom"
            type="range"
            min="1"
            :max="ZOOM_MAX"
            step="0.01"
            class="range range-sm flex-1"
            :aria-label="t('profile.avatarZoom')"
            @input="onZoom"
          />
        </label>
      </template>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!source || busy" @click="save">
          {{ t('common.save') }}
        </button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
