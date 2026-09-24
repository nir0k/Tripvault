<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/api/client'
import { getClientConfig } from '@/api/config'
import { parseLink, reversePlace, searchPlaces } from '@/api/geo'
import type { ClientConfig, GeoPlace } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MapPicker from '@/components/MapPicker.vue'
import { errorMessage } from '@/utils/errors'

/** LocationModel is a position as a form edits it: text fields, as typed. */
export interface LocationModel {
  address: string
  lat: string
  lng: string
}

// Where a place is, found in one of three ways: a search by name or address, a
// pasted pair of coordinates or map link, or a click on a small map. A found
// position fills the address by reverse lookup unless somebody typed one.
const props = defineProps<{
  /** Ranks search results near this point first. */
  focus: { lat: number; lng: number } | null
}>()
const model = defineModel<LocationModel>({ required: true })

const emit = defineEmits<{
  /** A search result or a link named the place. */
  named: [name: string, ref: string]
}>()

const { t, te, locale } = useI18n()

const config = ref<ClientConfig | null>(null)
const query = ref('')
const results = ref<GeoPlace[]>([])
const searching = ref(false)
const searched = ref(false)
const link = ref('')
const showMap = ref(false)
const message = ref('')
// The address came from a lookup and may be replaced by the next one.
let addressIsLookedUp = false
let searchTimer: ReturnType<typeof setTimeout> | undefined
let generation = 0

const geocodingEnabled = computed(() => config.value?.geocoding_enabled ?? false)
const lat = computed(() => (model.value.lat.trim() === '' ? null : Number(model.value.lat.replace(',', '.'))))
const lng = computed(() => (model.value.lng.trim() === '' ? null : Number(model.value.lng.replace(',', '.'))))

onMounted(async () => {
  try {
    config.value = await getClientConfig()
  } catch {
    // Without the configuration the search and the map stay hidden.
  }
})
onBeforeUnmount(() => clearTimeout(searchTimer))

// The search waits for a pause in typing, and shows only the newest answer.
watch(query, (text) => {
  clearTimeout(searchTimer)
  generation++
  results.value = []
  searched.value = false
  const trimmed = text.trim()
  if (trimmed.length < 2 || !geocodingEnabled.value) {
    searching.value = false
    return
  }
  searching.value = true
  searchTimer = setTimeout(async () => {
    const current = ++generation
    try {
      const found = await searchPlaces(trimmed, locale.value, props.focus)
      if (current === generation) {
        results.value = found
        message.value = ''
      }
    } catch (err) {
      if (current === generation) {
        message.value = errorMessage(err, t, te)
      }
    } finally {
      if (current === generation) {
        searching.value = false
        searched.value = true
      }
    }
  }, 350)
})

// setPosition stores a position with six decimals, about ten centimetres. The
// model is written once per change: the parent's new value arrives only after
// it re-renders, so a second write in a row would start from the old one.
function setPosition(nextLat: number, nextLng: number, address?: string): void {
  model.value = {
    ...model.value,
    lat: nextLat.toFixed(6),
    lng: nextLng.toFixed(6),
    ...(address === undefined ? {} : { address }),
  }
}

// choose takes a search result: position, address and name.
function choose(place: GeoPlace): void {
  setPosition(place.lat, place.lng, place.label)
  addressIsLookedUp = true
  emit('named', place.name, place.ref)
  query.value = ''
  results.value = []
}

/**
 * lookUpAddress fills the address of a position - the given one, else the
 * current one - by reverse lookup, unless somebody typed an address that
 * should stay.
 */
async function lookUpAddress(point?: { lat: number; lng: number }): Promise<void> {
  const position = point ?? (lat.value !== null && lng.value !== null ? { lat: lat.value, lng: lng.value } : null)
  if (!geocodingEnabled.value || position === null) {
    return
  }
  if (model.value.address.trim() !== '' && !addressIsLookedUp) {
    return
  }
  try {
    const [nearest] = await reversePlace(position.lat, position.lng, locale.value)
    if (nearest) {
      model.value = { ...model.value, address: nearest.label }
      addressIsLookedUp = true
    }
  } catch {
    // The position is kept; the address can be typed by hand.
  }
}

