<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import AppLogo from '@/components/AppLogo.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import { safeRedirect } from '@/router'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const email = ref('')
const password = ref('')
const showPassword = ref(false)
const error = ref('')
const busy = ref(false)

// submit signs in and continues to the page that asked for it; the router
// sends an account with a temporary password to change it first.
async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await session.login(email.value.trim(), password.value)
    await router.replace(safeRedirect(route.query.redirect))
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
        <h1 class="text-center text-lg font-semibold">{{ t('login.title') }}</h1>

        <label class="floating-label">
          <span>{{ t('login.email') }}</span>
          <input v-model="email" type="email" autocomplete="username" required class="input w-full" :placeholder="t('login.email')" />
        </label>
        <div class="relative">
          <label class="floating-label">
            <span>{{ t('login.password') }}</span>
            <input
              v-model="password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              required
              class="input w-full pr-10"
              :placeholder="t('login.password')"
            />
          </label>
          <button
            type="button"
            class="btn btn-ghost btn-sm btn-square absolute top-1/2 right-1 z-10 -translate-y-1/2"
            :aria-label="showPassword ? t('login.hidePassword') : t('login.showPassword')"
            :title="showPassword ? t('login.hidePassword') : t('login.showPassword')"
            :aria-pressed="showPassword"
            @click="showPassword = !showPassword"
          >
            <AppIcon :name="showPassword ? 'eyeSlash' : 'eye'" />
          </button>
        </div>

        <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

        <button type="submit" class="btn btn-primary" :disabled="busy">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('login.submit') }}
        </button>

        <div class="divider my-0"></div>
        <div class="flex items-center justify-between gap-2">
          <LanguageSelect />
          <ThemeSelect />
        </div>
      </form>
    </div>
  </main>
</template>
