<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientConfig } from '@/api/config'
import {
  legAlternatives, readGoogleLink, type LegChanges, type LegPartChange, type LegParts,
} from '@/api/documents'
import {
  TRAVEL_MODES, type ClientConfig, type GeoPoint, type Leg, type RouteOption, type RoutePreference, type TravelMode,
} from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import AppIcon from '@/components/AppIcon.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import LegRouteMap from '@/components/plan/LegRouteMap.vue'
import { errorMessage } from '@/utils/errors'
import { formatDistance, fromMetres, normalizeAmount, toMetres } from '@/utils/format'
import { formatDuration } from '@/utils/plan'
import { decodePolyline } from '@/utils/polyline'
import { activeUnits } from '@/utils/units'

// Typed values for a leg: distance and time override the calculation until
// cleared, plus the cost and a note such as "bus Strætó 51". A report has no
// row of mode buttons, so there the form also chooses the means, and it takes
// what the journey really cost.
//
// A road leg is also told how to be routed: by the fastest or the shortest
// route, along one of the routes the provider offers, or through points of its
// own - most easily the ones read out of a route drawn in Google Maps.
//
// A journey with changes - a walk to the station, a train, a bus - is edited
// part by part instead: each part's means and time, the change after it with
// the wait there, and the tickets, one of which may cover several parts. Adding
// the first change turns the leg into one; removing the last turns it back.
const props = defineProps<{
  /** The leg belongs to a report, whose form also offers the means. */
  report?: boolean
  /** Something is being saved or calculated. */
  busy?: boolean
  /** Only the figures and costs: the form is opened from the list of costs. */
  costsOnly?: boolean
}>()

const emit = defineEmits<{
  save: [leg: Leg, changes: LegChanges, route: RouteOption | null, parts: LegParts | null]
  recalculate: [leg: Leg]
}>()

const { t, te, locale } = useI18n()

/** ROUTED_MODES are the modes that follow roads, and so can be routed one way or another. */
const ROUTED_MODES: TravelMode[] = ['walk', 'bike', 'car', 'transit', 'bus']

/** GOOGLE_TRAVEL_MODES name a mode the way a Google Maps link does. */
const GOOGLE_TRAVEL_MODES: Partial<Record<TravelMode, string>> = {
  walk: 'walking', bike: 'bicycling', car: 'driving', transit: 'transit', bus: 'transit',
}

/** PartForm is one part of a journey with changes as the form edits it. */
interface PartForm {
  mode: TravelMode
  hours: string
  minutes: string
  /** A typed distance, in the reader's units; kept, not edited, part by part. */
  distance: string
  /** The calculated time, shown until one is typed. */
  calculated: number | null
  /** The position of the part's ticket in the list, or null. */
  ticket: number | null
  stopName: string
  stopWhere: LocationModel
  wait: string
}

/** TicketForm is one ticket as the form edits it. */
interface TicketForm {
  name: string
  cost: string
  actual: string
}

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const leg = ref<Leg | null>(null)
const error = ref('')
// The distance is typed in whatever the reader reads, and stored in metres like
// every other: somebody who measures in miles should not have to convert first.
const form = reactive({ mode: 'walk' as TravelMode, distance: '', hours: '', minutes: '', cost: '', actual: '', note: '' })

// How a road leg is routed, edited beside the figures and saved with them.
const config = ref<ClientConfig | null>(null)
const preference = ref<RoutePreference>('fastest')
const via = ref<GeoPoint[]>([])
const options = ref<RouteOption[]>([])
const chosen = ref(-1)
const loadingOptions = ref(false)
const link = ref('')
const readingLink = ref(false)
const routeError = ref('')

// The parts of a journey with changes; empty while the leg is travelled one
// way. The leg had parts when it was opened, which decides whether going back
// to one part must be saved as such.
const parts = ref<PartForm[]>([])
const tickets = ref<TicketForm[]>([])
const hadParts = ref(false)
const composite = computed(() => parts.value.length > 0)

