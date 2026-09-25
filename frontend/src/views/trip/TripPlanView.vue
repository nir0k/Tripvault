<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import * as documentsApi from '@/api/documents'
import { ApiError } from '@/api/client'
import { getClientConfig } from '@/api/config'
import type { ClientConfig, Leg, PlanDay, PlanItem, RemovedDay, Stay, TravelMode, TripDocument } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PlanDayList from '@/components/plan/PlanDayList.vue'
import PlanDayPanel from '@/components/plan/PlanDayPanel.vue'
import PlanLegDialog from '@/components/plan/PlanLegDialog.vue'
import PlanMap from '@/components/plan/PlanMap.vue'
import PlanPlaceDialog from '@/components/plan/PlanPlaceDialog.vue'
import PlanPlaceList from '@/components/plan/PlanPlaceList.vue'
import PlanStayDialog from '@/components/plan/PlanStayDialog.vue'
import PlanStays from '@/components/plan/PlanStays.vue'
import PlanTargetDialog from '@/components/plan/PlanTargetDialog.vue'
import { useLegCalculation } from '@/composables/useLegCalculation'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { addDays } from '@/utils/format'
import { describeRemovedDays, isVisit } from '@/utils/plan'

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const plan = ref<TripDocument | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
// The place dialog is for a new place in this day, or in the unassigned list
// when null.
const placeDay = ref<string | null>(null)

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const placeDialog = useTemplateRef<InstanceType<typeof PlanPlaceDialog>>('placeDialog')
const stayDialog = useTemplateRef<InstanceType<typeof PlanStayDialog>>('stayDialog')
const targetDialog = useTemplateRef<InstanceType<typeof PlanTargetDialog>>('targetDialog')
const legDialog = useTemplateRef<InstanceType<typeof PlanLegDialog>>('legDialog')
// The map is beside the list on a wide screen and a tab of its own below that,
// so each has a ref and only one of them is ever mounted.
const wideMap = useTemplateRef<InstanceType<typeof PlanMap>>('wideMap')
const mobileMap = useTemplateRef<InstanceType<typeof PlanMap>>('mobileMap')
const clientConfig = ref<ClientConfig | null>(null)
const routingEnabled = computed(() => clientConfig.value?.routing_enabled ?? true)
// Below extra-wide screens the map replaces the list instead of sitting beside it.
const mobileView = ref<'list' | 'map'>('list')
const sideBySide = useMediaQuery('(min-width: 1280px)')
// The same width at which the trip shows its own column, which then takes the days.
const wideNav = useMediaQuery('(min-width: 1024px)')

// Dragging needs a pointer and room; phones use the move buttons.
const wide = useMediaQuery('(min-width: 1024px) and (pointer: fine)')

const trip = computed(() => store.trip)
const canEdit = computed(() => trip.value?.role === 'owner' || trip.value?.role === 'editor')
const currency = computed(() => trip.value?.currency ?? 'EUR')

// Every day is shown, one under another. The address says which one to scroll to
// (?day=<n>, counting from one) so a link still opens where it pointed, and
// ?view=stays swaps the feed for the stays.
const showStays = computed(() => route.query.view === 'stays')
const dayIndex = computed(() => {
  const days = plan.value?.days.length ?? 0
  const requested = Number(route.query.day ?? 1)
  return Number.isInteger(requested) && requested >= 1 && requested <= days ? requested - 1 : 0
})

// viewedDay is the day on screen, which the list marks and the map follows. It is
// what the reader is looking at rather than what they last clicked.
const viewedDay = ref(0)
// collapsed holds the days folded away, by identifier. Empty means every day is
// open, which is how the plan starts.
const collapsed = ref(new Set<string>())
const dayElements = new Map<number, HTMLElement>()
let dayObserver: IntersectionObserver | null = null

const day = computed(() => plan.value?.days[viewedDay.value] ?? null)

/** dayAnchor names the element a day is scrolled to. */
function dayAnchor(index: number): string {
  return `plan-day-${index + 1}`
}

/**
 * registerDay keeps the element of each day, so the observer can watch it and a
 * click on the list can scroll to it. Vue passes null when a day leaves.
 */
function registerDay(index: number, element: unknown): void {
  const node = element instanceof HTMLElement ? element : null
  const previous = dayElements.get(index)
  if (previous && previous !== node) {
    dayObserver?.unobserve(previous)
  }
  if (!node) {
    dayElements.delete(index)
    return
  }
  dayElements.set(index, node)
  dayObserver?.observe(node)
}

