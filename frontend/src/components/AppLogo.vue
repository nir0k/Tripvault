<script setup lang="ts">
withDefaults(defineProps<{ size?: 'sm' | 'lg' }>(), { size: 'sm' })
</script>

<template>
  <span class="flex items-center gap-2 font-bold tracking-tight" :class="size === 'lg' ? 'text-2xl' : 'text-lg'">
    <img src="/logo-64.png" alt="" :class="size === 'lg' ? 'size-10' : 'size-8'" />
    <span>Tripvault</span>
  </span>
</template>