const routed = computed(() => !props.costsOnly && !composite.value && ROUTED_MODES.includes(form.mode)
  && (config.value?.routing_enabled ?? false))
// Alternatives run between the two ends only: points of one's own already
// decide the route.
const canListOptions = computed(() => (config.value?.routing_alternatives ?? false) && via.value.length === 0)
const mapRoutes = computed(() => {
  if (options.value.length > 0) {
    return options.value.map((option) => option.geometry)
  }
  return leg.value?.geometry ? [leg.value.geometry] : []
})
const mapSelected = computed(() => (options.value.length > 0 ? chosen.value : 0))

// ends are where the leg starts and ends, read from its line.
const ends = computed(() => {
  const line = decodePolyline(leg.value?.geometry ?? '')
  const first = line[0]
  const last = line.at(-1)
  return first && last && line.length > 1 ? { from: first, to: last } : null
})

// googleUrl opens the leg in Google Maps, through its own via points, to compare
// the two routes or to draw one there and bring it back.
const googleUrl = computed(() => {
  if (!ends.value) {
    return ''
  }
  const params = new URLSearchParams({
    api: '1',
    origin: ends.value.from.join(','),
    destination: ends.value.to.join(','),
    travelmode: GOOGLE_TRAVEL_MODES[form.mode] ?? 'driving',
  })
  if (via.value.length > 0) {
    params.set('waypoints', via.value.map((point) => `${point.lat},${point.lng}`).join('|'))
  }
  return `https://www.google.com/maps/dir/?${params.toString()}`
})

// describeOption sums a route up as its distance and time.
function describeOption(option: RouteOption): string {
  return `${formatDistance(option.distance_m, locale.value, activeUnits.value)} · ${
    formatDuration(Math.round(option.duration_s / 60), t)}`
}

// resetOptions forgets the routes offered, because what they were offered for changed.
function resetOptions(): void {
  options.value = []
  chosen.value = -1
}

// setPreference changes what the route is optimised for; the routes offered
// for the other preference no longer apply.
function setPreference(next: RoutePreference): void {
  if (preference.value !== next) {
    preference.value = next
    resetOptions()
  }
}

// listOptions asks for the routes the leg could take. They are asked for the
// preference as saved, so a changed preference is saved first.
async function listOptions(): Promise<void> {
  if (!leg.value) {
    return
  }
  loadingOptions.value = true
  routeError.value = ''
  try {
    options.value = await legAlternatives(leg.value.id)
    chosen.value = -1
  } catch (err) {
    routeError.value = errorMessage(err, t, te)
  } finally {
    loadingOptions.value = false
  }
}

// readLink reads the points of a route drawn in Google Maps into the form.
async function readLink(): Promise<void> {
  if (!leg.value || link.value.trim() === '') {
    return
  }
  readingLink.value = true
  routeError.value = ''
  try {
    const points = await readGoogleLink(leg.value.id, link.value.trim())
    via.value = points
    resetOptions()
    link.value = ''
    if (points.length === 0) {
      routeError.value = t('leg.viaNoneInLink')
    }
  } catch (err) {
    routeError.value = errorMessage(err, t, te)
  } finally {
    readingLink.value = false
  }
}

// removeVia drops one point the route passes through.
function removeVia(index: number): void {
  via.value = via.value.filter((_, position) => position !== index)
}

// sameVia reports whether two lists of points are the same, in the same order.
function sameVia(a: GeoPoint[], b: GeoPoint[]): boolean {
  return a.length === b.length && a.every((point, index) => point.lat === b[index]?.lat && point.lng === b[index]?.lng)
}

const distanceLabel = computed(() => t(`leg.distanceIn.${activeUnits.value}`))

// calculated is the provider's own distance, shown as the placeholder so that
// clearing the field visibly returns to it.
const calculated = computed(() => {
  const metres = leg.value?.calculated_distance_m
  return metres == null ? distanceLabel.value : String(fromMetres(metres, activeUnits.value))
})

