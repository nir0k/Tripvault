<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatSpeed } from '@/utils/format'
import { MAX_SPEED, MIN_SPEED, SLIDER_STEPS, SPEED_STOPS, clampSpeed, positionOf, speedAt, stopPosition } from '@/utils/trackTime'
import { activeUnits } from '@/utils/units'

// The speed a line is walked at on the flat, chosen on a scale that runs from
// green to red. The marks are the paces people walk at, an equal step apart
// however far apart their speeds are, so the scale is not half empty up to a
// run; near a mark the thumb is drawn onto it, and between the marks any speed
// can still be set.
const speed = defineModel<number>({ required: true })

defineProps<{
  /** What the slider sets, read out to a screen reader. */
  label: string
}>()

const { locale } = useI18n()

const position = computed(() => positionOf(speed.value))
const shown = computed(() => formatSpeed(speed.value, locale.value, activeUnits.value))

// onInput takes the speed at the thumb, and puts the thumb where that speed
// is, which is on the mark when it was drawn onto one.
function onInput(event: Event): void {
  const input = event.target as HTMLInputElement
  speed.value = speedAt(Number(input.value))
  input.value = String(positionOf(speed.value))
}

// onKey moves the speed from the keyboard by a tenth, or to the next mark with
// Page Up and Page Down. The slider's own steps would be drawn back onto a
// mark they start next to, so the keys are not left to it.
function onKey(event: KeyboardEvent): void {
  const marks = [MIN_SPEED, ...SPEED_STOPS, MAX_SPEED]
  const moves: Record<string, () => number> = {
    ArrowRight: () => clampSpeed(speed.value + 0.1),
    ArrowUp: () => clampSpeed(speed.value + 0.1),
    ArrowLeft: () => clampSpeed(speed.value - 0.1),
    ArrowDown: () => clampSpeed(speed.value - 0.1),
    PageUp: () => marks.find((mark) => mark > speed.value) ?? MAX_SPEED,
    PageDown: () => [...marks].reverse().find((mark) => mark < speed.value) ?? MIN_SPEED,
    Home: () => MIN_SPEED,
    End: () => MAX_SPEED,
  }
  const move = moves[event.key]
  if (move) {
    event.preventDefault()
    speed.value = move()
  }
}

// The last mark stands for everything faster.
const marks = computed(() => SPEED_STOPS.map((stop, index) => ({
  stop,
  left: stopPosition(index) / SLIDER_STEPS,
  label: formatSpeed(stop, locale.value, activeUnits.value, false) + (index === SPEED_STOPS.length - 1 ? '+' : ''),
})))
</script>

<template>
  <div class="speed-slider flex flex-col gap-1">
    <p class="text-center text-lg font-semibold tabular-nums" aria-hidden="true">{{ shown }}</p>
    <input
      type="range"
      class="speed-range"
      min="0"
      :max="SLIDER_STEPS"
      step="1"
      :value="position"
      :aria-label="label"
      :aria-valuetext="shown"
      @input="onInput"
      @keydown="onKey"
    />
    <div class="relative h-8">
      <button
        v-for="mark in marks"
        :key="mark.stop"
        type="button"
        class="speed-mark absolute top-0 flex -translate-x-1/2 flex-col items-center text-xs tabular-nums"
        :class="mark.stop === speed ? 'font-semibold text-base-content' : 'text-base-content/60'"
        :style="{ left: `calc(var(--speed-thumb) / 2 + (100% - var(--speed-thumb)) * ${mark.left})` }"
        tabindex="-1"
        @click="speed = mark.stop"
      >
        <span class="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
        {{ mark.label }}
      </button>
    </div>
  </div>
</template>
