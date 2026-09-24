<script setup lang="ts">
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTileLayer } from '@/composables/useTileLayer'

// A small map to pick a position with a click. It shows the current position,
// or starts around the focus when there is none.
const props = defineProps<{
  lat: number | null
  lng: number | null
  focus: { lat: number; lng: number } | null
  tileUrl: string
  attribution: string
}>()

const emit = defineEmits<{
  pick: [lat: number, lng: number]
}>()

const { t } = useI18n()
const container = useTemplateRef<HTMLDivElement>('container')
const tiles = useTileLayer()
let map: L.Map | null = null
let marker: L.CircleMarker | null = null

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
  if (props.lat !== null && props.lng !== null) {
    map.setView([props.lat, props.lng], 14)
  } else if (props.focus) {
    map.setView([props.focus.lat, props.focus.lng], 10)
  } else {
    map.setView([30, 0], 2)
  }
  placeMarker()
  map.on('click', (event: L.LeafletMouseEvent) => {
    const { lat, lng } = event.latlng.wrap()
    emit('pick', Number(lat.toFixed(6)), Number(lng.toFixed(6)))
  })
  // The picker opens inside a dialog that may still be laying out.
  setTimeout(() => map?.invalidateSize(), 50)
})

onBeforeUnmount(() => {
  map?.remove()
  map = null
})

watch(() => [props.lat, props.lng], placeMarker)
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
