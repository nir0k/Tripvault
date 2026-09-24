<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { ActivityType, PlaceCategory, PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import PlaceKindFields from '@/components/plan/PlaceKindFields.vue'
import { normalizeAmount } from '@/utils/format'

// The form for a place of a report. It edits what the report knows - when the
// place was reached and what it really cost - next to the few plan fields a
// place added on the spot still needs, such as its name and category. An
// activity - a hike, a canyon - is added and edited through the same form, with
// how hard it was, and the recording of it can be attached right here: the
// times it started and finished are then read from the file unless typed.
defineProps<{
  /** Ranks place search results near this point first. */
  focus: { lat: number; lng: number } | null
}>()

const emit = defineEmits<{
  /** track is a GPX or KML file to attach once the place is stored, or null. */
  save: [fields: PlaceFields, item: PlanItem | null, track: File | null]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<PlanItem | null>(null)
const error = ref('')
const form = reactive({
  kind: 'place' as 'place' | 'activity', activityType: 'hike' as ActivityType,
  name: '', category: 'other' as PlaceCategory, address: '', lat: '', lng: '',
  actualTime: '', actualEndTime: '', actualCost: '', perPerson: false,
  // difficulty is 0 while nobody chose one.
  difficulty: 0,
})
// The recording picked in the form, which is imported after the place is saved.
const track = ref<File | null>(null)
const trackField = useTemplateRef<HTMLInputElement>('trackField')

// location binds the position fields of the form to the location field.
const location = computed<LocationModel>({
  get: () => ({ address: form.address, lat: form.lat, lng: form.lng }),
  set: (value) => Object.assign(form, value),
})

// onNamed takes the name a search result or link carried, unless one is typed.
function onNamed(name: string): void {
  if (form.name.trim() === '' && name) {
    form.name = name
  }
}

/** open shows the form for a place of the report, or for a new place or activity. */
function open(item: PlanItem | null, kind: 'place' | 'activity' = 'place'): void {
  editing.value = item
  error.value = ''
  track.value = null
  if (trackField.value) {
    trackField.value.value = ''
  }
  Object.assign(form, {
    kind: item?.kind === 'activity' ? 'activity' : item ? 'place' : kind,
    activityType: item?.activity_type ?? 'hike',
    name: item?.name ?? '',
    category: item?.category ?? 'other',
    address: item?.address ?? '',
    lat: item?.lat?.toString() ?? '',
    lng: item?.lng?.toString() ?? '',
    actualTime: item?.actual_time ?? '',
    actualEndTime: item?.actual_end_time ?? '',
    actualCost: item?.actual_cost_amount ?? '',
    perPerson: item?.cost_per_person ?? false,
    difficulty: item?.difficulty ?? 0,
  })
  dialog.value?.showModal()
}

/** close hides the form. */
function close(): void {
  dialog.value?.close()
}

/** fail shows why saving did not work, keeping the form open. */
function fail(message: string): void {
  error.value = message
}

// parseCoordinate reads a typed coordinate, accepting a decimal comma.
function parseCoordinate(value: string): number | null {
  const cleaned = value.trim().replace(',', '.')
  return cleaned === '' ? null : Number(cleaned)
}

// onTrackPicked keeps the recording chosen in the form.
function onTrackPicked(event: Event): void {
  track.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

// forgetTrack drops the recording chosen in the form before it was saved.
function forgetTrack(): void {
  track.value = null
  if (trackField.value) {
    trackField.value.value = ''
  }
}

// Whether the times left empty will be read from a recording: one chosen here,
// or the one the place already has.
const timesFromTrack = computed(() => track.value !== null || editing.value?.track != null)

// submit collects the fields and hands them to the page. A place created here
// was never in the plan, so it is marked as such.
function submit(): void {
  emit('save', {
    kind: form.kind,
    name: form.name,
    ...(form.kind === 'activity'
      ? { activity_type: form.activityType, difficulty: form.difficulty || null }
      : { category: form.category }),
    address: form.address,
    lat: parseCoordinate(form.lat),
    lng: parseCoordinate(form.lng),
    actual_time: form.actualTime || null,
    actual_end_time: form.actualEndTime || null,
    actual_cost_amount: normalizeAmount(form.actualCost),
    cost_per_person: form.perPerson,
    ...(editing.value ? {} : { status: 'unplanned' as const }),
  }, editing.value, track.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-3 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('report.editPlace') : t('report.newPlace') }}</h2>
      <p v-if="!editing" class="text-sm text-base-content/70">{{ t('report.newPlaceHint') }}</p>

      <PlaceKindFields v-model:kind="form.kind" v-model:category="form.category" v-model:activity-type="form.activityType" />
      <label class="flex flex-col gap-1">
        <span class="label">{{ t('place.name') }} <span class="text-error" aria-hidden="true">*</span></span>
        <input v-model="form.name" type="text" required maxlength="200" class="input w-full" />
      </label>

      <LocationField v-model="location" :focus="focus" @named="onNamed" />

      <div v-if="form.kind === 'activity'" class="flex flex-col gap-1">
        <span class="label">{{ t('place.difficulty') }}</span>
        <!-- Pressing the chosen level again takes the difficulty away. -->
        <div class="join w-full" role="group" :aria-label="t('place.difficulty')">
          <button
            v-for="level in 5"
            :key="level"
            type="button"
            class="btn btn-sm join-item flex-1 px-1 text-xs"
            :class="[`difficulty-${level}`, form.difficulty === level ? 'difficulty-solid' : 'difficulty-soft']"
            :aria-pressed="form.difficulty === level"
            @click="form.difficulty = form.difficulty === level ? 0 : level"
          >
            {{ t(`place.difficulties.${level}`) }}
          </button>
        </div>
      </div>

      <!-- The recording and the times belong together: the times left empty
           are read from it. -->
      <fieldset class="fieldset space-y-2 rounded-box border border-base-300 p-3">
        <legend class="fieldset-legend">{{ t('report.trackAndTime') }}</legend>
        <div class="flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-sm btn-hover-outline" @click="trackField?.click()">
            <AppIcon name="upload" />
            {{ track || editing?.track ? t('track.replace') : t('track.choose') }}
          </button>
          <span v-if="track" class="flex min-w-0 items-center gap-1 text-sm">
            <span class="truncate">{{ track.name }}</span>
            <button type="button" class="btn btn-ghost btn-xs btn-square" :aria-label="t('track.remove')" @click="forgetTrack">
              <AppIcon name="close" />
            </button>
          </span>
          <span v-else-if="editing?.track" class="truncate text-sm text-base-content/70">{{ editing.track.original_name }}</span>
        </div>
        <input ref="trackField" type="file" accept=".gpx,.kml,application/gpx+xml" class="hidden" @change="onTrackPicked" />

        <div class="grid gap-3 pt-4 sm:grid-cols-2">
          <label class="floating-label">
            <span>{{ t('report.actualTime') }}</span>
            <input v-model="form.actualTime" type="time" class="input w-full" />
          </label>
          <label class="floating-label">
            <span>{{ t('report.actualEndTime') }}</span>
            <input v-model="form.actualEndTime" type="time" class="input w-full" />
          </label>
        </div>
        <p v-if="timesFromTrack" class="text-xs text-base-content/60">{{ t('report.timesFromTrack') }}</p>
      </fieldset>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('report.actualCost') }}</span>
          <input
            v-model="form.actualCost"
            type="text"
            inputmode="decimal"
            class="input w-full"
            :placeholder="t('report.actualCost')"
          />
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="form.perPerson" type="checkbox" class="checkbox checkbox-sm" />
          <span>{{ t('place.perPerson') }}</span>
        </label>
      </div>

      <p class="text-xs text-base-content/60"><span class="text-error" aria-hidden="true">*</span> {{ t('common.requiredField') }}</p>
      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
