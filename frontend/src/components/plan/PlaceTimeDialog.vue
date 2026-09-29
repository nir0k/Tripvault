<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { PlanItem } from '@/api/types'
import TimeInput from '@/components/TimeInput.vue'
import { minutesBetween, minutesOfDay, timeOfDay } from '@/utils/plan'

// When a place is planned: the time it is reached and either the time it is
// left or simply how long is spent there. Both come down to the two things a
// place keeps - the wished arrival and the length of the visit - so the
// schedule of the day reads them as it always did.
const emit = defineEmits<{
  save: [fields: PlaceFields]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const item = ref<PlanItem | null>(null)
const error = ref('')
const form = reactive({ start: '', mode: 'end' as 'end' | 'duration', end: '', hours: '' as number | '', minutes: '' as number | '' })

// visitMinutes is the length of the visit the form describes, or null while it
// describes none.
const visitMinutes = computed<number | null>(() => {
  if (form.mode === 'end') {
    return form.start && form.end ? minutesBetween(form.start, form.end) : null
  }
  const total = Number(form.hours || 0) * 60 + Number(form.minutes || 0)
  return total > 0 ? total : null
})

/** open shows the form filled with the place's times. */
function open(target: PlanItem): void {
  item.value = target
  error.value = ''
  const start = minutesOfDay(target.desired_time)
  const visit = target.visit_minutes
  // A visit with a known start reads best as the time it ends; one without a
  // start can only be a length.
  Object.assign(form, {
    start: target.desired_time ?? '',
    mode: start === null && visit > 0 ? 'duration' : 'end',
    end: start !== null && visit > 0 ? timeOfDay(start + visit) : '',
    hours: visit > 0 ? Math.floor(visit / 60) : '',
    minutes: visit > 0 ? visit % 60 : '',
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

// submit hands the times to the page. An end needs a start to be measured
// from, and a visit is at most a day long.
function submit(): void {
  if (form.mode === 'end' && form.end && !form.start) {
    error.value = t('place.time.endNeedsStart')
    return
  }
  const visit = visitMinutes.value ?? 0
  if (visit > 1440) {
    error.value = t('place.time.tooLong')
    return
  }
  emit('save', { desired_time: form.start || null, visit_minutes: visit })
}

// clear takes both times away.
function clear(): void {
  emit('save', { desired_time: null, visit_minutes: 0 })
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('place.time.title') }}</h2>

      <label class="floating-label">
        <span>{{ t('place.time.start') }}</span>
        <TimeInput v-model="form.start" class="input w-full" :label="t('place.time.start')" />
      </label>

      <div class="join" role="radiogroup" :aria-label="t('place.time.until')">
        <input v-model="form.mode" type="radio" value="end" class="btn btn-sm join-item" :aria-label="t('place.time.end')" />
        <input v-model="form.mode" type="radio" value="duration" class="btn btn-sm join-item" :aria-label="t('place.time.duration')" />
      </div>

      <label v-if="form.mode === 'end'" class="floating-label">
        <span>{{ t('place.time.end') }}</span>
        <TimeInput v-model="form.end" class="input w-full" :label="t('place.time.end')" />
      </label>
      <div v-else class="grid grid-cols-2 gap-3">
        <label class="floating-label">
          <span>{{ t('place.time.hours') }}</span>
          <input v-model.number="form.hours" type="number" min="0" max="24" class="input w-full" :placeholder="t('place.time.hours')" />
        </label>
        <label class="floating-label">
          <span>{{ t('place.time.minutes') }}</span>
          <input v-model.number="form.minutes" type="number" min="0" max="59" class="input w-full" :placeholder="t('place.time.minutes')" />
        </label>
      </div>
      <p class="text-xs text-base-content/60">{{ t('place.time.hint') }}</p>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button
          v-if="item && (item.desired_time || item.visit_minutes > 0)"
          type="button"
          class="btn btn-ghost text-error me-auto"
          @click="clear"
        >
          {{ t('place.time.clear') }}
        </button>
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