/**
 * The topmost day showing counts as the one being read. A plain "is it visible"
 * test would pick the last one to enter the screen, which on a fast scroll is the
 * one below the fold.
 */
function watchDays(): void {
  if (dayObserver || typeof IntersectionObserver === 'undefined') {
    return
  }
  dayObserver = new IntersectionObserver((entries) => {
    const visible = entries.filter((entry) => entry.isIntersecting)
    if (visible.length === 0) {
      return
    }
    const top = visible.reduce((first, entry) =>
      entry.boundingClientRect.top < first.boundingClientRect.top ? entry : first)
    for (const [index, element] of dayElements) {
      if (element === top.target) {
        viewedDay.value = index
      }
    }
  }, { rootMargin: '-10% 0px -70% 0px' })
  for (const element of dayElements.values()) {
    dayObserver.observe(element)
  }
}

// toggleDay folds a day away or opens it again.
function toggleDay(id: string): void {
  const next = new Set(collapsed.value)
  if (!next.delete(id)) {
    next.add(id)
  }
  collapsed.value = next
}

/** scrollToDay brings a day of the feed into view, opening it if it was folded. */
async function scrollToDay(index: number): Promise<void> {
  const target = plan.value?.days[index]
  if (target && collapsed.value.has(target.id)) {
    toggleDay(target.id)
  }
  await nextTick()
  dayElements.get(index)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

// searchFocus is where place searches look first: the middle of the open day's
// positions, else of the whole plan's.
const searchFocus = computed(() => {
  const centre = (items: PlanItem[]) => {
    const placed = items.filter((item) => item.lat !== null && item.lng !== null)
    if (placed.length === 0) {
      return null
    }
    return {
      lat: placed.reduce((sum, item) => sum + (item.lat ?? 0), 0) / placed.length,
      lng: placed.reduce((sum, item) => sum + (item.lng ?? 0), 0) / placed.length,
    }
  }
  const all = plan.value ? [...plan.value.days.flatMap((d) => d.items), ...plan.value.unassigned] : []
  return (day.value && !showStays.value ? centre(day.value.items) : null) ?? centre(all)
})
const missingNights = computed(() => plan.value?.nights.filter((night) => night.missing).length ?? 0)

// The plan's legs are calculated as it is edited; retryEstimates asks again
// about the straight lines a plan made before the provider was configured is
// full of.
const { retryEstimates } = useLegCalculation({
  document: plan,
  enabled: () => canEdit.value,
  busy,
  onError: (err) => { error.value = errorMessage(err, t, te) },
})

onBeforeUnmount(() => {
  dayObserver?.disconnect()
  dayObserver = null
})
onMounted(async () => {
  try {
    clientConfig.value = await getClientConfig()
  } catch {
    // Without the configuration nothing is claimed about routing.
  }
})

// load reads the plan of the open trip.
async function load(): Promise<void> {
  const planId = trip.value?.plan_id
  if (!planId) {
    plan.value = null
    return
  }
  loading.value = true
  error.value = ''
  try {
    plan.value = await store.readDocument(planId)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }

  // The feed exists now, so it can be watched and opened where the address
  // pointed. Without the address it opens at the first day, at the top.
  await nextTick()
  watchDays()
  if (dayIndex.value > 0) {
    viewedDay.value = dayIndex.value
    await scrollToDay(dayIndex.value)
  }
}

watch(() => trip.value?.plan_id, () => void load(), { immediate: true })

/**
 * apply runs a change and shows the document the server returns. A change that
 * would remove days holding content is confirmed first and then repeated with
 * confirmation. Changes to the number of days also move the trip's end date,
 * so the trip is read again after them.
 */
async function apply(
  change: () => Promise<TripDocument>,
  options: { confirmed?: () => Promise<TripDocument>; tripChanged?: boolean } = {},
): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    plan.value = await change()
    if (options.tripChanged && trip.value) {
      void store.load(trip.value.id)
    }
    return true
  } catch (err) {
    if (err instanceof ApiError && err.code === 'days_would_be_removed' && options.confirmed) {
      const days = (err.details.days ?? []) as RemovedDay[]
      const agreed = await confirmDialog.value?.ask(t('plan.confirmRemoveDays'), {
        details: describeRemovedDays(days, t, locale.value), danger: true,
      })
      if (agreed) {
        return apply(options.confirmed, { tripChanged: options.tripChanged })
      }
      return false
    }
    error.value = errorMessage(err, t, te)
    return false
  } finally {
    busy.value = false
  }
}

