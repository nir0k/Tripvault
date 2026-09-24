<script setup lang="ts">
import { computed, nextTick, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { ActivityType, CostCategory, PlaceCategory, PlanItem } from '@/api/types'
import IconSelect from '@/components/IconSelect.vue'
import PlaceKindFields from '@/components/plan/PlaceKindFields.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import { normalizeAmount } from '@/utils/format'
import { costCategoryOptions } from '@/utils/plan'

// The form for a new or an existing place or activity.
defineProps<{
  /** Ranks place search results near this point first. */
  focus: { lat: number; lng: number } | null
}>()

const emit = defineEmits<{
  save: [fields: PlaceFields, item: PlanItem | null]
}>()

const { t } = useI18n()

// An empty budget category means "the one the place's category implies", so it
// leads the list without a picture of its own.
const costCategories = computed(() => [
  { value: '', label: t('place.costCategoryDefault') },
  ...costCategoryOptions(t),
])

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const locationField = useTemplateRef<InstanceType<typeof LocationField>>('locationField')
const editing = ref<PlanItem | null>(null)
const error = ref('')
// A new place takes its category's visit time unless the person set one.
const visitTouched = ref(false)
const form = reactive({
  kind: 'place' as 'place' | 'activity', activityType: 'hike' as ActivityType,
  name: '', category: 'other' as PlaceCategory, visitMinutes: 30, desiredTime: '', isOptional: false,
  address: '', lat: '', lng: '', url: '', bookingRef: '', cost: '', perPerson: false,
  costCategory: '' as CostCategory | '', description: '', osmRef: '',
})

// location binds the position fields of the form to the location field.
const location = computed<LocationModel>({
  get: () => ({ address: form.address, lat: form.lat, lng: form.lng }),
  set: (value) => Object.assign(form, value),
})

// onNamed takes the name a search result or link carried, unless one is typed.
function onNamed(name: string, ref: string): void {
  if (form.name.trim() === '' && name) {
    form.name = name
  }
  form.osmRef = ref
}

/**
 * open shows the form empty for a new place or activity, or filled for an
 * existing one. A position picked on the plan's map comes pre-filled, with its
 * address looked up.
 */
function open(item: PlanItem | null, position: { lat: number; lng: number } | null = null,
  kind: 'place' | 'activity' = 'place'): void {
  editing.value = item
  error.value = ''
  visitTouched.value = item !== null
  Object.assign(form, {
    kind: item?.kind === 'activity' ? 'activity' : item ? 'place' : kind,
    activityType: item?.activity_type ?? 'hike',
    name: item?.name ?? '',
    category: item?.category ?? 'other',
    visitMinutes: item?.visit_minutes ?? 30,
    desiredTime: item?.desired_time ?? '',
    isOptional: item?.is_optional ?? false,
    address: item?.address ?? '',
    lat: item?.lat?.toString() ?? '',
    lng: item?.lng?.toString() ?? '',
    url: item?.url ?? '',
    bookingRef: item?.booking_ref ?? '',
    cost: item?.planned_cost_amount ?? '',
    perPerson: item?.cost_per_person ?? false,
    costCategory: item?.cost_category ?? '',
    description: item?.description_md ?? '',
    osmRef: item?.osm_ref ?? '',
  })
  if (position) {
    form.lat = position.lat.toFixed(6)
    form.lng = position.lng.toFixed(6)
  }
  dialog.value?.showModal()
  if (position) {
    void nextTick(() => locationField.value?.lookUpAddress())
  }
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
  const fields: PlaceFields = {
    kind: form.kind,
    name: form.name,
    // An activity keeps the category it had, which is what its cost defaults to.
    ...(form.kind === 'activity' ? { activity_type: form.activityType } : { category: form.category }),
    desired_time: form.desiredTime || null,
    is_optional: form.isOptional,
    address: form.address,
    osm_ref: form.osmRef,
    lat: parseCoordinate(form.lat),
    lng: parseCoordinate(form.lng),
    url: form.url,
    booking_ref: form.bookingRef,
    planned_cost_amount: normalizeAmount(form.cost),
    cost_per_person: form.perPerson,
    description_md: form.description,
  }
  if (visitTouched.value) {
    fields.visit_minutes = form.visitMinutes
  }
  if (form.costCategory) {
    fields.cost_category = form.costCategory
  }
  emit('save', fields, editing.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-3 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('place.editTitle') : t('place.newTitle') }}</h2>

      <PlaceKindFields v-model:kind="form.kind" v-model:category="form.category" v-model:activity-type="form.activityType" />

      <label class="floating-label">
        <span>{{ t('place.name') }}</span>
        <input v-model="form.name" type="text" required maxlength="200" class="input w-full" :placeholder="t('place.name')" />
      </label>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('place.visitMinutes') }}</span>
          <input
            v-model.number="form.visitMinutes"
            type="number"
            min="0"
            max="1440"
            class="input w-full"
            :placeholder="visitTouched ? t('place.visitMinutes') : t('place.visitDefault')"
            @input="visitTouched = true"
          />
        </label>
        <label class="floating-label">
          <span>{{ t('place.desiredTime') }}</span>
          <input v-model="form.desiredTime" type="time" class="input w-full" />
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="form.isOptional" type="checkbox" class="checkbox" />
          <span>{{ t('place.optionalField') }}</span>
        </label>
      </div>

      <LocationField ref="locationField" v-model="location" :focus="focus" @named="onNamed" />

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('place.cost') }}</span>
          <input v-model="form.cost" type="text" inputmode="decimal" class="input w-full" :placeholder="t('place.cost')" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="label">{{ t('place.costCategory') }}</span>
          <IconSelect
            v-model="form.costCategory"
            :options="costCategories"
            :label="t('place.costCategory')"
            block
            up
          />
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="form.perPerson" type="checkbox" class="checkbox" />
          <span>{{ t('place.perPerson') }}</span>
        </label>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('place.url') }}</span>
          <input v-model="form.url" type="url" maxlength="2000" class="input w-full" :placeholder="t('place.url')" />
        </label>
        <label class="floating-label">
          <span>{{ t('place.bookingRef') }}</span>
          <input v-model="form.bookingRef" type="text" maxlength="200" class="input w-full" :placeholder="t('place.bookingRef')" />
        </label>
      </div>

      <label class="floating-label">
        <span>{{ t('place.description') }}</span>
        <textarea v-model="form.description" rows="4" maxlength="20000" class="textarea w-full" :placeholder="t('place.description')"></textarea>
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
