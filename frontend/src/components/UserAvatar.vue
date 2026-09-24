<script setup lang="ts">
import { computed } from 'vue'
import { avatarPath } from '@/api/me'
import { useMediaUrl } from '@/composables/useMediaUrl'

// The picture an account is shown by, or the traveller everybody starts with.
// Like a trip's photographs it needs a token in a header, so it is fetched and
// shown through an object URL rather than handed to an <img> as an address; the
// moment it last changed is part of that address, so a new picture replaces the
// one the browser holds.
const props = withDefaults(defineProps<{
  userId: string
  /** Whether the account wears a picture at all; without one the placeholder stands. */
  hasAvatar?: boolean
  updatedAt?: string | null
  /** The width in Tailwind's scale, as daisyUI's avatar expects it. */
  size?: string
}>(), { hasAvatar: false, updatedAt: null, size: 'w-8' })

const path = computed(() => (props.hasAvatar && props.userId
  ? avatarPath(props.userId, props.updatedAt)
  : null))
const { url } = useMediaUrl(path)
</script>

<template>
  <div class="avatar">
    <!-- daisyUI rounds and crops the picture through .avatar > div, so the
         wrapper has to be a div for the circle to hold any image. -->
    <div class="rounded-full ring-1 ring-base-300" :class="size">
      <img :src="url ?? '/avatar-128.png'" alt="" />
    </div>
  </div>
</template>
