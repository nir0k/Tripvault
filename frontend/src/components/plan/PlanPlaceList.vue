<script setup lang="ts">
import { ref, watch } from 'vue'
import { VueDraggable, type DraggableEvent } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { Leg, PlanItem, TravelMode } from '@/api/types'
import PlanInsertButton from '@/components/plan/PlanInsertButton.vue'
import PlanItemCard from '@/components/plan/PlanItemCard.vue'
import PlanLegRow from '@/components/plan/PlanLegRow.vue'
import PlanQuickAdd from '@/components/plan/PlanQuickAdd.vue'

// The places of one list - a day's or the unassigned ones - with drag and drop
// between lists on wide screens and buttons everywhere. Changes are reported
// up; the list redraws from the document the server returns.
//
// A day's places are numbered, because that is how they are talked about: the
// third stop of the second day. An idea without a day is nowhere in an order,
// so the unassigned list carries no numbers.
//
// Every gap of the list - before each place, on its leg when it has one, and
// after the last - offers a "+" that begins a new place or activity right
// there, as a tile of a single field.
const props = withDefaults(defineProps<{
  places: PlanItem[]
  /** The day of the list, or null for the unassigned list. */
  dayId: string | null
  currency: string
  canEdit: boolean
  draggable: boolean
  /** The legs arriving at each place, by the place's identifier. */
  legsTo?: Record<string, Leg>
  /** The day's colour on the map, which the places' numbers are filled with. */
  color?: string
  /** Ranks the search of a new place near this point first. */
  focus?: { lat: number; lng: number } | null
  /**
   * create stores a new element at a position of this list, resolving to
   * whether it was stored. Without it the list offers no "+".
   */
  create?: (position: number, fields: PlaceFields) => Promise<boolean>
  /** Whether the list ends with a "+" of its own; a day ending at a stay puts it on the last leg. */
  trailing?: boolean
}>(), { legsTo: undefined, color: undefined, focus: null, create: undefined, trailing: true })

const emit = defineEmits<{
  locate: [item: PlanItem]
  move: [itemId: string, dayId: string | null, position: number]
  edit: [item: PlanItem]
  update: [item: PlanItem, fields: PlaceFields]
  editTime: [item: PlanItem]
  editCost: [item: PlanItem]
  importTrack: [item: PlanItem, file: File]
  removeTrack: [item: PlanItem]
  pickTarget: [item: PlanItem, mode: 'move' | 'copy']
  remove: [item: PlanItem]
  legMode: [leg: Leg, mode: TravelMode]
  legEdit: [leg: Leg]
  legRetry: [leg: Leg]
}>()

const { t } = useI18n()

// The drag library reorders this copy while dragging; it is replaced whenever
// the document changes.
const local = ref<PlanItem[]>([...props.places])
watch(() => props.places, (places) => {
  local.value = [...places]
})

// adding is the tile being filled in: where it goes and what it becomes.
const adding = ref<{ position: number; kind: 'place' | 'activity' } | null>(null)
const saving = ref(false)

/** startAdding opens the tile of a new element at a position of the list. */
function startAdding(position: number, kind: 'place' | 'activity'): void {
  adding.value = { position, kind }
}

// onCreate stores what the tile found and closes it; a refusal keeps the
// tile, whose text is still there to try again.
async function onCreate(fields: PlaceFields): Promise<void> {
  if (!adding.value || !props.create || saving.value) {
    return
  }
  saving.value = true
  try {
    if (await props.create(adding.value.position, fields)) {
      adding.value = null
    }
  } finally {
    saving.value = false
  }
}

// onDrop reports a place dropped into this list or moved within it.
function onDrop(event: DraggableEvent<PlanItem>): void {
  const item = event.data
  const position = event.newDraggableIndex ?? event.newIndex ?? 0
  if (item) {
    emit('move', item.id, props.dayId, position)
  }
}

defineExpose({ startAdding })
</script>

<template>
  <div class="relative flex flex-col gap-2">
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
        <PlanQuickAdd
          v-if="adding && adding.position === index"
          class="mb-2"
          :kind="adding.kind"
          :focus="focus"
          :saving="saving"
          @create="onCreate"
          @cancel="adding = null"
        />
        <PlanLegRow
          v-if="legsTo?.[item.id]"
          :leg="legsTo[item.id]!"
          :currency="currency"
          :can-edit="canEdit"
          :insertable="canEdit && !!create"
          @mode="(mode) => emit('legMode', legsTo![item.id]!, mode)"
          @edit="emit('legEdit', legsTo![item.id]!)"
          @retry="emit('legRetry', legsTo![item.id]!)"
          @insert="(kind) => startAdding(index, kind)"
        />
        <!-- A place with no leg before it still has a gap to put one into. -->
        <div v-else-if="canEdit && create" class="group relative h-5">
          <PlanInsertButton class="absolute -top-1 left-2 sm:left-8" @pick="(kind) => startAdding(index, kind)" />
        </div>
        <PlanItemCard
          :item="item"
          :currency="currency"
          :can-edit="canEdit"
          :first="index === 0"
          :last="index === local.length - 1"
          :number="dayId ? index + 1 : undefined"
          :color="color"
          @locate="emit('locate', item)"
          @edit="emit('edit', item)"
          @update="(fields) => emit('update', item, fields)"
          @edit-time="emit('editTime', item)"
          @edit-cost="emit('editCost', item)"
          @import-track="(file) => emit('importTrack', item, file)"
          @remove-track="emit('removeTrack', item)"
          @up="emit('move', item.id, dayId, index - 1)"
          @down="emit('move', item.id, dayId, index + 1)"
          @move="emit('pickTarget', item, 'move')"
          @copy="emit('pickTarget', item, 'copy')"
          @unassign="emit('move', item.id, null, -1)"
          @remove="emit('remove', item)"
        />
      </div>
    </VueDraggable>

    <PlanQuickAdd
      v-if="adding && adding.position >= local.length"
      :kind="adding.kind"
      :focus="focus"
      :saving="saving"
      @create="onCreate"
      @cancel="adding = null"
    />
    <!-- An empty list shows its "+" with words, since there is no row to hover
         over yet. It lies over the list's own room, where a place dragged from
         elsewhere is dropped, and lets the drag through. -->
    <div
      v-if="canEdit && create && !adding && local.length === 0"
      class="pointer-events-none absolute inset-x-0 top-3 flex items-center gap-2 ps-2 sm:ps-8"
    >
      <PlanInsertButton class="pointer-events-auto" visible @pick="(kind) => startAdding(0, kind)" />
      <span class="text-sm text-base-content/60">{{ t('plan.quickAdd.empty') }}</span>
    </div>
    <!-- The gap after the last place. -->
    <div v-else-if="canEdit && create && trailing && !adding" class="group relative h-5">
      <PlanInsertButton class="absolute -top-1 left-2 sm:left-8" @pick="(kind) => startAdding(local.length, kind)" />
    </div>
  </div>
</template>
