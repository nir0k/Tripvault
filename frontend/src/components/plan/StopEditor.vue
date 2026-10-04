<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import * as documentsApi from '@/api/documents'
import { getClientConfig } from '@/api/config'
import { listMembers } from '@/api/trips'
import type { ClientConfig, PlanItem, Stop, TripDocument, TripMember } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PlaceCostDialog from '@/components/plan/PlaceCostDialog.vue'
import StopDialog from '@/components/plan/StopDialog.vue'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { stopName } from '@/utils/plan'
import { decodePolyline } from '@/utils/polyline'

// The forms the stops along the lines of activities are changed through, held
// once by the page: the stop's own form, the form of its cost - the one a
// place's cost is set in - and the question asked before one is removed. They
// open on what the cards and the map ask for (StopEditing) and hand the
// document the server answered with back to the page.
defineProps<{
  /** Whether the page is a report, whose stops keep when they were reached. */
  report: boolean
}>()

const emit = defineEmits<{
  changed: [document: TripDocument]
}>()

const { t, te } = useI18n()
const store = useTripStore()

const stopDialog = useTemplateRef<InstanceType<typeof StopDialog>>('stopDialog')
const costDialog = useTemplateRef<InstanceType<typeof PlaceCostDialog>>('costDialog')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

const config = ref<ClientConfig | null>(null)
const members = ref<TripMember[]>([])
// item is the activity whose stop a form is open for, and costStop the stop
// whose cost is.
const item = ref<PlanItem | null>(null)
const costStop = ref<Stop | null>(null)
const error = ref('')

const currency = computed(() => store.trip?.currency ?? 'EUR')
const travelers = computed(() => store.trip?.travelers ?? 1)

onMounted(async () => {
  try {
    config.value = await getClientConfig()
  } catch {
    // Without the map's tiles the form still takes a stop picked on the main map.
  }
})

/** add opens the form of a new stop of an activity, at a point when one was picked. */
function add(target: PlanItem, point?: { lat: number; lng: number }): void {
  if (!target.track) {
    return
  }
  item.value = target
  stopDialog.value?.open(null, decodePolyline(target.track.geometry), point)
}

/** edit opens the form of a stop. */
function edit(target: PlanItem, stop: Stop): void {
  item.value = target
  stopDialog.value?.open(stop, target.track ? decodePolyline(target.track.geometry) : [])
}

/** editCost opens the form of a stop's cost, with the members who may pay and share it. */
async function editCost(target: PlanItem, stop: Stop): Promise<void> {
  const tripId = store.trip?.id
  if (!tripId) {
    return
  }
  try {
    members.value = await listMembers(tripId)
  } catch (err) {
    error.value = errorMessage(err, t, te)
    return
  }
  item.value = target
  costStop.value = stop
  await nextTick()
  costDialog.value?.open(stop)
}

/** remove deletes a stop after asking. */
async function remove(_target: PlanItem, stop: Stop): Promise<void> {
  const question = t('stop.confirmDelete', { name: stopName(stop, t) })
  if (!(await confirmDialog.value?.ask(question, { danger: true }))) {
    return
  }
  error.value = ''
  try {
    emit('changed', await documentsApi.deleteStop(stop.id))
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

// saveStop stores what the stop's form says: a new stop of the activity, or a
// change of one.
async function saveStop(fields: documentsApi.StopFields, stop: Stop | null): Promise<void> {
  const target = item.value
  if (!target) {
    return
  }
  try {
    emit('changed', stop ? await documentsApi.updateStop(stop.id, fields) : await documentsApi.createStop(target.id, fields))
    stopDialog.value?.close()
  } catch (err) {
    stopDialog.value?.fail(errorMessage(err, t, te))
  }
}

// saveCost stores what the cost form says about the stop's cost.
async function saveCost(fields: documentsApi.PlaceFields): Promise<void> {
  const stop = costStop.value
  if (!stop) {
    return
  }
  const { planned_cost_amount, cost_per_person, cost_category, cost_note, paid_by, cost_split, cost_shares } = fields
  try {
    emit('changed', await documentsApi.updateStop(stop.id, {
      planned_cost_amount, cost_per_person, cost_category, cost_note, paid_by, cost_split, cost_shares,
    }))
    costDialog.value?.close()
  } catch (err) {
    costDialog.value?.fail(errorMessage(err, t, te))
  }
}

defineExpose({ add, edit, editCost, remove })
</script>

<template>
  <p v-if="error" role="alert" class="alert alert-error">{{ error }}</p>
  <StopDialog ref="stopDialog" :report="report" :config="config" @save="saveStop" />
  <PlaceCostDialog
    ref="costDialog"
    :currency="currency"
    :travelers="travelers"
    :members="members"
    @save="saveCost"
  />
  <ConfirmDialog ref="confirmDialog" />
</template>
