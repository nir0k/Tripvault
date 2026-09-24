<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLogo from '@/components/AppLogo.vue'
import PasswordChangeForm from '@/components/PasswordChangeForm.vue'
import { safeRedirect } from '@/router'
import { useSessionStore } from '@/stores/session'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

// onChanged reads the account again - it no longer holds a temporary password -
// and continues to where the person was going.
async function onChanged(): Promise<void> {
  await session.reload()
  await router.replace(safeRedirect(route.query.redirect))
}

// signOut lets somebody leave without choosing a password now.
async function signOut(): Promise<void> {
  await session.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-sm bg-base-100 shadow-xl">
      <div class="card-body gap-4">
        <div class="flex justify-center"><AppLogo size="lg" /></div>
        <h1 class="text-lg font-semibold">{{ t('password.temporaryTitle') }}</h1>
        <p class="text-sm text-base-content/70">{{ t('password.temporaryText') }}</p>
        <PasswordChangeForm @changed="onChanged" />
        <button type="button" class="btn btn-ghost btn-sm" @click="signOut">{{ t('nav.signOut') }}</button>
      </div>
    </div>
  </main>
</template>
