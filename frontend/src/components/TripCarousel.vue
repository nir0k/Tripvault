<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TripCard from '@/components/TripCard.vue'

// A row of trip cards that scrolls sideways: swiped on a phone, stepped a
// screen at a time by the arrows on a wider window. The arrows show only while
// there is somewhere to go, so a row that fits shows none.
const props = defineProps<{
  trips: Trip[]
  /** Names the row for assistive technology. */
  label: string
  loading?: boolean
}>()

const { t } = useI18n()

const track = useTemplateRef<HTMLElement>('track')
const canBack = ref(false)
const canForward = ref(false)

// measure reads whether the row can scroll either way. A pixel of slack keeps
// fractional widths from leaving an arrow that moves nothing.
function measure(): void {
  const el = track.value
  if (!el) {
    return
  }
  canBack.value = el.scrollLeft > 1
  canForward.value = el.scrollLeft + el.clientWidth < el.scrollWidth - 1
}

// step scrolls by most of the visible width, so the card cut at the edge
// becomes the first one whole.
function step(direction: 1 | -1): void {
  const el = track.value
  if (el) {
    el.scrollBy({ left: direction * el.clientWidth * 0.9, behavior: 'smooth' })
  }
}

let observer: ResizeObserver | undefined

onMounted(() => {
  observer = new ResizeObserver(measure)
  if (track.value) {
    observer.observe(track.value)
  }
  measure()
})
onBeforeUnmount(() => observer?.disconnect())

watch(() => props.trips, () => void nextTick(measure))
</script>

<template>
  <div class="relative">
    <div
      ref="track"
      role="region"
      :aria-label="label"
      tabindex="0"
      class="flex snap-x snap-mandatory gap-4 overflow-x-auto scroll-smooth pb-2 focus-visible:outline-2 focus-visible:outline-primary"
      @scroll.passive="measure"
    >
      <template v-if="loading && trips.length === 0">
        <div v-for="n in 3" :key="n" class="skeleton h-56 w-72 shrink-0"></div>
      </template>
      <TripCard v-for="trip in trips" :key="trip.id" :trip="trip" class="w-72 shrink-0 snap-start" />
    </div>

    <button
      v-if="canBack"
      type="button"
      class="btn btn-circle btn-sm absolute top-1/2 left-0 hidden -translate-x-1/2 -translate-y-1/2 shadow-md sm:flex"
      :aria-label="t('home.previous')"
      @click="step(-1)"
    >
      <AppIcon name="chevronLeft" />
    </button>
    <button
      v-if="canForward"
      type="button"
      class="btn btn-circle btn-sm absolute top-1/2 right-0 hidden translate-x-1/2 -translate-y-1/2 shadow-md sm:flex"
      :aria-label="t('home.next')"
      @click="step(1)"
    >
      <AppIcon name="chevronRight" />
    </button>
  </div>
</template>
