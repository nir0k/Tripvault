<script setup lang="ts" generic="T">
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { justifyRows, justifyStrip, previewSize, tileRatio, type JustifiedRow } from '@/utils/justify'
import type { MediaSize } from '@/api/media'

// Pictures in rows, each in its own proportions, every row but the last as
// wide as the page - the way Google Photos and Immich lay a gallery out. The
// grid only places the tiles: what a tile holds, and what it does, is the
// parent's, handed each item with the size its tile was given and the preview
// that covers it.
//
// A strip is a single row of the first items, as many as fit, ending with a
// tile that stands for the rest when some are left out; that tile is the
// parent's too, handed the first item left out so it can show its picture.
const props = withDefaults(defineProps<{
  items: T[]
  /** A stable key for an item. */
  itemKey: (item: T) => string
  /** The picture's size in pixels, which gives its tile its proportions. */
  dimensions: (item: T) => { width: number; height: number }
  /** The row height aimed for on a wide screen; narrower screens use less. */
  rowHeight?: number
  /** Lay out a single row of the first items instead of all of them. */
  strip?: boolean
  /** The most items a strip lays out. */
  stripLimit?: number
}>(), { rowHeight: 240, strip: false, stripLimit: Infinity })

defineSlots<{
  default(props: { item: T; index: number; width: number; height: number; size: MediaSize }): unknown
  more(props: { item: T; hidden: number; width: number; height: number; size: MediaSize }): unknown
}>()

const root = useTemplateRef<HTMLElement>('root')
const width = ref(0)
let observer: ResizeObserver | null = null

// A phone shows half the height and a narrower gap, a tablet three quarters,
// so a row still holds a few pictures however narrow the page is.
const wide = useMediaQuery('(min-width: 1024px)')
const medium = useMediaQuery('(min-width: 640px)')
const target = computed(() => {
  if (wide.value) {
    return props.rowHeight
  }
  return Math.round(props.rowHeight * (medium.value ? 0.75 : 0.5))
})
const gap = computed(() => (medium.value ? 4 : 2))
const density = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1

// shown is how many items are laid out, from the first; the tile after them,
// when there is one, stands for the rest.
const layout = computed<{ rows: JustifiedRow[]; shown: number }>(() => {
  const ratios = props.items.map((item) => {
    const size = props.dimensions(item)
    return tileRatio(size.width, size.height)
  })
  if (!props.strip) {
    return { rows: justifyRows(ratios, width.value, target.value, gap.value), shown: ratios.length }
  }
  const strip = justifyStrip(ratios, width.value, target.value, gap.value, props.stripLimit, 1)
  return strip ? { rows: [strip.row], shown: strip.shown } : { rows: [], shown: 0 }
})
const rows = computed(() => layout.value.rows)

onMounted(() => {
  if (!root.value) {
    return
  }
  width.value = root.value.clientWidth
  observer = new ResizeObserver((entries) => {
    // Whole pixels, so a sub-pixel wobble of the page does not lay it all out again.
    width.value = Math.floor(entries[0]?.contentRect.width ?? width.value)
  })
  observer.observe(root.value)
})

onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="root" role="list" class="flex flex-col" :style="{ gap: `${gap}px` }">
    <div v-for="(row, rowIndex) in rows" :key="rowIndex" class="flex" :style="{ gap: `${gap}px` }">
      <div
        v-for="tile in row.tiles"
        :key="tile.index < layout.shown ? itemKey(items[tile.index]!) : 'more'"
        role="listitem"
        class="relative shrink-0"
        :style="{ width: `${tile.width}px`, height: `${tile.height}px` }"
      >
        <slot
          v-if="tile.index < layout.shown"
          :item="items[tile.index]!"
          :index="tile.index"
          :width="tile.width"
          :height="tile.height"
          :size="previewSize(tile.width, density)"
        />
        <slot
          v-else
          name="more"
          :item="items[tile.index]!"
          :hidden="items.length - layout.shown"
          :width="tile.width"
          :height="tile.height"
          :size="previewSize(tile.width, density)"
        />
      </div>
    </div>
  </div>
</template>
