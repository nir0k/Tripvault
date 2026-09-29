<script setup lang="ts">
import { onMounted, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { listIdeas } from '@/api/ideas'
import { listTrips, type TripSort } from '@/api/trips'
import type { DocumentKind, Idea, Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IdeaDialog from '@/components/ideas/IdeaDialog.vue'
import IdeaTile from '@/components/ideas/IdeaTile.vue'
import TripCarousel from '@/components/TripCarousel.vue'
import TripCreateDialog from '@/components/TripCreateDialog.vue'
import { errorMessage } from '@/utils/errors'
import { seasonalIdeas } from '@/utils/ideas'
import { listRouteName } from '@/utils/tripRoutes'

// The start page: a row of plans, the ongoing and nearest first, a row of
// reports, the one changed last first, and the ideas whose season is now or
// next. Each shows the head of its list and leads on to the whole of it, where
// the filters are.

/** HOME_ROW_SIZE is how many cards a row holds before "all" takes over. */
const HOME_ROW_SIZE = 12

/** HOME_IDEAS is how many ideas the start page shows. */
const HOME_IDEAS = 5

interface Row {
  kind: DocumentKind
  sort: TripSort
  trips: Trip[]
  loaded: boolean
  loading: boolean
  error: string
}

const { t, te } = useI18n()

const rows = reactive<Row[]>([
  { kind: 'plan', sort: 'relevance', trips: [], loaded: false, loading: false, error: '' },
  { kind: 'report', sort: 'updated', trips: [], loaded: false, loading: false, error: '' },
])
const creator = useTemplateRef<InstanceType<typeof TripCreateDialog>>('creator')
const ideaCreator = useTemplateRef<InstanceType<typeof IdeaDialog>>('ideaCreator')

// The ideas are read whole, as the list of ideas reads them, and the few whose
// season is now or next are kept: a person has tens of ideas, not thousands.
const ideas = ref<Idea[]>([])
const ideasLoaded = ref(false)
const ideasError = ref('')
async function loadIdeas(): Promise<void> {
  ideasError.value = ''
  try {
    ideas.value = seasonalIdeas(await listIdeas(), new Date().getMonth() + 1, HOME_IDEAS)
    ideasLoaded.value = true
  } catch (err) {
    ideasError.value = errorMessage(err, t, te)
  }
}

// load reads the head of one row's list. A row that fails says so on its own,
// leaving the other one standing.
async function load(row: Row): Promise<void> {
  row.loading = true
  row.error = ''
  try {
    row.trips = (await listTrips({ kind: row.kind, sort: row.sort, limit: HOME_ROW_SIZE })).items
    row.loaded = true
  } catch (err) {
    row.error = errorMessage(err, t, te)
  } finally {
    row.loading = false
  }
}

onMounted(() => {
  for (const row of rows) {
    void load(row)
  }
  void loadIdeas()
})
</script>

<template>
  <section class="space-y-10">
    <!-- The page names itself in the navigation; the heading stays for screen readers. -->
    <h1 class="sr-only">{{ t('home.title') }}</h1>

    <section v-for="row in rows" :key="row.kind" class="space-y-4" :aria-labelledby="`home-${row.kind}`">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 :id="`home-${row.kind}`" class="text-xl font-semibold">
            {{ t(row.kind === 'plan' ? 'home.plans' : 'home.reports') }}
          </h2>
          <p class="text-sm text-base-content/70">
            {{ t(row.kind === 'plan' ? 'home.plansHint' : 'home.reportsHint') }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <RouterLink :to="{ name: listRouteName(row.kind) }" class="btn btn-ghost btn-sm">
            {{ t(row.kind === 'plan' ? 'home.allPlans' : 'home.allReports') }}
            <AppIcon name="chevronRight" />
          </RouterLink>
          <button type="button" class="btn btn-primary btn-sm" @click="creator?.open(row.kind)">
            <AppIcon name="plus" />
            {{ t(`trips.new.${row.kind}`) }}
          </button>
        </div>
      </div>

      <p v-if="row.error" role="alert" class="text-error">{{ row.error }}</p>

      <div
        v-else-if="row.loaded && row.trips.length === 0"
        class="flex flex-col items-center gap-2 rounded-box border border-dashed border-base-300 px-6 py-10 text-center"
      >
        <span class="text-primary"><AppIcon :name="row.kind === 'report' ? 'report' : 'map'" /></span>
        <h3 class="font-semibold">{{ t(`trips.emptyTitle.${row.kind}`) }}</h3>
        <p class="max-w-md text-sm text-base-content/70">{{ t(`trips.emptyText.${row.kind}`) }}</p>
      </div>

      <TripCarousel
        v-else
        :trips="row.trips"
        :loading="row.loading"
        :label="t(row.kind === 'plan' ? 'home.plans' : 'home.reports')"
      />
    </section>

    <section class="space-y-4" aria-labelledby="home-ideas">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 id="home-ideas" class="text-xl font-semibold">{{ t('home.ideas') }}</h2>
          <p class="text-sm text-base-content/70">{{ t('home.ideasHint') }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <RouterLink :to="{ name: 'ideas' }" class="btn btn-ghost btn-sm">
            {{ t('home.allIdeas') }}
            <AppIcon name="chevronRight" />
          </RouterLink>
          <button type="button" class="btn btn-primary btn-sm" @click="ideaCreator?.open()">
            <AppIcon name="plus" />
            {{ t('ideas.new') }}
          </button>
        </div>
      </div>

      <p v-if="ideasError" role="alert" class="text-error">{{ ideasError }}</p>
      <div
        v-else-if="ideasLoaded && ideas.length === 0"
        class="flex flex-col items-center gap-2 rounded-box border border-dashed border-base-300 px-6 py-10 text-center"
      >
        <span class="text-primary"><AppIcon name="lightbulb" /></span>
        <h3 class="font-semibold">{{ t('home.ideasEmptyTitle') }}</h3>
        <p class="max-w-md text-sm text-base-content/70">{{ t('home.ideasEmptyText') }}</p>
      </div>
      <div v-else class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
        <IdeaTile v-for="idea in ideas" :key="idea.id" :idea="idea" />
      </div>
    </section>

    <TripCreateDialog ref="creator" />
    <IdeaDialog ref="ideaCreator" @saved="loadIdeas" />
  </section>
</template>
