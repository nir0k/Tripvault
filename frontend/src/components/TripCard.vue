<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import type { Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaImage from '@/components/media/MediaImage.vue'
import TagPill from '@/components/TagPill.vue'
import TripStatusBadge from '@/components/TripStatusBadge.vue'
import { formatDateRange } from '@/utils/format'
import { readingLanguage, translateTrip } from '@/utils/translate'
import { tripRoute } from '@/utils/tripRoutes'

const props = defineProps<{ trip: Trip }>()

const { t, locale } = useI18n()

// A report's title is shown in the reader's language when it has one.
const title = computed(() => translateTrip(props.trip, readingLanguage(props.trip.languages, locale.value)).title)
const period = computed(() => formatDateRange(props.trip.start_date, props.trip.end_date, locale.value))

// The card opens the trip's one document: the plan of a plan, the report of a
// report.
const target = computed(() => tripRoute(props.trip))
</script>

<template>
  <article class="card border border-base-300 bg-base-100 transition-shadow hover:shadow-md" :data-trip-id="trip.id">
    <!-- The cover strip keeps the proportions its frame is chosen in
         (CoverCropDialog), so the card shows exactly the part that was chosen. -->
    <RouterLink
      :to="target"
      class="flex aspect-[5/2] items-center justify-center overflow-hidden rounded-t-box text-primary"
      :class="trip.cover_media_id ? '' : 'bg-linear-to-br from-primary/25 to-secondary/25'"
      :aria-label="title"
    >
      <MediaImage v-if="trip.cover_media_id" :id="trip.cover_media_id" :alt="title" :size="640" :crop="trip.cover_crop" fill />
      <AppIcon v-else :name="trip.kind === 'report' ? 'report' : 'map'" />
    </RouterLink>
    <div class="card-body gap-2 p-4">
      <div class="flex flex-wrap items-center gap-2">
        <TripStatusBadge :trip="trip" />
        <span v-if="trip.role !== 'owner'" class="badge badge-outline badge-sm">{{ t(`trips.roles.${trip.role}`) }}</span>
      </div>
      <h2 class="card-title text-lg">
        <RouterLink :to="target" class="link-hover break-words">{{ title }}</RouterLink>
      </h2>
      <p class="flex flex-wrap items-center gap-x-2 text-sm text-base-content/70">
        <span>{{ period }}</span>
        <span aria-hidden="true">·</span>
        <span>{{ t('trips.days', trip.day_count) }}</span>
      </p>
      <p v-if="trip.role !== 'owner'" class="truncate text-sm text-base-content/70">
        {{ t('trips.ownedBy', { name: trip.owner.display_name }) }}
      </p>
      <p v-if="trip.tags.length > 0" class="flex flex-wrap gap-1" :aria-label="t('tags.onTrip')">
        <TagPill v-for="tag in trip.tags" :key="tag.id" :tag="tag" />
      </p>
    </div>
  </article>
</template>
