<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientConfig } from '@/api/config'
import {
  addIdeaMember, inviteIdeaMember, leaveIdeaList, listIdeaInvitations, listIdeaLists, listIdeaMembers,
  removeIdeaMember, revokeIdeaInvitation, updateIdeaMember,
} from '@/api/ideas'
import { searchUsers } from '@/api/trips'
import type { IdeaList, IdeaMember, MemberRole, TripInvitation, TripUser } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import UserPicker from '@/components/UserPicker.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'

// Who the reader's ideas are shared with, and the lists shared with the
// reader. The whole list is shared: a viewer reads every idea, an editor also
// adds, changes and deletes them, and only the owner decides who is a member.
// People are added from the existing accounts or, while mail is delivered,
// invited by email, as on a trip. A list shared with the reader can be left.

const emit = defineEmits<{ changed: [] }>()

const { t, te, locale } = useI18n()
const session = useSessionStore()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const memberPicker = useTemplateRef<InstanceType<typeof UserPicker>>('memberPicker')

const members = ref<IdeaMember[]>([])
const invitations = ref<TripInvitation[]>([])
const sharedWithMe = ref<IdeaList[]>([])
const newMember = ref<TripUser | null>(null)
const newRole = ref<MemberRole>('viewer')
const invitationEmail = ref('')
const mailEnabled = ref(false)
const inviting = ref(false)
const error = ref('')

// The reader cannot share their ideas with themselves or twice with anybody.
const excluded = computed(() => [session.user?.id ?? '', ...members.value.map((member) => member.user.id)])

