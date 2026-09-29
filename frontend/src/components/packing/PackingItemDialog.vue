<script setup lang="ts">
import { reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PackingItemFields } from '@/api/packing'
import type { PackingCategory, PackingItem, TripMember } from '@/api/types'
import { MAX_QUANTITY } from '@/utils/packing'

// The whole of one item: its name, how many, the note, who brings it and the
// category it is in. The line of a category adds an item by its name alone;
// this is where the rest is set, and where an item changes category without
// being dragged, which a phone makes awkward.
defineProps<{
  categories: PackingCategory[]
  /** The members who may bring something; empty through a read-only link. */
  members: TripMember[]
}>()

const emit = defineEmits<{
  save: [item: PackingItem, fields: PackingItemFields, categoryId: string | null]
}>()

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<PackingItem | null>(null)
const form = reactive({ name: '', quantity: 1, note: '', bringer: '', category: '' })
const error = ref('')

/** open shows the form filled with an item. */
function open(item: PackingItem): void {
  editing.value = item
  Object.assign(form, {
    name: item.name, quantity: item.quantity, note: item.note, bringer: item.bringer_id ?? '',
    category: item.category_id ?? '',
  })
  error.value = ''
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

// submit hands the fields to the page, with the category apart since a move
// is a request of its own.
function submit(): void {
  if (!editing.value) {
    return
  }
  const quantity = Math.min(Math.max(Math.round(Number(form.quantity) || 1), 1), MAX_QUANTITY)
  emit('save', editing.value, {
    name: form.name, quantity, note: form.note, bringer_id: form.bringer || null,
  }, form.category || null)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('packing.editItem') }}</h2>

      <label class="flex flex-col gap-1">
        <span class="label">{{ t('packing.name') }} <span class="text-error" aria-hidden="true">*</span></span>
        <input v-model="form.name" type="text" class="input w-full" required maxlength="200" />
      </label>
      <div class="grid grid-cols-[6rem_minmax(0,1fr)] gap-3">
        <label class="flex flex-col gap-1">
          <span class="label">{{ t('packing.quantity') }}</span>
          <input v-model.number="form.quantity" type="number" min="1" :max="MAX_QUANTITY" class="input w-full" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="label">{{ t('packing.category') }}</span>
          <select v-model="form.category" class="select w-full">
            <option v-for="category in categories" :key="category.id" :value="category.id">{{ category.name }}</option>
            <option value="">{{ t('packing.uncategorised') }}</option>
          </select>
        </label>
      </div>
      <label v-if="members.length > 0" class="flex flex-col gap-1">
        <span class="label">{{ t('packing.bringer') }}</span>
        <select v-model="form.bringer" class="select w-full">
          <option value="">{{ t('packing.nobody') }}</option>
          <option v-for="member in members" :key="member.user.id" :value="member.user.id">
            {{ member.user.display_name }}
          </option>
        </select>
      </label>
      <label class="flex flex-col gap-1">
        <span class="label">{{ t('packing.note') }}</span>
        <input v-model="form.note" type="text" class="input w-full" maxlength="500" :placeholder="t('packing.notePlaceholder')" />
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
