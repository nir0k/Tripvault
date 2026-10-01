<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPreviewStatus, getStatus, sendTestMail, setMailEnabled, setSelfRegistration, updateStorage } from '@/api/admin'
import type { PreviewStatus, ServiceStatus } from '@/api/types'
import PreviewStatusCard from '@/components/PreviewStatusCard.vue'
import ProviderStatusCard from '@/components/ProviderStatusCard.vue'
import { errorMessage } from '@/utils/errors'
import { formatFileSize } from '@/utils/format'

const { t, te, locale } = useI18n()
const status = ref<ServiceStatus | null>(null)
const previews = ref<PreviewStatus | null>(null)
const error = ref('')
const storageSaving = ref(false)
const storageSaved = ref(false)
const tripQuotaMB = ref(0)
const mailEnabled = ref(false)
const mailSaving = ref(false)
const mailTesting = ref(false)
const mailTestEmail = ref('')
const mailNotice = ref('')
const registrationEnabled = ref(false)
const registrationSaving = ref(false)
const registrationNotice = ref('')

// registrationBlocker names why registration cannot be opened: it confirms
// every address by email, so it needs SMTP configured and delivery enabled.
const registrationBlocker = computed(() => {
  if (!status.value) {
    return ''
  }
  if (!status.value.mail.configured) {
    return t('status.registration.needsSmtp')
  }
  if (!status.value.mail.enabled) {
    return t('status.registration.needsDelivery')
  }
  return ''
})
const frontendVersion = import.meta.env.VITE_APP_VERSION ?? 'dev'

// The previews are polled while a wave of rendering runs, so the bar moves
// instead of standing where it was when the page opened.
const pollInterval = 3000
let poll: ReturnType<typeof setTimeout> | undefined

// refreshPreviews reads the preview progress and keeps reading it while work
// is under way. A failed read ends the polling rather than repeating the error.
async function refreshPreviews(): Promise<void> {
  try {
    previews.value = await getPreviewStatus()
  } catch (err) {
    error.value = errorMessage(err, t, te)
    return
  }
  if (previews.value.active) {
    poll = setTimeout(() => void refreshPreviews(), pollInterval)
  }
}

onMounted(async () => {
  try {
    status.value = await getStatus()
    tripQuotaMB.value = Math.round(status.value.storage.trip_quota_bytes / (1024 * 1024))
    mailEnabled.value = status.value.mail.enabled
    registrationEnabled.value = status.value.mail.self_registration
  } catch (err) {
    error.value = errorMessage(err, t, te)
    return
  }
  await refreshPreviews()
})

// saveStorage stores the per-trip policy while leaving the operator's ceiling read-only.
async function saveStorage(): Promise<void> {
  if (!status.value || storageSaving.value) {
    return
  }
  storageSaving.value = true
  storageSaved.value = false
  error.value = ''
  try {
    status.value.storage = await updateStorage(tripQuotaMB.value)
    storageSaved.value = true
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    storageSaving.value = false
  }
}

// saveMail stores the administrator delivery switch and wakes queued work when enabled.
async function saveMail(): Promise<void> {
  if (!status.value || mailSaving.value) {
    return
  }
  mailSaving.value = true
  mailNotice.value = ''
  error.value = ''
  try {
    status.value.mail = await setMailEnabled(mailEnabled.value)
    mailNotice.value = t('status.mail.saved')
  } catch (err) {
    mailEnabled.value = status.value.mail.enabled
    error.value = errorMessage(err, t, te)
  } finally {
    mailSaving.value = false
  }
}

// saveRegistration stores the administrator's registration switch.
async function saveRegistration(): Promise<void> {
  if (!status.value || registrationSaving.value) {
    return
  }
  registrationSaving.value = true
  registrationNotice.value = ''
  error.value = ''
  try {
    status.value.mail = await setSelfRegistration(registrationEnabled.value)
    registrationNotice.value = t('status.registration.saved')
  } catch (err) {
    registrationEnabled.value = status.value.mail.self_registration
    error.value = errorMessage(err, t, te)
  } finally {
    registrationSaving.value = false
  }
}

// testMail sends immediately so an administrator can validate credentials before enabling delivery.
async function testMail(): Promise<void> {
  mailTesting.value = true
  mailNotice.value = ''
  error.value = ''
  try {
    await sendTestMail(mailTestEmail.value.trim())
    mailNotice.value = t('status.mail.testSent')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    mailTesting.value = false
  }
}

