<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStatus } from '@/api/admin'
import type { ServiceStatus } from '@/api/types'
import ProviderStatusCard from '@/components/ProviderStatusCard.vue'
import { errorMessage } from '@/utils/errors'

const { t, te } = useI18n()
const status = ref<ServiceStatus | null>(null)
const error = ref('')
const frontendVersion = import.meta.env.VITE_APP_VERSION ?? 'dev'

onMounted(async () => {
  try {
    status.value = await getStatus()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
})
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('status.title') }}</h1>
    <p v-if="error" role="alert" class="text-error">{{ error }}</p>

    <div v-if="status" class="stats stats-vertical w-full border border-base-300 sm:stats-horizontal">
      <div class="stat">
        <div class="stat-title">{{ t('status.version') }}</div>
        <div class="stat-value text-2xl">{{ status.version }}</div>
        <div class="stat-desc">{{ t('status.interfaceVersion', { version: frontendVersion }) }}</div>
      </div>
      <div class="stat">
        <div class="stat-title">{{ t('status.schema') }}</div>
        <div class="stat-value text-2xl">{{ status.schema_version }}</div>
      </div>
      <div class="stat">
        <div class="stat-title">{{ t('status.users') }}</div>
        <div class="stat-value text-2xl">{{ status.users.active }}</div>
        <div class="stat-desc">{{ t('status.usersDetail', { total: status.users.total, admins: status.users.admins }) }}</div>
      </div>
    </div>

    <div v-if="status" class="grid gap-6 lg:grid-cols-2">
      <ProviderStatusCard :title="t('status.routing.title')" :status="status.routing" />
      <ProviderStatusCard :title="t('status.geocodingTitle')" :status="status.geocoding" />
    </div>
  </section>
</template>
