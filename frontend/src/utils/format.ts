import type { DateFormat, TimeFormat, Units } from '@/api/types'
import { activeDateFormat, activeTimeFormat } from '@/utils/display'
import { resolveAmount } from '@/utils/amount'

// Dates and times are written the way the reader asked for. Like the units, the
// choice changes nothing that is carried - a date travels as YYYY-MM-DD and a
// time as minutes after midnight - so each function takes it as an argument and
// falls back to the reader's own setting, which is what nearly every caller
// wants and none of them has to pass.

/** formatDateTime shows a timestamp in the reader's language, zone and clock. */
export function formatDateTime(value: string | null | undefined, locale: string,
  clock: TimeFormat = activeTimeFormat.value): string {
  if (!value) {
    return ''
  }
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium', timeStyle: 'short', hour12: clock === 'h12',
  }).format(new Date(value))
}

/**
 * describeUserAgent shortens a User-Agent header to a browser and system a
 * person would recognise, falling back to the raw value.
 */
export function describeUserAgent(userAgent: string): string {
  const browser =
    /Edg\//.test(userAgent) ? 'Edge'
    : /Firefox\//.test(userAgent) ? 'Firefox'
    : /Chrome\//.test(userAgent) ? 'Chrome'
    : /Safari\//.test(userAgent) ? 'Safari'
    : ''
  const system =
    /Android/.test(userAgent) ? 'Android'
    : /iPhone|iPad/.test(userAgent) ? 'iOS'
    : /Windows/.test(userAgent) ? 'Windows'
    : /Mac OS X/.test(userAgent) ? 'macOS'
    : /Linux/.test(userAgent) ? 'Linux'
    : ''
  if (browser && system) {
    return `${browser} · ${system}`
  }
  return browser || system || userAgent
}

/** parseDate reads a YYYY-MM-DD calendar date as a local-midnight-free UTC date. */
function parseDate(value: string): Date {
  return new Date(`${value}T00:00:00Z`)
}

/**
 * formatDateRange shows a trip's period compactly, such as "20–27 Jun 2026",
 * letting Intl collapse the shared month and year. Dates are calendar dates,
 * so they are formatted in UTC and never shift with the reader's zone.
 */
export function formatDateRange(start: string | null, end: string | null, locale: string): string {
  if (!start || !end) {
    return ''
  }
  const format = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
  return format.formatRange(parseDate(start), parseDate(end))
}

/** formatMoney shows a decimal amount in a currency, rounded per the locale. */
export function formatMoney(amount: string | null, currency: string, locale: string): string {
  if (amount === null) {
    return ''
  }
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency }).format(Number(amount))
  } catch {
    // An unknown code still shows the amount rather than nothing.
    return `${new Intl.NumberFormat(locale).format(Number(amount))} ${currency}`
  }
}

/**
 * normalizeAmount turns a typed amount into the API's decimal string: a
 * formula computed, spaces dropped and a decimal comma accepted. Empty input
 * means no amount.
 */
export function normalizeAmount(value: string): string | null {
  const cleaned = resolveAmount(value).replace(/\s/g, '').replace(',', '.')
  return cleaned === '' ? null : cleaned
}

/** browserTimeZone returns the reader's IANA time zone, or UTC. */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

/**
 * formatClock shows minutes after midnight as a time of day. A schedule that
 * runs past midnight wraps around; dayOffset tells how many days it passed.
 */
export function formatClock(minutes: number,
  clock: TimeFormat = activeTimeFormat.value): { time: string; dayOffset: number } {
  const dayOffset = Math.floor(minutes / 1440)
  const inDay = minutes - dayOffset * 1440
  const hour = Math.floor(inDay / 60)
  const mins = String(inDay % 60).padStart(2, '0')
  if (clock === 'h12') {
    // Midnight and noon are twelve, not zero; the half of the day is written in
    // the way a twelve-hour clock is read everywhere it is used at all.
    const shown = hour % 12 === 0 ? 12 : hour % 12
    return { time: `${shown}:${mins} ${hour < 12 ? 'AM' : 'PM'}`, dayOffset }
  }
  return { time: `${String(hour).padStart(2, '0')}:${mins}`, dayOffset }
}

/**
 * formatTimeOfDay shows a time of day the API carries as "HH:MM" in the
 * reader's clock, or '' for no time.
 */
export function formatTimeOfDay(value: string | null | undefined,
  clock: TimeFormat = activeTimeFormat.value): string {
  const match = /^(\d{1,2}):(\d{2})/.exec(value ?? '')
  if (!match) {
    return ''
  }
  return formatClock(Number(match[1]) * 60 + Number(match[2]), clock).time
}

/**
 * parseTimeOfDay reads a time of day typed in either clock - "14:30", "1430",
 * "9.05", "2:30 pm", "2pm", "12 AM" - into the "HH:MM" the API carries. It
 * returns null for text that is not a time of day; empty text is not a time
 * either, and the caller decides what an empty field means.
 */
