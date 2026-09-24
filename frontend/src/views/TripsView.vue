<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listTripYears, listTrips, type TripScope } from '@/api/trips'
import type { DocumentKind, Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TripCard from '@/components/TripCard.vue'
import TripCreateDialog from '@/components/TripCreateDialog.vue'
import { errorMessage } from '@/utils/errors'

// The list of plans or of reports. They are separate trips in separate sections
// of the menu: a plan is a journey being prepared, a report one written up,
// usually with other people and other photographs.
const props = defineProps<{ kind: DocumentKind }>()

const { t, te } = useI18n()

const SCOPES: readonly TripScope[] = ['all', 'owned', 'shared']

const filters = reactive({ scope: 'all' as TripScope, year: 0, q: '' })
const trips = ref<Trip[]>([])
const years = ref<number[]>([])
const nextCursor = ref<string | null>(null)
const loaded = ref(false)
const loading = ref(false)
const error = ref('')
const creator = useTemplateRef<InstanceType<typeof TripCreateDialog>>('creator')

const filtered = computed(() => filters.scope !== 'all' || filters.year !== 0 || filters.q.trim() !== '')

let generation = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined

// load reads the first page for the current filters, or the next page when
// more is true. An answer for filters that changed meanwhile is dropped.
async function load(more = false): Promise<void> {
  const current = more ? generation : ++generation
  loading.value = true
  error.value = ''
  try {
    const page = await listTrips({
      scope: filters.scope,
      kind: props.kind,
      year: filters.year || undefined,
      q: filters.q.trim() || undefined,
      cursor: more ? nextCursor.value ?? undefined : undefined,
    })
    if (current !== generation) {
      return
    }
    trips.value = more ? [...trips.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor
    loaded.value = true
  } catch (err) {
    if (current === generation) {
      error.value = errorMessage(err, t, te)
    }
  } finally {
    if (current === generation) {
      loading.value = false
    }
  }
}

watch(() => [filters.scope, filters.year], () => void load())

// The title search waits for a pause in typing.
watch(() => filters.q, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void load(), 300)
})

// loadYears reads the years this list's trips touch, for the year filter.
async function loadYears(): Promise<void> {
  try {
    years.value = await listTripYears(props.kind)
  } catch {
    // Without the years the filter offers only "any year".
  }
}

// The same page serves both lists, so switching between them starts afresh.
watch(() => props.kind, () => {
  Object.assign(filters, { scope: 'all', year: 0, q: '' })
  trips.value = []
  loaded.value = false
  void load()
  void loadYears()
})

onMounted(async () => {
  await load()
  await loadYears()
})
onBeforeUnmount(() => clearTimeout(searchTimer))
</script>

<template>
  <section class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t(`trips.titles.${kind}`) }}</h1>
      <button type="button" class="btn btn-primary" @click="creator?.open(kind)">
        <AppIcon name="plus" />
        {{ t(`trips.new.${kind}`) }}
      </button>
    </div>

    <div class="flex flex-col gap-3 lg:flex-row lg:items-center">
      <div role="tablist" class="tabs tabs-border tabs-sm w-fit">
        <button
          v-for="scope in SCOPES"
          :key="scope"
          type="button"
          role="tab"
          class="tab"
          :class="{ 'tab-active': filters.scope === scope }"
          :aria-selected="filters.scope === scope"
          @click="filters.scope = scope"
        >
          {{ t(`trips.scope.${scope}`) }}
        </button>
      </div>
      <div class="flex flex-1 flex-wrap gap-2">
        <label class="input input-sm min-w-48 flex-1">
          <AppIcon name="search" />
          <input v-model="filters.q" type="search" :placeholder="t('trips.search')" :aria-label="t('trips.search')" />
        </label>
        <select v-model.number="filters.year" class="select select-sm select-hover-outline w-auto" :aria-label="t('trips.anyYear')">
          <option :value="0">{{ t('trips.anyYear') }}</option>
          <option v-for="year in years" :key="year" :value="year">{{ year }}</option>
        </select>
      </div>
    </div>

    <p v-if="error" role="alert" class="text-error">{{ error }}</p>

    <div v-if="trips.length > 0" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <TripCard v-for="trip in trips" :key="trip.id" :trip="trip" />
    </div>

    <div v-else-if="loaded && filtered" class="rounded-box border border-dashed border-base-300 px-6 py-10 text-center text-base-content/70">
      {{ t('trips.noMatches') }}
    </div>

    <div v-else-if="loaded" class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-6 py-16 text-center">
      <span class="text-primary"><AppIcon :name="kind === 'report' ? 'report' : 'map'" /></span>
      <h2 class="text-lg font-semibold">{{ t(`trips.emptyTitle.${kind}`) }}</h2>
      <p class="max-w-md text-base-content/70">{{ t(`trips.emptyText.${kind}`) }}</p>
      <button type="button" class="btn btn-primary btn-sm" @click="creator?.open(kind)">{{ t(`trips.new.${kind}`) }}</button>
    </div>

    <div v-if="loading" class="flex justify-center"><span class="loading loading-spinner"></span></div>
    <div v-else-if="nextCursor" class="flex justify-center">
      <button type="button" class="btn btn-ghost" @click="load(true)">{{ t('trips.loadMore') }}</button>
    </div>

    <TripCreateDialog ref="creator" />
  </section>
</template>
