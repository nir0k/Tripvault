<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// The months of the year as twelve buttons, each switched on and off by a
// click: the best time for an idea, or the months a filter looks for.
const months = defineModel<number[]>({ required: true })

defineProps<{
  /** What the months stand for, read out for the group. */
  label: string
}>()

const { locale } = useI18n()

// names are the short names of the months in the reader's language.
const names = computed(() => {
  const format = new Intl.DateTimeFormat(locale.value, { month: 'short', timeZone: 'UTC' })
  return Array.from({ length: 12 }, (_, index) => format.format(new Date(Date.UTC(2026, index, 1))))
})

// toggle switches a month on or off, keeping them in order.
function toggle(month: number): void {
  months.value = months.value.includes(month)
    ? months.value.filter((item) => item !== month)
    : [...months.value, month].sort((a, b) => a - b)
}
</script>

<template>
  <div role="group" :aria-label="label" class="grid grid-cols-6 gap-1 sm:grid-cols-12">
    <button
      v-for="(name, index) in names"
      :key="index"
      type="button"
      class="btn btn-xs px-0"
      :class="months.includes(index + 1) ? 'btn-primary' : 'btn-ghost border-base-300'"
      :aria-pressed="months.includes(index + 1)"
      @click="toggle(index + 1)"
    >
      {{ name }}
    </button>
  </div>
</template>