// splitSeconds writes a time as hours and minutes for two fields.
function splitSeconds(seconds: number | null): { hours: string; minutes: string } {
  return seconds === null
    ? { hours: '', minutes: '' }
    : { hours: String(Math.floor(seconds / 3600)), minutes: String(Math.round((seconds % 3600) / 60)) }
}

// joinSeconds reads hours and minutes back as seconds; both empty means none typed.
function joinSeconds(hours: string, minutes: string): number | null {
  return hours.trim() === '' && minutes.trim() === '' ? null : (Number(hours || 0) * 60 + Number(minutes || 0)) * 60
}

// typedMetres reads a typed distance in the reader's units as metres; empty is none.
function typedMetres(typed: string): number | null {
  const value = typed.trim().replace(',', '.')
  return value === '' ? null : toMetres(Number(value), activeUnits.value)
}

// emptyPart is a new part travelled a given way, with a change still to be named.
function emptyPart(mode: TravelMode): PartForm {
  return { mode, hours: '', minutes: '', distance: '', calculated: null, ticket: null, stopName: '',
    stopWhere: { address: '', lat: '', lng: '' }, wait: '' }
}

// readParts fills the parts and tickets of the form from a journey with changes.
function readParts(next: Leg): void {
  const ticketIndex = new Map(next.tickets.map((ticket, index) => [ticket.id, index]))
  tickets.value = next.tickets.map((ticket) => ({
    name: ticket.name, cost: ticket.planned_cost_amount ?? '', actual: ticket.actual_cost_amount ?? '',
  }))
  parts.value = next.segments.map((segment) => ({
    mode: segment.mode,
    ...splitSeconds(segment.manual_duration ? segment.duration_s : null),
    distance: segment.manual_distance && segment.distance_m !== null
      ? String(fromMetres(segment.distance_m, activeUnits.value))
      : '',
    calculated: segment.calculated_duration_s,
    ticket: segment.ticket_id === null ? null : ticketIndex.get(segment.ticket_id) ?? null,
    stopName: segment.stop?.name ?? '',
    stopWhere: {
      address: '',
      lat: segment.stop?.lat === null || segment.stop?.lat === undefined ? '' : String(segment.stop.lat),
      lng: segment.stop?.lng === null || segment.stop?.lng === undefined ? '' : String(segment.stop.lng),
    },
    wait: segment.stop && segment.stop.wait_minutes > 0 ? String(segment.stop.wait_minutes) : '',
  }))
}

// addChange puts a change after a part, splitting the journey there. The first
// change turns the leg into a journey of two parts: the way it was travelled
// and its typed time become the first part, and its cost the first ticket.
function addChange(after: number): void {
  if (!composite.value) {
    const first: PartForm = { ...emptyPart(form.mode), hours: form.hours, minutes: form.minutes,
      distance: form.distance, calculated: leg.value?.calculated_duration_s ?? null }
    tickets.value = form.cost.trim() !== '' || form.actual.trim() !== ''
      ? [{ name: '', cost: form.cost, actual: form.actual }]
      : []
    first.ticket = tickets.value.length > 0 ? 0 : null
    parts.value = [first, emptyPart('walk')]
    return
  }
  parts.value.splice(after + 1, 0, emptyPart('walk'))
}

// removeChange takes away the change after a part: the part after it goes, and
// this part now ends where that one did. Removing the last change turns the
// journey back into one travelled one way, whose cost is what its tickets add up to.
function removeChange(after: number): void {
  const next = parts.value[after + 1]
  const part = parts.value[after]
  if (!next || !part) {
    return
  }
  Object.assign(part, { stopName: next.stopName, stopWhere: next.stopWhere, wait: next.wait })
  parts.value.splice(after + 1, 1)
  if (parts.value.length === 1) {
    const only = parts.value[0]!
    form.mode = only.mode
    Object.assign(form, { hours: only.hours, minutes: only.minutes, distance: only.distance })
    form.cost = sumAmounts(tickets.value.map((ticket) => ticket.cost))
    form.actual = sumAmounts(tickets.value.map((ticket) => ticket.actual))
    parts.value = []
    tickets.value = []
  }
}

