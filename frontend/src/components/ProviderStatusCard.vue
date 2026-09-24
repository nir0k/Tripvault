<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProviderStatus } from '@/api/types'
import { formatDateTime } from '@/utils/format'

// The state of one external provider: whether it is configured, its usage
// against the daily limit, its last answers and its cache.
const props = defineProps<{ title: string; status: ProviderStatus }>()

const { t, locale } = useI18n()

// A service of one's own is usually left with no daily limit, and "12 of 0"
// would read as a service that has run out rather than one without a ceiling.
const usage = computed(() =>
  props.status.daily_limit > 0
    ? t('status.routing.usageValue', { used: props.status.requests_24h, limit: props.status.daily_limit })
    : t('status.routing.usageNoLimit', { used: props.status.requests_24h }),
)
</script>

<template>
  <div class="card border border-base-300 bg-base-100">
    <div class="card-body gap-2">
      <h2 class="card-title">{{ title }}</h2>
      <p>
        <span class="badge" :class="status.configured ? 'badge-success' : 'badge-warning'">
          {{ status.configured ? t('status.routing.configured') : t('status.routing.notConfigured') }}
        </span>
        <span class="ms-2 text-sm text-base-content/70">{{ status.provider }}</span>
      </p>
      <dl class="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        <dt class="text-base-content/70">{{ t('status.routing.usage') }}</dt>
        <dd>{{ usage }}</dd>
        <dt class="text-base-content/70">{{ t('status.routing.errors') }}</dt>
        <dd>{{ status.errors_24h }}</dd>
        <dt class="text-base-content/70">{{ t('status.routing.lastSuccess') }}</dt>
        <dd>{{ status.last_success_at ? formatDateTime(status.last_success_at, locale) : t('status.routing.never') }}</dd>
        <dt class="text-base-content/70">{{ t('status.routing.lastError') }}</dt>
        <dd>{{ status.last_error_at ? formatDateTime(status.last_error_at, locale) : t('status.routing.never') }}</dd>
        <dt class="text-base-content/70">{{ t('status.routing.cache') }}</dt>
        <dd>{{ t('status.routing.cacheValue', { entries: status.cache_entries, hits: status.cache_hits }) }}</dd>
      </dl>
    </div>
  </div>
</template>
