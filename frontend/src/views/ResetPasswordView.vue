<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { completePasswordReset } from '@/api/auth'
import AppLogo from '@/components/AppLogo.vue'
import { errorMessage } from '@/utils/errors'

const { t, te } = useI18n()
const router = useRouter()
const storageKey = 'tripvault.passwordResetToken'

// readToken moves the credential out of the address bar before any navigation.
function readToken(): string {
  const fragment = new URLSearchParams(window.location.hash.slice(1)).get('token') ?? ''
  if (fragment) {
    sessionStorage.setItem(storageKey, fragment)
    history.replaceState(history.state, '', window.location.pathname + window.location.search)
  }
  return fragment || sessionStorage.getItem(storageKey) || ''
}

const token = readToken()
const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref(token ? '' : t('mail.invalidLink'))

// submit stores the new password and discards the spent credential.
async function submit(): Promise<void> {
  if (password.value !== confirmation.value) {
    error.value = t('mail.passwordMismatch')
    return
  }
  busy.value = true
  error.value = ''
  try {
    await completePasswordReset(token, password.value)
    sessionStorage.removeItem(storageKey)
    await router.replace({ name: 'login', query: { reset: '1' } })
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-sm bg-base-100 shadow-xl">
      <form class="card-body gap-4" @submit.prevent="submit">
        <div class="flex justify-center"><AppLogo size="lg" /></div>
        <h1 class="text-center text-lg font-semibold">{{ t('mail.resetTitle') }}</h1>
        <label class="floating-label">
          <span>{{ t('mail.newPassword') }}</span>
          <input v-model="password" type="password" minlength="8" autocomplete="new-password" required class="input w-full" :placeholder="t('mail.newPassword')" />
        </label>
        <label class="floating-label">
          <span>{{ t('mail.confirmPassword') }}</span>
          <input v-model="confirmation" type="password" minlength="8" autocomplete="new-password" required class="input w-full" :placeholder="t('mail.confirmPassword')" />
        </label>
        <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
        <button type="submit" class="btn btn-primary" :disabled="busy || !token">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('mail.savePassword') }}
        </button>
      </form>
    </div>
  </main>
</template>
