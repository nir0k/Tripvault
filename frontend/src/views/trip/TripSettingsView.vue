<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  addMember, deleteTrip, listMembers, removeMember, searchUsers, updateMember, updateTrip,
} from '@/api/trips'
import { ApiError } from '@/api/client'
import type { CoverCrop, MemberRole, RemovedDay, TripMember, TripUser } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import CoverCropDialog from '@/components/media/CoverCropDialog.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import MediaPicker from '@/components/media/MediaPicker.vue'
import ReportLanguagesCard from '@/components/ReportLanguagesCard.vue'
import TripFields, { type TripFormModel } from '@/components/TripFields.vue'
import TripShareLinks from '@/components/TripShareLinks.vue'
import UserPicker from '@/components/UserPicker.vue'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { describeRemovedDays } from '@/utils/plan'
import { listRouteName } from '@/utils/tripRoutes'

type Section = 'general' | 'members' | 'links'

const SECTIONS: readonly Section[] = ['general', 'members', 'links']

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const section = computed<Section>(() => {
  const requested = route.query.section
  return SECTIONS.find((item) => item === requested) ?? 'general'
})
const trip = computed(() => store.trip)
const canEdit = computed(() => trip.value?.role === 'owner' || trip.value?.role === 'editor')
const isOwner = computed(() => trip.value?.role === 'owner')

const form = ref<TripFormModel | null>(null)
const saving = ref(false)
const saveMessage = ref('')
const saveError = ref('')
// One dialog asks every question on this page: the days a shorter period would
// remove, taking a member off the trip, and deleting the trip itself.
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const coverPicker = useTemplateRef<InstanceType<typeof MediaPicker>>('coverPicker')
// A picture chosen as the cover is framed before it is kept, and the frame of
// the one already kept can be moved again.
const coverCrop = useTemplateRef<InstanceType<typeof CoverCropDialog>>('coverCrop')

const members = ref<TripMember[]>([])
const membersError = ref('')
const newMember = ref<TripUser | null>(null)
const newRole = ref<MemberRole>('viewer')
const memberPicker = useTemplateRef<InstanceType<typeof UserPicker>>('memberPicker')

const memberIds = computed(() => members.value.map((member) => member.user.id))

// The sections differ only by a query parameter, so they are buttons rather than
// links: a router link would mark all three as the current page and every tab
// would come out underlined.
function show(next: Section): void {
  void router.replace({ query: { section: next } })
}

// fillForm copies the stored trip into the form, discarding unsaved edits.
function fillForm(): void {
  const current = trip.value
  if (!current) {
    return
  }
  form.value = {
    title: current.title,
    summary: current.summary,
    startDate: current.start_date,
    endDate: current.end_date,
    timezone: current.timezone,
    currency: current.currency,
    travelers: current.travelers,
    budget: current.budget_amount ?? '',
  }
}

// save stores the general settings. The budget is not among them: it belongs to
// the budget screen, next to the costs it is measured against. New dates that
// remove days holding content are confirmed first.
// The trip's own picture is set on the spot rather than with the form: it is
// chosen from the pictures the trip already carries, and a choice that waited
// for a save button would be a choice nobody trusted.
async function setCover(mediaId: string | null, crop: CoverCrop | null = null): Promise<void> {
  if (!trip.value) {
    return
  }
  saveError.value = ''
  try {
    store.set(await updateTrip(trip.value.id, { cover_media_id: mediaId, cover_crop: mediaId ? crop : null }))
  } catch (err) {
    saveError.value = errorMessage(err, t, te)
  }
}

async function save(confirm = false): Promise<void> {
  if (!trip.value || !form.value) {
    return
  }
  saving.value = true
  saveMessage.value = ''
  saveError.value = ''
  try {
    store.set(await updateTrip(trip.value.id, {
      confirm,
      title: form.value.title,
      summary: form.value.summary,
      start_date: form.value.startDate,
      end_date: form.value.endDate,
      timezone: form.value.timezone,
      currency: form.value.currency.toUpperCase(),
      travelers: form.value.travelers,
    }))
    fillForm()
    saveMessage.value = t('settings.saved')
  } catch (err) {
    if (err instanceof ApiError && err.code === 'days_would_be_removed' && !confirm) {
      saving.value = false
      const days = (err.details.days ?? []) as RemovedDay[]
      const agreed = await confirmDialog.value?.ask(t('plan.confirmRemoveDays'), {
        details: describeRemovedDays(days, t, locale.value), danger: true,
      })
      if (agreed) {
        await save(true)
      }
      return
    }
    saveError.value = errorMessage(err, t, te)
  } finally {
    saving.value = false
  }
}

