<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { requestPasswordReset } from '@/api/auth'
import AppLogo from '@/components/AppLogo.vue'
import { errorMessage } from '@/utils/errors'

const { t, te } = useI18n()
const email = ref('')
const sent = ref(false)
const busy = ref(false)
const error = ref('')

// submit asks for recovery and always shows the same result for a valid request.
async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await requestPasswordReset(email.value.trim())
    sent.value = true
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
        <h1 class="text-center text-lg font-semibold">{{ t('mail.forgotTitle') }}</h1>
        <p class="text-sm text-base-content/70">{{ t('mail.forgotHint') }}</p>
        <label class="floating-label">
          <span>{{ t('login.email') }}</span>
          <input v-model="email" type="email" autocomplete="email" required class="input w-full" :placeholder="t('login.email')" />
        </label>
        <p v-if="sent" role="status" class="alert alert-success text-sm">{{ t('mail.resetRequested') }}</p>
        <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
        <button type="submit" class="btn btn-primary" :disabled="busy || sent">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('mail.sendReset') }}
        </button>
        <RouterLink class="btn btn-ghost" :to="{ name: 'login' }">{{ t('mail.backToLogin') }}</RouterLink>
      </form>
    </div>
  </main>
</template>
