<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable, type DraggableEvent } from 'vue-draggable-plus'
import type { PlanDay, PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { formatDayDate } from '@/utils/format'
import { dayColor, isVisit } from '@/utils/plan'

// The list of days beside the plan. On wide screens days are reordered by
// dragging, and a place dropped onto a day moves to its end.
//
// Each day carries the colour the map draws it in, which is what makes the
// colours on the map mean anything.
const props = defineProps<{
  days: PlanDay[]
  /** The selected day's index, or null while the stays are shown. */
  selected: number | null
  staysCount: number
  missingNights: number
  canEdit: boolean
  draggable: boolean
}>()

const emit = defineEmits<{
  select: [index: number]
  selectStays: []
  add: []
  reorder: [dayIds: string[]]
  dropPlace: [itemId: string, dayId: string]
}>()

const { t, locale } = useI18n()

const local = ref<PlanDay[]>([...props.days])
watch(() => props.days, (days) => {
  local.value = [...days]
})

// onReorder reports the new order after a day was dragged.
function onReorder(): void {
  emit('reorder', local.value.map((day) => day.id))
}

// onPlaceDrop reports a place dropped onto a day.
function onPlaceDrop(day: PlanDay, event: DraggableEvent<PlanItem>): void {
  if (event.data) {
    emit('dropPlace', event.data.id, day.id)
  }
}

// placeCount counts a day's places, leaving out its stay marks.
function placeCount(day: PlanDay): number {
  return day.items.filter(isVisit).length
}
</script>

<template>
  <nav class="space-y-3" :aria-label="t('plan.days')">
    <!-- A row of chips that scrolls sideways on phones, a column on wide screens. -->
    <VueDraggable
      v-model="local"
      :disabled="!draggable || !canEdit"
      handle=".day-handle"
      :animation="150"
      ghost-class="opacity-40"
      tag="ul"
      class="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 lg:mx-0 lg:flex-col lg:overflow-visible lg:px-0 lg:pb-0"
      @update="onReorder"
    >
      <li v-for="(day, index) in local" :key="day.id" class="shrink-0">
        <VueDraggable
          :model-value="[]"
          :group="{ name: 'places', pull: false, put: true }"
          :disabled="!draggable || !canEdit"
          draggable=".never"
          class="w-full"
          @add="(event: DraggableEvent<PlanItem>) => onPlaceDrop(day, event)"
        >
          <button
            type="button"
            class="btn h-auto min-h-10 w-full flex-nowrap justify-start gap-2 px-3 py-1.5 font-normal"
            :class="selected === index ? 'btn-primary' : 'btn-ghost'"
            :aria-current="selected === index ? 'true' : undefined"
            @click="emit('select', index)"
          >
            <span v-if="draggable && canEdit" class="day-handle cursor-grab opacity-50" aria-hidden="true">⋮⋮</span>
            <span
              class="size-2.5 shrink-0 rounded-full ring-1 ring-base-100"
              :style="{ backgroundColor: dayColor(index) }"
              aria-hidden="true"
            ></span>
            <span class="font-semibold tabular-nums lg:w-6 lg:text-right">{{ index + 1 }}</span>
            <span class="max-w-40 min-w-0 flex-1 truncate text-left lg:max-w-none">
              <span class="block truncate">{{ day.title || formatDayDate(day.date, locale) || t('plan.dayNumber', { n: index + 1 }) }}</span>
              <span v-if="day.title && day.date" class="block text-xs opacity-70">{{ formatDayDate(day.date, locale, true) }}</span>
            </span>
            <span v-if="placeCount(day) > 0" class="badge badge-sm">{{ placeCount(day) }}</span>
          </button>
        </VueDraggable>
      </li>
    </VueDraggable>

    <div class="flex gap-2 lg:flex-col">
      <button v-if="canEdit" type="button" class="btn btn-ghost btn-sm justify-start lg:w-full" @click="emit('add')">
        <AppIcon name="plus" />
        {{ t('plan.addDay') }}
      </button>

      <button
        type="button"
        class="btn btn-sm flex-1 justify-between lg:w-full lg:flex-none"
        :class="selected === null ? 'btn-primary' : 'btn-hover-outline'"
        @click="emit('selectStays')"
      >
        <span>{{ t('stay.title') }} ({{ staysCount }})</span>
        <span v-if="missingNights > 0" class="badge badge-warning badge-sm">{{ t('stay.missingShort', missingNights) }}</span>
      </button>
    </div>
  </nav>
</template>
