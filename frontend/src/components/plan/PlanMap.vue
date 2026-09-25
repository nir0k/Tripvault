<script setup lang="ts">
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTileLayer } from '@/composables/useTileLayer'
import { loadMediaUrl, useMediaBase } from '@/composables/useMediaUrl'
import { mediaThumbnailPath } from '@/api/media'
import type { Leg, PlanItem, TripDocument } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { OUTLINE } from '@/components/icons'
import { formatClock, formatDistance } from '@/utils/format'
import { markdownExcerpt } from '@/utils/markdown'
import { dayColor, isVisit, itemIcon, itemKindLabel } from '@/utils/plan'
import { activeUnits } from '@/utils/units'
import { decodePolyline, type LatLng } from '@/utils/polyline'

// The map of a plan: places, stays and the lines of the legs, each day in its
// own colour. Every day of the document is drawn at once, because a trip is
// read as a whole; the day being looked at is drawn heavier than the others, so
// it stands out without the rest disappearing. Everything drawn comes from the
// document; the map only reports which element a person wants to read about.
const props = defineProps<{
  document: TripDocument
  /** The day open in the plan, drawn heavier; null while the stays are shown. */
  selectedDay: number | null
  tileUrl: string
  attribution: string
  /** Whether a click on the map offers to add a place there. */
  canEdit?: boolean
}>()

const emit = defineEmits<{
  focus: [itemId: string, dayIndex: number | null]
  addAt: [lat: number, lng: number]
}>()

const { t, locale } = useI18n()
const tiles = useTileLayer()
// Where the pictures of the popups are read from: the application's own path,
// or the one a read-only link opens.
const mediaBase = useMediaBase()

/** The weight and opacity a line is drawn with, open day against the rest. */
const CURRENT_LINE = { weight: 6, opacity: 0.95 }
const OTHER_LINE = { weight: 3, opacity: 0.45 }

const container = useTemplateRef<HTMLDivElement>('container')
const frame = useTemplateRef<HTMLDivElement>('frame')
const showDistances = ref(false)
const empty = ref(false)
const fullscreen = ref(false)
const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled

let map: L.Map | null = null
let layers: L.LayerGroup | null = null
let bounds: L.LatLngBounds | null = null
// The open day's own bounds: what the map frames while a day is being read,
// instead of pulling back to the whole trip.
let dayBounds: L.LatLngBounds | null = null
// The marker of every element drawn, so the plan can send the map to one.
const markers = new Map<string, L.Marker>()
let resizeObserver: ResizeObserver | null = null

// Every day of the document, with its position, which is what its colour and
// its weight are read from.
const shownDays = computed(() => props.document.days.map((day, index) => ({ day, index })))

// PinGlyph is what a pin may show: a place's category, an activity's type, a
// stay or a flight - any of the outline shapes.
type PinGlyph = keyof typeof OUTLINE

// pinIcon draws a round pin with a glyph and, for a place of a day, a small
// badge with its number in the day - the same number the list beside the map
// shows. Only the fixed glyph paths, the colours above and an integer reach the
// HTML, never text people typed.
function pinIcon(glyph: PinGlyph, color: string, muted = false, number?: number): L.DivIcon {
  const svg = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${OUTLINE[glyph]}"/></svg>`
  const badge = number === undefined ? '' : `<span class="map-pin-number">${Math.trunc(number)}</span>`
  return L.divIcon({
    className: 'map-pin-wrapper',
    html: `<span class="map-pin${muted ? ' map-pin-muted' : ''}" style="--pin:${color}">${svg}${badge}</span>`,
    iconSize: [30, 30],
    iconAnchor: [15, 15],
    popupAnchor: [0, -14],
  })
}

// popupPicture is the picture a popup shows: the place's cover, or its first
// picture when it has no cover.
function popupPicture(item: PlanItem): string | null {
  return item.cover_media_id ?? item.media[0]?.id ?? null
}