/**
 * selectDay brings a day into view. It also records the day in the address, so a
 * link still opens on it, and leaves the stays if they were showing.
 */
async function selectDay(index: number): Promise<void> {
  viewedDay.value = index
  await router.replace({ query: { day: String(index + 1) } })
  await scrollToDay(index)
}

// selectStays opens the stays.
function selectStays(): void {
  void router.replace({ query: { view: 'stays' } })
}

// addDay appends a day and opens it.
async function addDay(): Promise<void> {
  const current = plan.value
  if (current && (await apply(() => documentsApi.addDay(current.id), { tripChanged: true }))) {
    selectDay((plan.value?.days.length ?? 1) - 1)
  }
}

// removeDay deletes a day of the feed after confirmation.
async function removeDay(current: PlanDay): Promise<void> {
  const places = current.items.filter(isVisit).length
  const question = places > 0 ? t('plan.confirmDeleteDayPlaces', places) : t('plan.confirmDeleteDay')
  if (!(await confirmDialog.value?.ask(question, { danger: true }))) {
    return
  }
  await apply(() => documentsApi.deleteDay(current.id), {
    confirmed: () => documentsApi.deleteDay(current.id, true),
    tripChanged: true,
  })
  if (plan.value && viewedDay.value >= plan.value.days.length) {
    viewedDay.value = Math.max(plan.value.days.length - 1, 0)
  }
}

// openPlace opens the place form for a new place or activity in a day or the
// unassigned list, at a position picked on the map when there is one.
function openPlace(dayId: string | null, position: { lat: number; lng: number } | null = null,
  kind: 'place' | 'activity' = 'place'): void {
  placeDay.value = dayId
  placeDialog.value?.open(null, position, kind)
}

// addAt opens the place form at a position clicked on the map: in the day being
// read, or among the unassigned places while the stays are shown.
function addAt(lat: number, lng: number): void {
  mobileView.value = 'list'
  openPlace(showStays.value ? null : day.value?.id ?? null, { lat, lng })
}

// savePlace creates or changes a place from the form.
async function savePlace(fields: documentsApi.PlaceFields, item: PlanItem | null): Promise<void> {
  const current = plan.value
  if (!current) {
    return
  }
  const ok = await apply(() =>
    item ? documentsApi.updatePlace(item.id, fields) : documentsApi.createPlace(current.id, placeDay.value, fields),
  )
  if (ok) {
    placeDialog.value?.close()
  } else {
    placeDialog.value?.fail(error.value)
    error.value = ''
  }
}

// removePlace deletes a place after confirmation.
async function removePlace(item: PlanItem): Promise<void> {
  if (await confirmDialog.value?.ask(t('plan.confirmDeletePlace', { name: item.name }), { danger: true })) {
    await apply(() => documentsApi.deletePlace(item.id))
  }
}

// movePlace puts a place at a position in a day or the unassigned list.
async function movePlace(itemId: string, dayId: string | null, position: number): Promise<void> {
  const current = plan.value
  if (current) {
    await apply(() => documentsApi.movePlace(current.id, itemId, dayId, position))
  }
}

// chooseTarget performs a move or copy picked in the day dialog.
async function chooseTarget(item: PlanItem, mode: 'move' | 'copy', dayId: string | null): Promise<void> {
  if (mode === 'move') {
    await movePlace(item.id, dayId, -1)
  } else {
    await apply(() => documentsApi.copyPlace(item.id, dayId))
  }
}

// openStay opens the stay form; asked from a day, it proposes that day's night.
function openStay(stay: Stay | null, fromDay: PlanDay | null = null): void {
  const date = fromDay?.date ?? null
  stayDialog.value?.open(stay, date ? { checkIn: date, checkOut: addDays(date, 1) } : {})
}

// saveStay creates or changes a stay from the form.
async function saveStay(fields: documentsApi.StayFields, stay: Stay | null): Promise<void> {
  const current = plan.value
  if (!current) {
    return
  }
  const ok = await apply(() => (stay ? documentsApi.updateStay(stay.id, fields) : documentsApi.createStay(current.id, fields)))
  if (ok) {
    stayDialog.value?.close()
  } else {
    stayDialog.value?.fail(error.value)
    error.value = ''
  }
}

// setLegMode switches a leg to another travel mode.
async function setLegMode(leg: Leg, mode: TravelMode): Promise<void> {
  await apply(() => documentsApi.updateLeg(leg.id, { mode }))
}

