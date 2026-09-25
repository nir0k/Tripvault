<script setup lang="ts">
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GeoPoint } from '@/api/types'
import { useTileLayer } from '@/composables/useTileLayer'
import { decodePolyline } from '@/utils/polyline'

// The routes a leg could take, side by side on a small map, so a choice between
// them is a choice between roads one can see rather than between two numbers.
// The chosen line is drawn over the others; a click on another chooses it. The
// points a leg is routed through are drawn as dots along the way.
const props = defineProps<{
  tileUrl: string
  attribution: string
  /** Encoded lines, precision 5, in the order they are listed. */
  routes: string[]
  /** The index of the chosen route, or -1 when none is. */
  selected: number
  via: GeoPoint[]
}>()

const emit = defineEmits<{
  select: [index: number]
}>()

const { t } = useI18n()
const container = useTemplateRef<HTMLElement>('container')
const tiles = useTileLayer()

let map: L.Map | null = null
let layers: L.LayerGroup | null = null
let resizeObserver: ResizeObserver | null = null

// draw puts the routes and the via points on the map and frames them all.
function draw(): void {
  if (!map || !layers) {
    return
  }
  layers.clearLayers()
  const framed: L.LatLngExpression[] = []
  const style = getComputedStyle(container.value ?? document.body)
  const chosen = style.getPropertyValue('--color-primary').trim() || '#2563eb'
  const other = style.getPropertyValue('--color-neutral').trim() || '#6b7280'

  // The chosen route is drawn last, so it lies on top where the lines share a road.
  const order = props.routes.map((_, index) => index).sort((a, b) =>
    Number(a === props.selected) - Number(b === props.selected))
  for (const index of order) {
    const line = decodePolyline(props.routes[index] ?? '')
    if (line.length < 2) {
      continue
    }
    framed.push(...line)
    const active = index === props.selected
    const polyline = L.polyline(line, {
      color: active ? chosen : other,
      weight: active ? 6 : 4,
      opacity: active ? 0.95 : 0.55,
      dashArray: active ? undefined : '6 6',
    }).addTo(layers)
    polyline.bindTooltip(t('leg.routeOption', { number: index + 1 }), { sticky: true })
    polyline.on('click', () => emit('select', index))
  }
  for (const point of props.via) {
    framed.push([point.lat, point.lng])
    L.circleMarker([point.lat, point.lng], {
      radius: 5, color: chosen, weight: 2, fillColor: '#ffffff', fillOpacity: 1,
    }).addTo(layers)
  }
  if (framed.length > 0) {
    map.fitBounds(L.latLngBounds(framed), { padding: [16, 16], maxZoom: 15 })
  }
}

onMounted(() => {
  if (!container.value) {
    return
  }
  map = L.map(container.value, { zoomControl: true, worldCopyJump: true, attributionControl: true })
  tiles.attach(map, props.tileUrl, props.attribution)
  layers = L.layerGroup().addTo(map)
  draw()
  // The map is made inside a dialog that may still be settling its size.
  resizeObserver = new ResizeObserver(() => {
    map?.invalidateSize()
    draw()
  })
  resizeObserver.observe(container.value)
})

watch(() => [props.routes, props.selected, props.via], draw, { deep: true })

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  map?.remove()
  map = null
})
</script>

<template>
  <div class="relative isolate h-56 overflow-hidden rounded-box border border-base-300">
    <div ref="container" class="size-full"></div>
    <p v-if="tiles.failed.value" class="absolute inset-x-2 bottom-2 z-[500] rounded bg-base-100/90 p-1 text-xs text-error">
      {{ t('map.tilesUnavailable', { host: tiles.host.value }) }}
    </p>
  </div>
</template>
