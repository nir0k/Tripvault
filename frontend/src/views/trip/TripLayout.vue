<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { downloadPlanPDF } from '@/api/documents'
import { downloadSharedPlanPDF } from '@/api/shared'
import { setTripTags } from '@/api/tags'
import { createReportFromPlan } from '@/api/trips'
import type { DocumentKind } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import TripStatusBadge from '@/components/TripStatusBadge.vue'
import TagsPicker from '@/components/TagsPicker.vue'
import { useContentLanguage } from '@/composables/useContentLanguage'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { formatDateRange } from '@/utils/format'
import { translateTrip } from '@/utils/translate'
import { activeUnits } from '@/utils/units'
import {
  listRouteName, sectionOfRoute, SHARED_TRIP_ID, tripRoute, tripSections, type TripSection,
} from '@/utils/tripRoutes'

interface Tab {
  section: TripSection
  to: RouteLocationRaw
  label: string
  icon: IconName
}

// The pages of a plan and of a report are laid out alike; what differs is the
// document tab, the list the back link returns to, and that a plan can be
// written up as a report of its own.
//
// A read-only link opens the same layout around the same pages, with the trip
// read through the link: it has no list to go back to, no budget or settings,
// and nothing that would change the trip.
const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const TAB_ICONS: Record<TripSection, IconName> = {
  document: 'plan', packing: 'check2Square', media: 'image', budget: 'cashStack', settings: 'settings',
}

// shared says the trip was opened by a read-only link.
const shared = computed(() => route.meta.shared === true)
// kind is the section the address is in, which is known before the trip is
// read; a link's address is the same for both, so there the trip tells.
const kind = computed<DocumentKind>(() => (shared.value ? store.trip?.kind : route.meta.kind) ?? 'plan')

const tabs = computed<Tab[]>(() => tripSections(kind.value, shared.value).map((section) => ({
  section,
  to: tripRoute({ id: shared.value ? SHARED_TRIP_ID : tripId.value, kind: kind.value }, section),
  label: section === 'document' ? t(`trip.tabs.${kind.value}`) : t(`trip.tabs.${section}`),
  icon: section === 'document' && kind.value === 'report' ? 'report' : TAB_ICONS[section],
})))
const activeSection = computed(() => sectionOfRoute(route.name))

const tripId = computed(() => String(route.params.tripId))
const backTo = computed(() => ({ name: listRouteName(kind.value) }))
const period = computed(() => formatDateRange(store.trip?.start_date ?? null, store.trip?.end_date ?? null, locale.value))
// A report's title follows the language its words are read in.
const { lang: contentLang } = useContentLanguage(() => store.trip?.languages ?? [])
const title = computed(() => (store.trip ? translateTrip(store.trip, contentLang.value).title : ''))
const loadError = computed(() => (store.error ? errorMessage(store.error, t, te) : ''))
// A plan is closed or reopened by whoever may change it; a link only reads.
const canEdit = computed(() => !shared.value && (store.trip?.role === 'owner' || store.trip?.role === 'editor'))
const writing = ref(false)
const writeError = ref('')

// The trip of a link is read by the page that took the link's token.
watch(tripId, (id) => {
  if (!shared.value) {
    void store.load(id)
  }
}, { immediate: true })

// A trip opened under the other section - an old link, a typed address - moves
// to its own, on the same page, so a report never shows under the plans.
watch(() => store.trip, (trip) => {
  if (!shared.value && trip && trip.id === tripId.value && trip.kind !== kind.value) {
    void router.replace(tripRoute(trip, sectionOfRoute(route.name) ?? 'document'))
  }
})

// saveTags puts the reader's tags on the trip and shows the trip it answers with.
async function saveTags(ids: string[]): Promise<void> {
  if (store.trip) {
    store.set(await setTripTags(store.trip.id, ids))
  }
}

// exporting is true while the plan's PDF is being made.
const exporting = ref(false)

