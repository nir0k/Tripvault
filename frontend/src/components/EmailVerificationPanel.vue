<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { resendVerification, verifyEmailByCode } from '@/api/auth'
import { ApiError } from '@/api/client'
import { errorMessage } from '@/utils/errors'

// The account's address and password: the code is checked against the
// address, and asking for another message proves the account with both.
const props = defineProps<{ email: string; password: string; resendAvailableIn: number }>()
const emit = defineEmits<{ verified: [] }>()

const { t, te } = useI18n()
const code = ref('')
const busy = ref(false)
const sending = ref(false)
const error = ref('')
const notice = ref('')

// The server lets one message through in five minutes; the button counts the
// wait down to the moment it would, from a deadline rather than by decrements,
// so a sleeping tab does not fall behind.
const deadline = ref(0)
const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined

// startCountdown disables the button for the given number of seconds.
function startCountdown(seconds: number): void {
  now.value = Date.now()
  deadline.value = now.value + Math.max(0, seconds) * 1000
  clearInterval(ticker)
  if (seconds > 0) {
    ticker = setInterval(() => {
      now.value = Date.now()
      if (now.value >= deadline.value) {
        clearInterval(ticker)
      }
    }, 1000)
  }
}

watch(() => props.resendAvailableIn, (seconds) => startCountdown(seconds), { immediate: true })
onBeforeUnmount(() => clearInterval(ticker))

const remaining = computed(() => Math.max(0, Math.ceil((deadline.value - now.value) / 1000)))
const remainingLabel = computed(() => {
  const minutes = Math.floor(remaining.value / 60)
  const seconds = String(remaining.value % 60).padStart(2, '0')
  return `${minutes}:${seconds}`
})

// confirm checks the typed code; a right one activates the account.
async function confirm(): Promise<void> {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await verifyEmailByCode(props.email, code.value)
    emit('verified')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// resend asks for a new message, which replaces the link and code of the last one.
async function resend(): Promise<void> {
  sending.value = true
  error.value = ''
  notice.value = ''
  try {
    const sent = await resendVerification(props.email, props.password)
    code.value = ''
    notice.value = t('registration.resent')
    startCountdown(sent.resend_available_in)
  } catch (err) {
    if (err instanceof ApiError && err.code === 'resend_too_soon') {
      startCountdown(Number(err.details.retry_after ?? 0))
    }
    error.value = errorMessage(err, t, te)
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-4" @submit.prevent="confirm">
    <h1 class="text-center text-lg font-semibold">{{ t('registration.confirmTitle') }}</h1>
    <p role="status" class="alert alert-warning text-sm">{{ t('registration.pendingNotice', { email }) }}</p>
    <label class="floating-label">
      <span>{{ t('registration.code') }}</span>
      <input
        v-model="code"
        type="text"
        inputmode="numeric"
        autocomplete="one-time-code"
        maxlength="12"
        required
        class="input w-full text-center font-mono text-lg tracking-widest"
        :placeholder="t('registration.code')"
      />
    </label>
    <p v-if="notice" role="status" class="text-sm text-success">{{ notice }}</p>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <button type="submit" class="btn btn-primary" :disabled="busy || !code.trim()">
      <span v-if="busy" class="loading loading-spinner loading-sm"></span>
      {{ t('registration.confirm') }}
    </button>
    <button type="button" class="btn btn-hover-outline" :disabled="sending || remaining > 0" @click="resend">
      <span v-if="sending" class="loading loading-spinner loading-sm"></span>
      {{ remaining > 0 ? t('registration.resendIn', { time: remainingLabel }) : t('registration.resend') }}
    </button>
    <p class="text-center text-xs text-base-content/60">{{ t('registration.deleteNotice') }}</p>
  </form>
</template>
