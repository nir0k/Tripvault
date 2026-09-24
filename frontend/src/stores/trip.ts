import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getTrip } from '@/api/trips'
import type { Trip } from '@/api/types'

/** useTripStore holds the trip whose pages are open, shared by its tabs. */
export const useTripStore = defineStore('trip', () => {
  const trip = ref<Trip | null>(null)
  const error = ref<unknown>(null)
  const loading = ref(false)

  /** load reads a trip, dropping the previous one first so no tab shows stale data. */
  async function load(tripId: string): Promise<void> {
    if (trip.value?.id !== tripId) {
      trip.value = null
    }
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

  /** set replaces the trip after a change the server confirmed. */
  function set(next: Trip): void {
    trip.value = next
  }

  return { trip, error, loading, load, set }
})
