<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { deleteIdea, getIdea, listIdeas, setIdeaTags } from '@/api/ideas'
import { IDEA_COSTS, type Idea } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import IdeaDialog from '@/components/ideas/IdeaDialog.vue'
import IdeaPicture from '@/components/ideas/IdeaPicture.vue'
import IdeaResults from '@/components/ideas/IdeaResults.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import TagsPicker from '@/components/TagsPicker.vue'
import TripCreateDialog from '@/components/TripCreateDialog.vue'
import { errorMessage } from '@/utils/errors'
import { formatMoney } from '@/utils/format'
import { useSessionStore } from '@/stores/session'
import {
  activeFilters, countryFlag, countryName, filterFromQuery, filterIdeas, formatDays, formatMonths, formatRange,
} from '@/utils/ideas'
import { formatDuration } from '@/utils/plan'

// One idea read in full: where and when, how long, the ways of getting there
// with their costs and times, whether a visa is needed, what the trip roughly
// costs - from the cheapest way of getting there to the dearest - and the
// description. From here it is changed, tagged, deleted, or made into a plan
// once the time comes; the idea stays after that.
const props = defineProps<{ ideaId: string }>()

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const idea = ref<Idea | null>(null)
const error = ref('')
const editor = useTemplateRef<InstanceType<typeof IdeaDialog>>('editor')
const creator = useTemplateRef<InstanceType<typeof TripCreateDialog>>('creator')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

