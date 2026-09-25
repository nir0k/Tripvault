import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getDocument } from '@/api/documents'
import { getShared, getSharedDocument } from '@/api/shared'
import { getTrip } from '@/api/trips'
import type { Shared, Trip, TripDocument } from '@/api/types'
import { SHARED_TRIP_ID } from '@/utils/tripRoutes'

/**
 * sharedTrip dresses what a read-only link tells of its trip as the trip a
 * member reads, so the same pages show it. The reader is a viewer, which is
 * what switches every way of changing it off, and the document is named by
 * the link rather than by an identifier.
 */
function sharedTrip(shared: Shared): Trip {
  const { trip } = shared
  return {
    id: SHARED_TRIP_ID,
    kind: shared.kind,
    source_trip_id: null,
    title: trip.title,
    summary: trip.summary,
    start_date: trip.start_date,
    end_date: trip.end_date,
    timezone: trip.timezone,
    currency: trip.currency,
    travelers: trip.travelers,
    budget_amount: null,
    status: trip.status,
    day_count: trip.day_count,
    role: 'viewer',
    owner: { id: '', display_name: trip.owner_name, email: '' },
    plan_id: shared.kind === 'plan' ? SHARED_TRIP_ID : null,
    report_id: shared.kind === 'report' ? SHARED_TRIP_ID : null,
    cover_media_id: null,
    cover_crop: null,
    languages: trip.languages,
    translations: trip.translations,
    created_at: '',
    updated_at: '',
  }
}

/** useTripStore holds the trip whose pages are open, shared by its tabs. */
export const useTripStore = defineStore('trip', () => {
  const trip = ref<Trip | null>(null)
  const error = ref<unknown>(null)
  const loading = ref(false)
  // shared says the trip was opened by a read-only link, and is read through it.
  const shared = ref(false)

  /** load reads a trip, dropping the previous one first so no tab shows stale data. */
  async function load(tripId: string): Promise<void> {
    if (shared.value || trip.value?.id !== tripId) {
      trip.value = null
    }
    shared.value = false
    loading.value = true
    error.value = null
    try {
      trip.value = await getTrip(tripId)
    } catch (err) {
      error.value = err
    } finally {
      loading.value = false
    }
  }

  /** loadShared reads the trip the tab's read-only link opens. */
  async function loadShared(): Promise<void> {
    trip.value = null
    shared.value = true
    loading.value = true
    error.value = null
    try {
      trip.value = sharedTrip(await getShared())
    } catch (err) {
      error.value = err
    } finally {
      loading.value = false
    }
  }

  /**
   * readDocument reads one document of the open trip: by its identifier for a
   * member, and through the link, which opens exactly one, for a link.
   */
  async function readDocument(documentId: string): Promise<TripDocument> {
    return shared.value ? getSharedDocument() : getDocument(documentId)
  }

  /** set replaces the trip after a change the server confirmed. */
  function set(next: Trip): void {
    trip.value = next
  }

  return { trip, error, loading, shared, load, loadShared, readDocument, set }
})
