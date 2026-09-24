<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'

const route = useRoute()
</script>

<template>
  <RouterView v-if="route.meta.bare" />
  <AppLayout v-else />
</template>