// saveLeg stores typed values from the leg form.
async function saveLeg(leg: Leg, changes: documentsApi.LegChanges): Promise<void> {
  if (await apply(() => documentsApi.updateLeg(leg.id, changes))) {
    legDialog.value?.close()
  } else {
    legDialog.value?.fail(error.value)
    error.value = ''
  }
}

// focusItem opens the day of an element picked on the map and scrolls to its
// card, briefly highlighting it.
async function focusItem(itemId: string, targetDay: number | null): Promise<void> {
  mobileView.value = 'list'
  // An unassigned place is listed after the days, so the stays make way for the
  // feed; a place of a day also needs its day open and in view.
  if (showStays.value) {
    await router.replace({ query: { day: String((targetDay ?? viewedDay.value) + 1) } })
  }
  if (targetDay !== null) {
    await selectDay(targetDay)
  }
  await nextTick()
  const card = document.querySelector<HTMLElement>(`[data-card-id="${CSS.escape(itemId)}"]`)
  if (card) {
    card.scrollIntoView({ behavior: 'smooth', block: 'center' })
    card.classList.add('ring-2', 'ring-primary')
    setTimeout(() => card.classList.remove('ring-2', 'ring-primary'), 2000)
  }
}

// locateItem takes the map to a place picked in the plan, which is focusItem the
// other way round. Below the width that shows both, the map is a tab of its own
// and has to be opened - and mounted - before it can be told where to go.
async function locateItem(item: PlanItem): Promise<void> {
  if (!sideBySide.value) {
    mobileView.value = 'map'
    await nextTick()
  }
  const map = sideBySide.value ? wideMap.value : mobileMap.value
  map?.locate(item.id)
}

// removeStay deletes a stay after confirmation.
async function removeStay(stay: Stay): Promise<void> {
  if (await confirmDialog.value?.ask(t('stay.confirmDelete', { name: stay.name }), { danger: true })) {
    await apply(() => documentsApi.deleteStay(stay.id))
  }
}
</script>

