<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { listIdeas } from '@/api/ideas'
import { VISA_REQUIREMENTS, type Idea, type TravelMode, type TripTag, type VisaRequirement } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import CountrySelect from '@/components/ideas/CountrySelect.vue'
import DaysRangeSlider from '@/components/ideas/DaysRangeSlider.vue'
import IdeaDialog from '@/components/ideas/IdeaDialog.vue'
import IdeaPicture from '@/components/ideas/IdeaPicture.vue'
import MonthPicker from '@/components/ideas/MonthPicker.vue'
import TagPill from '@/components/TagPill.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'
import { formatMoney } from '@/utils/format'
import {
  IDEA_TRAVEL_MODES, MAX_IDEA_DAYS, activeFilters, countryFlag, countryName, emptyFilter, filterFromQuery,
  filterIdeas, filterToQuery, formatDays, formatMonths, formatRange, otherCurrencies, type IdeaFilter, type IdeaSort,
} from '@/utils/ideas'

// The reader's ideas of where to go, as a table narrowed by any of their
// fields and ordered by a click on a column's heading, the last changed first
// until one is clicked. The whole list is read
// once and filtered here as the filter changes; the filter is kept in the
// address, so a narrowed list can be opened again.

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const ideas = ref<Idea[]>([])
const loaded = ref(false)
const error = ref('')
const creator = useTemplateRef<InstanceType<typeof IdeaDialog>>('creator')
const showFilters = ref(false)

// The reader's own currency is what costs are compared in unless the filter names another.
const homeCurrency = computed(() => session.user?.default_currency ?? 'EUR')

// filter is read from the address and written back to it, never kept apart.
const filter = computed<IdeaFilter>(() => filterFromQuery(route.query, homeCurrency.value))

// change writes one change of the filter into the address.
function change(fields: Partial<IdeaFilter>): void {
  void router.replace({ query: filterToQuery({ ...filter.value, ...fields }, homeCurrency.value) })
}

// reset clears every filter and the search, keeping the order.
function reset(): void {
  change({ ...emptyFilter(homeCurrency.value), sort: filter.value.sort, desc: filter.value.desc })
}

// sortBy orders the table by a column; a second click on it turns the order
// round. The changes start with the newest, the other columns with the least.
function sortBy(sort: IdeaSort): void {
  change(filter.value.sort === sort ? { desc: !filter.value.desc } : { sort, desc: sort === 'updated' })
}

// ariaSort tells a screen reader how a column orders the table.
function ariaSort(sort: IdeaSort): 'ascending' | 'descending' | 'none' {
  if (filter.value.sort !== sort) {
    return 'none'
  }
  return filter.value.desc ? 'descending' : 'ascending'
}

// toggle adds a value to a list of the filter, or takes it off.
function toggle<T>(values: T[], value: T): T[] {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value]
}

// The days filter covers every length until it is narrowed.
const daysFrom = computed(() => filter.value.days?.[0] ?? 1)
const daysTo = computed(() => filter.value.days?.[1] ?? MAX_IDEA_DAYS)
function setDays(from: number, to: number): void {
  change({ days: from === 1 && to === MAX_IDEA_DAYS ? null : [from, to] })
}

const shown = computed(() => filterIdeas(ideas.value, filter.value, locale.value))

// The table shows a page of rows at a time: as many as the window holds, ten
// on a phone, and as many again each time its end comes into view.
const ROW_HEIGHT = 57
function pageSize(): number {
  if (window.matchMedia('(max-width: 639px)').matches) {
    return 10
  }
  return Math.max(10, Math.ceil((window.innerHeight - 220) / ROW_HEIGHT))
}
const limit = ref(pageSize())
const page = computed(() => shown.value.slice(0, limit.value))
function showMore(): void {
  limit.value += pageSize()
}
// A new filter starts again from the first page.
watch(() => route.query, () => {
  limit.value = pageSize()
})
const sentinel = useTemplateRef<HTMLElement>('sentinel')
let observer: IntersectionObserver | null = null
watch(sentinel, (element) => {
  observer?.disconnect()
  if (element && 'IntersectionObserver' in window) {
    observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        showMore()
      }
    })
    observer.observe(element)
  }
})
onBeforeUnmount(() => observer?.disconnect())
const hiddenByCurrency = computed(() => otherCurrencies(ideas.value, filter.value, locale.value))
const narrowed = computed(() => activeFilters(filter.value) > 0 || filter.value.query.trim() !== '')

