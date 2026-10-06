<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'
import {
  acceptUserInvitation, previewIdeaInvitation, previewTripInvitation, previewUserInvitation, registerIdeaInvitation,
  registerTripInvitation,
} from '@/api/auth'
import { acceptIdeaInvitation } from '@/api/ideas'
import { acceptTripInvitation } from '@/api/trips'
import type { IdeaInvitationPreview, TripInvitationPreview, UserInvitationPreview } from '@/api/types'
import AppLogo from '@/components/AppLogo.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'

// An invitation creates an account, or gives access to a trip or to somebody's
// list of ideas; the last two are accepted by the matching account or create one.
const props = defineProps<{ kind: 'account' | 'trip' | 'ideas' }>()
const { t, te, locale } = useI18n()
const router = useRouter()
const session = useSessionStore()
const storageKey = `tripvault.${props.kind}InvitationToken`

// readToken keeps a credential in session storage and removes it from the URL.
function readToken(): string {
  const fragment = new URLSearchParams(window.location.hash.slice(1)).get('token') ?? ''
  if (fragment) {
    sessionStorage.setItem(storageKey, fragment)
    history.replaceState(history.state, '', window.location.pathname + window.location.search)
  }
  return fragment || sessionStorage.getItem(storageKey) || ''
}

const token = readToken()
const account = ref<UserInvitationPreview | null>(null)
const trip = ref<TripInvitationPreview | null>(null)
const ideas = ref<IdeaInvitationPreview | null>(null)
// joining is the access offered to a person, who may already have an account.
const joining = computed(() => trip.value ?? ideas.value)
const displayName = ref('')
const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref(token ? '' : t('mail.invalidLink'))

// load verifies the credential and shows what it offers.
async function load(): Promise<void> {
  if (!token) {
    return
  }
  if (props.kind !== 'account' && session.signedIn && session.mustChangePassword) {
    await router.replace({ name: 'change-password', query: { redirect: `/invite/${props.kind}` } })
    return
  }
  try {
    if (props.kind === 'account') {
      account.value = await previewUserInvitation(token)
      displayName.value = account.value.display_name
    } else if (props.kind === 'trip') {
      trip.value = await previewTripInvitation(token)
    } else {
      ideas.value = await previewIdeaInvitation(token)
    }
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

// accept uses the current account or creates the account offered by the link.
async function accept(): Promise<void> {
  if (password.value !== confirmation.value && (!session.signedIn || props.kind === 'account')) {
    error.value = t('mail.passwordMismatch')
    return
  }
  busy.value = true
  error.value = ''
  try {
    if (props.kind === 'account') {
      await acceptUserInvitation(token, password.value, locale.value)
    } else if (props.kind === 'trip') {
      await (session.signedIn
        ? acceptTripInvitation(token)
        : registerTripInvitation(token, displayName.value, password.value, locale.value))
    } else {
      await (session.signedIn
        ? acceptIdeaInvitation(token)
        : registerIdeaInvitation(token, displayName.value, password.value, locale.value))
    }
    sessionStorage.removeItem(storageKey)
    if (!session.signedIn) {
      await router.replace({ name: 'login', query: { invited: '1' } })
    } else {
      await router.replace({ name: props.kind === 'ideas' ? 'ideas' : 'home' })
    }
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <main class="flex min-h-dvh items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-md bg-base-100 shadow-xl">
      <form class="card-body gap-4" @submit.prevent="accept">
        <div class="flex justify-center"><AppLogo size="lg" /></div>
        <h1 class="text-center text-lg font-semibold">{{ t(`mail.${kind}InvitationTitle`) }}</h1>
        <p v-if="account" class="text-sm">{{ t('mail.accountInvitationFor', { email: account.email }) }}</p>
        <p v-if="trip" class="text-sm">{{ t('mail.tripInvitationFor', { trip: trip.trip_title, email: trip.email, role: t(`trips.roles.${trip.role}`) }) }}</p>
        <p v-if="ideas" class="text-sm">{{ t('mail.ideasInvitationFor', { owner: ideas.owner_name, email: ideas.email, role: t(`trips.roles.${ideas.role}`) }) }}</p>

        <template v-if="account || (joining && !session.signedIn)">
          <label v-if="kind !== 'account'" class="floating-label">
            <span>{{ t('users.displayName') }}</span>
            <input v-model="displayName" type="text" maxlength="120" required class="input w-full" :placeholder="t('users.displayName')" />
          </label>
          <label class="floating-label">
            <span>{{ t('mail.newPassword') }}</span>
            <input v-model="password" type="password" minlength="8" autocomplete="new-password" required class="input w-full" :placeholder="t('mail.newPassword')" />
          </label>
          <label class="floating-label">
            <span>{{ t('mail.confirmPassword') }}</span>
            <input v-model="confirmation" type="password" minlength="8" autocomplete="new-password" required class="input w-full" :placeholder="t('mail.confirmPassword')" />
          </label>
        </template>

        <p v-if="joining && session.signedIn" class="text-sm text-base-content/70">{{ t('mail.acceptAs', { email: session.user?.email }) }}</p>
        <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
        <button type="submit" class="btn btn-primary" :disabled="busy || (!account && !joining)">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('mail.acceptInvitation') }}
        </button>
        <RouterLink v-if="kind !== 'account' && !session.signedIn" class="btn btn-ghost" :to="{ name: 'login', query: { redirect: `/invite/${kind}` } }">{{ t('mail.haveAccount') }}</RouterLink>
      </form>
    </div>
  </main>
</template>
