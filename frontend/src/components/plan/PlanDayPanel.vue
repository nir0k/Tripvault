<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DayChanges } from '@/api/documents'
import type { Leg, PlanDay, PlanItem, Transfer, TravelMode } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IconSelect from '@/components/IconSelect.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import PlanItemCard from '@/components/plan/PlanItemCard.vue'
import PlanLegRow from '@/components/plan/PlanLegRow.vue'
import PlanPlaceList from '@/components/plan/PlanPlaceList.vue'
import TransferRow from '@/components/plan/TransferRow.vue'
import { formatClock, formatDayDate, formatDistance, formatMoney } from '@/utils/format'
import { activeUnits } from '@/utils/units'
import { formatDuration, isVisit, travelModeOptions } from '@/utils/plan'

// One day of the plan: its header, settings, elements in schedule order and
// totals.
//
// The plan shows every day one under another, so a day can be folded away. Folded
// it keeps its header and a single line of figures: a day reduced to a bare strip
// would say nothing about whether it is worth opening.
const props = defineProps<{
  day: PlanDay
  /** The transfers departing or arriving on the day's date. */
  transfers?: Transfer[]
  currency: string
  canEdit: boolean
  draggable: boolean
  collapsed?: boolean
}>()

const emit = defineEmits<{
  locate: [item: PlanItem]
  update: [changes: DayChanges]
  duplicate: []
  remove: []
  addPlace: []
  addActivity: []
  addStay: []
  addTransfer: []
  editTransfer: [transfer: Transfer]
  move: [itemId: string, dayId: string | null, position: number]
  edit: [item: PlanItem]
  pickTarget: [item: PlanItem, mode: 'move' | 'copy']
  removePlace: [item: PlanItem]
  legMode: [leg: Leg, mode: TravelMode]
  legEdit: [leg: Leg]
  legRetry: [leg: Leg]
  recalculate: []
  toggle: []
}>()

const { t, locale } = useI18n()

const title = ref(props.day.title)
const startTime = ref(props.day.start_time)
const notes = ref(props.day.notes_md)
const editingNotes = ref(false)
const showSettings = ref(false)

watch(() => props.day, (day) => {
  title.value = day.title
  startTime.value = day.start_time
  if (!editingNotes.value) {
    notes.value = day.notes_md
  }
})

const morning = computed(() => props.day.items.find((item) => item.anchor === 'morning') ?? null)
const evening = computed(() => props.day.items.find((item) => item.anchor === 'evening') ?? null)
const places = computed(() => props.day.items.filter(isVisit))
const legsTo = computed<Record<string, Leg>>(() => Object.fromEntries(props.day.legs.map((leg) => [leg.to_item_id, leg])))
const eveningLeg = computed(() => (evening.value ? legsTo.value[evening.value.id] ?? null : null))
const distance = computed(() => formatDistance(props.day.summary.distance_m, locale.value, activeUnits.value))

const heading = computed(() => {
  const number = t('plan.dayNumber', { n: props.day.position + 1 })
  const date = formatDayDate(props.day.date, locale.value, true)
  return date ? `${number} · ${date}` : number
})
const endTime = computed(() => formatClock(props.day.summary.end_minutes))
const plannedCost = computed(() => formatMoney(props.day.summary.planned_cost, props.currency, locale.value))

// folded is the one line a collapsed day shows: how many places, how far, what it
// costs. Empty parts are left out rather than shown as zeroes.
const folded = computed(() => {
  const parts = [t('plan.placesCount', places.value.length)]
  if (props.day.summary.distance_m > 0) {
    parts.push(distance.value)
  }
  if (Number(props.day.summary.planned_cost) > 0) {
    parts.push(plannedCost.value)
  }
  return parts.join(' · ')
})

// saveTitle stores the title when it changed.
function saveTitle(): void {
  if (title.value.trim() !== props.day.title) {
    emit('update', { title: title.value })
  }
}

// saveStartTime stores a valid new start time.
function saveStartTime(): void {
  if (startTime.value && startTime.value !== props.day.start_time) {
    emit('update', { start_time: startTime.value })
  }
}

// saveNotes stores the notes and leaves the editor.
function saveNotes(): void {
  editingNotes.value = false
  if (notes.value !== props.day.notes_md) {
    emit('update', { notes_md: notes.value })
  }
}

// cancelNotes drops unsaved notes.
function cancelNotes(): void {
  notes.value = props.day.notes_md
  editingNotes.value = false
}

// The day may follow the trip's mode instead of naming one of its own, so that
// choice leads the list and carries no picture: it stands for whatever the trip
// says rather than for a way of travelling.
const modes = computed(() => [{ value: '', label: t('plan.inheritMode') }, ...travelModeOptions(t)])

