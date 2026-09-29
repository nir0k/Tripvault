<script setup lang="ts">
import { computed } from 'vue'
import { ideaPhotoPath } from '@/api/ideas'
import { useMediaUrl } from '@/composables/useMediaUrl'

// One photo of an idea, or its preview. The picture needs the reader's token,
// so it is fetched with it and shown as an object URL, as a trip's pictures are.
const props = withDefaults(defineProps<{
  ideaId: string
  photoId: string
  /** The small preview, for a grid or a list; the whole photo otherwise. */
  preview?: boolean
  alt?: string
}>(), { preview: true, alt: '' })

const path = computed(() => ideaPhotoPath(props.ideaId, props.photoId, props.preview))
const { url } = useMediaUrl(path)
</script>

<template>
  <img v-if="url" :src="url" :alt="alt" class="object-cover" draggable="false" />
  <span v-else class="block animate-pulse bg-base-200" aria-hidden="true"></span>
</template>