// applyLink reads a position from pasted coordinates or a map link.
async function applyLink(): Promise<void> {
  const text = link.value.trim()
  if (!text) {
    return
  }
  message.value = ''
  try {
    const location = await parseLink(text)
    setPosition(location.lat, location.lng)
    if (location.name) {
      emit('named', location.name, '')
    }
    link.value = ''
    await lookUpAddress(location)
  } catch (err) {
    message.value = err instanceof ApiError && err.code === 'validation_failed'
      ? t('location.unrecognized')
      : errorMessage(err, t, te)
  }
}

// onMapPick takes a position clicked on the map.
async function onMapPick(nextLat: number, nextLng: number): Promise<void> {
  setPosition(nextLat, nextLng)
  await lookUpAddress({ lat: nextLat, lng: nextLng })
}

// onAddressInput marks the address as typed, so lookups leave it alone.
function onAddressInput(): void {
  addressIsLookedUp = false
}

// clear removes the position.
function clear(): void {
  model.value = { ...model.value, lat: '', lng: '' }
}

defineExpose({ lookUpAddress })
</script>

<template>
  <fieldset class="fieldset space-y-2 rounded-box border border-base-300 p-3">
    <legend class="fieldset-legend">{{ t('location.title') }}</legend>

    <div v-if="geocodingEnabled" class="relative">
      <label class="input w-full">
        <AppIcon name="search" />
        <input
          v-model="query"
          type="search"
          autocomplete="off"
          :placeholder="t('location.search')"
          :aria-label="t('location.search')"
          @keydown.enter.prevent
        />
        <span v-if="searching" class="loading loading-spinner loading-xs"></span>
      </label>
      <ul v-if="results.length > 0" class="menu absolute z-30 mt-1 max-h-64 w-full flex-nowrap overflow-y-auto rounded-box border border-base-300 bg-base-100 p-1 shadow-lg">
        <li v-for="place in results" :key="place.ref || `${place.lat},${place.lng}`">
          <button type="button" class="flex flex-col items-start gap-0" @click="choose(place)">
            <span class="font-medium">{{ place.name }}</span>
            <span class="text-xs text-base-content/70">{{ place.label }}</span>
          </button>
        </li>
      </ul>
      <p v-else-if="searched && query.trim().length >= 2" class="mt-1 text-sm text-base-content/70">{{ t('location.noResults') }}</p>
    </div>
    <p v-else-if="config" class="text-sm text-base-content/70">{{ t('location.searchDisabled') }}</p>

    <div class="flex gap-2">
      <input
        v-model="link"
        type="text"
        class="input w-full"
        :placeholder="t('location.link')"
        :aria-label="t('location.link')"
        @keydown.enter.prevent="applyLink"
      />
      <button type="button" class="btn" :disabled="link.trim() === ''" @click="applyLink">{{ t('location.apply') }}</button>
    </div>

    <button v-if="config" type="button" class="btn btn-ghost btn-sm justify-start" :aria-expanded="showMap" @click="showMap = !showMap">
      <AppIcon name="map" />
      {{ showMap ? t('location.hideMap') : t('location.pickOnMap') }}
    </button>
    <MapPicker
      v-if="showMap && config"
      :lat="lat"
      :lng="lng"
      :focus="focus"
      :tile-url="config.map_tile_url"
      :attribution="config.map_attribution"
      @pick="onMapPick"
    />

    <label class="floating-label">
      <span>{{ t('place.address') }}</span>
      <input v-model="model.address" type="text" maxlength="500" class="input w-full" :placeholder="t('place.address')" @input="onAddressInput" />
    </label>
    <div class="flex flex-wrap items-center gap-2 text-sm">
      <span v-if="lat !== null && lng !== null" class="font-mono">{{ model.lat }}, {{ model.lng }}</span>
      <span v-else class="text-base-content/70">{{ t('location.noPosition') }}</span>
      <button v-if="lat !== null" type="button" class="btn btn-ghost btn-xs" @click="clear">{{ t('location.clear') }}</button>
    </div>

    <p v-if="message" role="alert" class="text-sm text-error">{{ message }}</p>
  </fieldset>
</template>
