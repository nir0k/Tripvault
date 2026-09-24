<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { readShareToken, storeShareToken } from '@/api/client'
import { getClientConfig } from '@/api/config'
import { SHARED_MEDIA_BASE } from '@/api/media'
import { downloadSharedReportPDF, getShared, getSharedDocument } from '@/api/shared'
import type { ClientConfig, DocumentKind, PlanItem, Shared, TripDocument } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import AppLogo from '@/components/AppLogo.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import PlanDayList from '@/components/plan/PlanDayList.vue'
import PlanDayPanel from '@/components/plan/PlanDayPanel.vue'
import PlanMap from '@/components/plan/PlanMap.vue'
import PlanPlaceList from '@/components/plan/PlanPlaceList.vue'
import PlanStays from '@/components/plan/PlanStays.vue'
import ContentLanguageSwitch from '@/components/report/ContentLanguageSwitch.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import ReportDayNav from '@/components/report/ReportDayNav.vue'
import ReportDaySection from '@/components/report/ReportDaySection.vue'
import ReportTotalsBar from '@/components/report/ReportTotalsBar.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import UnitsSelect from '@/components/UnitsSelect.vue'
import { useContentLanguage } from '@/composables/useContentLanguage'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { provideMediaBase } from '@/composables/useMediaUrl'
import { activeUnits } from '@/utils/units'
import { errorMessage } from '@/utils/errors'
import { formatDateRange } from '@/utils/format'
import { revealElement } from '@/utils/reveal'
import { translateDocument, translateTrip } from '@/utils/translate'

// A trip opened by a read-only link, without an account. The plan is rendered by
// the same components the application uses, with editing switched off, so the
// two never drift apart.
//
// The token arrives in the fragment of the address, which browsers never send to
// a server. This page takes it into sessionStorage and removes it from the
// address bar at once, so it does not sit in the history or get copied out of it
// by accident; the API client then sends it in a header.
//
// A report in several languages is read in the reader's own when it has it,
// and a row of buttons above it switches to any other.

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()

// The pictures of this page are read through the link, not through an account:
// the token travels in a header either way, but the paths are different.
provideMediaBase(SHARED_MEDIA_BASE)

const shared = ref<Shared | null>(null)
const plan = ref<TripDocument | null>(null)
const rawReport = ref<TripDocument | null>(null)
const clientConfig = ref<ClientConfig | null>(null)
const loading = ref(true)
const error = ref('')
// Exporting a report of a few hundred photographs takes a moment, so the button
// says so rather than looking as if nothing happened.
const exporting = ref(false)

const sideBySide = useMediaQuery('(min-width: 1280px)')
const mobileView = ref<'list' | 'map'>('list')
// The map sits beside the plan on a wide screen and is a tab of its own below
// that, so each has a ref and only one of them is ever mounted.
const wideMap = useTemplateRef<InstanceType<typeof PlanMap>>('wideMap')
const mobileMap = useTemplateRef<InstanceType<typeof PlanMap>>('mobileMap')

const period = computed(() =>
  formatDateRange(shared.value?.trip.start_date ?? null, shared.value?.trip.end_date ?? null, locale.value))
const currency = computed(() => shared.value?.trip.currency ?? 'EUR')

const languages = computed(() => shared.value?.trip.languages ?? [])
const { lang: contentLang, choose: chooseLanguage } = useContentLanguage(() => languages.value)
// report is the shared report in the language chosen, the original standing in
// for every field nobody translated.
const report = computed(() => (rawReport.value ? translateDocument(rawReport.value, contentLang.value) : null))
const title = computed(() => (shared.value ? translateTrip(shared.value.trip, contentLang.value).title : ''))

