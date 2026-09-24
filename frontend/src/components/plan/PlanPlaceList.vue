<script setup lang="ts">
import { ref, watch } from 'vue'
import { VueDraggable, type DraggableEvent } from 'vue-draggable-plus'
import type { Leg, PlanItem, TravelMode } from '@/api/types'
import PlanItemCard from '@/components/plan/PlanItemCard.vue'
import PlanLegRow from '@/components/plan/PlanLegRow.vue'

// The places of one list - a day's or the unassigned ones - with drag and drop
// between lists on wide screens and buttons everywhere. Changes are reported
// up; the list redraws from the document the server returns.
//
// A day's places are numbered, because that is how they are talked about: the
// third stop of the second day. An idea without a day is nowhere in an order,
// so the unassigned list carries no numbers.
const props = defineProps<{
  places: PlanItem[]
  /** The day of the list, or null for the unassigned list. */
  dayId: string | null
  currency: string
  canEdit: boolean
  draggable: boolean
  /** The legs arriving at each place, by the place's identifier. */
  legsTo?: Record<string, Leg>
}>()

const emit = defineEmits<{
  locate: [item: PlanItem]
  move: [itemId: string, dayId: string | null, position: number]
  edit: [item: PlanItem]
  pickTarget: [item: PlanItem, mode: 'move' | 'copy']
  remove: [item: PlanItem]
  legMode: [leg: Leg, mode: TravelMode]
  legEdit: [leg: Leg]
  legRetry: [leg: Leg]
}>()

// The drag library reorders this copy while dragging; it is replaced whenever
// the document changes.
const local = ref<PlanItem[]>([...props.places])
watch(() => props.places, (places) => {
  local.value = [...places]
})

// onDrop reports a place dropped into this list or moved within it.
function onDrop(event: DraggableEvent<PlanItem>): void {
  const item = event.data
  const position = event.newDraggableIndex ?? event.newIndex ?? 0
  if (item) {
    emit('move', item.id, props.dayId, position)
  }
}
</script>

<template>
  <VueDraggable
    v-model="local"
    :group="{ name: 'places', pull: true, put: true }"
    :disabled="!draggable || !canEdit"
    handle=".drag-handle"
    :animation="150"
    ghost-class="opacity-40"
    class="flex min-h-12 flex-col gap-2"
    :data-day-id="dayId ?? ''"
    @add="onDrop"
    @update="onDrop"
  >
    <div v-for="(item, index) in local" :key="item.id" :data-item-id="item.id">
      <PlanLegRow
        v-if="legsTo?.[item.id]"
        :leg="legsTo[item.id]!"
        :currency="currency"
        :can-edit="canEdit"
        @mode="(mode) => emit('legMode', legsTo![item.id]!, mode)"
        @edit="emit('legEdit', legsTo![item.id]!)"
        @retry="emit('legRetry', legsTo![item.id]!)"
      />
      <PlanItemCard
        :item="item"
        :currency="currency"
        :can-edit="canEdit"
        :first="index === 0"
        :last="index === local.length - 1"
        :number="dayId ? index + 1 : undefined"
        @locate="emit('locate', item)"
        @edit="emit('edit', item)"
        @up="emit('move', item.id, dayId, index - 1)"
        @down="emit('move', item.id, dayId, index + 1)"
        @move="emit('pickTarget', item, 'move')"
        @copy="emit('pickTarget', item, 'copy')"
        @unassign="emit('move', item.id, null, -1)"
        @remove="emit('remove', item)"
      />
    </div>
  </VueDraggable>
</template>