// loadMembers reads the people on the trip.
async function loadMembers(): Promise<void> {
  if (!trip.value) {
    return
  }
  membersError.value = ''
  try {
    members.value = await listMembers(trip.value.id)
  } catch (err) {
    membersError.value = errorMessage(err, t, te)
  }
}

// add gives the chosen person access with the chosen role.
async function add(): Promise<void> {
  if (!trip.value || !newMember.value) {
    return
  }
  membersError.value = ''
  try {
    await addMember(trip.value.id, newMember.value.id, newRole.value)
    memberPicker.value?.reset()
    await loadMembers()
  } catch (err) {
    membersError.value = errorMessage(err, t, te)
  }
}

// changeRole switches a member between editor and viewer.
async function changeRole(member: TripMember, event: Event): Promise<void> {
  if (!trip.value) {
    return
  }
  membersError.value = ''
  try {
    await updateMember(trip.value.id, member.user.id, (event.target as HTMLSelectElement).value as MemberRole)
    await loadMembers()
  } catch (err) {
    membersError.value = errorMessage(err, t, te)
    await loadMembers()
  }
}

// removeOne takes a member off the trip once the owner has confirmed.
async function removeOne(member: TripMember): Promise<void> {
  if (!trip.value) {
    return
  }
  const agreed = await confirmDialog.value?.ask(
    t('members.removeConfirm', { name: member.user.display_name }), { danger: true },
  )
  if (!agreed) {
    return
  }
  membersError.value = ''
  try {
    await removeMember(trip.value.id, member.user.id)
  } catch (err) {
    membersError.value = errorMessage(err, t, te)
  }
  await loadMembers()
}

// destroy deletes the trip for everybody. Nothing on this page can bring it
// back, so the word has to be typed before the dialog lets the click through.
async function destroy(): Promise<void> {
  const current = trip.value
  if (!current) {
    return
  }
  const agreed = await confirmDialog.value?.ask(t('settings.deleteConfirm', { title: current.title }), {
    danger: true, confirmWord: t('settings.deleteWord'),
  })
  if (!agreed) {
    return
  }
  saveError.value = ''
  try {
    await deleteTrip(current.id)
    // Back to the list the trip was found in: a report's is Reports.
    await router.push({ name: listRouteName(current.kind) })
  } catch (err) {
    saveError.value = errorMessage(err, t, te)
  }
}

watch(() => trip.value?.id, () => {
  fillForm()
  void loadMembers()
}, { immediate: true })
</script>

