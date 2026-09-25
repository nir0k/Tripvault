<script setup lang="ts">
import { useTripStore } from '@/stores/trip'
import TripDocumentView from '@/views/trip/TripDocumentView.vue'
import TripPlanView from '@/views/trip/TripPlanView.vue'

// The document a read-only link opens. The address is the same for a plan and
// a report, because the link rather than the address says which trip it is, so
// the page is chosen once the trip has been read.
const store = useTripStore()
</script>

<template>
  <TripDocumentView v-if="store.trip?.kind === 'report'" />
  <TripPlanView v-else-if="store.trip" />
</template>
