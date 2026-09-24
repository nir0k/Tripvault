<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanDay, PlanItem } from '@/api/types'
import { formatDayDate } from '@/utils/format'

// Asks which day a place moves or is copied to. It is how places change days
// where dragging is not available, and how an unassigned place gets a day.
const props = defineProps<{ days: PlanDay[] }>()

const emit = defineEmits<{
  choose: [item: PlanItem, mode: 'move' | 'copy', dayId: string | null]
}>()

const { t, locale } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const item = ref<PlanItem | null>(null)
const mode = ref<'move' | 'copy'>('move')
// "" stands for the unassigned list.
const target = ref('')

/** open asks where to move or copy a place, proposing another day than its own. */
function open(next: PlanItem, nextMode: 'move' | 'copy'): void {
  item.value = next
  mode.value = nextMode
  target.value = props.days.find((day) => day.id !== next.day_id)?.id ?? ''
  dialog.value?.showModal()
}

// submit reports the choice.
function submit(): void {
  if (item.value) {
    emit('choose', item.value, mode.value, target.value || null)
  }
  dialog.value?.close()
}

// dayLabel names a day in the list of choices.
function dayLabel(day: PlanDay): string {
  const parts = [t('plan.dayNumber', { n: day.position + 1 })]
  if (day.date) {
    parts.push(formatDayDate(day.date, locale.value, true))
  }
  if (day.title) {
    parts.push(day.title)
  }
  return parts.join(' · ')
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ mode === 'move' ? t('plan.moveTitle', { name: item?.name ?? '' }) : t('plan.copyTitle', { name: item?.name ?? '' }) }}</h2>
      <select v-model="target" class="select w-full" :aria-label="t('plan.targetDay')">
        <option v-for="day in days" :key="day.id" :value="day.id" :disabled="mode === 'move' && day.id === item?.day_id">
          {{ dayLabel(day) }}
        </option>
        <option value="" :disabled="mode === 'move' && item?.day_id === null">{{ t('plan.unassigned') }}</option>
      </select>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.confirm') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
