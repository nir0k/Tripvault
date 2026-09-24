<script setup lang="ts">
import { onMounted, reactive, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { listTrips, type TripSort } from '@/api/trips'
import type { DocumentKind, Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TripCarousel from '@/components/TripCarousel.vue'
import TripCreateDialog from '@/components/TripCreateDialog.vue'
import { errorMessage } from '@/utils/errors'
import { listRouteName } from '@/utils/tripRoutes'

// The start page: a row of plans, the ongoing and nearest first, and a row of
// reports, the one changed last first. Each row shows the head of its list and
// leads on to the whole of it, where the filters are.

/** HOME_ROW_SIZE is how many cards a row holds before "all" takes over. */
const HOME_ROW_SIZE = 12

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
})
</script>

<template>
  <section class="space-y-10">
    <h1 class="text-2xl font-bold">{{ t('home.title') }}</h1>

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

    <TripCreateDialog ref="creator" />
  </section>
</template>
