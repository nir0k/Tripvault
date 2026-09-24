<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import { listTrips } from '@/api/trips'
import type { Trip } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { errorMessage } from '@/utils/errors'
import { readingLanguage, translateTrip } from '@/utils/translate'
import { tripRoute } from '@/utils/tripRoutes'

/** SearchHit is one plan or report the search offers. */
interface SearchHit {
  key: string
  title: string
  label: string
  icon: IconName
  route: RouteLocationRaw
}

const { t, te, locale } = useI18n()
const root = useTemplateRef<HTMLElement>('root')
const field = useTemplateRef<HTMLInputElement>('field')

const query = ref('')
const trips = ref<Trip[]>([])
const loading = ref(false)
const error = ref('')
// The field is always visible, so the results panel follows the focus rather
// than a toggle: it opens when the field is used and folds on Escape or on a
// press outside.
const open = ref(false)

let generation = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined

// The search looks through the plans and the reports at once, each a trip of
// its own, and says which one a row is.
const hits = computed<SearchHit[]>(() => trips.value.map((trip) => ({
  key: trip.id,
  title: translateTrip(trip, readingLanguage(trip.languages, locale.value)).title,
  label: t(`trip.tabs.${trip.kind}`),
  icon: trip.kind,
  route: tripRoute(trip),
})))

// search reads the trips whose title matches the current query. An answer for
// a query that changed meanwhile is dropped.
async function search(): Promise<void> {
  const text = query.value.trim()
  const current = ++generation
  if (text === '') {
    trips.value = []
    loading.value = false
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const page = await listTrips({ q: text })
    if (current === generation) {
      trips.value = page.items
    }
  } catch (err) {
    if (current === generation) {
      trips.value = []
      error.value = errorMessage(err, t, te)
    }
  } finally {
    if (current === generation) {
      loading.value = false
    }
  }
}

// onInput opens the panel and waits for a pause in typing before asking the
// server.
function onInput(): void {
  open.value = true
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void search(), 300)
}

// close folds the panel; clear also empties the field, so the next search
// starts from nothing.
function close(clear = false): void {
  open.value = false
  if (clear) {
    clearTimeout(searchTimer)
    generation += 1
    query.value = ''
    trips.value = []
    loading.value = false
    error.value = ''
  }
}

// onPointerDown folds the panel when the press lands outside the search.
function onPointerDown(event: PointerEvent): void {
  if (open.value && !root.value?.contains(event.target as Node)) {
    close()
  }
}

// onKeyDown folds the panel on Escape and keeps the focus in the field.
function onKeyDown(event: KeyboardEvent): void {
  if (event.key === 'Escape' && open.value) {
    close()
    field.value?.focus()
  }
}

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown)
  document.addEventListener('keydown', onKeyDown)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onPointerDown)
  document.removeEventListener('keydown', onKeyDown)
  clearTimeout(searchTimer)
})
</script>

<template>
  <div ref="root" class="relative w-32 sm:w-56 lg:w-64">
    <label class="input input-sm w-full">
      <AppIcon name="search" />
      <input
        ref="field"
        v-model="query"
        type="search"
        :placeholder="t('nav.searchPlaceholder')"
        :aria-label="t('nav.search')"
        @input="onInput"
        @focus="open = true"
      />
    </label>
    <div
      v-if="open && query.trim() !== ''"
      class="absolute end-0 z-30 mt-1 w-80 max-w-[calc(100vw-2rem)] rounded-box border border-base-300 bg-base-100 p-3 shadow-lg"
    >
      <p v-if="error" class="text-sm text-error">{{ error }}</p>
      <span v-else-if="loading" class="loading loading-spinner loading-sm"></span>
      <p v-else-if="hits.length === 0" class="text-sm text-base-content/70">{{ t('nav.searchEmpty') }}</p>
      <ul v-else class="menu w-full p-0">
        <li v-for="hit in hits" :key="hit.key">
          <RouterLink :to="hit.route" @click="close(true)">
            <AppIcon :name="hit.icon" />
            <span class="min-w-0 flex-1 truncate">{{ hit.title }}</span>
            <span class="badge badge-ghost badge-sm">{{ hit.label }}</span>
          </RouterLink>
        </li>
      </ul>
    </div>
  </div>
</template>
