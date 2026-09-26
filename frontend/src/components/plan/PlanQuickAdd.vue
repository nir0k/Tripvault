<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/api/client'
import { getClientConfig } from '@/api/config'
import type { PlaceFields } from '@/api/documents'
import { parseLink, reversePlace, searchPlaces } from '@/api/geo'
import type { GeoPlace } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { errorMessage } from '@/utils/errors'

// A new place or activity, begun where it goes in the day with a single field.
// The field takes whatever the person has: a name or an address to search for,
// a pair of coordinates, or a Google Maps or OpenStreetMap link. What it finds
// names the place and is saved at once; everything else about it is filled in
// on its card afterwards.
//
// A tile left without anything typed in it goes away and saves nothing, so a
// "+" pressed by mistake costs only a click elsewhere.
const props = defineProps<{
  kind: 'place' | 'activity'
  /** Ranks search results near this point first. */
  focus: { lat: number; lng: number } | null
  /** Whether the page is saving what the tile handed up. */
  saving?: boolean
}>()

const emit = defineEmits<{
  create: [fields: PlaceFields]
  cancel: []
}>()

const { t, te, locale } = useI18n()

const field = useTemplateRef<HTMLInputElement>('field')
const text = ref('')
const results = ref<GeoPlace[]>([])
const searching = ref(false)
const resolving = ref(false)
const message = ref('')
const geocodingEnabled = ref(false)
const highlighted = ref(0)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let generation = 0

const busy = computed(() => resolving.value || props.saving)

// A link or a pair of numbers is a position to read, not words to search for.
const COORDINATES = /^\s*-?\d{1,3}(?:[.,]\d+)?\s*[,;\s]\s*-?\d{1,3}(?:[.,]\d+)?\s*$/
function isPosition(value: string): boolean {
  return /^https?:\/\//i.test(value.trim()) || COORDINATES.test(value)
}

onMounted(async () => {
  await nextTick()
  field.value?.focus()
  try {
    geocodingEnabled.value = (await getClientConfig()).geocoding_enabled
  } catch {
    // Without the configuration the tile still takes a link or a bare name.
  }
})
onBeforeUnmount(() => clearTimeout(searchTimer))

// The search waits for a pause in typing, and shows only the newest answer.
watch(text, (value) => {
  clearTimeout(searchTimer)
  generation++
  results.value = []
  highlighted.value = 0
  message.value = ''
  const trimmed = value.trim()
  if (trimmed.length < 2 || !geocodingEnabled.value || isPosition(trimmed)) {
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
      }
    } catch (err) {
      if (current === generation) {
        message.value = errorMessage(err, t, te)
      }
    } finally {
      if (current === generation) {
        searching.value = false
      }
    }
  }, 350)
})

// fieldsFor is what a new element is created with: its kind with the kind's
// default type, and whatever the field found.
function fieldsFor(found: Pick<PlaceFields, 'name' | 'lat' | 'lng' | 'address' | 'osm_ref'>): PlaceFields {
  return {
    kind: props.kind,
    ...(props.kind === 'activity' ? { activity_type: 'hike' } : { category: 'other' }),
    ...found,
  }
}

// choose creates the element from a search result.
function choose(place: GeoPlace): void {
  emit('create', fieldsFor({
    name: place.name.slice(0, 200),
    lat: place.lat,
    lng: place.lng,
    address: place.label,
    osm_ref: place.ref,
  }))
}

// resolvePosition reads a link or a pair of coordinates, names the place by
// what stands there when the link did not name it, and creates it.
async function resolvePosition(value: string): Promise<void> {
  resolving.value = true
  message.value = ''
  try {
    const location = await parseLink(value.trim())
    let name = location.name
    let address = ''
    if (geocodingEnabled.value) {
      try {
        const [nearest] = await reversePlace(location.lat, location.lng, locale.value)
        name ||= nearest?.name ?? ''
        address = nearest?.label ?? ''
      } catch {
        // The position is enough; the name falls back to the numbers.
      }
    }
    emit('create', fieldsFor({
      name: (name || `${location.lat.toFixed(5)}, ${location.lng.toFixed(5)}`).slice(0, 200),
      lat: location.lat,
      lng: location.lng,
      address,
    }))
  } catch (err) {
    message.value = err instanceof ApiError && err.code === 'validation_failed'
      ? t('location.unrecognized')
      : errorMessage(err, t, te)
  } finally {
    resolving.value = false
  }
}

// submit takes the highlighted result, reads a position, or - with nothing
// found - keeps the words as the name of a place with no position yet.
function submit(): void {
  const value = text.value.trim()
  if (!value || busy.value) {
    return
  }
  if (isPosition(value)) {
    void resolvePosition(value)
    return
  }
  const result = results.value[highlighted.value]
  if (result) {
    choose(result)
    return
  }
  if (!searching.value) {
    emit('create', fieldsFor({ name: value.slice(0, 200) }))
  }
}

// onPaste reads a pasted link or pair at once, without waiting for Enter.
function onPaste(event: ClipboardEvent): void {
  const pasted = event.clipboardData?.getData('text') ?? ''
  if (text.value.trim() === '' && isPosition(pasted)) {
    event.preventDefault()
    text.value = pasted.trim()
    void resolvePosition(pasted)
  }
}

// onBlur drops a tile nothing was typed in.
function onBlur(): void {
  if (text.value.trim() === '' && !busy.value) {
    emit('cancel')
  }
}

// move walks the list of results with the arrow keys.
function move(step: number): void {
  if (results.value.length > 0) {
    highlighted.value = (highlighted.value + step + results.value.length) % results.value.length
  }
}
</script>

<template>
  <div class="relative rounded-box border border-primary/60 bg-base-100 p-3 shadow-sm">
    <label class="input input-sm w-full">
      <AppIcon :name="kind === 'activity' ? 'activity' : 'mapPin'" class="size-4!" />
      <input
        ref="field"
        v-model="text"
        type="text"
        autocomplete="off"
        maxlength="2000"
        :placeholder="kind === 'activity' ? t('plan.quickAdd.activityPlaceholder') : t('plan.quickAdd.placePlaceholder')"
        :aria-label="kind === 'activity' ? t('plan.quickAdd.activityPlaceholder') : t('plan.quickAdd.placePlaceholder')"
        :disabled="busy"
        @keydown.enter.prevent="submit"
        @keydown.esc.prevent="emit('cancel')"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @paste="onPaste"
        @blur="onBlur"
      />
      <span v-if="searching || busy" class="loading loading-spinner loading-xs"></span>
    </label>
    <ul
      v-if="results.length > 0"
      class="menu absolute inset-x-3 z-30 mt-1 max-h-64 flex-nowrap overflow-y-auto rounded-box border border-base-300 bg-base-100 p-1 shadow-lg"
    >
      <li v-for="(place, index) in results" :key="place.ref || `${place.lat},${place.lng}`">
        <!-- mousedown keeps the focus in the field, whose blur would drop the tile. -->
        <button
          type="button"
          class="flex flex-col items-start gap-0"
          :class="{ 'menu-active': index === highlighted }"
          @mousedown.prevent
          @click="choose(place)"
        >
          <span class="font-medium">{{ place.name }}</span>
          <span class="text-xs text-base-content/70">{{ place.label }}</span>
        </button>
      </li>
    </ul>
    <p class="mt-1 text-xs text-base-content/60">{{ t('plan.quickAdd.hint') }}</p>
    <p v-if="message" role="alert" class="mt-1 text-sm text-error">{{ message }}</p>
  </div>
</template>