export function parseTimeOfDay(text: string): string | null {
  const match = /^(\d{1,2})(?:[:.\s]?(\d{2}))?\s*([ap])?\.?\s*m?\.?$/i.exec(text.trim())
  if (!match) {
    return null
  }
  let hour = Number(match[1])
  const minute = Number(match[2] ?? 0)
  const half = match[3]?.toLowerCase()
  if (minute > 59) {
    return null
  }
  if (half) {
    // A twelve-hour clock runs from 12 to 11: 12 AM is midnight, 12 PM noon.
    if (hour < 1 || hour > 12) {
      return null
    }
    hour = (hour % 12) + (half === 'p' ? 12 : 0)
  } else if (hour > 23) {
    return null
  }
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

/** splitDuration breaks minutes into whole hours and the remaining minutes. */
export function splitDuration(minutes: number): { hours: number; minutes: number } {
  return { hours: Math.floor(minutes / 60), minutes: minutes % 60 }
}

/**
 * formatDayDate shows a calendar date, such as "22 Jun" or "Jun 22", without
 * shifting it by zone. The order is the reader's own: the parts are asked for
 * one at a time and put together here, because Intl writes them in the order of
 * the language rather than the order that was asked for.
 */
export function formatDayDate(value: string | null, locale: string, withWeekday = false,
  order: DateFormat = activeDateFormat.value): string {
  if (!value) {
    return ''
  }
  const date = new Date(`${value}T00:00:00Z`)
  const part = (options: Intl.DateTimeFormatOptions): string =>
    new Intl.DateTimeFormat(locale, { ...options, timeZone: 'UTC' }).format(date)
  const day = part({ day: 'numeric' })
  const month = part({ month: 'short' })
  const written = order === 'mdy' ? `${month} ${day}` : `${day} ${month}`
  return withWeekday ? `${part({ weekday: 'short' })}, ${written}` : written
}

/** addDays moves a YYYY-MM-DD date by whole days. */
export function addDays(value: string, days: number): string {
  const date = new Date(`${value}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

/** metresPerMile is the exact length of a statute mile. */
const METRES_PER_MILE = 1609.344

/** metresPerFoot is the exact length of a foot. */
const METRES_PER_FOOT = 0.3048

/**
 * formatDistance shows a distance in the reader's units.
 *
 * Distances are carried in metres whatever the reader has chosen. Short ones are
 * shown in the small unit - metres, or feet - because "0.2 km" reads as a
 * measurement and "200 m" reads as a walk.
 */
export function formatDistance(metres: number, locale: string, units: Units = 'km'): string {
  if (units === 'mi') {
    const miles = metres / METRES_PER_MILE
    if (miles < 0.1) {
      return new Intl.NumberFormat(locale, {
        style: 'unit', unit: 'foot', maximumFractionDigits: 0,
      }).format(metres / METRES_PER_FOOT)
    }
    return new Intl.NumberFormat(locale, {
      style: 'unit', unit: 'mile', maximumFractionDigits: miles < 10 ? 1 : 0,
    }).format(miles)
  }

  if (metres < 1000) {
    return new Intl.NumberFormat(locale, { style: 'unit', unit: 'meter', maximumFractionDigits: 0 }).format(metres)
  }
  const km = metres / 1000
  return new Intl.NumberFormat(locale, {
    style: 'unit', unit: 'kilometer', maximumFractionDigits: km < 10 ? 1 : 0,
  }).format(km)
}

/**
 * formatHeight shows a height gained or lost: metres, or feet for somebody who
 * counts distances in miles.
 */
export function formatHeight(metres: number, locale: string, units: Units = 'km'): string {
  if (units === 'mi') {
    return new Intl.NumberFormat(locale, { style: 'unit', unit: 'foot', maximumFractionDigits: 0 })
      .format(metres / METRES_PER_FOOT)
  }
  return new Intl.NumberFormat(locale, { style: 'unit', unit: 'meter', maximumFractionDigits: 0 }).format(metres)
}

/**
 * toMetres reads a distance typed in the reader's units.
 *
 * It is the other half of formatDistance: the one field that takes a distance -
 * the one on a leg somebody measured themselves - is typed in whatever they
 * read, and stored in metres like every other.
 */
export function toMetres(value: number, units: Units): number {
  return Math.round(units === 'mi' ? value * METRES_PER_MILE : value * 1000)
}

/** fromMetres writes a stored distance as a number in the reader's units. */
export function fromMetres(metres: number, units: Units): number {
  const value = units === 'mi' ? metres / METRES_PER_MILE : metres / 1000
  // Three decimals is a metre in miles and a metre in kilometres: enough not to
  // move a distance somebody typed, short enough not to show them noise.
  return Math.round(value * 1000) / 1000
}