/**
 * popupFor builds a marker's popup from DOM nodes, so names and text people
 * typed are never parsed as HTML. It is built when the popup opens: the picture
 * at its top is fetched only then, and let go when the popup closes.
 *
 * Arguments:
 *   - item: the place, activity or stay mark the marker stands for.
 *   - dayIndex: the day it belongs to, or null for an unassigned idea.
 *   - marker: the marker the popup opens on.
 *
 * Returns:
 *   - the popup's content.
 */
function popupFor(item: PlanItem, dayIndex: number | null, marker: L.Marker): HTMLElement {
  const box = document.createElement('div')
  box.className = 'map-popup space-y-1'

  const pictureId = popupPicture(item)
  if (pictureId) {
    const frame = document.createElement('div')
    frame.className = 'map-popup-picture'
    box.append(frame)
    const load = loadMediaUrl(mediaThumbnailPath(mediaBase, pictureId, 320))
    let objectUrl: string | null = null
    load.done.then((url) => {
      objectUrl = url
      const image = document.createElement('img')
      image.src = url
      image.alt = ''
      frame.append(image)
    }, () => frame.remove())
    marker.once('popupclose', () => {
      load.cancel()
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl)
      }
    })
  }

  const name = document.createElement('p')
  name.className = 'font-semibold'
  name.textContent = item.name
  box.append(name)

  const details = document.createElement('p')
  const parts: string[] = []
  if (isVisit(item)) {
    parts.push(itemKindLabel(item, t))
  }
  if (dayIndex !== null) {
    parts.push(t('plan.dayNumber', { n: dayIndex + 1 }))
  } else {
    parts.push(t('plan.unassigned'))
  }
  // A report knows when the place was really reached; a plan, when it should be.
  if (item.actual_time) {
    parts.push(item.actual_end_time ? `${item.actual_time}–${item.actual_end_time}` : item.actual_time)
  } else if (item.schedule) {
    parts.push(formatClock(item.anchor === 'morning' ? item.schedule.departure_minutes : item.schedule.arrival_minutes).time)
  }
  details.textContent = parts.join(' · ')
  box.append(details)

  // What happened there when the report says, else what the plan said about it.
  const excerpt = markdownExcerpt(item.story_md || item.description_md)
  if (excerpt) {
    const text = document.createElement('p')
    text.className = 'map-popup-text'
    text.textContent = excerpt
    box.append(text)
  }

  const button = document.createElement('button')
  button.type = 'button'
  button.className = 'btn btn-xs btn-primary mt-1'
  button.textContent = t('map.toDescription')
  button.addEventListener('click', () => {
    map?.closePopup()
    // The description is on the page behind the map, which a map on the whole
    // screen would keep covering.
    if (document.fullscreenElement) {
      void document.exitFullscreen()
    }
    emit('focus', item.id, dayIndex)
  })
  box.append(button)
  return box
}

// legPoints finds a leg's line: its stored geometry, else the straight line
// between its ends when both have positions. An element with a recording is
// left where the recording ended and reached where it began, as the backend
// routes it.
function legPoints(leg: Leg, items: Map<string, PlanItem>): LatLng[] {
  if (leg.geometry) {
    return decodePolyline(leg.geometry)
  }
  const from = items.get(leg.from_item_id)
  const to = items.get(leg.to_item_id)
  const start = from?.track ? decodePolyline(from.track.geometry).at(-1) : undefined
  const end = to?.track ? decodePolyline(to.track.geometry)[0] : undefined
  const first = start ?? (from?.lat != null && from.lng != null ? [from.lat, from.lng] as LatLng : undefined)
  const last = end ?? (to?.lat != null && to.lng != null ? [to.lat, to.lng] as LatLng : undefined)
  return first && last ? [first, last] : []
}

