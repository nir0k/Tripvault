<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientConfig } from '@/api/config'
import { legAlternatives, readGoogleLink, type LegChanges } from '@/api/documents'
import {
  TRAVEL_MODES, type ClientConfig, type GeoPoint, type Leg, type RouteOption, type RoutePreference, type TravelMode,
} from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import AppIcon from '@/components/AppIcon.vue'
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
const props = defineProps<{
  /** The leg belongs to a report, whose form also offers the means. */
  report?: boolean
  /** Something is being saved or calculated. */
  busy?: boolean
  /** Only the figures and costs: the form is opened from the list of costs. */
  costsOnly?: boolean
}>()

const emit = defineEmits<{
  save: [leg: Leg, changes: LegChanges, route: RouteOption | null]
  recalculate: [leg: Leg]
}>()

const { t, te, locale } = useI18n()

/** ROUTED_MODES are the modes that follow roads, and so can be routed one way or another. */
const ROUTED_MODES: TravelMode[] = ['walk', 'bike', 'car', 'transit']

/** GOOGLE_TRAVEL_MODES name a mode the way a Google Maps link does. */
const GOOGLE_TRAVEL_MODES: Partial<Record<TravelMode, string>> = {
  walk: 'walking', bike: 'bicycling', car: 'driving', transit: 'transit',
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

const routed = computed(() => !props.costsOnly && ROUTED_MODES.includes(form.mode)
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
  emit('save', leg.value, changes, route)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex w-[min(96vw,40rem)] max-w-none flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('leg.editTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('leg.manualHint') }}</p>

      <label v-if="report" class="floating-label">
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
          v-if="leg && !costsOnly && (report || leg.route_pinned)"
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
