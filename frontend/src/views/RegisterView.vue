<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'
import { register } from '@/api/auth'
import { getClientConfig } from '@/api/config'
import AppLogo from '@/components/AppLogo.vue'
import EmailVerificationPanel from '@/components/EmailVerificationPanel.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'

const { t, te, locale } = useI18n()
const router = useRouter()
const session = useSessionStore()

// open is null until the configuration says whether registration is open.
const open = ref<boolean | null>(null)
const displayName = ref('')
const email = ref('')
const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')
// sentWait is set once the account is registered: the seconds before another
// confirmation message may be requested.
const sentWait = ref<number | null>(null)

onMounted(async () => {
  try {
    open.value = (await getClientConfig()).self_registration
  } catch {
    open.value = false
  }
})

// submit registers the account, which waits for its address to be confirmed.
async function submit(): Promise<void> {
  if (password.value !== confirmation.value) {
    error.value = t('mail.passwordMismatch')
    return
  }
  busy.value = true
  error.value = ''
  try {
    email.value = email.value.trim()
    sentWait.value = (await register(displayName.value.trim(), email.value, password.value, locale.value)).resend_available_in
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// signIn opens the session of the account that was just confirmed.
async function signIn(): Promise<void> {
  try {
    await session.login(email.value, password.value)
    await router.replace({ name: 'home' })
  } catch {
    await router.replace({ name: 'login', query: { verified: '1' } })
  }
}
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-sm bg-base-100 shadow-xl">
      <div class="card-body gap-4">
        <div class="flex justify-center"><AppLogo size="lg" /></div>

        <EmailVerificationPanel
          v-if="sentWait !== null"
          :email="email"
          :password="password"
          :resend-available-in="sentWait"
          @verified="signIn"
        />

        <div v-else-if="open === null" class="flex justify-center py-6">
          <span class="loading loading-spinner"></span>
        </div>

        <template v-else-if="!open">
          <h1 class="text-center text-lg font-semibold">{{ t('registration.title') }}</h1>
          <p role="status" class="alert text-sm">{{ t('registration.closed') }}</p>
        </template>

        <form v-else class="flex flex-col gap-4" @submit.prevent="submit">
          <h1 class="text-center text-lg font-semibold">{{ t('registration.title') }}</h1>
          <label class="floating-label">
            <span>{{ t('registration.displayName') }}</span>
            <input v-model="displayName" type="text" autocomplete="name" required class="input w-full" :placeholder="t('registration.displayName')" />
          </label>
          <label class="floating-label">
            <span>{{ t('login.email') }}</span>
            <input v-model="email" type="email" autocomplete="email" required class="input w-full" :placeholder="t('login.email')" />
          </label>
          <label class="floating-label">
            <span>{{ t('login.password') }}</span>
            <input v-model="password" type="password" autocomplete="new-password" required minlength="8" class="input w-full" :placeholder="t('login.password')" />
          </label>
          <label class="floating-label">
            <span>{{ t('mail.confirmPassword') }}</span>
            <input v-model="confirmation" type="password" autocomplete="new-password" required minlength="8" class="input w-full" :placeholder="t('mail.confirmPassword')" />
          </label>
          <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            <span v-if="busy" class="loading loading-spinner loading-sm"></span>
            {{ t('registration.submit') }}
          </button>
        </form>

        <RouterLink class="btn btn-ghost" :to="{ name: 'login' }">{{ t('mail.backToLogin') }}</RouterLink>
        <div class="divider my-0"></div>
        <div class="flex items-center justify-between gap-2">
          <LanguageSelect />
          <ThemeSelect />
        </div>
      </div>
    </div>
  </main>
</template>