// draw replaces everything on the map with the current document. Every day is
// drawn; the open one is drawn heavier, and the others are dimmed rather than
// hidden, so the shape of the whole trip stays on the map.
function draw(): void {
  if (!map || !layers) {
    return
  }
  layers.clearLayers()
  markers.clear()
  const points: LatLng[] = []
  const dayPoints: LatLng[] = []
  // Every element of the document, because the leg that opens a day starts at
  // the last element of an earlier one.
  const items = new Map(props.document.days.flatMap((day) => day.items).map((item) => [item.id, item]))

  for (const { day, index } of shownDays.value) {
    const color = dayColor(index)
    const current = props.selectedDay === null || index === props.selectedDay
    const line = current ? CURRENT_LINE : OTHER_LINE
    // The points of the open day are collected apart: they are what the map
    // frames while that day is being read.
    const collect = (positions: LatLng[]): void => {
      points.push(...positions)
      if (current && props.selectedDay !== null) {
        dayPoints.push(...positions)
      }
    }

    for (const leg of day.legs) {
      const route = legPoints(leg, items)
      if (route.length < 2) {
        continue
      }
      collect(route)
      // Roads are solid; straight lines and estimates are dashed.
      const polyline = L.polyline(route, {
        color, ...line, dashArray: leg.source === 'provider' ? undefined : '8 8',
      }).addTo(layers)
      if (leg.mode === 'flight') {
        const middle = route[Math.floor(route.length / 2)] ?? route[0]!
        L.marker(middle, { icon: pinIcon('flight', color), interactive: false }).addTo(layers)
      }
      if (showDistances.value && leg.distance_m !== null) {
        polyline.bindTooltip(formatDistance(leg.distance_m, locale.value, activeUnits.value), {
          permanent: true, direction: 'center', className: 'map-distance',
        })
      }
    }

    // A place or an activity may carry a recording of its own - a hike, a walk
    // around a lake - which is drawn beside the legs rather than instead of
    // them: the drive to the start of a hike is still a journey of the day.
    for (const item of day.items) {
      const recorded = item.track ? decodePolyline(item.track.geometry) : []
      if (!item.track || recorded.length < 2) {
        continue
      }
      collect(recorded)
      const track = L.polyline(recorded, { color, ...line }).addTo(layers)
      if (showDistances.value) {
        track.bindTooltip(formatDistance(item.track.distance_m, locale.value, activeUnits.value), {
          permanent: true, direction: 'center', className: 'map-distance',
        })
      }
    }

    // Places are numbered in their order in the day, counting those without a
    // position too, so a pin carries the number its card in the list shows.
    let placeNumber = 0
    for (const item of day.items) {
      const number = isVisit(item) ? ++placeNumber : undefined
      if (item.lat === null || item.lng === null) {
        continue
      }
      collect([[item.lat, item.lng]])
      const glyph = item.kind === 'stay_anchor' ? 'stay' : itemIcon(item)
      const marker = L.marker([item.lat, item.lng], {
        icon: pinIcon(glyph, item.kind === 'stay_anchor' ? '#334155' : color, item.is_optional || !current, number),
        title: item.name,
      })
      marker.bindPopup(() => popupFor(item, index, marker)).addTo(layers)
      markers.set(item.id, marker)
    }
  }

  // Ideas without a day belong to no day's colour, so they are grey and dimmed.
  for (const item of props.document.unassigned) {
    if (item.lat === null || item.lng === null) {
      continue
    }
    points.push([item.lat, item.lng])
    const marker = L.marker([item.lat, item.lng], { icon: pinIcon(itemIcon(item), '#6b7280', true), title: item.name })
    marker.bindPopup(() => popupFor(item, null, marker)).addTo(layers)
    markers.set(item.id, marker)
  }

  empty.value = points.length === 0
  bounds = points.length > 0 ? L.latLngBounds(points) : null
  dayBounds = dayPoints.length > 0 ? L.latLngBounds(dayPoints) : null
}

/**
 * fit frames the day being read, or everything drawn when no day is open, or
 * the whole world when nothing is drawn at all.
 */
function fit(): void {
  if (!map) {
    return
  }
  const framed = dayBounds ?? bounds
  if (!framed) {
    map.setView([30, 0], 2)
  } else if (framed.getNorthEast().equals(framed.getSouthWest())) {
    map.setView(framed.getCenter(), 13)
  } else {
    map.fitBounds(framed, { padding: [32, 32], maxZoom: 15 })
  }
}