// sumAmounts adds typed amounts up in hundredths; nothing typed gives nothing.
function sumAmounts(amounts: string[]): string {
  const typed = amounts.map((amount) => normalizeAmount(amount)).filter((amount): amount is string => amount !== null)
  if (typed.length === 0) {
    return ''
  }
  const cents = typed.reduce((sum, amount) => sum + Math.round(Number(amount) * 100), 0)
  return (cents / 100).toFixed(2)
}

// addTicket adds a ticket, and puts a part on it when one asked for it.
function addTicket(forPart: number | null = null): void {
  tickets.value.push({ name: '', cost: '', actual: '' })
  if (forPart !== null && parts.value[forPart]) {
    parts.value[forPart]!.ticket = tickets.value.length - 1
  }
}

// removeTicket drops a ticket; the parts it covered are left without one.
function removeTicket(index: number): void {
  tickets.value.splice(index, 1)
  for (const part of parts.value) {
    if (part.ticket === index) {
      part.ticket = null
    } else if (part.ticket !== null && part.ticket > index) {
      part.ticket--
    }
  }
}

// chooseTicket reads the ticket picked for a part; the last choice makes a new one.
function chooseTicket(part: number, value: string): void {
  if (value === 'new') {
    addTicket(part)
    return
  }
  parts.value[part]!.ticket = value === '' ? null : Number(value)
}

// ticketLabel names a ticket by its name, or by its number while it has none.
function ticketLabel(index: number): string {
  const name = tickets.value[index]?.name.trim()
  return name || t('leg.ticketNumber', { number: index + 1 })
}

// coveredBy says which parts a ticket covers, by their numbers; nothing when none.
function coveredBy(index: number): string {
  const numbers = parts.value
    .map((part, position) => (part.ticket === index ? String(position + 1) : ''))
    .filter(Boolean)
  return numbers.length === 0 ? '' : t('leg.ticketCovers', { parts: numbers.join(', ') }, numbers.length)
}

// buildParts writes the form's parts and tickets as they are saved.
function buildParts(): LegParts {
  const segments: LegPartChange[] = parts.value.map((part, index) => {
    const lat = part.stopWhere.lat.trim()
    const lng = part.stopWhere.lng.trim()
    const last = index === parts.value.length - 1
    return {
      mode: part.mode,
      distance_m: typedMetres(part.distance),
      duration_s: joinSeconds(part.hours, part.minutes),
      ticket: part.ticket,
      stop: last ? null : {
        name: part.stopName,
        lat: lat === '' ? null : Number(lat.replace(',', '.')),
        lng: lng === '' ? null : Number(lng.replace(',', '.')),
        wait_minutes: Number(part.wait || 0),
      },
    }
  })
  return {
    segments,
    tickets: tickets.value.map((ticket) => ({
      name: ticket.name,
      planned_cost_amount: normalizeAmount(ticket.cost),
      ...(props.report ? { actual_cost_amount: normalizeAmount(ticket.actual) } : {}),
    })),
  }
}