onBeforeUnmount(() => clearTimeout(poll))
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
      <section class="card border border-base-300 bg-base-100">
        <form class="card-body gap-4" @submit.prevent="saveStorage">
          <div>
            <h2 class="card-title">{{ t('status.storage.title') }}</h2>
            <p class="text-sm text-base-content/70">{{ t('status.storage.description') }}</p>
          </div>
          <dl class="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt class="text-base-content/60">{{ t('status.storage.used') }}</dt>
              <dd class="font-medium tabular-nums">{{ formatFileSize(status.storage.used_bytes, locale) }}</dd>
            </div>
            <div>
              <dt class="text-base-content/60">{{ t('status.storage.instanceLimit') }}</dt>
              <dd class="font-medium tabular-nums">
                {{ status.storage.limit_bytes ? formatFileSize(status.storage.limit_bytes, locale) : t('status.storage.unlimited') }}
              </dd>
            </div>
            <div>
              <dt class="text-base-content/60">{{ t('status.storage.media') }}</dt>
              <dd class="tabular-nums">{{ formatFileSize(status.storage.media_bytes, locale) }}</dd>
            </div>
            <div>
              <dt class="text-base-content/60">{{ t('status.storage.databaseFiles') }}</dt>
              <dd class="tabular-nums">{{ formatFileSize(status.storage.database_bytes, locale) }}</dd>
            </div>
          </dl>
          <label class="form-control w-full max-w-xs">
            <span class="label-text">{{ t('status.storage.tripQuota') }}</span>
            <input v-model.number="tripQuotaMB" class="input input-bordered" type="number" min="0" step="1" required>
            <span class="label-text-alt mt-1">{{ t('status.storage.tripQuotaHint') }}</span>
          </label>
          <div class="card-actions items-center">
            <button class="btn btn-primary" type="submit" :disabled="storageSaving">
              <span v-if="storageSaving" class="loading loading-spinner loading-sm"></span>
              {{ t('common.save') }}
            </button>
            <span v-if="storageSaved" class="text-sm text-success">{{ t('settings.saved') }}</span>
          </div>
        </form>
      </section>
      <section class="card border border-base-300 bg-base-100">
        <div class="card-body gap-4">
          <div>
            <h2 class="card-title">{{ t('status.mail.title') }}</h2>
            <p class="text-sm text-base-content/70">{{ t('status.mail.description') }}</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <span class="badge" :class="status.mail.configured ? 'badge-success' : 'badge-warning'">
              {{ status.mail.configured ? t('status.mail.configured') : t('status.mail.notConfigured') }}
            </span>
            <span class="badge badge-outline">{{ t('status.mail.queued', { count: status.mail.queued }) }}</span>
            <span v-if="status.mail.failed" class="badge badge-error">{{ t('status.mail.failed', { count: status.mail.failed }) }}</span>
          </div>
          <form class="space-y-3" @submit.prevent="saveMail">
            <label class="label cursor-pointer justify-start gap-3">
              <input v-model="mailEnabled" type="checkbox" class="toggle toggle-primary" :disabled="!status.mail.configured && !status.mail.enabled" />
              <span>{{ t('status.mail.enabled') }}</span>
            </label>
            <button type="submit" class="btn btn-primary btn-sm" :disabled="mailSaving || (!status.mail.configured && !status.mail.enabled)">
              <span v-if="mailSaving" class="loading loading-spinner loading-xs"></span>
              {{ t('common.save') }}
            </button>
          </form>
          <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="testMail">
            <input v-model="mailTestEmail" type="email" required class="input flex-1" :placeholder="t('status.mail.testAddress')" />
            <button type="submit" class="btn btn-hover-outline" :disabled="mailTesting || !status.mail.configured">
              <span v-if="mailTesting" class="loading loading-spinner loading-xs"></span>
              {{ t('status.mail.sendTest') }}
            </button>
          </form>
          <p v-if="mailNotice" role="status" class="text-sm text-success">{{ mailNotice }}</p>
          <p v-if="status.mail.last_error" class="text-sm text-error break-words">{{ status.mail.last_error }}</p>
        </div>
      </section>
      <section class="card border border-base-300 bg-base-100">
        <div class="card-body gap-4">
          <div>
            <h2 class="card-title">{{ t('status.registration.title') }}</h2>
            <p class="text-sm text-base-content/70">{{ t('status.registration.description') }}</p>
          </div>
          <form class="space-y-2" @submit.prevent="saveRegistration">
            <label class="label cursor-pointer justify-start gap-3">
              <input
                v-model="registrationEnabled"
                type="checkbox"
                class="toggle toggle-primary"
                :disabled="Boolean(registrationBlocker) && !status.mail.self_registration"
              />
              <span>{{ t('status.registration.enabled') }}</span>
            </label>
            <p v-if="registrationBlocker && !status.mail.self_registration" role="note" class="alert alert-info text-sm">
              {{ registrationBlocker }}
            </p>
            <p v-else-if="status.mail.self_registration && !status.mail.enabled" role="note" class="alert alert-warning text-sm">
              {{ t('status.registration.suspended') }}
            </p>
            <button
              type="submit"
              class="btn btn-primary btn-sm"
              :disabled="registrationSaving || registrationEnabled === status.mail.self_registration"
            >
              <span v-if="registrationSaving" class="loading loading-spinner loading-xs"></span>
              {{ t('common.save') }}
            </button>
            <p v-if="registrationNotice" role="status" class="text-sm text-success">{{ registrationNotice }}</p>
          </form>
        </div>
      </section>
      <ProviderStatusCard :title="t('status.routing.title')" :status="status.routing" />
      <ProviderStatusCard :title="t('status.geocodingTitle')" :status="status.geocoding" />
      <PreviewStatusCard v-if="previews" :status="previews" />
    </div>
  </section>
</template>
