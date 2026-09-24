<script setup lang="ts">
import { computed, onMounted, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { createUser, listUsers, resetPassword, updateUser, type UserChanges } from '@/api/admin'
import type { AdminUser } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'
import { generatePassword } from '@/utils/password'

type DialogMode = 'create' | 'rename' | 'reset' | 'confirm'

interface PendingChange {
  user: AdminUser
  changes: UserChanges
  question: string
}

const { t, te, locale } = useI18n()

const users = ref<AdminUser[]>([])
const loadError = ref('')
const notice = ref('')

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const mode = ref<DialogMode>('create')
const target = ref<AdminUser | null>(null)
const pending = ref<PendingChange | null>(null)
const form = reactive({ email: '', displayName: '', password: '', isAdmin: false })
const dialogError = ref('')
const busy = ref(false)

const dialogTitle = computed(() => {
  switch (mode.value) {
    case 'create':
      return t('users.createTitle')
    case 'rename':
      return t('users.renameTitle', { name: target.value?.display_name ?? '' })
    case 'reset':
      return t('users.resetTitle', { name: target.value?.display_name ?? '' })
    default:
      return t('users.confirmTitle')
  }
})

// load reads every account.
async function load(): Promise<void> {
  loadError.value = ''
  try {
    users.value = await listUsers()
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// open prepares the dialog for one action and shows it.
function open(nextMode: DialogMode, user: AdminUser | null = null): void {
  mode.value = nextMode
  target.value = user
  dialogError.value = ''
  form.email = ''
  form.displayName = user?.display_name ?? ''
  form.password = nextMode === 'create' || nextMode === 'reset' ? generatePassword() : ''
  form.isAdmin = false
  dialog.value?.showModal()
}

// ask confirms a change to somebody's rights or access before sending it.
function ask(user: AdminUser, changes: UserChanges, question: string): void {
  pending.value = { user, changes, question }
  open('confirm', user)
}

// close hides the dialog.
function close(): void {
  dialog.value?.close()
}

// submit performs the dialog's action and refreshes the list.
async function submit(): Promise<void> {
  busy.value = true
  dialogError.value = ''
  notice.value = ''
  try {
    const user = target.value
    if (mode.value === 'create') {
      const created = await createUser({
        email: form.email.trim(),
        display_name: form.displayName,
        password: form.password,
        is_admin: form.isAdmin,
      })
      notice.value = t('users.created', { name: created.display_name, password: form.password })
    } else if (mode.value === 'rename' && user) {
      await updateUser(user.id, { display_name: form.displayName })
    } else if (mode.value === 'reset' && user) {
      await resetPassword(user.id, form.password)
      notice.value = t('users.passwordReset', { name: user.display_name, password: form.password })
    } else if (mode.value === 'confirm' && pending.value) {
      await updateUser(pending.value.user.id, pending.value.changes)
    }
    close()
    await load()
  } catch (err) {
    dialogError.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// toggleAdmin grants or withdraws administration after confirmation.
function toggleAdmin(user: AdminUser): void {
  const question = user.is_admin
    ? t(user.is_self ? 'users.confirmRevokeSelf' : 'users.confirmRevoke', { name: user.display_name })
    : t('users.confirmGrant', { name: user.display_name })
  ask(user, { is_admin: !user.is_admin }, question)
}

// toggleActive deactivates or reactivates an account after confirmation.
function toggleActive(user: AdminUser): void {
  const question = user.is_active
    ? t(user.is_self ? 'users.confirmDeactivateSelf' : 'users.confirmDeactivate', { name: user.display_name })
    : t('users.confirmActivate', { name: user.display_name })
  ask(user, { is_active: !user.is_active }, question)
}

onMounted(load)
</script>

<template>
  <section class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('users.title') }}</h1>
      <button type="button" class="btn btn-primary" @click="open('create')">
        <AppIcon name="plus" />
        {{ t('users.create') }}
      </button>
    </div>

    <p v-if="loadError" role="alert" class="text-error">{{ loadError }}</p>
    <div v-if="notice" role="status" class="alert alert-success">
      <span class="break-all">{{ notice }}</span>
      <button type="button" class="btn btn-ghost btn-sm" @click="notice = ''">{{ t('common.close') }}</button>
    </div>

    <ul class="divide-y divide-base-300 rounded-box border border-base-300">
      <li v-for="user in users" :key="user.id" class="flex flex-wrap items-center gap-3 p-4" :data-user-email="user.email">
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2 font-medium">
            <span class="truncate">{{ user.display_name }}</span>
            <span v-if="user.is_self" class="badge badge-ghost badge-sm">{{ t('users.you') }}</span>
            <span v-if="user.is_admin" class="badge badge-primary badge-sm">{{ t('users.admin') }}</span>
            <span v-if="!user.is_active" class="badge badge-error badge-sm">{{ t('users.inactive') }}</span>
            <span v-if="user.must_change_password" class="badge badge-warning badge-sm">{{ t('users.temporaryPassword') }}</span>
          </p>
          <p class="truncate text-sm text-base-content/70">{{ user.email }}</p>
          <p class="text-sm text-base-content/70">
            {{ user.last_login_at ? t('users.lastLogin', { time: formatDateTime(user.last_login_at, locale) }) : t('users.neverLoggedIn') }}
          </p>
        </div>

        <div class="dropdown dropdown-end">
          <div tabindex="0" role="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('users.actions', { name: user.display_name })">
            <AppIcon name="dots" />
          </div>
          <ul tabindex="0" class="menu dropdown-content z-10 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
            <li><button type="button" @click="open('rename', user)">{{ t('users.rename') }}</button></li>
            <li><button type="button" @click="open('reset', user)">{{ t('users.resetPassword') }}</button></li>
            <li>
              <button type="button" @click="toggleAdmin(user)">
                {{ user.is_admin ? t('users.revokeAdmin') : t('users.grantAdmin') }}
              </button>
            </li>
            <li>
              <button type="button" @click="toggleActive(user)">
                {{ user.is_active ? t('users.deactivate') : t('users.activate') }}
              </button>
            </li>
          </ul>
        </div>
      </li>
    </ul>

    <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
      <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
        <h2 class="text-lg font-bold">{{ dialogTitle }}</h2>

        <template v-if="mode === 'create'">
          <label class="floating-label">
            <span>{{ t('users.email') }}</span>
            <input v-model="form.email" type="email" required autocomplete="off" class="input w-full" :placeholder="t('users.email')" />
          </label>
        </template>

        <label v-if="mode === 'create' || mode === 'rename'" class="floating-label">
          <span>{{ t('users.displayName') }}</span>
          <input v-model="form.displayName" type="text" required maxlength="120" class="input w-full" :placeholder="t('users.displayName')" />
        </label>

        <template v-if="mode === 'create' || mode === 'reset'">
          <label class="floating-label">
            <span>{{ t('users.temporaryPasswordField') }}</span>
            <input v-model="form.password" type="text" required minlength="8" autocomplete="off" class="input w-full font-mono" :placeholder="t('users.temporaryPasswordField')" />
          </label>
          <p class="text-sm text-base-content/70">{{ t('users.temporaryPasswordHint') }}</p>
        </template>

        <label v-if="mode === 'create'" class="label cursor-pointer justify-start gap-3">
          <input v-model="form.isAdmin" type="checkbox" class="checkbox" />
          <span>{{ t('users.makeAdmin') }}</span>
        </label>

        <p v-if="mode === 'confirm'">{{ pending?.question }}</p>

        <p v-if="dialogError" role="alert" class="text-sm text-error">{{ dialogError }}</p>

        <div class="modal-action">
          <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="busy">
            <span v-if="busy" class="loading loading-spinner loading-sm"></span>
            {{ mode === 'create' ? t('users.create') : t('common.confirm') }}
          </button>
        </div>
      </form>
      <form method="dialog" class="modal-backdrop">
        <button type="submit">{{ t('common.close') }}</button>
      </form>
    </dialog>
  </section>
</template>