// load reads the idea named in the address.
async function load(): Promise<void> {
  error.value = ''
  try {
    idea.value = await getIdea(props.ideaId)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}
watch(() => props.ideaId, load, { immediate: true })

// When the idea was opened from a search or a filter, the rest of what they
// found stands beside it: in a column on a wide screen, behind a button on a
// phone. The filter comes in the address, as the list keeps it.
const filter = computed(() => filterFromQuery(route.query, session.user?.default_currency ?? 'EUR'))
const narrowed = computed(() => activeFilters(filter.value) > 0 || filter.value.query.trim() !== '')
const allIdeas = ref<Idea[]>([])
const results = computed(() => (narrowed.value ? filterIdeas(allIdeas.value, filter.value, locale.value) : []))
const showResults = ref(false)
watch(narrowed, async (on) => {
  if (on && allIdeas.value.length === 0) {
    try {
      allIdeas.value = await listIdeas()
    } catch {
      // Without the list the idea is still read; the column stays away.
    }
  }
}, { immediate: true })

// viewing is the photo opened across the screen, by its place among the idea's photos.
const viewer = useTemplateRef<HTMLDialogElement>('viewer')
const viewing = ref(0)
function openPhoto(index: number): void {
  viewing.value = index
  viewer.value?.showModal()
}
// step shows the photo before or after, going round.
function step(delta: number): void {
  const count = idea.value?.photos.length ?? 0
  if (count > 0) {
    viewing.value = (viewing.value + delta + count) % count
  }
}

const months = computed(() => {
  const value = idea.value
  if (!value || value.months.length === 0) {
    return ''
  }
  return value.months.length === 12 ? t('ideas.anyTime') : formatMonths(value.months, locale.value)
})
const days = computed(() => (idea.value ? formatDays(idea.value, t) : ''))

// mapLink opens a place in a map, when it has a point.
function mapLink(lat: number | null, lng: number | null): string {
  return lat !== null && lng !== null ? `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=12/${lat}/${lng}` : ''
}

// money formats an amount in the idea's currency.
function money(amount: string): string {
  return formatMoney(amount, idea.value?.currency ?? '', locale.value)
}

// costRows are the parts of the cost that are known, getting there first as
// the range of its ways; each carries its share of the whole at its least for
// the bar.
const COST_COLORS = { transport: 'bg-info', stay: 'bg-primary', food: 'bg-success', other: 'bg-warning' } as const
const costRows = computed(() => {
  const value = idea.value
  if (!value?.cost_min) {
    return []
  }
  const total = Number(value.cost_min)
  const rows = [
    { part: 'transport' as const, amount: value.transport_min, text: formatRange(value.transport_min, value.transport_max, money) },
    ...IDEA_COSTS.map((part) => {
      const amount = value.costs[part]
      return { part, amount, text: amount === null ? '' : money(amount) }
    }),
  ]
  return rows
    .filter((row) => row.amount !== null)
    .map((row) => ({ ...row, share: total > 0 ? (Number(row.amount) / total) * 100 : 0, color: COST_COLORS[row.part] }))
})
const costTotal = computed(() => (idea.value ? formatRange(idea.value.cost_min, idea.value.cost_max, money) : ''))

// saveTags puts the reader's tags on the idea.
async function saveTags(ids: string[]): Promise<void> {
  if (idea.value) {
    idea.value = await setIdeaTags(idea.value.id, ids)
  }
}

// makePlan opens the form of a new plan filled from the idea, its budget the
// dearest the trip may come to.
function makePlan(): void {
  const value = idea.value
  if (value) {
    creator.value?.open('plan', {
      title: value.title, currency: value.currency, budget: value.cost_max ?? '', intro: value.description_md,
    })
  }
}

// remove deletes the idea after asking, and goes back to the list.
async function remove(): Promise<void> {
  const value = idea.value
  if (!value || !(await confirmDialog.value?.ask(t('ideas.confirmDelete', { title: value.title }), { danger: true }))) {
    return
  }
  try {
    await deleteIdea(value.id)
    await router.push({ name: 'ideas', query: route.query })
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}
</script>

<template>
  <div class="flex gap-6">
    <aside v-if="narrowed && results.length > 0" class="hidden w-72 shrink-0 lg:block">
      <div class="sticky top-4 max-h-[calc(100dvh-2rem)] overflow-y-auto">
        <IdeaResults :ideas="results" :current-id="ideaId" :query="route.query" />
      </div>
    </aside>
    <!-- On a phone the results slide in from the left over the idea, and over
         the bars of the page: the page's content is a layer of its own, so the
         panel is put straight into the body. -->
    <Teleport to="body">
      <div v-if="showResults" class="fixed inset-0 z-50 flex lg:hidden">
        <div class="h-full w-80 max-w-[85vw] overflow-y-auto bg-base-100 p-4 shadow-xl">
          <IdeaResults :ideas="results" :current-id="ideaId" :query="route.query" @chosen="showResults = false" />
        </div>
        <button type="button" class="flex-1 bg-black/40" :aria-label="t('common.close')" @click="showResults = false"></button>
      </div>
    </Teleport>

    <section class="min-w-0 flex-1 space-y-4">
      <div class="flex flex-wrap items-center gap-2">
        <RouterLink :to="{ name: 'ideas', query: route.query }" class="btn btn-ghost btn-sm">
          <AppIcon name="arrowLeft" />
          {{ t('ideas.back') }}
        </RouterLink>
        <button
          v-if="narrowed && results.length > 0"
          type="button"
          class="btn btn-sm btn-hover-outline lg:hidden"
          @click="showResults = true"
        >
          {{ t('ideas.resultsCount', { n: results.length }) }}
        </button>
      </div>

      <p v-if="error" role="alert" class="text-error">{{ error }}</p>
      <div v-else-if="!idea" class="flex justify-center py-8"><span class="loading loading-spinner"></span></div>

      <template v-else>
        <header class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0 space-y-1">
            <h1 class="text-2xl font-bold break-words">{{ idea.title }}</h1>
            <p v-if="idea.countries.length > 0">
              <span v-for="(code, index) in idea.countries" :key="code">
                {{ index > 0 ? ', ' : '' }}<span aria-hidden="true">{{ countryFlag(code) }}</span> {{ countryName(code, locale) }}
              </span>
            </p>
            <ul v-if="idea.places.length > 0" class="text-sm text-base-content/80">
              <li v-for="(place, index) in idea.places" :key="index" class="flex items-center gap-1">
                <AppIcon name="map" class="size-4! opacity-60" />
                {{ place.name || t('ideas.placeNumber', { n: index + 1 }) }}
                <a
                  v-if="mapLink(place.lat, place.lng)"
                  :href="mapLink(place.lat, place.lng)"
                  target="_blank"
                  rel="noopener"
                  class="link ms-1"
                >{{ t('ideas.onMap') }}</a>
              </li>
            </ul>
            <TagsPicker :tags="idea.tags" kind="idea" :save="saveTags" />
          </div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-primary btn-sm" @click="makePlan">
              <AppIcon name="plan" />
              {{ t('ideas.makePlan') }}
            </button>
            <button type="button" class="btn btn-sm btn-hover-outline" @click="editor?.open(idea)">
              <AppIcon name="pencil" />
              {{ t('ideas.edit') }}
            </button>
            <button type="button" class="btn btn-ghost btn-sm text-error" :aria-label="t('ideas.delete')" :title="t('ideas.delete')" @click="remove">
              <AppIcon name="trash" />
            </button>
          </div>
        </header>

        <div class="grid gap-4 md:grid-cols-2">
          <div class="card border border-base-300 bg-base-100">
            <dl class="card-body gap-3 p-4">
              <div>
                <dt class="text-sm text-base-content/60">{{ t('ideas.months') }}</dt>
                <dd>{{ months || t('ideas.notSaid') }}</dd>
              </div>
              <div>
                <dt class="text-sm text-base-content/60">{{ t('ideas.duration') }}</dt>
                <dd>{{ days || t('ideas.notSaid') }}</dd>
              </div>
              <div>
                <dt class="text-sm text-base-content/60">{{ t('ideas.gettingThere') }}</dt>
                <dd v-if="idea.transports.length > 0">
                  <ul class="flex flex-col gap-1">
                    <li
                      v-for="(transport, index) in idea.transports"
                      :key="index"
                      class="flex flex-wrap items-center gap-x-3 gap-y-1"
                    >
                      <span class="flex items-center gap-1">
                        <template v-for="(mode, order) in transport.modes" :key="mode">
                          <span v-if="order > 0" class="opacity-50">+</span>
                          <AppIcon :name="TRAVEL_MODE_ICONS[mode]" class="size-4!" />{{ t(`modes.${mode}`) }}
                        </template>
                      </span>
                      <span v-if="transport.minutes !== null" class="text-sm text-base-content/70">
                        {{ formatDuration(transport.minutes, t) }}
                      </span>
                      <span v-if="transport.cost !== null" class="text-sm font-medium">{{ money(transport.cost) }}</span>
                    </li>
                  </ul>
                </dd>
                <dd v-else>{{ t('ideas.notSaid') }}</dd>
              </div>
              <div>
                <dt class="text-sm text-base-content/60">{{ t('ideas.visa') }}</dt>
                <dd>{{ t(`ideas.visas.${idea.visa}`) }}</dd>
              </div>
            </dl>
          </div>

          <div class="card border border-base-300 bg-base-100">
            <div class="card-body gap-3 p-4">
              <div class="flex flex-wrap items-baseline justify-between gap-2">
                <h2 class="font-semibold">{{ t('ideas.cost') }}</h2>
                <span v-if="costTotal" class="text-xl font-bold">≈ {{ costTotal }}</span>
              </div>
              <template v-if="costRows.length > 0">
                <div class="flex h-2 overflow-hidden rounded-full bg-base-200" aria-hidden="true">
                  <div v-for="row in costRows" :key="row.part" :class="row.color" :style="{ width: `${row.share}%` }"></div>
                </div>
                <ul class="flex flex-col gap-1 text-sm">
                  <li v-for="row in costRows" :key="row.part" class="flex items-center gap-2">
                    <span class="size-2.5 rounded-full" :class="row.color" aria-hidden="true"></span>
                    <span class="flex-1">{{ row.part === 'transport' ? t('ideas.gettingThere') : t(`ideas.costs.${row.part}`) }}</span>
                    <span class="tabular-nums">{{ row.text }}</span>
                  </li>
                </ul>
              </template>
              <p v-else class="text-sm text-base-content/60">{{ t('ideas.noCost') }}</p>
            </div>
          </div>
        </div>

        <div v-if="idea.photos.length > 0" class="grid grid-cols-3 gap-2 sm:grid-cols-5 xl:grid-cols-10">
          <button
            v-for="(photo, index) in idea.photos"
            :key="photo.id"
            type="button"
            class="aspect-square cursor-pointer overflow-hidden rounded-box"
            :aria-label="t('ideas.openPhoto', { n: index + 1 })"
            @click="openPhoto(index)"
          >
            <IdeaPicture :idea-id="idea.id" :photo-id="photo.id" class="size-full transition hover:scale-105" />
          </button>
        </div>

        <div v-if="idea.description_md" class="card border border-base-300 bg-base-100">
          <div class="card-body p-4">
            <MarkdownText :source="idea.description_md" />
          </div>
        </div>
      </template>

      <IdeaDialog ref="editor" @saved="(saved) => (idea = saved)" />
      <TripCreateDialog ref="creator" />
      <ConfirmDialog ref="confirmDialog" />

      <!-- A photo across the screen, with the ones before and after a click or an arrow key away. -->
      <dialog ref="viewer" class="modal" @keydown.left="step(-1)" @keydown.right="step(1)">
        <div v-if="idea && idea.photos[viewing]" class="modal-box flex max-h-[95dvh] max-w-[95vw] flex-col items-center gap-2 bg-base-300 p-2">
          <IdeaPicture
            :key="idea.photos[viewing]!.id"
            :idea-id="idea.id"
            :photo-id="idea.photos[viewing]!.id"
            :preview="false"
            class="max-h-[85dvh] w-auto max-w-full object-contain!"
          />
          <div class="flex items-center gap-3">
            <button type="button" class="btn btn-circle btn-sm" :aria-label="t('ideas.previousPhoto')" @click="step(-1)">
              <AppIcon name="chevronLeft" />
            </button>
            <span class="text-sm tabular-nums">{{ viewing + 1 }} / {{ idea.photos.length }}</span>
            <button type="button" class="btn btn-circle btn-sm" :aria-label="t('ideas.nextPhoto')" @click="step(1)">
              <AppIcon name="chevronRight" />
            </button>
          </div>
        </div>
        <form method="dialog" class="modal-backdrop">
          <button type="submit">{{ t('common.close') }}</button>
        </form>
      </dialog>
    </section>
  </div>
</template>