/**
 * locate - sends the map to one element of the document, which is how a card in
 * the plan shows where it is.
 *
 * Arguments:
 *   - itemId: the place, stay mark or unassigned idea to go to.
 *
 * Returns:
 *   - true when the element is on the map; false when it has no position yet.
 */
function locate(itemId: string): boolean {
  const marker = markers.get(itemId)
  if (!map || !marker) {
    return false
  }
  map.setView(marker.getLatLng(), Math.max(map.getZoom(), 15))
  marker.openPopup()
  return true
}

// toggleFullscreen puts the map alone on the screen and back.
async function toggleFullscreen(): Promise<void> {
  if (document.fullscreenElement) {
    await document.exitFullscreen()
  } else {
    await frame.value?.requestFullscreen()
  }
}

// onFullscreenChange follows the browser's fullscreen state.
function onFullscreenChange(): void {
  fullscreen.value = document.fullscreenElement === frame.value
  map?.invalidateSize()
}

onMounted(() => {
  if (!container.value) {
    return
  }
  map = L.map(container.value, { zoomControl: true, worldCopyJump: true })
  tiles.attach(map, props.tileUrl, props.attribution)
  layers = L.layerGroup().addTo(map)
  map.on('click', (event: L.LeafletMouseEvent) => {
    if (!props.canEdit || !map) {
      return
    }
    const { lat, lng } = event.latlng.wrap()
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'btn btn-xs btn-primary'
    button.textContent = t('map.addHere')
    button.addEventListener('click', () => {
      map?.closePopup()
      emit('addAt', Number(lat.toFixed(6)), Number(lng.toFixed(6)))
    })
    L.popup().setLatLng(event.latlng).setContent(button).openOn(map)
  })
  draw()
  fit()
  resizeObserver = new ResizeObserver(() => map?.invalidateSize())
  resizeObserver.observe(container.value)
  document.addEventListener('fullscreenchange', onFullscreenChange)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  map?.remove()
  map = null
})

// A newly opened day is framed; an edit to the same view is redrawn in place,
// so the map does not jump after every change.
watch(() => props.selectedDay, () => {
  draw()
  fit()
})
watch(() => [props.document, showDistances.value, locale.value], () => {
  // The first positions on an empty map are framed, like a new day would be.
  const wasEmpty = bounds === null
  draw()
  if (wasEmpty && bounds !== null) {
    fit()
  }
})

defineExpose({ locate })
</script>

<template>
  <div ref="frame" class="relative isolate h-full min-h-80 overflow-hidden rounded-box border border-base-300 bg-base-200">
    <!-- isolate keeps Leaflet's panes, stacked in the hundreds, inside the map,
         so the page's floating buttons stay above it while it scrolls past. -->
    <div ref="container" class="absolute inset-0" :aria-label="t('map.label')" role="region"></div>

    <div class="absolute top-2 right-2 left-14 z-[1000] flex flex-wrap justify-end gap-1">
      <button type="button" class="btn btn-xs shadow" :class="{ 'btn-active': showDistances }" :aria-pressed="showDistances" @click="showDistances = !showDistances">
        {{ t('map.distances') }}
      </button>
      <button type="button" class="btn btn-xs shadow" @click="fit">{{ t('map.fit') }}</button>
      <button v-if="canFullscreen" type="button" class="btn btn-xs btn-square shadow" :aria-label="fullscreen ? t('map.exitFullscreen') : t('map.fullscreen')" @click="toggleFullscreen">
        <AppIcon :name="fullscreen ? 'collapse' : 'expand'" />
      </button>
    </div>

    <p v-if="empty" class="absolute bottom-8 left-1/2 z-[1000] -translate-x-1/2 rounded-box bg-base-100/90 px-3 py-2 text-center text-sm shadow">
      {{ t('map.empty') }}
    </p>

    <p
      v-if="tiles.failed.value"
      role="status"
      class="pointer-events-none absolute top-14 left-1/2 z-[1000] -translate-x-1/2 rounded-box bg-base-100/90 px-3 py-2 text-center text-sm shadow"
    >
      {{ t('map.tilesUnavailable', { host: tiles.host.value }) }}
    </p>
  </div>
</template>