// The tags and currencies worth offering are the ones the ideas carry.
const tags = computed(() => {
  const seen = new Map<string, TripTag>()
  ideas.value.forEach((idea) => idea.tags.forEach((tag) => seen.set(tag.id, tag)))
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name, locale.value))
})
const currencies = computed(() => [...new Set([homeCurrency.value, ...ideas.value.map((idea) => idea.currency)])].sort())

// numberOrNull reads a number field of the filter, empty meaning no filter.
function numberOrNull(event: Event): number | null {
  const value = (event.target as HTMLInputElement).value.trim()
  return value === '' || !Number.isFinite(Number(value)) ? null : Number(value)
}

// months reads an idea's best months for the table.
function months(idea: Idea): string {
  return idea.months.length === 12 ? t('ideas.anyTime') : formatMonths(idea.months, locale.value)
}

// cost reads an idea's whole cost for the table, from the cheapest way there to the dearest.
function cost(idea: Idea): string {
  return formatRange(idea.cost_min, idea.cost_max, (amount) => formatMoney(amount, idea.currency, locale.value))
}

// modes lists every way of travelling an idea's ways of getting there use, once.
function modes(idea: Idea): TravelMode[] {
  return IDEA_TRAVEL_MODES.filter((mode) => idea.transports.some((transport) => transport.modes.includes(mode)))
}

// load reads the reader's ideas.
async function load(): Promise<void> {
  error.value = ''
  try {
    ideas.value = await listIdeas()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loaded.value = true
  }
}
onMounted(load)

// created opens a new idea's page once it is saved.
async function created(idea: Idea): Promise<void> {
  await router.push({ name: 'idea', params: { ideaId: idea.id } })
}

const COLUMNS: { key: string; sort?: IdeaSort }[] = [
  { key: 'title', sort: 'title' }, { key: 'countries' }, { key: 'days', sort: 'days' },
  { key: 'cost', sort: 'cost' }, { key: 'transport' }, { key: 'visa' }, { key: 'tags' },
]
</script>

