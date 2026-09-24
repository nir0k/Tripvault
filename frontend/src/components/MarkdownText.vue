<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '@/utils/markdown'

const props = defineProps<{ source: string }>()

// The HTML is sanitised by renderMarkdown before it reaches v-html.
const html = computed(() => renderMarkdown(props.source))
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html -->
  <div class="markdown text-sm leading-relaxed" v-html="html"></div>
</template>