// The selection lives in the address, exactly as it does in the application:
// which document is open, which day of the plan, and whether the report leaves
// out what was skipped.
const kinds = computed<DocumentKind[]>(() => [
  ...(plan.value ? ['plan' as const] : []),
  ...(report.value ? ['report' as const] : []),
])
const kind = computed<DocumentKind | null>(() => {
  const requested = route.query.doc
  if (requested === 'report' && report.value) {
    return 'report'
  }
  if (requested === 'plan' && plan.value) {
    return 'plan'
  }
  return kinds.value[0] ?? null
})
const hideSkipped = computed(() => route.query.skipped === 'hide')
const showStays = computed(() => route.query.view === 'stays')
const dayIndex = computed(() => {
  const days = plan.value?.days.length ?? 0
  const requested = Number(route.query.day ?? 1)
  return Number.isInteger(requested) && requested >= 1 && requested <= days ? requested - 1 : 0
})
const day = computed(() => plan.value?.days[dayIndex.value] ?? null)
const missingNights = computed(() => plan.value?.nights.filter((night) => night.missing).length ?? 0)

// A search engine must not keep a link somebody was given in confidence. The
// served page carries an X-Robots-Tag as well; this covers the rest.
const robots = document.createElement('meta')
robots.name = 'robots'
robots.content = 'noindex, nofollow'
onMounted(() => document.head.append(robots))
onBeforeUnmount(() => robots.remove())

/**
 * takeToken moves the token from the fragment into this tab's storage and clears
 * the address bar. Without a fragment the token stored earlier is kept, so a
 * reload of the page still works.
 */
function takeToken(): void {
  const fragment = window.location.hash.replace(/^#/, '')
  const found = /(?:^|&)token=([^&]*)/.exec(fragment)?.[1]
  if (!found) {
    return
  }
  storeShareToken(decodeURIComponent(found))
  history.replaceState(null, '', window.location.pathname + window.location.search)
}

// load reads what the link opens, and the one document of its trip.
async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const opened = await getShared()
    shared.value = opened
    const content = await getSharedDocument()
    if (opened.kind === 'report') {
      rawReport.value = content
    } else {
      plan.value = content
    }
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  takeToken()
  if (!readShareToken()) {
    loading.value = false
    error.value = t('errors.invalid_share_token')
    return
  }
  void load()
  void getClientConfig().then((config) => {
    clientConfig.value = config
  }).catch(() => {
    // Without the configuration the plan is still readable, just without a map.
  })
})

// selectDay opens a day of the plan, keeping the choice in the address.
function selectDay(index: number): void {
  void router.replace({ query: { doc: route.query.doc, day: String(index + 1) } })
}

// selectStays opens the stays of the plan.
function selectStays(): void {
  void router.replace({ query: { doc: route.query.doc, view: 'stays' } })
}

// openKind switches between the plan and the report of a link that covers both.
function openKind(next: DocumentKind): void {
  void router.replace({ query: { doc: next, skipped: route.query.skipped, lang: route.query.lang } })
}

// toggleSkipped hides or brings back the places the trip did not reach.
function toggleSkipped(): void {
  void router.replace({ query: { ...route.query, skipped: hideSkipped.value ? undefined : 'hide' } })
}

// exportPDF saves the shared report as a file. The reader's language goes with
// the request: a link has no account behind it for the server to take one from.
async function exportPDF(photos: boolean): Promise<void> {
  if (exporting.value) {
    return
  }
  exporting.value = true
  error.value = ''
  try {
    await downloadSharedReportPDF(locale.value, activeUnits.value, photos, contentLang.value || undefined)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    exporting.value = false
  }
}

// locateItem takes the map to a place picked in the plan. Below the width that
// shows both, the map is a tab of its own and has to be opened - and mounted -
// before it can be told where to go.
async function locateItem(item: PlanItem): Promise<void> {
  if (!sideBySide.value) {
    mobileView.value = 'map'
    await nextTick()
  }
  const map = sideBySide.value ? wideMap.value : mobileMap.value
  map?.locate(item.id)
}

// focusItem opens the day a place on the map belongs to.
function focusItem(_itemId: string, dayIndexOfItem: number | null): void {
  if (dayIndexOfItem !== null) {
    selectDay(dayIndexOfItem)
  }
}

