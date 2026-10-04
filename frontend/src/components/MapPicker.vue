<script setup lang="ts">
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTileLayer } from '@/composables/useTileLayer'
import { nearestOnLine, type LatLng } from '@/utils/polyline'

// A small map to pick a position with a click. It shows the current position,
// or starts around the focus when there is none.
//
// Given a line - the track a stop is put on - it shows the line, frames it, and
// a click picks the nearest point of the line rather than the point clicked,
// which is where the server will put it too.
//
// Given a circle - the zone hidden around a home - it shows the circle too and
// keeps it in view.
const props = defineProps<{
  lat: number | null
  lng: number | null
  focus: { lat: number; lng: number } | null
  tileUrl: string
  attribution: string
  /** A line the position is picked on. */
  line?: LatLng[]
  /** A circle shown beside the position, its radius in metres. */
  circle?: { lat: number; lng: number; radiusM: number } | null
}>()

const emit = defineEmits<{
  pick: [lat: number, lng: number]
}>()

const { t } = useI18n()
const container = useTemplateRef<HTMLDivElement>('container')
const tiles = useTileLayer()
let map: L.Map | null = null
let marker: L.CircleMarker | null = null
let area: L.Circle | null = null

// placeCircle draws the circle where it now is, or removes it, and frames it.
function placeCircle(): void {
  if (!map) {
    return
  }
  if (!props.circle) {
    area?.remove()
    area = null
    return
  }
  const center: L.LatLngExpression = [props.circle.lat, props.circle.lng]
  if (area) {
    area.setLatLng(center)
    area.setRadius(props.circle.radiusM)
  } else {
    area = L.circle(center, {
      radius: props.circle.radiusM, color: '#c2410c', weight: 2, fillOpacity: 0.12, interactive: false,
    }).addTo(map)
  }
  map.fitBounds(area.getBounds(), { padding: [16, 16] })
}

// placeMarker moves the marker to the current position, or removes it.
function placeMarker(): void {
  if (!map) {
    return
  }
  if (props.lat === null || props.lng === null) {
    marker?.remove()
    marker = null
    return
  }
  const position: L.LatLngExpression = [props.lat, props.lng]
  if (marker) {
    marker.setLatLng(position)
  } else {
    marker = L.circleMarker(position, { radius: 9, color: '#ffffff', weight: 3, fillColor: '#c2410c', fillOpacity: 1 }).addTo(map)
  }
}

onMounted(() => {
  if (!container.value) {
    return
  }
  map = L.map(container.value)
  tiles.attach(map, props.tileUrl, props.attribution)
  const line = props.line && props.line.length > 1 ? props.line : null
  if (line) {
    L.polyline(line, { color: '#1f2937', weight: 7, opacity: 0.7, interactive: false }).addTo(map)
    L.polyline(line, { color: '#c2410c', weight: 4, interactive: false }).addTo(map)
    map.fitBounds(L.latLngBounds(line), { padding: [16, 16] })
  } else if (props.lat !== null && props.lng !== null) {
    map.setView([props.lat, props.lng], 14)
  } else if (props.focus) {
    map.setView([props.focus.lat, props.focus.lng], 10)
  } else {
    map.setView([30, 0], 2)
  }
  placeMarker()
  placeCircle()
  map.on('click', (event: L.LeafletMouseEvent) => {
    const { lat, lng } = event.latlng.wrap()
    const [pickedLat, pickedLng] = line ? nearestOnLine(line, [lat, lng]) : [lat, lng]
    emit('pick', Number(pickedLat.toFixed(6)), Number(pickedLng.toFixed(6)))
  })
  // The picker opens inside a dialog that may still be laying out.
  setTimeout(() => {
    map?.invalidateSize()
    placeCircle()
  }, 50)
})

onBeforeUnmount(() => {
  map?.remove()
  map = null
})

watch(() => [props.lat, props.lng], placeMarker)
watch(() => props.circle, placeCircle, { deep: true })
</script>

<template>
  <div class="relative">
    <div ref="container" class="h-56 w-full rounded-box border border-base-300" role="application"></div>
    <p
      v-if="tiles.failed.value"
      role="status"
      class="pointer-events-none absolute inset-x-2 top-2 rounded-box bg-base-100/90 p-2 text-center text-xs"
    >
      {{ t('map.tilesUnavailable', { host: tiles.host.value }) }}
    </p>
  </div>
</template>
