<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TransferFields } from '@/api/documents'
import { TRANSFER_KINDS, type Transfer, type TransferKind } from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import { normalizeAmount } from '@/utils/format'

// The form for a new or an existing transfer: a flight, a train, a ferry, a
// car booked to the hotel. Each end has a name of its own and, when it is
// known, a position found the way a place's is.
defineProps<{
  /** Ranks place search results near this point first. */
  focus: { lat: number; lng: number } | null
}>()

const emit = defineEmits<{
  save: [fields: TransferFields, transfer: Transfer | null]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<Transfer | null>(null)
const error = ref('')
const form = reactive({
  kind: 'flight' as TransferKind, name: '',
  fromName: '', fromAddress: '', fromLat: '', fromLng: '',
  toName: '', toAddress: '', toLat: '', toLng: '',
  departureDate: '', departureTime: '', arrivalDate: '', arrivalTime: '',
  cost: '', perPerson: false, bookingRef: '', url: '', notes: '',
})

// from and to bind the position fields of each end to its location field.
const from = computed<LocationModel>({
  get: () => ({ address: form.fromAddress, lat: form.fromLat, lng: form.fromLng }),
  set: (value) => Object.assign(form, { fromAddress: value.address, fromLat: value.lat, fromLng: value.lng }),
})
const to = computed<LocationModel>({
  get: () => ({ address: form.toAddress, lat: form.toLat, lng: form.toLng }),
  set: (value) => Object.assign(form, { toAddress: value.address, toLat: value.lat, toLng: value.lng }),
})

// named takes the name a search result carried for one end, unless one is typed.
function named(end: 'fromName' | 'toName', name: string): void {
  if (form[end].trim() === '' && name) {
    form[end] = name
  }
}

/**
 * open shows the form for an existing transfer, or for a new one departing on
 * the given date, as adding one from a day does.
 */
function open(transfer: Transfer | null, departureDate = ''): void {
  editing.value = transfer
  error.value = ''
  Object.assign(form, {
    kind: transfer?.kind ?? 'flight',
    name: transfer?.name ?? '',
    fromName: transfer?.from_name ?? '',
    fromAddress: transfer?.from_address ?? '',
    fromLat: transfer?.from_lat?.toString() ?? '',
    fromLng: transfer?.from_lng?.toString() ?? '',
    toName: transfer?.to_name ?? '',
    toAddress: transfer?.to_address ?? '',
    toLat: transfer?.to_lat?.toString() ?? '',
    toLng: transfer?.to_lng?.toString() ?? '',
    departureDate: transfer?.departure_date ?? departureDate,
    departureTime: transfer?.departure_time ?? '',
    arrivalDate: transfer?.arrival_date ?? '',
    arrivalTime: transfer?.arrival_time ?? '',
    cost: transfer?.planned_cost_amount ?? '',
    perPerson: transfer?.cost_per_person ?? false,
    bookingRef: transfer?.booking_ref ?? '',
    url: transfer?.url ?? '',
    notes: transfer?.notes_md ?? '',
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

// submit collects the fields and hands them to the page. An arrival on the
// day of departure is sent as no arrival date, which is how it is stored.
function submit(): void {
  emit('save', {
    kind: form.kind,
    name: form.name,
    from_name: form.fromName,
    from_address: form.fromAddress,
    from_lat: parseCoordinate(form.fromLat),
    from_lng: parseCoordinate(form.fromLng),
    to_name: form.toName,
    to_address: form.toAddress,
    to_lat: parseCoordinate(form.toLat),
    to_lng: parseCoordinate(form.toLng),
    departure_date: form.departureDate,
    departure_time: form.departureTime || null,
    arrival_date: form.arrivalDate && form.arrivalDate !== form.departureDate ? form.arrivalDate : null,
    arrival_time: form.arrivalTime || null,
    planned_cost_amount: normalizeAmount(form.cost),
    cost_per_person: form.perPerson,
    booking_ref: form.bookingRef,
    url: form.url,
    notes_md: form.notes,
  }, editing.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-3 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('transfer.editTitle') : t('transfer.newTitle') }}</h2>

      <div class="grid gap-3 sm:grid-cols-[auto_1fr]">
        <label class="select w-full">
          <span class="label">{{ t('transfer.kind') }}</span>
          <select v-model="form.kind">
            <option v-for="kind in TRANSFER_KINDS" :key="kind" :value="kind">{{ t(`transferKinds.${kind}`) }}</option>
          </select>
        </label>
        <label class="floating-label">
          <span>{{ t('transfer.name') }}</span>
          <input v-model="form.name" type="text" maxlength="200" class="input w-full" :placeholder="t('transfer.name')" />
        </label>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <label class="floating-label">
          <span>{{ t('transfer.departureDate') }}</span>
          <input v-model="form.departureDate" type="date" required class="input w-full" />
        </label>
        <label class="floating-label">
          <span>{{ t('transfer.departureTime') }}</span>
          <input v-model="form.departureTime" type="time" class="input w-full" />
        </label>
        <label class="floating-label">
          <span>{{ t('transfer.arrivalDate') }}</span>
          <input v-model="form.arrivalDate" type="date" class="input w-full" />
        </label>
        <label class="floating-label">
          <span>{{ t('transfer.arrivalTime') }}</span>
          <input v-model="form.arrivalTime" type="time" class="input w-full" />
        </label>
      </div>
      <p class="text-xs text-base-content/60">{{ t('transfer.arrivalDateHint') }}</p>

      <label class="floating-label">
        <span>{{ t('transfer.fromName') }}</span>
        <input v-model="form.fromName" type="text" required maxlength="200" class="input w-full" :placeholder="t('transfer.fromName')" />
      </label>
      <LocationField v-model="from" :focus="focus" :legend="t('transfer.from')" @named="(name) => named('fromName', name)" />

      <label class="floating-label">
        <span>{{ t('transfer.toName') }}</span>
        <input v-model="form.toName" type="text" required maxlength="200" class="input w-full" :placeholder="t('transfer.toName')" />
      </label>
      <LocationField v-model="to" :focus="focus" :legend="t('transfer.to')" @named="(name) => named('toName', name)" />

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('transfer.cost') }}</span>
          <AmountInput v-model="form.cost" class="input w-full" :placeholder="t('transfer.cost')" />
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="form.perPerson" type="checkbox" class="checkbox" />
          <span>{{ t('place.perPerson') }}</span>
        </label>
        <label class="floating-label">
          <span>{{ t('transfer.bookingRef') }}</span>
          <input v-model="form.bookingRef" type="text" maxlength="200" class="input w-full" :placeholder="t('transfer.bookingRef')" />
        </label>
        <label class="floating-label">
          <span>{{ t('place.url') }}</span>
          <input v-model="form.url" type="url" maxlength="2000" class="input w-full" :placeholder="t('place.url')" />
        </label>
      </div>
      <label class="floating-label">
        <span>{{ t('transfer.notes') }}</span>
        <textarea v-model="form.notes" rows="3" maxlength="20000" class="textarea w-full" :placeholder="t('transfer.notes')"></textarea>
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