// exportPlan saves the plan as a PDF to take on the way. A link has no account
// behind it for the server to take the language and the units from, so those
// travel with its request.
async function exportPlan(): Promise<void> {
  const current = store.trip
  if (!current || exporting.value) {
    return
  }
  exporting.value = true
  writeError.value = ''
  try {
    if (shared.value) {
      await downloadSharedPlanPDF(locale.value, activeUnits.value)
    } else {
      await downloadPlanPDF(current.id)
    }
  } catch (err) {
    writeError.value = errorMessage(err, t, te)
  } finally {
    exporting.value = false
  }
}

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
        <RouterLink v-if="!shared" :to="backTo" class="btn btn-ghost btn-sm -ms-3 mb-3">
          <AppIcon name="arrowLeft" />
          {{ t(`trip.back.${kind}`) }}
        </RouterLink>

        <h1 class="text-xl font-bold break-words">{{ title }}</h1>
        <p class="mt-1 text-sm text-base-content/70">
          <span>{{ period }}</span>
          <span aria-hidden="true"> · </span>
          <span>{{ t('trips.days', store.trip.day_count) }}</span>
        </p>
        <div class="mt-2 flex flex-wrap gap-2">
          <TripStatusBadge :trip="store.trip" :editable="canEdit" @updated="store.set" />
          <span v-if="shared" class="badge badge-outline badge-sm">{{ t('shared.readOnly') }}</span>
          <span v-else-if="store.trip.role !== 'owner'" class="badge badge-outline badge-sm">{{ t(`trips.roles.${store.trip.role}`) }}</span>
        </div>
        <TagsPicker v-if="!shared" :tags="store.trip.tags" kind="trip" :save="saveTags" class="mt-2" />
        <p v-if="shared" class="mt-2 text-sm text-base-content/70">{{ t('trips.ownedBy', { name: store.trip.owner.display_name }) }}</p>

        <nav class="mt-4" :aria-label="t('trip.tabsLabel')">
          <ul class="menu w-full gap-1 p-0">
            <li v-for="tab in tabs" :key="tab.section">
              <RouterLink
                :to="tab.to"
                :class="{ 'menu-active': activeSection === tab.section }"
                :aria-current="activeSection === tab.section ? 'page' : undefined"
              >
                <AppIcon :name="tab.icon" />
                {{ tab.label }}
              </RouterLink>
            </li>
          </ul>
        </nav>

        <button
          v-if="!shared && store.trip.kind === 'plan'"
          type="button"
          class="btn btn-sm btn-hover-outline mt-4 w-full"
          :disabled="writing"
          @click="writeReport"
        >
          <span v-if="writing" class="loading loading-spinner loading-xs"></span>
          <AppIcon v-else name="report" />
          {{ t('trip.writeReport') }}
        </button>
        <button
          v-if="store.trip.kind === 'plan'"
          type="button"
          class="btn btn-sm btn-hover-outline mt-2 w-full"
          :disabled="exporting"
          @click="exportPlan"
        >
          <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
          <AppIcon v-else name="download" />
          {{ t('trip.planPdf') }}
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
          <RouterLink v-if="!shared" :to="backTo" class="btn btn-ghost btn-sm -ms-3">
            <AppIcon name="arrowLeft" />
            {{ t(`trip.back.${kind}`) }}
          </RouterLink>

          <header class="space-y-2">
            <h1 class="text-2xl font-bold break-words">{{ title }}</h1>
            <div class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
              <span>{{ period }}</span>
              <span aria-hidden="true">·</span>
              <span>{{ t('trips.days', store.trip.day_count) }}</span>
              <TripStatusBadge :trip="store.trip" :editable="canEdit" @updated="store.set" />
              <span v-if="shared" class="badge badge-outline badge-sm">{{ t('shared.readOnly') }}</span>
              <span v-else-if="store.trip.role !== 'owner'" class="badge badge-outline badge-sm">{{ t(`trips.roles.${store.trip.role}`) }}</span>
              <template v-if="shared">
                <span aria-hidden="true">·</span>
                <span>{{ t('trips.ownedBy', { name: store.trip.owner.display_name }) }}</span>
              </template>
            </div>
            <TagsPicker v-if="!shared" :tags="store.trip.tags" kind="trip" :save="saveTags" />
            <button
              v-if="!shared && store.trip.kind === 'plan'"
              type="button"
              class="btn btn-sm btn-hover-outline"
              :disabled="writing"
              @click="writeReport"
            >
              <span v-if="writing" class="loading loading-spinner loading-xs"></span>
              <AppIcon v-else name="report" />
              {{ t('trip.writeReport') }}
            </button>
            <button
              v-if="store.trip.kind === 'plan'"
              type="button"
              class="btn btn-sm btn-hover-outline"
              :disabled="exporting"
              @click="exportPlan"
            >
              <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
              <AppIcon v-else name="download" />
              {{ t('trip.planPdf') }}
            </button>
            <p v-if="writeError" role="alert" class="text-sm text-error">{{ writeError }}</p>
          </header>

          <nav role="tablist" class="tabs tabs-border overflow-x-auto" :aria-label="t('trip.tabsLabel')">
            <RouterLink
              v-for="tab in tabs"
              :key="tab.section"
              role="tab"
              :to="tab.to"
              class="tab gap-2 whitespace-nowrap"
              :class="{ 'tab-active': activeSection === tab.section }"
              :aria-selected="activeSection === tab.section"
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
