<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { MAX_IDEA_DAYS } from '@/utils/ideas'

// How many days a trip takes, from the fewest to the most, chosen with two
// thumbs on one bar from one day to MAX_IDEA_DAYS. Two range inputs lie over
// each other and keep the keyboard and the screen reader of a slider each; a
// thumb never passes the other. A pointer is taken by a layer over the bar
// instead, which moves one thumb only: two inputs laid over each other would
// both follow a drag that starts where the thumbs meet.
const from = defineModel<number>('from', { required: true })
const to = defineModel<number>('to', { required: true })

defineProps<{
  /** What the days stand for, read out for each thumb. */
  label: string
}>()

const { t } = useI18n()

const MARKS = [1, 7, 14, 21, 30]
// share is where a number of days sits along the bar, as a fraction.
const share = (days: number): number => (days - 1) / (MAX_IDEA_DAYS - 1)
const shown = computed(() => (from.value === to.value
  ? t('ideas.days', { n: from.value }, from.value)
  : t('ideas.daysRange', { from: from.value, to: to.value })))

// setFrom and setTo move one thumb from the keyboard, stopping at the other.
function setFrom(event: Event): void {
  const input = event.target as HTMLInputElement
  from.value = Math.min(Number(input.value), to.value)
  input.value = String(from.value)
}
function setTo(event: Event): void {
  const input = event.target as HTMLInputElement
  to.value = Math.max(Number(input.value), from.value)
  input.value = String(to.value)
}

const track = useTemplateRef<HTMLElement>('track')
const fromInput = useTemplateRef<HTMLInputElement>('fromInput')
const toInput = useTemplateRef<HTMLInputElement>('toInput')
// dragging is the thumb a pointer holds; undecided while it presses where the
// two thumbs meet and has not moved yet.
const dragging = ref<'from' | 'to' | 'undecided' | null>(null)

// daysAt reads the number of days under a pointer, from where the thumbs'
// centres can reach: half a thumb in from either end of the bar.
function daysAt(clientX: number): number {
  const element = track.value
  if (!element) {
    return from.value
  }
  const rect = element.getBoundingClientRect()
  const thumb = parseFloat(getComputedStyle(element).getPropertyValue('--days-thumb')) * 16 || 20
  const fraction = (clientX - rect.left - thumb / 2) / Math.max(rect.width - thumb, 1)
  return Math.round(Math.min(Math.max(fraction, 0), 1) * (MAX_IDEA_DAYS - 1)) + 1
}

// moveTo puts the held thumb on a day, never past the other one.
function moveTo(days: number): void {
  if (dragging.value === 'from') {
    from.value = Math.min(days, to.value)
  } else if (dragging.value === 'to') {
    to.value = Math.max(days, from.value)
  }
}

// grab takes the thumb nearest the pointer. Where the thumbs meet, the first
// move decides: to the left it is the lower thumb, to the right the upper.
function grab(event: PointerEvent): void {
  const days = daysAt(event.clientX)
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  if (from.value === to.value) {
    dragging.value = days < from.value ? 'from' : days > to.value ? 'to' : 'undecided'
  } else {
    dragging.value = Math.abs(days - from.value) <= Math.abs(days - to.value) ? 'from' : 'to'
  }
  moveTo(days)
}

// drag follows the pointer with the held thumb.
function drag(event: PointerEvent): void {
  if (dragging.value === null) {
    return
  }
  const days = daysAt(event.clientX)
  if (dragging.value === 'undecided') {
    if (days === from.value) {
      return
    }
    dragging.value = days < from.value ? 'from' : 'to'
  }
  moveTo(days)
}

// release lets the thumb go and leaves the keyboard on it, so the arrows go on
// where the pointer stopped.
function release(): void {
  if (dragging.value === 'from') {
    fromInput.value?.focus()
  } else if (dragging.value === 'to') {
    toInput.value?.focus()
  }
  dragging.value = null
}
</script>

<template>
  <div class="days-range flex flex-col gap-1">
    <p class="text-center font-semibold tabular-nums" aria-hidden="true">{{ shown }}</p>
    <div ref="track" class="relative h-5">
      <div class="days-range-bar absolute inset-x-[calc(var(--days-thumb)/2)] top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-base-300">
        <div
          class="absolute inset-y-0 rounded-full bg-primary"
          :style="{ left: `${share(from) * 100}%`, right: `${(1 - share(to)) * 100}%` }"
        ></div>
      </div>
      <input
        ref="fromInput"
        type="range"
        class="days-range-input"
        min="1"
        :max="MAX_IDEA_DAYS"
        :value="from"
        :aria-label="`${label}: ${t('ideas.daysFrom')}`"
        @input="setFrom"
      />
      <input
        ref="toInput"
        type="range"
        class="days-range-input"
        min="1"
        :max="MAX_IDEA_DAYS"
        :value="to"
        :aria-label="`${label}: ${t('ideas.daysTo')}`"
        @input="setTo"
      />
      <!-- The layer a pointer drags with; the inputs under it keep the keyboard. -->
      <div
        class="absolute inset-0 cursor-pointer touch-none"
        aria-hidden="true"
        @pointerdown.prevent="grab"
        @pointermove="drag"
        @pointerup="release"
        @pointercancel="release"
      ></div>
    </div>
    <div class="relative h-4 text-xs text-base-content/60 tabular-nums" aria-hidden="true">
      <span
        v-for="mark in MARKS"
        :key="mark"
        class="absolute -translate-x-1/2"
        :style="{ left: `calc(var(--days-thumb) / 2 + (100% - var(--days-thumb)) * ${share(mark)})` }"
      >{{ mark }}</span>
    </div>
  </div>
</template>