<template>
  <div v-if="trip">
    <div v-if="!trip.plan_id" class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-6 py-16 text-center">
      <span class="text-primary"><AppIcon name="plan" /></span>
      <p class="max-w-md text-base-content/70">{{ t('trip.planMissing') }}</p>
    </div>

    <div v-else-if="!plan" class="flex justify-center">
      <span v-if="loading" class="loading loading-spinner"></span>
      <p v-else-if="error" role="alert" class="text-error">{{ error }}</p>
    </div>

    <div v-else class="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]">
      <!-- With room for it the trip column holds the days; on a narrow screen
           they stay here, as a row of chips above the plan. -->
      <Teleport defer to="#trip-sidebar-days" :disabled="!wideNav">
        <PlanDayList
          :days="plan.days"
          :selected="showStays ? null : viewedDay"
          :stays-count="plan.stays.length"
          :missing-nights="missingNights"
          :can-edit="canEdit"
          :draggable="wide"
          @select="(index) => void selectDay(index)"
          @select-stays="selectStays"
          @add="addDay"
          @reorder="(ids) => plan && apply(() => documentsApi.reorderDays(plan!.id, ids))"
          @drop-place="(itemId, dayId) => movePlace(itemId, dayId, -1)"
        />
      </Teleport>

      <div class="min-w-0 space-y-8">
        <div v-if="!sideBySide" role="tablist" class="tabs tabs-box tabs-sm w-fit">
          <button type="button" role="tab" class="tab" :class="{ 'tab-active': mobileView === 'list' }" :aria-selected="mobileView === 'list'" @click="mobileView = 'list'">
            {{ t('map.list') }}
          </button>
          <button type="button" role="tab" class="tab" :class="{ 'tab-active': mobileView === 'map' }" :aria-selected="mobileView === 'map'" @click="mobileView = 'map'">
            {{ t('map.map') }}
          </button>
        </div>

        <div v-if="!sideBySide && mobileView === 'map' && clientConfig" class="h-[70dvh]">
          <PlanMap
            ref="mobileMap"
            :document="plan"
            :selected-day="showStays ? null : viewedDay"
            :tile-url="clientConfig.map_tile_url"
            :attribution="clientConfig.map_attribution"
            :can-edit="canEdit"
            @focus="focusItem"
            @add-at="addAt"
          />
        </div>

        <template v-if="sideBySide || mobileView === 'list'">
          <p v-if="error" role="alert" class="alert alert-error">{{ error }}</p>
          <p v-if="!routingEnabled && canEdit" role="status" class="alert alert-info text-sm">{{ t('leg.routingDisabled') }}</p>

          <div
            v-else-if="canEdit && plan.estimated_legs > 0"
            role="status"
            class="alert alert-info flex-wrap gap-2 text-sm"
          >
            <span>{{ t('leg.estimatesPending', plan.estimated_legs) }}</span>
            <button type="button" class="btn btn-sm" :disabled="busy" @click="retryEstimates">
              <span v-if="busy" class="loading loading-spinner loading-xs"></span>
              {{ t('leg.retryEstimates') }}
            </button>
          </div>

          <PlanStays
            v-if="showStays"
            :document="plan"
            :currency="currency"
            :can-edit="canEdit"
            @add="openStay(null)"
            @edit="(stay) => openStay(stay)"
            @remove="removeStay"
          />

          <template v-else>
            <div
              v-for="(each, index) in plan.days"
              :id="dayAnchor(index)"
              :key="each.id"
              :ref="(element) => registerDay(index, element)"
              class="scroll-mt-4 rounded-box border p-4"
              :class="index === viewedDay ? 'border-primary/40' : 'border-base-300'"
            >
              <PlanDayPanel
                :day="each"
                :currency="currency"
                :can-edit="canEdit"
                :draggable="wide"
                :collapsed="collapsed.has(each.id)"
                @locate="locateItem"
                @toggle="toggleDay(each.id)"
                @update="(changes) => apply(() => documentsApi.updateDay(each.id, changes))"
                @duplicate="apply(() => documentsApi.duplicateDay(each.id), { tripChanged: true })"
                @remove="removeDay(each)"
                @add-place="openPlace(each.id)"
                @add-activity="openPlace(each.id, null, 'activity')"
                @add-stay="openStay(null, each)"
                @move="movePlace"
                @edit="(item) => placeDialog?.open(item)"
                @pick-target="(item, mode) => targetDialog?.open(item, mode)"
                @remove-place="removePlace"
                @leg-mode="setLegMode"
                @leg-edit="(leg) => legDialog?.open(leg)"
                @leg-retry="(leg) => apply(() => documentsApi.recalculateLeg(leg.id))"
                @recalculate="apply(() => documentsApi.recalculateDay(each.id))"
              />
            </div>
          </template>

          <section v-if="!showStays" class="space-y-3" :aria-label="t('plan.unassigned')">
            <header class="flex flex-wrap items-center justify-between gap-2">
              <h2 class="text-lg font-semibold">{{ t('plan.unassigned') }} ({{ plan.unassigned.length }})</h2>
              <button v-if="canEdit" type="button" class="btn btn-sm btn-hover-outline" @click="openPlace(null)">
                <AppIcon name="plus" />
                {{ t('plan.addIdea') }}
              </button>
            </header>
            <PlanPlaceList
              :places="plan.unassigned"
              :day-id="null"
              :currency="currency"
              :can-edit="canEdit"
              :draggable="wide"
              @locate="locateItem"
              @move="movePlace"
              @edit="(item) => placeDialog?.open(item)"
              @pick-target="(item, mode) => targetDialog?.open(item, mode)"
              @remove="removePlace"
            />
            <p v-if="plan.unassigned.length === 0" class="text-sm text-base-content/60">{{ t('plan.unassignedEmpty') }}</p>
          </section>
        </template>
      </div>

      <aside v-if="sideBySide && clientConfig" class="sticky top-20 h-[calc(100dvh-6rem)] self-start">
        <PlanMap
          ref="wideMap"
          :document="plan"
          :selected-day="showStays ? null : viewedDay"
          :tile-url="clientConfig.map_tile_url"
          :attribution="clientConfig.map_attribution"
          :can-edit="canEdit"
          @focus="focusItem"
          @add-at="addAt"
        />
      </aside>
    </div>

    <PlanPlaceDialog ref="placeDialog" :focus="searchFocus" @save="savePlace" />
    <PlanStayDialog ref="stayDialog" :focus="searchFocus" @save="saveStay" />
    <PlanLegDialog ref="legDialog" @save="saveLeg" />
    <PlanTargetDialog v-if="plan" ref="targetDialog" :days="plan.days" @choose="chooseTarget" />
    <ConfirmDialog ref="confirmDialog" />
  </div>
</template>