<template>
  <div v-if="trip && form" class="max-w-5xl space-y-6">
    <nav role="tablist" class="tabs tabs-border overflow-x-auto" :aria-label="t('settings.sectionsLabel')">
      <button
        v-for="item in SECTIONS"
        :key="item"
        type="button"
        role="tab"
        class="tab whitespace-nowrap"
        :class="{ 'tab-active': section === item }"
        :aria-selected="section === item"
        @click="show(item)"
      >
        {{ t(`settings.sections.${item}`) }}
      </button>
    </nav>

    <template v-if="section === 'general'">
      <div class="card border border-base-300 bg-base-100">
        <form class="card-body gap-3" @submit.prevent="save()">
          <p v-if="!canEdit" class="text-sm text-base-content/70">{{ t('settings.readOnly') }}</p>
          <fieldset :disabled="!canEdit" class="contents">
            <TripFields v-model="form" extended />
          </fieldset>
          <p v-if="saveMessage" role="status" class="text-sm text-success">{{ saveMessage }}</p>
          <p v-if="saveError" role="alert" class="text-sm text-error">{{ saveError }}</p>
          <div v-if="canEdit" class="card-actions justify-end">
            <button type="submit" class="btn btn-primary" :disabled="saving">{{ t('common.save') }}</button>
          </div>
        </form>
      </div>

      <ReportLanguagesCard
        v-if="trip.kind === 'report'"
        :trip="trip"
        :can-edit="canEdit"
        @changed="(next) => store.set(next)"
      />

      <div v-if="canEdit || trip.cover_media_id" class="card border border-base-300 bg-base-100">
        <div class="card-body gap-3">
          <h2 class="card-title">{{ t('settings.cover') }}</h2>
          <p class="text-sm text-base-content/70">{{ t('settings.coverHint') }}</p>
          <div class="flex flex-wrap items-center gap-3">
            <div class="aspect-[5/2] w-56 overflow-hidden rounded-box border border-base-300">
              <MediaImage v-if="trip.cover_media_id" :id="trip.cover_media_id" :size="640" :crop="trip.cover_crop" fill />
              <div v-else class="flex size-full items-center justify-center text-base-content/40">
                <AppIcon name="image" />
              </div>
            </div>
            <div v-if="canEdit" class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-sm btn-hover-outline" @click="coverPicker?.open()">
                {{ trip.cover_media_id ? t('settings.changeCover') : t('settings.chooseCover') }}
              </button>
              <button
                v-if="trip.cover_media_id"
                type="button"
                class="btn btn-sm btn-hover-outline"
                @click="coverCrop?.open(trip.cover_media_id, trip.cover_crop)"
              >
                {{ t('settings.cropCover') }}
              </button>
              <button
                v-if="trip.cover_media_id"
                type="button"
                class="btn btn-ghost btn-sm"
                @click="setCover(null)"
              >
                {{ t('settings.removeCover') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="isOwner" class="card border border-error/40 bg-base-100">
        <div class="card-body gap-4">
          <h2 class="card-title">{{ t('settings.ownerActions') }}</h2>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="text-sm text-base-content/70">{{ t('settings.deleteHint') }}</p>
            <button type="button" class="btn btn-error btn-hover-outline" @click="destroy">
              <AppIcon name="trash" />
              {{ t('settings.delete') }}
            </button>
          </div>
        </div>
      </div>
    </template>

    <TripShareLinks v-else-if="section === 'links'" :trip-id="trip.id" :is-owner="isOwner" />

    <div v-else class="card border border-base-300 bg-base-100">
      <div class="card-body gap-4">
        <p v-if="!isOwner" class="text-sm text-base-content/70">{{ t('members.onlyOwner') }}</p>
        <p v-if="membersError" role="alert" class="text-sm text-error">{{ membersError }}</p>

        <ul class="divide-y divide-base-300">
          <li v-for="member in members" :key="member.user.id" class="flex flex-wrap items-center gap-3 py-3">
            <div class="min-w-0 flex-1">
              <p class="truncate font-medium">{{ member.user.display_name }}</p>
              <p class="truncate text-sm text-base-content/70">{{ member.user.email }}</p>
            </div>
            <select
              v-if="isOwner && member.role !== 'owner'"
              class="select select-sm w-36"
              :value="member.role"
              :aria-label="t('members.role')"
              @change="changeRole(member, $event)"
            >
              <option value="editor">{{ t('trips.roles.editor') }}</option>
              <option value="viewer">{{ t('trips.roles.viewer') }}</option>
            </select>
            <span v-else class="badge badge-outline">{{ t(`trips.roles.${member.role}`) }}</span>
            <button
              v-if="isOwner && member.role !== 'owner'"
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              :aria-label="t('members.remove', { name: member.user.display_name })"
              @click="removeOne(member)"
            >
              <AppIcon name="trash" />
            </button>
          </li>
        </ul>

        <form v-if="isOwner" class="flex flex-col gap-2 sm:flex-row" @submit.prevent="add">
          <div class="flex-1">
            <UserPicker ref="memberPicker" v-model="newMember" :search="searchUsers" :exclude="memberIds" />
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
      </div>
    </div>

    <MediaPicker ref="coverPicker" :trip-id="trip.id" @choose="(media) => coverCrop?.open(media.id)" />
    <CoverCropDialog ref="coverCrop" @save="(mediaId, crop) => setCover(mediaId, crop)" />
    <ConfirmDialog ref="confirmDialog" />
  </div>
</template>
