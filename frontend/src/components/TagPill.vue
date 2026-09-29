<script setup lang="ts">
import type { TripTag } from '@/api/types'

// A tag as a pill in its own colour. Whatever is put in the slot - a button
// that takes the tag off - follows the name inside the pill.
defineProps<{ tag: Pick<TripTag, 'name' | 'color'> }>()
</script>

<template>
  <span class="badge badge-sm tag-pill max-w-full gap-1 rounded-full" :class="`tag-${tag.color}`">
    <span class="truncate">{{ tag.name }}</span>
    <slot />
  </span>
</template>
