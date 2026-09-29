<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { LegSegment } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import { formatDuration } from '@/utils/plan'

// The parts of a journey with changes as one line: each way of travelling with
// its time, and the change after it with the wait there, "walk 15 min → Rossio
// (10 min) → train 40 min → …". The plan and the report show it alike.
defineProps<{ segments: LegSegment[] }>()

const { t } = useI18n()

// minutes turns a part's seconds into the words the rest of the page uses.
function minutes(seconds: number | null): string {
  return seconds === null ? '' : formatDuration(Math.round(seconds / 60), t)
}
</script>

<template>
  <span class="inline-flex flex-wrap items-center gap-x-1.5 gap-y-1">
    <template v-for="(segment, index) in segments" :key="segment.id">
      <span class="inline-flex items-center gap-1" :title="t(`modes.${segment.mode}`)">
        <AppIcon :name="TRAVEL_MODE_ICONS[segment.mode]" class="size-4!" />
        <span class="sr-only">{{ t(`modes.${segment.mode}`) }}</span>
        <span v-if="segment.duration_s !== null">{{ minutes(segment.duration_s) }}</span>
      </span>
      <template v-if="segment.stop && index < segments.length - 1">
        <span aria-hidden="true">→</span>
        <span class="font-medium text-base-content/80">{{ segment.stop.name }}</span>
        <span v-if="segment.stop.wait_minutes > 0" class="text-base-content/60">
          ({{ t('leg.waitShort', { time: formatDuration(segment.stop.wait_minutes, t) }) }})
        </span>
        <span aria-hidden="true">→</span>
      </template>
    </template>
  </span>
</template>
