<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PreviewStatus } from '@/api/types'
import { formatDateTime } from '@/utils/format'

// How the rendering of photo previews is going: a bar for the wave of work
// under way, what it is made of, and what has come of it since the start.
const props = defineProps<{ status: PreviewStatus }>()

const { t, locale } = useI18n()

const percent = computed(() =>
  props.status.total > 0 ? Math.min(100, Math.round((props.status.done / props.status.total) * 100)) : 0)
</script>

<template>
  <div class="card border border-base-300 bg-base-100">
    <div class="card-body gap-3">
      <h2 class="card-title">{{ t('status.previews.title') }}</h2>

      <template v-if="status.active">
        <p class="text-sm">{{ t('status.previews.progress', { done: status.done, total: status.total }) }}</p>
        <progress class="progress progress-primary w-full" :value="percent" max="100"></progress>
      </template>
      <p v-else>
        <span class="badge" :class="status.background ? 'badge-success' : 'badge-warning'">
          {{ status.background ? t('status.previews.ready') : t('status.previews.onRequest') }}
        </span>
      </p>

      <dl class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        <template v-if="status.backfill.running">
          <dt class="text-base-content/70">{{ t('status.previews.backfill') }}</dt>
          <dd>{{ t('status.previews.backfillValue', { checked: status.backfill.checked, total: status.backfill.total }) }}</dd>
        </template>
        <dt class="text-base-content/70">{{ t('status.previews.queued') }}</dt>
        <dd>{{ status.queued }}</dd>
        <dt class="text-base-content/70">{{ t('status.previews.rendering') }}</dt>
        <dd>{{ status.rendering }}</dd>
        <dt class="text-base-content/70">{{ t('status.previews.rendered') }}</dt>
        <dd>{{ status.rendered }}</dd>
        <dt class="text-base-content/70">{{ t('status.previews.failed') }}</dt>
        <dd :class="{ 'text-error': status.failed > 0 }">{{ status.failed }}</dd>
        <template v-if="status.started_at">
          <dt class="text-base-content/70">{{ t('status.previews.lastWave') }}</dt>
          <dd>{{ formatDateTime(status.started_at, locale) }}</dd>
        </template>
      </dl>
      <p class="text-xs text-base-content/60">{{ t('status.previews.sinceStart') }}</p>
    </div>
  </div>
</template>
