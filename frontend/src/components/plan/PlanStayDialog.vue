<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { StayFields } from '@/api/documents'
import { STAY_KINDS, type Stay, type StayKind } from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import { normalizeAmount } from '@/utils/format'

// The form for a new or an existing stay.
defineProps<{
  /** Ranks place search results near this point first. */
  focus: { lat: number; lng: number } | null
}>()

const emit = defineEmits<{
  save: [fields: StayFields, stay: Stay | null]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<Stay | null>(null)
const error = ref('')
const form = reactive({
  name: '', kind: 'hotel' as StayKind, address: '', lat: '', lng: '',
  checkInDate: '', checkInTime: '', checkOutDate: '', checkOutTime: '',
  cost: '', bookingRef: '', url: '', contacts: '', notes: '',
})

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

/**
 * open shows the form for an existing stay, or for a new one with the given
 * dates filled in, as "add a night" from a day does.
 */
function open(stay: Stay | null, dates: { checkIn?: string; checkOut?: string } = {}): void {
  editing.value = stay
  error.value = ''
  Object.assign(form, {
    name: stay?.name ?? '',
    kind: stay?.kind ?? 'hotel',
    address: stay?.address ?? '',
    lat: stay?.lat?.toString() ?? '',
    lng: stay?.lng?.toString() ?? '',
    checkInDate: stay?.check_in_date ?? dates.checkIn ?? '',
    checkInTime: stay?.check_in_time ?? '',
    checkOutDate: stay?.check_out_date ?? dates.checkOut ?? '',
    checkOutTime: stay?.check_out_time ?? '',
    cost: stay?.planned_cost_amount ?? '',
    bookingRef: stay?.booking_ref ?? '',
    url: stay?.url ?? '',
    contacts: stay?.contacts ?? '',
    notes: stay?.notes_md ?? '',
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

// submit collects the fields and hands them to the page.
function submit(): void {
  emit('save', {
    name: form.name,
    kind: form.kind,
    address: form.address,
    lat: parseCoordinate(form.lat),
    lng: parseCoordinate(form.lng),
    check_in_date: form.checkInDate,
    check_in_time: form.checkInTime || null,
    check_out_date: form.checkOutDate,
    check_out_time: form.checkOutTime || null,
    planned_cost_amount: normalizeAmount(form.cost),
    booking_ref: form.bookingRef,
    url: form.url,
    contacts: form.contacts,
    notes_md: form.notes,
  }, editing.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-3 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('stay.editTitle') : t('stay.newTitle') }}</h2>

      <div class="grid gap-3 sm:grid-cols-[1fr_auto]">
        <label class="floating-label">
          <span>{{ t('stay.name') }}</span>
          <input v-model="form.name" type="text" required maxlength="200" class="input w-full" :placeholder="t('stay.name')" />
        </label>
        <label class="select w-full">
          <span class="label">{{ t('stay.kind') }}</span>
          <select v-model="form.kind">
            <option v-for="kind in STAY_KINDS" :key="kind" :value="kind">{{ t(`stayKinds.${kind}`) }}</option>
          </select>
        </label>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <label class="floating-label">
          <span>{{ t('stay.checkIn') }}</span>
          <input v-model="form.checkInDate" type="date" required class="input w-full" :max="form.checkOutDate || undefined" />
        </label>
        <label class="floating-label">
          <span>{{ t('stay.checkInTime') }}</span>
          <input v-model="form.checkInTime" type="time" class="input w-full" />
        </label>
        <label class="floating-label">
          <span>{{ t('stay.checkOut') }}</span>
          <input v-model="form.checkOutDate" type="date" required class="input w-full" :min="form.checkInDate || undefined" />
        </label>
        <label class="floating-label">
          <span>{{ t('stay.checkOutTime') }}</span>
          <input v-model="form.checkOutTime" type="time" class="input w-full" />
        </label>
      </div>

      <LocationField v-model="location" :focus="focus" @named="onNamed" />

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('stay.cost') }}</span>
          <AmountInput v-model="form.cost" class="input w-full" :placeholder="t('stay.cost')" />
        </label>
        <label class="floating-label">
          <span>{{ t('stay.bookingRef') }}</span>
          <input v-model="form.bookingRef" type="text" maxlength="200" class="input w-full" :placeholder="t('stay.bookingRef')" />
        </label>
      </div>
      <label class="floating-label">
        <span>{{ t('place.url') }}</span>
        <input v-model="form.url" type="url" maxlength="2000" class="input w-full" :placeholder="t('place.url')" />
      </label>
      <label class="floating-label">
        <span>{{ t('stay.contacts') }}</span>
        <textarea v-model="form.contacts" rows="2" maxlength="500" class="textarea w-full" :placeholder="t('stay.contacts')"></textarea>
      </label>
      <label class="floating-label">
        <span>{{ t('stay.notes') }}</span>
        <textarea v-model="form.notes" rows="3" maxlength="20000" class="textarea w-full" :placeholder="t('stay.notes')"></textarea>
      </label>

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
