<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { mediaFilePath, mediaThumbnailPath, type MediaSize } from '@/api/media'
import type { CoverCrop } from '@/api/types'
import { useMediaBase, useMediaUrl } from '@/composables/useMediaUrl'
import { useVisible } from '@/composables/useVisible'

// One stored picture. It is fetched only once it is about to be seen, and shown
// through an object URL, because the file itself needs a token in a header.
//
// The picture is named by its identifier rather than by a whole record: a cover
// is all a trip or a day keeps of it, and that is enough to show it.
const props = withDefaults(defineProps<{
  id: string
  alt?: string
  /** The preview width to fetch; the whole file when it is null. */
  size?: MediaSize | null
  /** The picture's own proportions, which shape the tile before it loads. */
  width?: number
  height?: number
  /** Crop to a square, so a row of tiles lines up whatever shape each picture is. */
  square?: boolean
  /** Fill the box the parent gives, cropping whatever does not fit. */
  fill?: boolean
  /**
   * The part of the picture a filled box shows, chosen for a box of the same
   * proportions; the middle of the picture when it is not given.
   */
  crop?: CoverCrop | null
}>(), { alt: '', size: 320, width: 0, height: 0, square: false, fill: false, crop: null })

const base = useMediaBase()
const root = useTemplateRef<HTMLElement>('root')
// The picture is wanted while it is near the screen. One scrolled past before
// its turn in the loading queue comes leaves the queue, so a reader flicking
// through a long gallery gets what is in front of them first rather than
// everything they passed; once it has arrived it stays.
const near = useVisible(root, { once: false })
const arrived = ref(false)

const path = computed(() => {
  if ((!near.value && !arrived.value) || !props.id) {
    return null
  }
  return props.size === null
    ? mediaFilePath(base, props.id)
    : mediaThumbnailPath(base, props.id, props.size)
})
// The picture's own place on the page orders it in the loading queue, so a
// gallery fills from the first tile to the last.
const { url, loading } = useMediaUrl(path, { element: root })
watch(url, (shown) => {
  if (shown) {
    arrived.value = true
  }
})
// Another picture or size is a new load, which waits for the screen again.
watch(() => [props.id, props.size], () => {
  arrived.value = false
})

// A framed picture is stretched so that its frame covers the box exactly and
// moved so the frame's corner sits on the box's. The frame was chosen for a box
// of these proportions, so nothing is distorted.
const cropStyle = computed(() => {
  const crop = props.fill ? props.crop : null
  if (!crop || crop.w <= 0 || crop.h <= 0) {
    return undefined
  }
  return {
    width: `${100 / crop.w}%`,
    height: `${100 / crop.h}%`,
    left: `${(-crop.x / crop.w) * 100}%`,
    top: `${(-crop.y / crop.h) * 100}%`,
  }
})

// A tile keeps its shape before its picture arrives, so a gallery does not jump
// about as the pictures load: the parent's box where the picture fills it, a
// square where tiles line up, and the picture's own proportions otherwise.
const ratio = computed(() => {
  if (props.fill) {
    return undefined
  }
  if (props.square || props.width <= 0 || props.height <= 0) {
    return '1 / 1'
  }
  return `${props.width} / ${props.height}`
})
</script>

<template>
  <span
    ref="root"
    class="block max-w-full overflow-hidden bg-base-200"
    :class="{ 'size-full': fill, relative: !!cropStyle }"
    :style="{ aspectRatio: ratio }"
  >
    <img
      v-if="url && cropStyle"
      :src="url"
      :alt="alt"
      class="absolute max-w-none"
      :style="cropStyle"
    />
    <img
      v-else-if="url"
      :src="url"
      :alt="alt"
      class="size-full"
      :class="square || fill ? 'object-cover' : 'object-contain'"
      :width="width || undefined"
      :height="height || undefined"
    />
    <span v-else class="flex size-full items-center justify-center">
      <span v-if="loading" class="loading loading-spinner loading-sm opacity-60"></span>
    </span>
  </span>
</template>
