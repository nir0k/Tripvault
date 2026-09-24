<script setup lang="ts">
import { computed } from 'vue'
import { FILLED, OUTLINE, type IconName } from '@/components/icons'

// Icons drawn inline, so the interface loads nothing from elsewhere. The shapes
// live in ./icons.ts, which the map reads as well.

export type { IconName }

const props = defineProps<{ name: IconName }>()

// filled picks the solid shapes of an icon, or nothing for an outline one.
const filled = computed(() => (props.name in FILLED ? FILLED[props.name as keyof typeof FILLED] : null))
</script>

<template>
  <svg
    v-if="filled"
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 16 16"
    fill="currentColor"
    class="size-5 shrink-0"
    aria-hidden="true"
  >
    <path v-for="(shape, index) in filled" :key="index" :d="shape" />
  </svg>
  <svg
    v-else
    xmlns="http://www.w3.org/2000/svg"
    fill="none"
    viewBox="0 0 24 24"
    stroke-width="1.5"
    stroke="currentColor"
    class="size-5 shrink-0"
    aria-hidden="true"
  >
    <path stroke-linecap="round" stroke-linejoin="round" :d="OUTLINE[name as keyof typeof OUTLINE]" />
  </svg>
</template>