<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('ideas.titleList') }}</h1>
      <button type="button" class="btn btn-primary" @click="creator?.open()">
        <AppIcon name="plus" />
        {{ t('ideas.new') }}
      </button>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <label class="input input-sm min-w-48 flex-1">
        <AppIcon name="search" />
        <input
          type="search"
          :value="filter.query"
          :placeholder="t('ideas.search')"
          :aria-label="t('ideas.search')"
          @input="change({ query: ($event.target as HTMLInputElement).value })"
        />
      </label>
      <button
        type="button"
        class="btn btn-sm"
        :class="showFilters ? 'btn-active' : 'btn-hover-outline'"
        :aria-expanded="showFilters"
        @click="showFilters = !showFilters"
      >
        <AppIcon name="filter" />
        {{ t('ideas.filters') }}
        <span v-if="activeFilters(filter) > 0" class="badge badge-primary badge-sm">{{ activeFilters(filter) }}</span>
      </button>
    </div>

    <div v-if="showFilters" class="card border border-base-300 bg-base-100">
      <div class="card-body grid gap-4 p-4 md:grid-cols-2">
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.countries') }}</span>
          <CountrySelect
            :model-value="filter.countries"
            :label="t('ideas.anyCountry')"
            @update:model-value="(countries) => change({ countries })"
          />
        </div>
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.filterMonths') }}</span>
          <MonthPicker
            :model-value="filter.months"
            :label="t('ideas.filterMonths')"
            @update:model-value="(months) => change({ months })"
          />
        </div>
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.filterDays') }}</span>
          <DaysRangeSlider
            :from="daysFrom"
            :to="daysTo"
            :label="t('ideas.filterDays')"
            @update:from="(from) => setDays(from, daysTo)"
            @update:to="(to) => setDays(daysFrom, to)"
          />
        </div>
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.filterCost') }}</span>
          <div class="flex gap-2">
            <input
              type="number"
              min="0"
              class="input input-sm min-w-0 flex-1"
              :value="filter.maxCost ?? ''"
              :placeholder="t('ideas.anyCost')"
              :aria-label="t('ideas.filterCost')"
              @change="change({ maxCost: numberOrNull($event) })"
            />
            <select
              class="select select-sm w-auto"
              :value="filter.currency"
              :aria-label="t('ideas.currency')"
              @change="change({ currency: ($event.target as HTMLSelectElement).value })"
            >
              <option v-for="code in currencies" :key="code" :value="code">{{ code }}</option>
            </select>
          </div>
          <p class="text-xs text-base-content/60">{{ t('ideas.costFilterHint') }}</p>
          <p v-if="hiddenByCurrency > 0" class="text-xs text-base-content/60">
            {{ t('ideas.otherCurrencies', { n: hiddenByCurrency }, hiddenByCurrency) }}
          </p>
        </div>
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.gettingThere') }}</span>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="mode in IDEA_TRAVEL_MODES"
              :key="mode"
              type="button"
              class="btn btn-xs"
              :class="filter.modes.includes(mode) ? 'btn-primary' : 'btn-ghost border-base-300'"
              :aria-pressed="filter.modes.includes(mode)"
              @click="change({ modes: toggle<TravelMode>(filter.modes, mode) })"
            >
              <AppIcon :name="TRAVEL_MODE_ICONS[mode]" class="size-3.5!" />
              {{ t(`modes.${mode}`) }}
            </button>
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <span class="label">{{ t('ideas.visa') }}</span>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="visa in VISA_REQUIREMENTS"
              :key="visa"
              type="button"
              class="btn btn-xs"
              :class="filter.visas.includes(visa) ? 'btn-primary' : 'btn-ghost border-base-300'"
              :aria-pressed="filter.visas.includes(visa)"
              @click="change({ visas: toggle<VisaRequirement>(filter.visas, visa) })"
            >
              {{ t(`ideas.visas.${visa}`) }}
            </button>
          </div>
        </div>
        <div v-if="tags.length > 0" class="flex flex-col gap-1 md:col-span-2">
          <span class="label">{{ t('ideas.tags') }}</span>
          <div class="flex flex-wrap gap-1">
            <button
              v-for="tag in tags"
              :key="tag.id"
              type="button"
              class="cursor-pointer rounded-full"
              :class="filter.tags.includes(tag.id) ? 'ring-2 ring-primary' : 'opacity-60 hover:opacity-100'"
              :aria-pressed="filter.tags.includes(tag.id)"
              @click="change({ tags: toggle(filter.tags, tag.id) })"
            >
              <TagPill :tag="tag" />
            </button>
          </div>
        </div>
        <div class="flex justify-end md:col-span-2">
          <button type="button" class="btn btn-ghost btn-sm" :disabled="!narrowed" @click="reset">{{ t('ideas.resetFilters') }}</button>
        </div>
      </div>
    </div>

    <p v-if="error" role="alert" class="text-error">{{ error }}</p>
    <div v-else-if="!loaded" class="flex justify-center py-8"><span class="loading loading-spinner"></span></div>
    <div v-else-if="ideas.length === 0" class="rounded-box border border-dashed border-base-300 p-6 text-center">
      <AppIcon name="lightbulb" class="mx-auto mb-2 size-8! opacity-50" />
      <p class="font-semibold">{{ t('ideas.emptyTitle') }}</p>
      <p class="text-sm text-base-content/70">{{ t('ideas.emptyText') }}</p>
    </div>
    <template v-else>
      <p v-if="narrowed" class="text-sm text-base-content/70">{{ t('ideas.found', { n: shown.length, total: ideas.length }) }}</p>
      <!-- The table scrolls on its own, so a phone's page never grows wider than the screen. -->
      <template v-if="shown.length > 0">
        <div class="overflow-x-auto rounded-box border border-base-300 bg-base-100">
          <table class="table table-zebra table-sm">
            <thead>
              <tr>
                <th v-for="column in COLUMNS" :key="column.key" :aria-sort="column.sort ? ariaSort(column.sort) : undefined" class="whitespace-nowrap">
                  <button
                    v-if="column.sort"
                    type="button"
                    class="inline-flex cursor-pointer items-center gap-1 hover:text-base-content"
                    @click="sortBy(column.sort)"
                  >
                    {{ t(`ideas.columns.${column.key}`) }}
                    <span v-if="filter.sort === column.sort" aria-hidden="true">{{ filter.desc ? '↓' : '↑' }}</span>
                  </button>
                  <template v-else>{{ t(`ideas.columns.${column.key}`) }}</template>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="idea in page" :key="idea.id" class="row-hover">
                <td class="min-w-48">
                  <RouterLink
                    :to="{ name: 'idea', params: { ideaId: idea.id }, query: route.query }"
                    class="flex items-center gap-3 font-semibold hover:underline"
                  >
                    <IdeaPicture
                      v-if="idea.photos[0]"
                      :idea-id="idea.id"
                      :photo-id="idea.photos[0].id"
                      class="size-10 shrink-0 rounded-field"
                    />
                    <span v-else class="flex size-10 shrink-0 items-center justify-center rounded-field bg-base-200" aria-hidden="true">
                      <AppIcon name="lightbulb" class="size-4! opacity-40" />
                    </span>
                    {{ idea.title }}
                  </RouterLink>
                </td>
                <td class="min-w-32 max-w-56">
                  <span v-for="(code, index) in idea.countries" :key="code" class="whitespace-nowrap">
                    {{ index > 0 ? ', ' : '' }}<span aria-hidden="true">{{ countryFlag(code) }}</span> {{ countryName(code, locale) }}
                  </span>
                </td>
                <td class="min-w-32">
                  <p class="whitespace-nowrap">{{ months(idea) }}</p>
                  <p class="text-xs text-base-content/60">{{ formatDays(idea, t) }}</p>
                </td>
                <td class="whitespace-nowrap font-medium tabular-nums">{{ cost(idea) }}</td>
                <td>
                  <span class="flex gap-1 text-base-content/70">
                    <span v-for="mode in modes(idea)" :key="mode" :title="t(`modes.${mode}`)">
                      <AppIcon :name="TRAVEL_MODE_ICONS[mode]" class="size-4!" />
                      <span class="sr-only">{{ t(`modes.${mode}`) }}</span>
                    </span>
                  </span>
                </td>
                <td>
                  <span
                    class="badge badge-sm whitespace-nowrap"
                    :class="{
                      'badge-success': idea.visa === 'not_needed', 'badge-info': idea.visa === 'on_arrival',
                      'badge-warning': idea.visa === 'needed', 'badge-ghost': idea.visa === 'unknown',
                    }"
                  >
                    {{ t(`ideas.visas.${idea.visa}`) }}
                  </span>
                </td>
                <td>
                  <span class="flex flex-wrap gap-1">
                    <TagPill v-for="tag in idea.tags" :key="tag.id" :tag="tag" />
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <!-- More rows come as the end of the table scrolls into view; the button
           is there for when it does not, and says how many there are. -->
        <div v-if="shown.length > limit" ref="sentinel" class="flex flex-col items-center gap-1 py-2">
          <button type="button" class="btn btn-sm btn-hover-outline" @click="showMore">{{ t('ideas.more') }}</button>
          <span class="text-xs text-base-content/60">{{ t('ideas.shownOf', { n: page.length, total: shown.length }) }}</span>
        </div>
      </template>
      <p v-else class="text-sm text-base-content/70">{{ t('ideas.noneFound') }}</p>
    </template>

    <IdeaDialog ref="creator" @saved="created" />
  </section>
</template>
