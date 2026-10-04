<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getClientConfig } from '@/api/config'
import { deleteHome, getHome, setHome } from '@/api/me'
import type { ClientConfig, HomeSettings } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import MapPicker from '@/components/MapPicker.vue'
import { errorMessage } from '@/utils/errors'
import { formatDistance } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// Where the person lives. The circle around it is kept out of everything their
// trips show to a reader from outside them - read-only links and every PDF - so
// a report shared from the front door does not print the address. The server
// moves the circle's centre off the home when it is saved; until then the map
// shows the circle around the point itself, as a preview of its size.

const MIN_RADIUS = 200
const MAX_RADIUS = 2000
const DEFAULT_RADIUS = 500

const { t, te, locale } = useI18n()

const config = ref<ClientConfig | null>(null)
const saved = ref<HomeSettings>({ home: null, zone: null })
const location = ref<LocationModel>({ address: '', lat: '', lng: '' })
const radius = ref(DEFAULT_RADIUS)
const busy = ref(false)
const message = ref('')
const error = ref('')

const lat = computed(() => (location.value.lat.trim() === '' ? null : Number(location.value.lat.replace(',', '.'))))
const lng = computed(() => (location.value.lng.trim() === '' ? null : Number(location.value.lng.replace(',', '.'))))

/** unchanged says the form still holds the home as it is stored. */
const unchanged = computed(() => {
  const home = saved.value.home
  return home !== null && home.lat === lat.value && home.lng === lng.value && home.radius_m === radius.value
})

/** circle is the stored zone while nothing changed, else the preview around the point. */
const circle = computed(() => {
  if (unchanged.value && saved.value.zone) {
    return { lat: saved.value.zone.lat, lng: saved.value.zone.lng, radiusM: saved.value.zone.radius_m }
  }
  if (lat.value === null || lng.value === null) {
    return null
  }
  return { lat: lat.value, lng: lng.value, radiusM: radius.value }
})

// show puts a stored home into the form.
function show(settings: HomeSettings): void {
  saved.value = settings
  location.value = {
    address: '',
    lat: settings.home ? String(settings.home.lat) : '',
    lng: settings.home ? String(settings.home.lng) : '',
  }
  radius.value = settings.home?.radius_m ?? DEFAULT_RADIUS
}

onMounted(async () => {
  try {
    const [settings, loaded] = await Promise.all([getHome(), getClientConfig()])
    show(settings)
    config.value = loaded
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
})

// onPick moves the home to a point clicked on the map.
function onPick(nextLat: number, nextLng: number): void {
  location.value = { ...location.value, lat: nextLat.toFixed(6), lng: nextLng.toFixed(6) }
}

// save stores the home; the server answers with the circle it drew.
async function save(): Promise<void> {
  if (lat.value === null || lng.value === null) {
    return
  }
  busy.value = true
  message.value = ''
  error.value = ''
  try {
    show(await setHome({ lat: lat.value, lng: lng.value, radius_m: radius.value }))
    message.value = t('profile.saved')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// remove forgets the home, and the trips are shown whole again.
async function remove(): Promise<void> {
  busy.value = true
  message.value = ''
  error.value = ''
  try {
    await deleteHome()
    show({ home: null, zone: null })
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card border border-base-300 bg-base-100 lg:col-span-2">
    <form class="card-body gap-3" @submit.prevent="save">
      <h2 class="card-title">{{ t('profile.homeZone.title') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('profile.homeZone.hint') }}</p>

      <div class="grid gap-4 lg:grid-cols-2">
        <div class="space-y-3">
          <LocationField v-model="location" :focus="null" :legend="t('profile.homeZone.where')" no-address no-map />
          <label class="flex flex-col gap-1">
            <span class="text-sm">
              {{ t('profile.homeZone.radius', { distance: formatDistance(radius, locale, activeUnits) }) }}
            </span>
            <input
              v-model.number="radius"
              type="range"
              class="range range-primary range-sm"
              :min="MIN_RADIUS"
              :max="MAX_RADIUS"
              step="100"
              :aria-label="t('profile.homeZone.radiusLabel')"
            />
          </label>
        </div>
        <div class="space-y-2">
          <MapPicker
            v-if="config"
            :lat="lat"
            :lng="lng"
            :focus="null"
            :circle="circle"
            :tile-url="config.map_tile_url"
            :attribution="config.map_attribution"
            @pick="onPick"
          />
          <p class="text-xs text-base-content/60">
            {{ unchanged ? t('profile.homeZone.moved') : t('profile.homeZone.preview') }}
          </p>
        </div>
      </div>

      <p v-if="message" role="status" class="text-sm text-success">{{ message }}</p>
      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn btn-primary" :disabled="busy || lat === null || unchanged">
          {{ t('common.save') }}
        </button>
        <button v-if="saved.home" type="button" class="btn btn-ghost" :disabled="busy" @click="remove">
          <AppIcon name="trash" />
          {{ t('profile.homeZone.remove') }}
        </button>
      </div>
    </form>
  </div>
</template>
