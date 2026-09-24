<script setup lang="ts">
import { computed, onMounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { deleteAccount, deleteAvatar, listSessions, revokeSession, setAvatar, updateMe } from '@/api/me'
import type { DateFormat, SessionItem, TimeFormat, Units } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import AvatarEditor from '@/components/AvatarEditor.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CurrencyMenu from '@/components/CurrencyMenu.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import PasswordChangeForm from '@/components/PasswordChangeForm.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { useSessionStore } from '@/stores/session'
import { activeDateFormat, activeTimeFormat } from '@/utils/display'
import { errorMessage } from '@/utils/errors'
import { describeUserAgent, formatDateTime } from '@/utils/format'
import { activeUnits } from '@/utils/units'

const { t, te, locale } = useI18n()
const router = useRouter()
const session = useSessionStore()

const account = reactive({
  displayName: session.user?.display_name ?? '',
  currency: session.user?.default_currency ?? 'EUR',
})
const accountBusy = ref(false)
const accountMessage = ref('')
const accountError = ref('')
const passwordMessage = ref('')
const avatarError = ref('')

const sessions = ref<SessionItem[]>([])
const sessionsError = ref('')

const avatarEditor = useTemplateRef<InstanceType<typeof AvatarEditor>>('avatarEditor')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

const user = computed(() => session.user)

// The name and the currency are typed rather than switched, so the fields
// follow the account when something else changes it - an avatar, a format.
watch(user, (next) => {
  if (next) {
    account.displayName = next.display_name
    account.currency = next.default_currency
  }
})

/** The three ways of writing a measurement this page offers, with an example each. */
const dateFormats: DateFormat[] = ['mdy', 'dmy']
const distanceUnits: Units[] = ['mi', 'km']
const timeFormats: TimeFormat[] = ['h12', 'h24']

// saveAccount stores the name the account is shown by.
async function saveAccount(): Promise<void> {
  accountBusy.value = true
  accountMessage.value = ''
  accountError.value = ''
  try {
    session.setUser(await updateMe({ display_name: account.displayName }))
    accountMessage.value = t('profile.saved')
  } catch (err) {
    accountError.value = errorMessage(err, t, te)
  } finally {
    accountBusy.value = false
  }
}

// saveCurrency stores the currency new trips are proposed in. It is a choice
// rather than a typed value, so it saves itself the way the theme does.
function saveCurrency(code: string): void {
  accountError.value = ''
  void updateMe({ default_currency: code.toUpperCase() })
    .then((updated) => session.setUser(updated))
    .catch((err) => {
      accountError.value = errorMessage(err, t, te)
    })
}

// storeAvatar sends the square the editor cut out and shows the account with it.
async function storeAvatar(picture: Blob): Promise<void> {
  avatarError.value = ''
  try {
    session.setUser(await setAvatar(picture))
  } catch (err) {
    avatarError.value = errorMessage(err, t, te)
  }
}

// removeAvatar takes the picture away, leaving the traveller everybody starts with.
async function removeAvatar(): Promise<void> {
  if (!(await confirmDialog.value?.ask(t('profile.avatarRemoveConfirm'), { danger: true }))) {
    return
  }
  avatarError.value = ''
  try {
    session.setUser(await deleteAvatar())
  } catch (err) {
    avatarError.value = errorMessage(err, t, te)
  }
}

// loadSessions reads the devices signed in to this account.
async function loadSessions(): Promise<void> {
  sessionsError.value = ''
  try {
    sessions.value = await listSessions()
  } catch (err) {
    sessionsError.value = errorMessage(err, t, te)
  }
}

// endSession signs one device out; ending the current one signs out here too.
async function endSession(item: SessionItem): Promise<void> {
  try {
    await revokeSession(item.id)
    if (item.current) {
      await session.logout()
      await router.push({ name: 'login' })
      return
    }
    await loadSessions()
  } catch (err) {
    sessionsError.value = errorMessage(err, t, te)
  }
}

// onPasswordChanged confirms the change; other devices were signed out.
async function onPasswordChanged(): Promise<void> {
  passwordMessage.value = t('password.changed')
  await loadSessions()
}

// signOut ends this session.
async function signOut(): Promise<void> {
  await session.logout()
  await router.push({ name: 'login' })
}

// removeAccount deletes the account and everything only it holds. It is asked
// for by typing the word, as deleting a trip is, because there is no undo and
// the trips this account owns go with it.
async function removeAccount(): Promise<void> {
  const confirmed = await confirmDialog.value?.ask(t('profile.deleteConfirm'), {
    danger: true,
    confirmWord: t('settings.deleteWord'),
  })
  if (!confirmed) {
    return
  }
  accountError.value = ''
  try {
    await deleteAccount()
    session.forget()
    await router.push({ name: 'login' })
  } catch (err) {
    accountError.value = errorMessage(err, t, te)
  }
}

onMounted(loadSessions)
</script>

<template>
  <section class="space-y-6">
    <h1 class="text-2xl font-bold">{{ t('profile.title') }}</h1>

    <div class="grid gap-6 lg:grid-cols-2">
      <div class="card border border-base-300 bg-base-100">
        <form class="card-body gap-3" @submit.prevent="saveAccount">
          <h2 class="card-title">{{ t('profile.account') }}</h2>

          <div class="flex items-center gap-4">
            <div class="relative">
              <UserAvatar
                v-if="user"
                :user-id="user.id"
                :has-avatar="user.has_avatar"
                :updated-at="user.avatar_updated_at"
                size="w-20"
              />
              <button
                type="button"
                class="btn btn-circle btn-xs btn-primary absolute -end-1 -bottom-1"
                :aria-label="t('profile.avatarEdit')"
                :title="t('profile.avatarEdit')"
                @click="avatarEditor?.open()"
              >
                <AppIcon name="pencil" class="size-3!" />
              </button>
              <button
                v-if="user?.has_avatar"
                type="button"
                class="btn btn-circle btn-xs btn-error absolute -start-1 -bottom-1"
                :aria-label="t('profile.avatarRemove')"
                :title="t('profile.avatarRemove')"
                @click="removeAvatar"
              >
                <AppIcon name="trash" class="size-3!" />
              </button>
            </div>
            <p class="text-sm text-base-content/70">{{ t('profile.avatarHint') }}</p>
          </div>
          <p v-if="avatarError" role="alert" class="text-sm text-error">{{ avatarError }}</p>

          <!-- The address is how the account signs in, so only an administrator
               changes it; here it is read, not edited. -->
          <div>
            <p class="text-xs text-base-content/60">{{ t('profile.email') }}</p>
            <p class="truncate">{{ user?.email }}</p>
          </div>
          <label class="floating-label">
            <span>{{ t('profile.displayName') }}</span>
            <input v-model="account.displayName" type="text" required maxlength="120" class="input w-full" :placeholder="t('profile.displayName')" />
          </label>
          <p v-if="accountMessage" role="status" class="text-sm text-success">{{ accountMessage }}</p>
          <p v-if="accountError" role="alert" class="text-sm text-error">{{ accountError }}</p>
          <button type="submit" class="btn btn-primary" :disabled="accountBusy">{{ t('common.save') }}</button>
        </form>
      </div>

      <div class="card border border-base-300 bg-base-100">
        <div class="card-body gap-3">
          <h2 class="card-title">{{ t('profile.preferences') }}</h2>
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm">{{ t('preferences.language') }}</span>
            <LanguageSelect labelled />
          </div>
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm">{{ t('preferences.theme') }}</span>
            <ThemeSelect labelled />
          </div>
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm">{{ t('profile.defaultCurrency') }}</span>
            <CurrencyMenu
              :model-value="account.currency"
              :label="t('profile.defaultCurrency')"
              @update:model-value="saveCurrency"
            />
          </div>
        </div>
      </div>

      <div class="card border border-base-300 bg-base-100">
        <div class="card-body gap-4">
          <h2 class="card-title">{{ t('preferences.display') }}</h2>

          <fieldset class="space-y-1">
            <legend class="text-sm font-medium">{{ t('preferences.dateFormat') }}</legend>
            <label v-for="format in dateFormats" :key="format" class="label flex w-fit cursor-pointer justify-start gap-2 py-1">
              <input
                type="radio"
                class="radio radio-sm radio-primary"
                name="date-format"
                :checked="format === activeDateFormat"
                @change="session.setDateFormat(format).catch(() => {})"
              />
              <span>{{ t(`preferences.dateFormats.${format}`) }}</span>
            </label>
          </fieldset>

          <fieldset class="space-y-1">
            <legend class="text-sm font-medium">{{ t('preferences.distanceFormat') }}</legend>
            <label v-for="value in distanceUnits" :key="value" class="label flex w-fit cursor-pointer justify-start gap-2 py-1">
              <input
                type="radio"
                class="radio radio-sm radio-primary"
                name="distance-format"
                :checked="value === activeUnits"
                @change="session.setUnits(value).catch(() => {})"
              />
              <span>{{ t(`preferences.distanceFormats.${value}`) }}</span>
            </label>
          </fieldset>

          <fieldset class="space-y-1">
            <legend class="text-sm font-medium">{{ t('preferences.timeFormat') }}</legend>
            <label v-for="format in timeFormats" :key="format" class="label flex w-fit cursor-pointer justify-start gap-2 py-1">
              <input
                type="radio"
                class="radio radio-sm radio-primary"
                name="time-format"
                :checked="format === activeTimeFormat"
                @change="session.setTimeFormat(format).catch(() => {})"
              />
              <span>{{ t(`preferences.timeFormats.${format}`) }}</span>
            </label>
          </fieldset>
        </div>
      </div>

      <div class="card border border-base-300 bg-base-100">
        <div class="card-body gap-3">
          <h2 class="card-title">{{ t('password.title') }}</h2>
          <p v-if="passwordMessage" role="status" class="text-sm text-success">{{ passwordMessage }}</p>
          <PasswordChangeForm @changed="onPasswordChanged" />
        </div>
      </div>

      <div class="card border border-base-300 bg-base-100 lg:col-span-2">
        <div class="card-body gap-3">
          <h2 class="card-title">{{ t('sessions.title') }}</h2>
          <p v-if="sessionsError" role="alert" class="text-sm text-error">{{ sessionsError }}</p>
          <ul class="divide-y divide-base-300">
            <li v-for="item in sessions" :key="item.id" class="flex items-center gap-3 py-3">
              <div class="min-w-0 flex-1">
                <p class="truncate font-medium">
                  {{ describeUserAgent(item.user_agent) || t('sessions.unknownDevice') }}
                  <span v-if="item.current" class="badge badge-primary badge-sm ms-2">{{ t('sessions.current') }}</span>
                </p>
                <p class="text-sm text-base-content/70">
                  {{ t('sessions.lastUsed', { time: formatDateTime(item.last_used_at, locale) }) }}
                </p>
              </div>
              <button type="button" class="btn btn-ghost btn-sm" @click="endSession(item)">{{ t('sessions.end') }}</button>
            </li>
          </ul>
        </div>
      </div>
    </div>

    <button type="button" class="btn btn-hover-outline lg:hidden" @click="signOut">
      <AppIcon name="logout" />
      {{ t('nav.signOut') }}
    </button>

    <!-- Deleting the account stands apart, below everything else, so it is
         never reached for on the way to an ordinary setting. -->
    <div class="border-t border-base-300 pt-6">
      <div class="card border border-error/40 bg-base-100">
        <div class="card-body gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div class="space-y-1">
            <h2 class="card-title text-error">{{ t('profile.deleteTitle') }}</h2>
            <p class="text-sm text-base-content/70">{{ t('profile.deleteHint') }}</p>
          </div>
          <button type="button" class="btn btn-error btn-outline w-fit shrink-0" @click="removeAccount">
            <AppIcon name="trash" />
            {{ t('profile.delete') }}
          </button>
        </div>
      </div>
    </div>

    <AvatarEditor ref="avatarEditor" @save="storeAvatar" />
    <ConfirmDialog ref="confirmDialog" />
  </section>
</template>
