import { readonly, ref } from 'vue'
import type { Units } from '@/api/types'

// How far a distance is shown in. Everything the API carries is metres; this
// decides only what a reader sees, so two people on one trip may read it
// differently.
//
// The choice is kept on the browser as well as in the profile, for the same
// reason the theme is: a page opened before signing in, and the page a
// read-only link opens, have no profile to read it from.

const STORED_UNITS_KEY = 'tripvault.units'

const current = ref<Units>('km')

/** activeUnits is the units shown now, for controls that display the choice. */
export const activeUnits = readonly(current)

/** isUnits reports whether a value names units the interface knows. */
function isUnits(value: unknown): value is Units {
  return value === 'km' || value === 'mi'
}

/** readStoredUnits returns the units chosen on this browser, kilometres by default. */
export function readStoredUnits(): Units {
  try {
    const value = localStorage.getItem(STORED_UNITS_KEY)
    return isUnits(value) ? value : 'km'
  } catch {
    return 'km'
  }
}

/** applyUnits shows distances in these units and remembers them on this browser. */
export function applyUnits(units: Units): void {
  current.value = units
  try {
    localStorage.setItem(STORED_UNITS_KEY, units)
  } catch {
    // The choice still applies to this page.
  }
}