/** open shows the form for a leg, filled with its typed values and its routing. */
function open(next: Leg): void {
  leg.value = next
  error.value = ''
  routeError.value = ''
  preference.value = next.route_preference
  via.value = [...next.via]
  link.value = ''
  resetOptions()
  void getClientConfig().then((loaded) => {
    config.value = loaded
  }).catch(() => {
    config.value = null
  })
  const seconds = next.manual_duration ? next.duration_s ?? 0 : null
  Object.assign(form, {
    mode: next.mode,
    distance: next.manual_distance && next.distance_m !== null
      ? String(fromMetres(next.distance_m, activeUnits.value))
      : '',
    hours: seconds === null ? '' : String(Math.floor(seconds / 3600)),
    minutes: seconds === null ? '' : String(Math.round((seconds % 3600) / 60)),
    cost: next.planned_cost_amount ?? '',
    actual: next.actual_cost_amount ?? '',
    note: next.note,
  })
  hadParts.value = next.segments.length > 0
  parts.value = []
  tickets.value = []
  if (hadParts.value) {
    readParts(next)
  }
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

// submit turns the typed distance and time into metres and seconds; empty
// fields return to the calculated values.
function submit(): void {
  if (!leg.value) {
    return
  }
  // A journey with changes is saved part by part, with its note; one that
  // lost its last change is saved as a single part, which makes it plain again.
  if (composite.value) {
    emit('save', leg.value, { note: form.note }, null, buildParts())
    return
  }
  if (hadParts.value) {
    const cost = normalizeAmount(form.cost)
    const actual = normalizeAmount(form.actual)
    const paid = cost !== null || actual !== null
    emit('save', leg.value, { note: form.note }, null, {
      segments: [{ mode: form.mode, distance_m: typedMetres(form.distance), duration_s: joinSeconds(form.hours, form.minutes),
        ticket: paid ? 0 : null, stop: null }],
      tickets: paid ? [{ name: '', planned_cost_amount: cost, ...(props.report ? { actual_cost_amount: actual } : {}) }] : [],
    })
    return
  }
  const typed = form.distance.trim().replace(',', '.')
  const timed = form.hours.trim() !== '' || form.minutes.trim() !== ''
  const changes: LegChanges = {
    distance_m: typed === '' ? null : toMetres(Number(typed), activeUnits.value),
    duration_s: timed ? (Number(form.hours || 0) * 60 + Number(form.minutes || 0)) * 60 : null,
    planned_cost_amount: normalizeAmount(form.cost),
    note: form.note,
  }
  if (props.report) {
    changes.actual_cost_amount = normalizeAmount(form.actual)
    // A new mode asks for a new calculation, so it is only sent when it moved.
    if (form.mode !== leg.value.mode) {
      changes.mode = form.mode
    }
  }
  // The routing, like the mode, sends the leg back to the provider, so it too
  // is sent only when it changed.
  let route: RouteOption | null = null
  if (routed.value) {
    if (preference.value !== leg.value.route_preference) {
      changes.route_preference = preference.value
    }
    if (!sameVia(via.value, leg.value.via)) {
      changes.via = via.value.length > 0 ? via.value : null
    }
    if (via.value.length === 0) {
      route = options.value[chosen.value] ?? null
    }
  }
  emit('save', leg.value, changes, route, null)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <form class="modal-box flex w-[min(96vw,40rem)] max-w-none flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('leg.editTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('leg.manualHint') }}</p>

      <template v-if="!composite">
        <label v-if="report || hadParts" class="floating-label">
          <span>{{ t('leg.mode') }}</span>
          <select v-model="form.mode" class="select w-full">
            <option v-for="mode in TRAVEL_MODES" :key="mode" :value="mode">{{ t(`modes.${mode}`) }}</option>
          </select>
        </label>
        <label class="floating-label">
          <span>{{ distanceLabel }}</span>
          <input v-model="form.distance" type="text" inputmode="decimal" class="input w-full" :placeholder="calculated" />
        </label>
        <div class="grid grid-cols-2 gap-3">
          <label class="floating-label">
            <span>{{ t('leg.hours') }}</span>
            <input v-model="form.hours" type="number" min="0" max="168" class="input w-full" :placeholder="t('leg.hours')" />
          </label>
          <label class="floating-label">
            <span>{{ t('leg.minutes') }}</span>
            <input v-model="form.minutes" type="number" min="0" max="59" class="input w-full" :placeholder="t('leg.minutes')" />
          </label>
        </div>
        <label class="floating-label">
          <span>{{ t('leg.cost') }}</span>
          <AmountInput v-model="form.cost" class="input w-full" :placeholder="t('leg.cost')" />
        </label>
        <label v-if="report" class="floating-label">
          <span>{{ t('report.actualCost') }}</span>
          <AmountInput v-model="form.actual" class="input w-full" :placeholder="t('report.actualCost')" />
        </label>
        <button type="button" class="btn btn-sm btn-hover-outline self-start" @click="addChange(0)">
          <AppIcon name="plus" />
          {{ t('leg.addChange') }}
        </button>
      </template>

      <template v-else>
        <p class="text-sm text-base-content/70">{{ t('leg.partsHint') }}</p>
        <ol class="flex flex-col gap-3">
          <li v-for="(part, index) in parts" :key="index" class="flex flex-col gap-3">
            <div class="flex flex-col gap-2 rounded-box border border-base-300 p-3">
              <div class="flex items-center justify-between gap-2">
                <span class="text-sm font-semibold">{{ t('leg.partNumber', { number: index + 1 }) }}</span>
              </div>
              <div class="grid gap-2 sm:grid-cols-2">
                <label class="floating-label">
                  <span>{{ t('leg.mode') }}</span>
                  <select v-model="part.mode" class="select w-full">
                    <option v-for="mode in TRAVEL_MODES" :key="mode" :value="mode">{{ t(`modes.${mode}`) }}</option>
                  </select>
                </label>
                <label class="floating-label">
                  <span>{{ t('leg.ticket') }}</span>
                  <select
                    class="select w-full"
                    :value="part.ticket === null ? '' : String(part.ticket)"
                    @change="chooseTicket(index, ($event.target as HTMLSelectElement).value)"
                  >
                    <option value="">{{ t('leg.noTicket') }}</option>
                    <option v-for="(_, ticketIndex) in tickets" :key="ticketIndex" :value="String(ticketIndex)">
                      {{ ticketLabel(ticketIndex) }}
                    </option>
                    <option value="new">{{ t('leg.newTicket') }}</option>
                  </select>
                </label>
              </div>
              <div class="grid grid-cols-2 gap-2">
                <label class="floating-label">
                  <span>{{ t('leg.hours') }}</span>
                  <input
                    v-model="part.hours"
                    type="number"
                    min="0"
                    max="168"
                    class="input w-full"
                    :placeholder="part.calculated === null ? t('leg.hours') : String(Math.floor(part.calculated / 3600))"
                  />
                </label>
                <label class="floating-label">
                  <span>{{ t('leg.minutes') }}</span>
                  <input
                    v-model="part.minutes"
                    type="number"
                    min="0"
                    max="59"
                    class="input w-full"
                    :placeholder="part.calculated === null ? t('leg.minutes') : String(Math.round((part.calculated % 3600) / 60))"
                  />
                </label>
              </div>
            </div>

            <div v-if="index < parts.length - 1" class="flex flex-col gap-2 border-s-2 border-dashed border-base-300 ps-3">
              <div class="flex items-center justify-between gap-2">
                <span class="text-sm font-semibold">{{ t('leg.change') }}</span>
                <button type="button" class="btn btn-ghost btn-xs" @click="removeChange(index)">
                  <AppIcon name="close" class="size-3.5!" />
                  {{ t('leg.removeChange') }}
                </button>
              </div>
              <div class="grid grid-cols-[minmax(0,1fr)_7rem] gap-2">
                <label class="floating-label">
                  <span>{{ t('leg.changeName') }}</span>
                  <input
                    v-model="part.stopName"
                    type="text"
                    required
                    maxlength="200"
                    class="input w-full"
                    :placeholder="t('leg.changeNamePlaceholder')"
                  />
                </label>
                <label class="floating-label">
                  <span>{{ t('leg.wait') }}</span>
                  <input v-model="part.wait" type="number" min="0" max="1440" class="input w-full" :placeholder="t('leg.wait')" />
                </label>
              </div>
              <details class="text-sm">
                <summary class="cursor-pointer text-base-content/70">
                  {{ part.stopWhere.lat && part.stopWhere.lng
                    ? t('leg.changeAt', { lat: part.stopWhere.lat, lng: part.stopWhere.lng })
                    : t('leg.changeWhere') }}
                </summary>
                <LocationField
                  v-model="part.stopWhere"
                  :focus="null"
                  :legend="t('leg.changeWhere')"
                  class="mt-2"
                  @named="(name) => { if (!part.stopName.trim()) part.stopName = name }"
                />
              </details>
            </div>
          </li>
        </ol>
        <button type="button" class="btn btn-sm btn-hover-outline self-start" @click="addChange(parts.length - 1)">
          <AppIcon name="plus" />
          {{ t('leg.addChange') }}
        </button>

        <section class="flex flex-col gap-2 border-t border-base-300 pt-3">
          <h3 class="font-semibold">{{ t('leg.tickets') }}</h3>
          <p v-if="tickets.length === 0" class="text-sm text-base-content/70">{{ t('leg.noTickets') }}</p>
          <div v-for="(ticket, index) in tickets" :key="index" class="flex flex-col gap-2 rounded-box border border-base-300 p-3">
            <div class="flex items-center justify-between gap-2">
              <span class="text-sm font-medium">
                {{ ticketLabel(index) }}
                <span v-if="coveredBy(index)" class="font-normal text-base-content/60">
                  · {{ coveredBy(index) }}
                </span>
              </span>
              <button
                type="button"
                class="btn btn-ghost btn-square btn-xs"
                :aria-label="t('leg.removeTicket', { name: ticketLabel(index) })"
                @click="removeTicket(index)"
              >
                <AppIcon name="close" class="size-3.5!" />
              </button>
            </div>
            <label class="floating-label">
              <span>{{ t('leg.ticketName') }}</span>
              <input v-model="ticket.name" type="text" maxlength="200" class="input w-full" :placeholder="t('leg.ticketNamePlaceholder')" />
            </label>
            <div class="grid gap-2" :class="report ? 'grid-cols-2' : ''">
              <label class="floating-label">
                <span>{{ t('leg.cost') }}</span>
                <AmountInput v-model="ticket.cost" class="input w-full" :placeholder="t('leg.cost')" />
              </label>
              <label v-if="report" class="floating-label">
                <span>{{ t('report.actualCost') }}</span>
                <AmountInput v-model="ticket.actual" class="input w-full" :placeholder="t('report.actualCost')" />
              </label>
            </div>
          </div>
          <button type="button" class="btn btn-sm btn-hover-outline self-start" @click="addTicket()">
            <AppIcon name="plus" />
            {{ t('leg.addTicket') }}
          </button>
        </section>
      </template>

      <label class="floating-label">
        <span>{{ t('leg.note') }}</span>
        <input v-model="form.note" type="text" maxlength="500" class="input w-full" :placeholder="t('leg.notePlaceholder')" />
      </label>

      <section v-if="routed && leg" class="flex flex-col gap-3 border-t border-base-300 pt-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 class="font-semibold">{{ t('leg.route') }}</h3>
          <div class="join" role="radiogroup" :aria-label="t('leg.route')">
            <button
              type="button"
              class="btn join-item btn-sm"
              :class="preference === 'fastest' ? 'btn-primary' : 'btn-hover-outline'"
              role="radio"
              :aria-checked="preference === 'fastest'"
              @click="setPreference('fastest')"
            >
              {{ t('leg.routeFastest') }}
            </button>
            <button
              type="button"
              class="btn join-item btn-sm"
              :class="preference === 'shortest' ? 'btn-primary' : 'btn-hover-outline'"
              role="radio"
              :aria-checked="preference === 'shortest'"
              :disabled="!config?.routing_shortest"
              :title="config?.routing_shortest ? undefined : t('leg.shortestUnavailable')"
              @click="setPreference('shortest')"
            >
              {{ t('leg.routeShortest') }}
            </button>
          </div>
        </div>
        <p v-if="leg.route_pinned && options.length === 0" class="text-sm text-base-content/70">
          {{ t('leg.routePinned') }}
        </p>

        <LegRouteMap
          v-if="config && mapRoutes.length > 0"
          :tile-url="config.map_tile_url"
          :attribution="config.map_attribution"
          :routes="mapRoutes"
          :selected="mapSelected"
          :via="via"
          @select="(index) => (chosen = index)"
        />

        <div v-if="canListOptions" class="flex flex-col gap-2">
          <button
            v-if="options.length === 0"
            type="button"
            class="btn btn-sm btn-hover-outline self-start"
            :disabled="loadingOptions || preference !== leg.route_preference"
            :title="preference !== leg.route_preference ? t('leg.routeOptionsSaveFirst') : undefined"
            @click="listOptions"
          >
            <span v-if="loadingOptions" class="loading loading-spinner loading-xs"></span>
            {{ t('leg.routeOptions') }}
          </button>
          <template v-else>
            <p class="text-sm text-base-content/70">{{ t('leg.routeOptionsHint') }}</p>
            <label
              v-for="(option, index) in options"
              :key="index"
              class="flex cursor-pointer items-center gap-3 rounded-box border px-3 py-2 text-sm"
              :class="chosen === index ? 'border-primary bg-primary/5' : 'border-base-300'"
            >
              <input v-model="chosen" type="radio" class="radio radio-sm" name="route-option" :value="index" />
              <span class="font-medium">{{ t('leg.routeOption', { number: index + 1 }) }}</span>
              <span class="text-base-content/70">{{ describeOption(option) }}</span>
            </label>
          </template>
        </div>

        <div class="flex flex-col gap-2">
          <div class="flex gap-2">
            <label class="floating-label flex-1">
              <span>{{ t('leg.googleLink') }}</span>
              <input
                v-model="link"
                type="url"
                class="input w-full"
                :placeholder="t('leg.googleLinkPlaceholder')"
                @keydown.enter.prevent="readLink"
              />
            </label>
            <button
              type="button"
              class="btn btn-hover-outline"
              :disabled="readingLink || link.trim() === ''"
              @click="readLink"
            >
              <span v-if="readingLink" class="loading loading-spinner loading-xs"></span>
              {{ t('leg.googleLinkRead') }}
            </button>
          </div>
          <p class="text-xs text-base-content/60">{{ t('leg.googleLinkHint') }}</p>
        </div>

        <div v-if="via.length > 0" class="flex flex-col gap-1">
          <div class="flex items-center justify-between gap-2">
            <span class="text-sm font-medium">{{ t('leg.viaCount', { count: via.length }, { plural: via.length }) }}</span>
            <button type="button" class="btn btn-ghost btn-xs" @click="via = []">{{ t('leg.viaClear') }}</button>
          </div>
          <ol class="max-h-32 overflow-y-auto text-xs text-base-content/70">
            <li v-for="(point, index) in via" :key="index" class="flex items-center gap-2">
              <span class="w-5 text-end">{{ index + 1 }}.</span>
              <span class="flex-1 tabular-nums">{{ point.lat.toFixed(5) }}, {{ point.lng.toFixed(5) }}</span>
              <button
                type="button"
                class="btn btn-ghost btn-square btn-xs"
                :aria-label="t('leg.viaRemove', { number: index + 1 })"
                @click="removeVia(index)"
              >
                <AppIcon name="close" class="size-3.5!" />
              </button>
            </li>
          </ol>
        </div>

        <a
          v-if="googleUrl"
          :href="googleUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="link link-hover self-start text-sm"
        >
          {{ t('leg.openInGoogle') }}
        </a>
        <p v-if="routeError" role="alert" class="text-sm text-error">{{ routeError }}</p>
      </section>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button
          v-if="leg && !costsOnly && (report || leg.route_pinned || composite)"
          type="button"
          class="btn btn-ghost me-auto"
          :disabled="busy"
          :title="t('leg.recalculateHint')"
          @click="emit('recalculate', leg)"
        >
          <span v-if="busy" class="loading loading-spinner loading-xs"></span>
          {{ t('leg.recalculate') }}
        </button>
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
