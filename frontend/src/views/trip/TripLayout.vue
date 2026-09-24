<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { createReportFromPlan } from '@/api/trips'
import type { DocumentKind } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useContentLanguage } from '@/composables/useContentLanguage'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { formatDateRange } from '@/utils/format'
import { translateTrip } from '@/utils/translate'
import { listRouteName, sectionOfRoute, TRIP_SECTIONS, tripRoute, tripRouteName, type TripSection } from '@/utils/tripRoutes'

interface Tab {
  name: string
  label: string
  icon: IconName
}

// The pages of a plan and of a report are laid out alike; what differs is the
// document tab, the list the back link returns to, and that a plan can be
// written up as a report of its own.
const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const TAB_ICONS: Record<TripSection, IconName> = { document: 'plan', media: 'image', budget: 'wallet', settings: 'settings' }

// kind is the section the address is in, which is known before the trip is read.
const kind = computed<DocumentKind>(() => route.meta.kind ?? 'plan')

const tabs = computed<Tab[]>(() => TRIP_SECTIONS.map((section) => ({
  name: tripRouteName(kind.value, section),
  label: section === 'document' ? t(`trip.tabs.${kind.value}`) : t(`trip.tabs.${section}`),
  icon: section === 'document' && kind.value === 'report' ? 'report' : TAB_ICONS[section],
})))

const tripId = computed(() => String(route.params.tripId))
const backTo = computed(() => ({ name: listRouteName(kind.value) }))
const period = computed(() => formatDateRange(store.trip?.start_date ?? null, store.trip?.end_date ?? null, locale.value))
// A report's title follows the language its words are read in.
const { lang: contentLang } = useContentLanguage(() => store.trip?.languages ?? [])
const title = computed(() => (store.trip ? translateTrip(store.trip, contentLang.value).title : ''))
const loadError = computed(() => (store.error ? errorMessage(store.error, t, te) : ''))
const writing = ref(false)
const writeError = ref('')

watch(tripId, (id) => void store.load(id), { immediate: true })

// A trip opened under the other section - an old link, a typed address - moves
// to its own, on the same page, so a report never shows under the plans.
watch(() => store.trip, (trip) => {
  if (trip && trip.id === tripId.value && trip.kind !== kind.value) {
    void router.replace(tripRoute(trip, sectionOfRoute(route.name) ?? 'document'))
  }
})

// writeReport copies the plan into a new report owned by the reader and opens
// it. Reading the plan is enough: whoever travelled may write it up.
async function writeReport(): Promise<void> {
  const plan = store.trip
  if (!plan || writing.value) {
    return
  }
  writing.value = true
  writeError.value = ''
  try {
    const report = await createReportFromPlan(plan.id)
    await router.push(tripRoute(report))
  } catch (err) {
    writeError.value = errorMessage(err, t, te)
  } finally {
    writing.value = false
  }
}
</script>

<template>
  <section class="space-y-6">
    <p v-if="loadError" role="alert" class="text-error">{{ loadError }}</p>
    <div v-else-if="!store.trip" class="flex justify-center"><span class="loading loading-spinner"></span></div>

    <div v-else class="gap-8 lg:grid lg:grid-cols-[16rem_minmax(0,1fr)]">
      <!-- On wide screens the trip keeps a column of its own: where it sits in
           the product, where to go inside it, and the days of its plan. -->
      <aside class="hidden lg:sticky lg:top-20 lg:block lg:max-h-[calc(100dvh-6rem)] lg:self-start lg:overflow-y-auto">
        <RouterLink :to="backTo" class="btn btn-ghost btn-sm -ms-3">
          <AppIcon name="arrowLeft" />
          {{ t(`trip.back.${kind}`) }}
        </RouterLink>

        <h1 class="mt-3 text-xl font-bold break-words">{{ title }}</h1>
        <p class="mt-1 text-sm text-base-content/70">
          <span>{{ period }}</span>
          <span aria-hidden="true"> · </span>
          <span>{{ t('trips.days', store.trip.day_count) }}</span>
        </p>
        <p class="mt-2 flex flex-wrap gap-2">
          <span class="badge badge-sm">{{ t(`trips.status.${store.trip.status}`) }}</span>
          <span v-if="store.trip.role !== 'owner'" class="badge badge-outline badge-sm">{{ t(`trips.roles.${store.trip.role}`) }}</span>
        </p>

        <nav class="mt-4" :aria-label="t('trip.tabsLabel')">
          <ul class="menu w-full gap-1 p-0">
            <li v-for="tab in tabs" :key="tab.name">
              <RouterLink
                :to="{ name: tab.name, params: { tripId } }"
                :class="{ 'menu-active': route.name === tab.name }"
                :aria-current="route.name === tab.name ? 'page' : undefined"
              >
                <AppIcon :name="tab.icon" />
                {{ tab.label }}
              </RouterLink>
            </li>
          </ul>
        </nav>

        <button
          v-if="store.trip.kind === 'plan'"
          type="button"
          class="btn btn-sm btn-hover-outline mt-4 w-full"
          :disabled="writing"
          @click="writeReport"
        >
          <span v-if="writing" class="loading loading-spinner loading-xs"></span>
          <AppIcon v-else name="report" />
          {{ t('trip.writeReport') }}
        </button>
        <p v-if="writeError" role="alert" class="mt-2 text-sm text-error">{{ writeError }}</p>

        <!-- The plan and the report hang their days here; every other tab
             leaves it empty. -->
        <div id="trip-sidebar-days" class="mt-4"></div>
      </aside>

      <div class="min-w-0 space-y-6">
        <!-- Without room for the column the trip wears its old head: the title
             above a row of tabs. -->
        <div class="space-y-4 lg:hidden">
          <RouterLink :to="backTo" class="btn btn-ghost btn-sm -ms-3">
            <AppIcon name="arrowLeft" />
            {{ t(`trip.back.${kind}`) }}
          </RouterLink>

          <header class="space-y-2">
            <h1 class="text-2xl font-bold break-words">{{ title }}</h1>
            <p class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
              <span>{{ period }}</span>
              <span aria-hidden="true">·</span>
              <span>{{ t('trips.days', store.trip.day_count) }}</span>
              <span class="badge badge-sm">{{ t(`trips.status.${store.trip.status}`) }}</span>
              <span v-if="store.trip.role !== 'owner'" class="badge badge-outline badge-sm">{{ t(`trips.roles.${store.trip.role}`) }}</span>
            </p>
            <button
              v-if="store.trip.kind === 'plan'"
              type="button"
              class="btn btn-sm btn-hover-outline"
              :disabled="writing"
              @click="writeReport"
            >
              <span v-if="writing" class="loading loading-spinner loading-xs"></span>
              <AppIcon v-else name="report" />
              {{ t('trip.writeReport') }}
            </button>
            <p v-if="writeError" role="alert" class="text-sm text-error">{{ writeError }}</p>
          </header>

          <nav role="tablist" class="tabs tabs-border overflow-x-auto" :aria-label="t('trip.tabsLabel')">
            <RouterLink
              v-for="tab in tabs"
              :key="tab.name"
              role="tab"
              :to="{ name: tab.name, params: { tripId } }"
              class="tab gap-2 whitespace-nowrap"
              :class="{ 'tab-active': route.name === tab.name }"
              :aria-selected="route.name === tab.name"
            >
              <AppIcon :name="tab.icon" />
              {{ tab.label }}
            </RouterLink>
          </nav>
        </div>

        <RouterView />
      </div>
    </div>
  </section>
</template>