// setMode stores the day's default travel mode; empty inherits the trip's.
function setMode(value: string): void {
  emit('update', { default_mode: value === '' ? null : (value as TravelMode) })
}
</script>

<template>
  <section class="space-y-4" :aria-label="heading">
    <header class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <button
          type="button"
          class="btn btn-ghost btn-sm btn-square"
          :aria-expanded="!collapsed"
          :aria-label="collapsed ? t('plan.expandDay', { day: heading }) : t('plan.collapseDay', { day: heading })"
          :title="collapsed ? t('plan.expandDay', { day: heading }) : t('plan.collapseDay', { day: heading })"
          @click="emit('toggle')"
        >
          <AppIcon name="chevronDown" class="transition-transform" :class="{ '-rotate-90': collapsed }" />
        </button>
        <p class="flex-1 text-sm font-medium text-base-content/70">{{ heading }}</p>
        <label v-if="!collapsed" class="flex items-center gap-2 text-sm">
          <span class="text-base-content/70">{{ t('plan.startTime') }}</span>
          <input v-model="startTime" type="time" class="input input-sm w-28" :disabled="!canEdit" @change="saveStartTime" />
        </label>
        <div v-if="canEdit" class="dropdown dropdown-end">
          <div tabindex="0" role="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('plan.dayActions')">
            <AppIcon name="dots" />
          </div>
          <ul tabindex="0" class="menu dropdown-content z-20 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
            <li><button type="button" @click="showSettings = !showSettings">{{ t('plan.daySettings') }}</button></li>
            <li><button type="button" @click="emit('recalculate')">{{ t('leg.recalculateDay') }}</button></li>
            <li><button type="button" @click="emit('duplicate')">{{ t('plan.duplicateDay') }}</button></li>
            <li><button type="button" class="text-error" @click="emit('remove')">{{ t('plan.deleteDay') }}</button></li>
          </ul>
        </div>
      </div>
      <input
        v-if="canEdit && !collapsed"
        v-model="title"
        type="text"
        maxlength="200"
        class="input input-ghost w-full px-1 text-xl font-bold"
        :placeholder="t('plan.dayTitlePlaceholder')"
        :aria-label="t('plan.dayTitle')"
        @blur="saveTitle"
        @keydown.enter.prevent="($event.target as HTMLInputElement).blur()"
      />
      <h2 v-else class="text-xl font-bold">{{ day.title || heading }}</h2>
    </header>

    <p v-if="collapsed" class="text-sm text-base-content/70">{{ folded }}</p>

    <div v-if="showSettings && canEdit && !collapsed" class="grid gap-3 rounded-box border border-base-300 p-3 sm:grid-cols-2">
      <label class="flex flex-col gap-1">
        <span class="label">{{ t('tripForm.defaultMode') }}</span>
        <IconSelect
          :model-value="day.default_mode ?? ''"
          :options="modes"
          :label="t('tripForm.defaultMode')"
          compact
          block
          @update:model-value="setMode"
        />
      </label>
      <label class="label cursor-pointer justify-start gap-2">
        <input type="checkbox" class="checkbox checkbox-sm" :checked="day.morning_anchor" @change="emit('update', { morning_anchor: !day.morning_anchor })" />
        <span>{{ t('plan.morningAnchor') }}</span>
      </label>
      <label class="label cursor-pointer justify-start gap-2">
        <input type="checkbox" class="checkbox checkbox-sm" :checked="day.evening_anchor" @change="emit('update', { evening_anchor: !day.evening_anchor })" />
        <span>{{ t('plan.eveningAnchor') }}</span>
      </label>
      <label class="label cursor-pointer justify-start gap-2">
        <input type="checkbox" class="checkbox checkbox-sm" :checked="day.no_overnight" @change="emit('update', { no_overnight: !day.no_overnight })" />
        <span>{{ t('plan.noOvernight') }}</span>
      </label>
    </div>

    <div v-if="!collapsed" class="space-y-2">
      <div v-if="editingNotes" class="space-y-2">
        <textarea v-model="notes" rows="4" maxlength="20000" class="textarea w-full" :placeholder="t('plan.notesPlaceholder')"></textarea>
        <div class="flex gap-2">
          <button type="button" class="btn btn-primary btn-sm" @click="saveNotes">{{ t('common.save') }}</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="cancelNotes">{{ t('common.cancel') }}</button>
        </div>
      </div>
      <template v-else>
        <MarkdownText v-if="day.notes_md" :source="day.notes_md" />
        <button v-if="canEdit" type="button" class="btn btn-ghost btn-xs" @click="editingNotes = true">
          {{ day.notes_md ? t('plan.editNotes') : t('plan.addNotes') }}
        </button>
      </template>
    </div>

    <!-- A flight or a train frames the day but takes no part in its schedule, so
         it is listed above the places rather than among them. -->
    <ul v-if="!collapsed && transfers?.length" class="flex flex-col gap-2" :aria-label="t('transfer.title')">
      <li
        v-for="transfer in transfers"
        :key="transfer.id"
        class="flex items-start gap-2 rounded-box border border-dashed border-base-300 bg-base-200/60 p-3"
      >
        <TransferRow :transfer="transfer" :currency="currency" :date="day.date" class="flex-1" />
        <button
          v-if="canEdit"
          type="button"
          class="btn btn-ghost btn-xs"
          @click="emit('editTransfer', transfer)"
        >
          {{ t('plan.edit') }}
        </button>
      </li>
    </ul>

    <div v-if="!collapsed" class="flex flex-col gap-2">
      <PlanItemCard
        v-if="morning"
        :item="morning"
        :currency="currency"
        :can-edit="canEdit"
        @locate="emit('locate', morning)"
        @hide-anchor="emit('update', { morning_anchor: false })"
      />
      <PlanPlaceList
        :places="places"
        :day-id="day.id"
        :currency="currency"
        :can-edit="canEdit"
        :draggable="draggable"
        :legs-to="legsTo"
        @locate="(item) => emit('locate', item)"
        @move="(itemId, dayId, position) => emit('move', itemId, dayId, position)"
        @edit="(item) => emit('edit', item)"
        @pick-target="(item, mode) => emit('pickTarget', item, mode)"
        @remove="(item) => emit('removePlace', item)"
        @leg-mode="(leg, mode) => emit('legMode', leg, mode)"
        @leg-edit="(leg) => emit('legEdit', leg)"
        @leg-retry="(leg) => emit('legRetry', leg)"
      />
      <p v-if="places.length === 0" class="rounded-box border border-dashed border-base-300 p-4 text-center text-sm text-base-content/60">
        {{ canEdit ? t('plan.emptyDay') : t('plan.emptyDayReadOnly') }}
      </p>
      <PlanLegRow
        v-if="eveningLeg"
        :leg="eveningLeg"
        :currency="currency"
        :can-edit="canEdit"
        @mode="(mode) => emit('legMode', eveningLeg!, mode)"
        @edit="emit('legEdit', eveningLeg!)"
        @retry="emit('legRetry', eveningLeg!)"
      />
      <PlanItemCard
        v-if="evening"
        :item="evening"
        :currency="currency"
        :can-edit="canEdit"
        @locate="emit('locate', evening)"
        @hide-anchor="emit('update', { evening_anchor: false })"
      />
    </div>

    <div v-if="canEdit && !collapsed" class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-sm btn-primary" @click="emit('addPlace')">
        <AppIcon name="plus" />
        {{ t('plan.addPlace') }}
      </button>
      <button type="button" class="btn btn-sm btn-hover-outline" @click="emit('addActivity')">
        <AppIcon name="plus" />
        {{ t('plan.addActivity') }}
      </button>
      <button type="button" class="btn btn-sm btn-hover-outline" @click="emit('addStay')">
        <AppIcon name="plus" />
        {{ t('plan.addStay') }}
      </button>
      <button v-if="day.date" type="button" class="btn btn-sm btn-hover-outline" @click="emit('addTransfer')">
        <AppIcon name="plus" />
        {{ t('plan.addTransfer') }}
      </button>
    </div>

    <footer v-if="!collapsed" class="flex flex-wrap gap-x-4 gap-y-1 rounded-box bg-base-200 px-4 py-3 text-sm">
      <span v-if="day.summary.distance_m > 0 || day.summary.travel_minutes > 0">
        {{ t('plan.summary.travel', { distance, duration: formatDuration(day.summary.travel_minutes, t) }) }}
      </span>
      <template v-if="day.summary.by_mode.length > 1">
        <span v-for="total in day.summary.by_mode" :key="total.mode" class="text-base-content/70">
          {{ t(`modes.${total.mode}`) }}: {{ formatDistance(total.distance_m, locale, activeUnits) }} · {{ formatDuration(Math.round(total.duration_s / 60), t) }}
        </span>
      </template>
      <span>{{ t('plan.summary.visits', { duration: formatDuration(day.summary.visit_minutes, t) }) }}</span>
      <span>
        {{ t('plan.summary.end', { time: endTime.time }) }}<template v-if="endTime.dayOffset > 0"> (+{{ endTime.dayOffset }})</template>
      </span>
      <span>{{ t('plan.summary.cost', { amount: plannedCost }) }}</span>
      <span v-if="day.summary.unknown_travel" class="basis-full text-base-content/70">{{ t('plan.summary.unknownTravel') }}</span>
    </footer>
  </section>
</template>
