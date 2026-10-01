<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { verifyEmailByToken } from '@/api/auth'
import AppLogo from '@/components/AppLogo.vue'
import { errorMessage } from '@/utils/errors'

const { t, te } = useI18n()

// readToken takes the credential from the fragment and removes it from the
// address, so it stays out of the history the browser keeps.
function readToken(): string {
  const token = new URLSearchParams(window.location.hash.slice(1)).get('token') ?? ''
  if (token) {
    history.replaceState(history.state, '', window.location.pathname + window.location.search)
  }
  return token
}

const token = readToken()
const busy = ref(Boolean(token))
const done = ref(false)
const error = ref(token ? '' : t('mail.invalidLink'))

// The link confirms the address only; signing in stays with the password.
onMounted(async () => {
  if (!token) {
    return
  }
  try {
    await verifyEmailByToken(token)
    done.value = true
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
})
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-sm bg-base-100 shadow-xl">
      <div class="card-body gap-4">
        <div class="flex justify-center"><AppLogo size="lg" /></div>
        <h1 class="text-center text-lg font-semibold">{{ t('registration.confirmTitle') }}</h1>
        <p v-if="busy" role="status" class="flex items-center justify-center gap-2 text-sm">
          <span class="loading loading-spinner loading-sm"></span>
          {{ t('registration.verifyingLink') }}
        </p>
        <p v-if="done" role="status" class="alert alert-success text-sm">{{ t('registration.verified') }}</p>
        <p v-if="error" role="alert" class="alert alert-error text-sm">{{ error }}</p>
        <RouterLink class="btn btn-primary" :to="{ name: 'login' }">{{ t('mail.backToLogin') }}</RouterLink>
      </div>
    </div>
  </main>
</template>