// focusReportItem scrolls the report to the card of a place picked on its map,
// or to its day when the place has no card on the page.
function focusReportItem(itemId: string, dayIndexOfItem: number | null): void {
  if (!revealElement(`item-${itemId}`) && dayIndexOfItem !== null) {
    revealElement(`day-${dayIndexOfItem + 1}`, false)
  }
}
</script>

<template>
  <div class="min-h-dvh bg-base-200">
    <header class="border-b border-base-300 bg-base-100">
      <div class="mx-auto flex max-w-7xl flex-wrap items-center justify-between gap-3 px-4 py-3">
        <AppLogo />
        <div class="flex items-center gap-2">
          <LanguageSelect />
          <UnitsSelect />
          <ThemeSelect />
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-7xl space-y-6 px-4 py-6">
      <div v-if="loading" class="flex justify-center py-16"><span class="loading loading-spinner"></span></div>
      <p v-else-if="error" role="alert" class="alert alert-error">{{ error }}</p>

      <template v-else-if="shared">
        <header class="space-y-2">
          <h1 class="text-2xl font-bold break-words">{{ title }}</h1>
          <p class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
            <span>{{ period }}</span>
            <span aria-hidden="true">·</span>
            <span>{{ t('trips.days', shared.trip.day_count) }}</span>
            <span class="badge badge-sm">{{ t(`trips.status.${shared.trip.status}`) }}</span>
            <span aria-hidden="true">·</span>
            <span>{{ t('trips.ownedBy', { name: shared.trip.owner_name }) }}</span>
          </p>
          <p class="badge badge-outline badge-sm h-auto py-0.5">{{ t('shared.readOnly') }}</p>
        </header>

        <p v-if="kinds.length === 0" class="text-base-content/70">{{ t('shared.nothingToShow') }}</p>

        <!-- A link may open the plan, the report, or both; with both the reader
             picks, and the choice stays in the address. -->
        <nav v-if="kinds.length > 1" role="tablist" class="tabs tabs-border w-fit" :aria-label="t('trip.tabsLabel')">
          <button
            v-for="item in kinds"
            :key="item"
            type="button"
            role="tab"
            class="tab gap-2"
            :class="{ 'tab-active': kind === item }"
            :aria-selected="kind === item"
            @click="openKind(item)"
          >
            {{ t(`trip.tabs.${item}`) }}
          </button>
        </nav>

        <div
          v-if="kind === 'plan' && plan"
          class="grid gap-6 lg:grid-cols-[14rem_minmax(0,1fr)] xl:grid-cols-[14rem_minmax(0,1.15fr)_minmax(0,1fr)]"
        >
          <aside class="min-w-0 lg:sticky lg:top-4 lg:self-start">
            <PlanDayList
              :days="plan.days"
              :selected="showStays ? null : dayIndex"
              :stays-count="plan.stays.length"
              :missing-nights="missingNights"
              :can-edit="false"
              :draggable="false"
              @select="selectDay"
              @select-stays="selectStays"
            />
          </aside>

          <div class="min-w-0 space-y-8">
            <div v-if="!sideBySide" role="tablist" class="tabs tabs-box tabs-sm w-fit">
              <button
                type="button"
                role="tab"
                class="tab"
                :class="{ 'tab-active': mobileView === 'list' }"
                :aria-selected="mobileView === 'list'"
                @click="mobileView = 'list'"
              >
                {{ t('map.list') }}
              </button>
              <button
                type="button"
                role="tab"
                class="tab"
                :class="{ 'tab-active': mobileView === 'map' }"
                :aria-selected="mobileView === 'map'"
                @click="mobileView = 'map'"
              >
                {{ t('map.map') }}
              </button>
            </div>

            <div v-if="!sideBySide && mobileView === 'map' && clientConfig" class="h-[70dvh]">
              <PlanMap
                ref="mobileMap"
                :document="plan"
                :selected-day="showStays ? null : dayIndex"
                :tile-url="clientConfig.map_tile_url"
                :attribution="clientConfig.map_attribution"
                @focus="focusItem"
              />
            </div>

            <template v-if="sideBySide || mobileView === 'list'">
              <PlanStays v-if="showStays" :document="plan" :currency="currency" :can-edit="false" />

              <PlanDayPanel
                v-else-if="day"
                :key="day.id"
                :day="day"
                :currency="currency"
                :can-edit="false"
                :draggable="false"
                @locate="locateItem"
              />

              <section v-if="!showStays && plan.unassigned.length > 0" class="space-y-3" :aria-label="t('plan.unassigned')">
                <h2 class="text-lg font-semibold">{{ t('plan.unassigned') }} ({{ plan.unassigned.length }})</h2>
                <PlanPlaceList
                  :places="plan.unassigned"
                  :day-id="null"
                  :currency="currency"
                  :can-edit="false"
                  :draggable="false"
                  @locate="locateItem"
                />
              </section>
            </template>
          </div>

          <aside v-if="sideBySide && clientConfig" class="sticky top-4 h-[calc(100dvh-2rem)] self-start">
            <PlanMap
              ref="wideMap"
              :document="plan"
              :selected-day="showStays ? null : dayIndex"
              :tile-url="clientConfig.map_tile_url"
              :attribution="clientConfig.map_attribution"
              @focus="focusItem"
            />
          </aside>
        </div>

        <!-- The report is the reading mode of the application with editing off,
             so a link shows exactly what its owner reads. -->
        <div
          v-else-if="kind === 'report' && report"
          class="mx-auto w-full"
          :class="report.days.length > 1 ? 'max-w-7xl gap-8 lg:grid lg:grid-cols-[14rem_minmax(0,1fr)]' : 'max-w-5xl'"
        >
          <!-- The days of the report, beside it on a wide screen and a row of
               chips above it on a narrow one, as in the application. -->
          <aside
            v-if="report.days.length > 1"
            class="mb-6 lg:sticky lg:top-4 lg:mb-0 lg:max-h-[calc(100dvh-2rem)] lg:self-start lg:overflow-y-auto"
          >
            <ReportDayNav :days="report.days" />
          </aside>

          <section class="min-w-0 space-y-8">
            <div class="flex flex-wrap items-center justify-end gap-3">
              <ContentLanguageSwitch
                v-if="languages.length > 1"
                class="me-auto"
                :languages="languages"
                :model-value="contentLang"
                :label="t('report.contentLanguage')"
                @update:model-value="chooseLanguage"
              />
              <label class="label cursor-pointer gap-2 text-sm">
                <input type="checkbox" class="toggle toggle-sm" :checked="hideSkipped" @change="toggleSkipped" />
                <span>{{ t('report.hideSkipped') }}</span>
              </label>

              <div class="dropdown dropdown-end">
                <div tabindex="0" role="button" class="btn btn-sm" :class="{ 'btn-disabled': exporting }">
                  <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
                  <AppIcon v-else name="download" />
                  {{ t('report.exportPdf') }}
                </div>
                <ul tabindex="0" class="menu dropdown-content z-10 w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
                  <li><button type="button" @click="exportPDF(true)">{{ t('report.exportWithPhotos') }}</button></li>
                  <li><button type="button" @click="exportPDF(false)">{{ t('report.exportWithoutPhotos') }}</button></li>
                </ul>
              </div>
            </div>

            <ReportTotalsBar v-if="report.totals" :totals="report.totals" :currency="currency" />

            <EditableMarkdown :source="report.intro_md" :editing="false" placeholder="" />

            <div v-if="clientConfig" class="h-[60dvh]">
              <PlanMap
                :document="report"
                :selected-day="null"
                :tile-url="clientConfig.map_tile_url"
                :attribution="clientConfig.map_attribution"
                @focus="focusReportItem"
              />
            </div>

            <div class="space-y-10">
              <ReportDaySection
                v-for="each in report.days"
                :key="each.id"
                :day="each"
                :stays="report.stays"
                :document-items="report.days.flatMap((day) => day.items)"
                :currency="currency"
                :editing="false"
                :hide-skipped="hideSkipped"
              />
            </div>

            <EditableMarkdown :source="report.summary_md" :editing="false" placeholder="" />
          </section>
        </div>
      </template>
    </main>
  </div>
</template>
