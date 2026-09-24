import { readonly, ref } from 'vue'
import type { DateFormat, TimeFormat } from '@/api/types'

// How a date and a clock are written for this reader. Like the units, they
// change nothing that is carried: a date travels as YYYY-MM-DD and a time as
// minutes, and this decides only what is read.
//
// Both are kept on the browser as well as in the profile, for the same reason
// the theme and the units are: the sign-in page and the page a read-only link
// opens have no profile to read them from.

const STORED_DATE_FORMAT_KEY = 'tripvault.dateFormat'
const STORED_TIME_FORMAT_KEY = 'tripvault.timeFormat'

const currentDate = ref<DateFormat>('dmy')
const currentTime = ref<TimeFormat>('h24')

/** activeDateFormat is the date order in use, for the controls that show it. */
export const activeDateFormat = readonly(currentDate)

/** activeTimeFormat is the clock in use, for the controls that show it. */
export const activeTimeFormat = readonly(currentTime)

/** isDateFormat reports whether a value names a date order the interface knows. */
function isDateFormat(value: unknown): value is DateFormat {
  return value === 'dmy' || value === 'mdy'
}

/** isTimeFormat reports whether a value names a clock the interface knows. */
function isTimeFormat(value: unknown): value is TimeFormat {
  return value === 'h24' || value === 'h12'
}

/** readStoredDateFormat returns the order chosen on this browser, day first by default. */
export function readStoredDateFormat(): DateFormat {
  try {
    const value = localStorage.getItem(STORED_DATE_FORMAT_KEY)
    return isDateFormat(value) ? value : 'dmy'
  } catch {
    return 'dmy'
  }
}

/** readStoredTimeFormat returns the clock chosen on this browser, 24-hour by default. */
export function readStoredTimeFormat(): TimeFormat {
  try {
    const value = localStorage.getItem(STORED_TIME_FORMAT_KEY)
    return isTimeFormat(value) ? value : 'h24'
  } catch {
    return 'h24'
  }
}

/** applyDateFormat writes dates this way and remembers the choice on this browser. */
export function applyDateFormat(format: DateFormat): void {
  currentDate.value = format
  try {
    localStorage.setItem(STORED_DATE_FORMAT_KEY, format)
  } catch {
    // The choice still applies to this page.
  }
}

/** applyTimeFormat reads times on this clock and remembers the choice. */
export function applyTimeFormat(format: TimeFormat): void {
  currentTime.value = format
  try {
    localStorage.setItem(STORED_TIME_FORMAT_KEY, format)
  } catch {
    // The choice still applies to this page.
  }
}
