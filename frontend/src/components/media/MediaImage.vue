<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { mediaFilePath, mediaThumbnailPath, type MediaSize } from '@/api/media'
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
}>(), { alt: '', size: 320, width: 0, height: 0, square: false, fill: false })

const base = useMediaBase()
const root = useTemplateRef<HTMLElement>('root')
const visible = useVisible(root)

const path = computed(() => {
  if (!visible.value || !props.id) {
    return null
  }
  return props.size === null
    ? mediaFilePath(base, props.id)
    : mediaThumbnailPath(base, props.id, props.size)
})
// The picture's own place on the page orders it in the loading queue, so a
// gallery fills from the first tile to the last.
const { url, loading } = useMediaUrl(path, { element: root })

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
    :class="{ 'size-full': fill }"
    :style="{ aspectRatio: ratio }"
  >
    <img
      v-if="url"
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