// load reads the members, the invitations and the lists shared with the reader.
async function load(): Promise<void> {
  error.value = ''
  try {
    const [loadedMembers, lists, config] = await Promise.all([listIdeaMembers(), listIdeaLists(), getClientConfig()])
    members.value = loadedMembers
    sharedWithMe.value = lists.filter((list) => list.role !== 'owner')
    mailEnabled.value = config.mail_enabled
    invitations.value = mailEnabled.value ? await listIdeaInvitations() : []
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

// run does one change, reporting a failure, then reads everything again.
async function run(change: () => Promise<unknown>): Promise<void> {
  error.value = ''
  try {
    await change()
    emit('changed')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
  await load()
}

// add shares the reader's ideas with the chosen person.
async function add(): Promise<void> {
  const user = newMember.value
  if (user) {
    await run(async () => {
      await addIdeaMember(user.id, newRole.value)
      memberPicker.value?.reset()
    })
  }
}

// changeRole switches a member between editor and viewer.
async function changeRole(member: IdeaMember, event: Event): Promise<void> {
  await run(() => updateIdeaMember(member.user.id, (event.target as HTMLSelectElement).value as MemberRole))
}

// remove stops sharing the reader's ideas with a member once confirmed.
async function remove(member: IdeaMember): Promise<void> {
  if (await confirmDialog.value?.ask(t('ideas.sharing.removeConfirm', { name: member.user.display_name }), { danger: true })) {
    await run(() => removeIdeaMember(member.user.id))
  }
}

// invite emails an invitation to an address that may not have an account yet.
async function invite(): Promise<void> {
  const email = invitationEmail.value.trim()
  if (!email) {
    return
  }
  inviting.value = true
  await run(async () => {
    await inviteIdeaMember(email, newRole.value, locale.value)
    invitationEmail.value = ''
  })
  inviting.value = false
}

// leave takes the reader off a list shared with them once confirmed.
async function leave(list: IdeaList): Promise<void> {
  if (await confirmDialog.value?.ask(t('ideas.sharing.leaveConfirm', { name: list.owner.display_name }), { danger: true })) {
    await run(() => leaveIdeaList(list.owner.id))
  }
}

/** open shows the dialog with what is stored now. */
function open(): void {
  newMember.value = null
  invitationEmail.value = ''
  dialog.value?.showModal()
  void load()
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <div class="modal-box flex max-w-2xl flex-col gap-4">
      <h2 class="text-lg font-bold">{{ t('ideas.sharing.title') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('ideas.sharing.hint') }}</p>
      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

      <ul v-if="members.length > 0" class="divide-y divide-base-300">
        <li v-for="member in members" :key="member.user.id" class="flex flex-wrap items-center gap-3 py-3">
          <div class="min-w-0 flex-1">
            <p class="truncate font-medium">{{ member.user.display_name }}</p>
            <p class="truncate text-sm text-base-content/70">{{ member.user.email }}</p>
          </div>
          <select class="select select-sm w-36" :value="member.role" :aria-label="t('members.role')" @change="changeRole(member, $event)">
            <option value="editor">{{ t('trips.roles.editor') }}</option>
            <option value="viewer">{{ t('trips.roles.viewer') }}</option>
          </select>
          <button
            type="button"
            class="btn btn-ghost btn-sm btn-square"
            :aria-label="t('members.remove', { name: member.user.display_name })"
            :title="t('members.remove', { name: member.user.display_name })"
            @click="remove(member)"
          >
            <AppIcon name="trash" />
          </button>
        </li>
      </ul>
      <p v-else class="text-sm text-base-content/60">{{ t('ideas.sharing.nobody') }}</p>

      <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="add">
        <div class="flex-1">
          <UserPicker ref="memberPicker" v-model="newMember" :search="searchUsers" :exclude="excluded" />
        </div>
        <select v-model="newRole" class="select w-full sm:w-36" :aria-label="t('members.role')">
          <option value="editor">{{ t('trips.roles.editor') }}</option>
          <option value="viewer">{{ t('trips.roles.viewer') }}</option>
        </select>
        <button type="submit" class="btn btn-primary" :disabled="!newMember">
          <AppIcon name="plus" />
          {{ t('members.add') }}
        </button>
      </form>

      <template v-if="mailEnabled">
        <div class="divider my-0">{{ t('members.orInvite') }}</div>
        <form class="flex flex-col gap-2 sm:flex-row" @submit.prevent="invite">
          <input v-model="invitationEmail" type="email" required class="input flex-1" :placeholder="t('members.inviteEmail')" />
          <select v-model="newRole" class="select w-full sm:w-36" :aria-label="t('members.role')">
            <option value="editor">{{ t('trips.roles.editor') }}</option>
            <option value="viewer">{{ t('trips.roles.viewer') }}</option>
          </select>
          <button type="submit" class="btn btn-primary" :disabled="inviting">
            <span v-if="inviting" class="loading loading-spinner loading-sm"></span>
            {{ t('members.invite') }}
          </button>
        </form>
        <ul v-if="invitations.length > 0" class="divide-y divide-base-300 border-t border-base-300">
          <li v-for="invitation in invitations" :key="invitation.id" class="flex flex-wrap items-center gap-3 py-3">
            <div class="min-w-0 flex-1">
              <p class="truncate">{{ invitation.email }}</p>
              <p class="text-xs text-base-content/60">{{ t(`trips.roles.${invitation.role}`) }}</p>
            </div>
            <span class="badge badge-outline">{{ t(`mail.status.${invitation.status}`) }}</span>
            <button
              v-if="invitation.status === 'pending'"
              type="button"
              class="btn btn-ghost btn-sm"
              @click="run(() => revokeIdeaInvitation(invitation.id))"
            >
              {{ t('members.revokeInvitation') }}
            </button>
          </li>
        </ul>
      </template>

      <template v-if="sharedWithMe.length > 0">
        <h3 class="font-semibold">{{ t('ideas.sharing.sharedWithMe') }}</h3>
        <ul class="divide-y divide-base-300">
          <li v-for="list in sharedWithMe" :key="list.owner.id" class="flex flex-wrap items-center gap-3 py-3">
            <div class="min-w-0 flex-1">
              <p class="truncate font-medium">{{ list.owner.display_name }}</p>
              <p class="truncate text-sm text-base-content/70">{{ list.owner.email }}</p>
            </div>
            <span class="badge badge-outline">{{ t(`trips.roles.${list.role}`) }}</span>
            <button type="button" class="btn btn-ghost btn-sm" @click="leave(list)">{{ t('ideas.sharing.leave') }}</button>
          </li>
        </ul>
      </template>

      <div class="modal-action">
        <form method="dialog"><button type="submit" class="btn">{{ t('common.close') }}</button></form>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop"><button type="submit">{{ t('common.close') }}</button></form>
    <ConfirmDialog ref="confirmDialog" />
  </dialog>
</template>
