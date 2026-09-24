<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LegChanges } from '@/api/documents'
import { TRAVEL_MODES, type Leg, type TravelMode } from '@/api/types'
import { fromMetres, normalizeAmount, toMetres } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// Typed values for a leg: distance and time override the calculation until
// cleared, plus the cost and a note such as "bus Strætó 51". A report has no
// row of mode buttons, so there the form also chooses the means, and it takes
// what the journey really cost.
const props = defineProps<{
  /** The leg belongs to a report, whose form also recalculates the leg. */
  report?: boolean
  /** Something is being saved or calculated. */
  busy?: boolean
}>()

const emit = defineEmits<{
  save: [leg: Leg, changes: LegChanges]
  recalculate: [leg: Leg]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const leg = ref<Leg | null>(null)
const error = ref('')
// The distance is typed in whatever the reader reads, and stored in metres like
// every other: somebody who measures in miles should not have to convert first.
const form = reactive({ mode: 'walk' as TravelMode, distance: '', hours: '', minutes: '', cost: '', actual: '', note: '' })

const distanceLabel = computed(() => t(`leg.distanceIn.${activeUnits.value}`))

// calculated is the provider's own distance, shown as the placeholder so that
// clearing the field visibly returns to it.
const calculated = computed(() => {
  const metres = leg.value?.calculated_distance_m
  return metres == null ? distanceLabel.value : String(fromMetres(metres, activeUnits.value))
})

/** open shows the form for a leg, filled with its typed values. */
function open(next: Leg): void {
  leg.value = next
  error.value = ''
  const seconds = next.manual_duration ? next.duration_s ?? 0 : null
  Object.assign(form, {
    mode: next.mode,
    distance: next.manual_distance && next.distance_m !== null
      ? String(fromMetres(next.distance_m, activeUnits.value))
      : '',
    hours: seconds === null ? '' : String(Math.floor(seconds / 3600)),
    minutes: seconds === null ? '' : String(Math.round((seconds % 3600) / 60)),
    cost: next.planned_cost_amount ?? '',
    actual: next.actual_cost_amount ?? '',
    note: next.note,
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

// submit turns the typed distance and time into metres and seconds; empty
// fields return to the calculated values.
function submit(): void {
  if (!leg.value) {
    return
  }
  const typed = form.distance.trim().replace(',', '.')
  const timed = form.hours.trim() !== '' || form.minutes.trim() !== ''
  const changes: LegChanges = {
    distance_m: typed === '' ? null : toMetres(Number(typed), activeUnits.value),
    duration_s: timed ? (Number(form.hours || 0) * 60 + Number(form.minutes || 0)) * 60 : null,
    planned_cost_amount: normalizeAmount(form.cost),
    note: form.note,
  }
  if (props.report) {
    changes.actual_cost_amount = normalizeAmount(form.actual)
    // A new mode asks for a new calculation, so it is only sent when it moved.
    if (form.mode !== leg.value.mode) {
      changes.mode = form.mode
    }
  }
  emit('save', leg.value, changes)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('leg.editTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('leg.manualHint') }}</p>

      <label v-if="report" class="floating-label">
        <span>{{ t('leg.mode') }}</span>
        <select v-model="form.mode" class="select w-full">
          <option v-for="mode in TRAVEL_MODES" :key="mode" :value="mode">{{ t(`modes.${mode}`) }}</option>
        </select>
      </label>
      <label class="floating-label">
        <span>{{ distanceLabel }}</span>
        <input v-model="form.distance" type="text" inputmode="decimal" class="input w-full" :placeholder="calculated" />
      </label>
      <div class="grid grid-cols-2 gap-3">
        <label class="floating-label">
          <span>{{ t('leg.hours') }}</span>
          <input v-model="form.hours" type="number" min="0" max="168" class="input w-full" :placeholder="t('leg.hours')" />
        </label>
        <label class="floating-label">
          <span>{{ t('leg.minutes') }}</span>
          <input v-model="form.minutes" type="number" min="0" max="59" class="input w-full" :placeholder="t('leg.minutes')" />
        </label>
      </div>
      <label class="floating-label">
        <span>{{ t('leg.cost') }}</span>
        <input v-model="form.cost" type="text" inputmode="decimal" class="input w-full" :placeholder="t('leg.cost')" />
      </label>
      <label v-if="report" class="floating-label">
        <span>{{ t('report.actualCost') }}</span>
        <input v-model="form.actual" type="text" inputmode="decimal" class="input w-full" :placeholder="t('report.actualCost')" />
      </label>
      <label class="floating-label">
        <span>{{ t('leg.note') }}</span>
        <input v-model="form.note" type="text" maxlength="500" class="input w-full" :placeholder="t('leg.notePlaceholder')" />
      </label>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button
          v-if="report && leg"
          type="button"
          class="btn btn-ghost me-auto"
          :disabled="busy"
          :title="t('leg.recalculateHint')"
          @click="emit('recalculate', leg)"
        >
          <span v-if="busy" class="loading loading-spinner loading-xs"></span>
          {{ t('leg.recalculate') }}
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
