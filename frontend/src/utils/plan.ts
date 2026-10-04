import { ACTIVITY_TYPES, COST_CATEGORIES, PLACE_CATEGORIES, STOP_KINDS, TRAVEL_MODES, type PlanItem, type RemovedDay, type Stop } from '@/api/types'
import type { IconOption } from '@/components/IconSelect.vue'
import { ACTIVITY_ICONS, COST_CATEGORY_ICONS, PLACE_CATEGORY_ICONS, STOP_KIND_ICONS, TRAVEL_MODE_ICONS, type OUTLINE } from '@/components/icons'
import { formatClock, formatDayDate, splitDuration } from '@/utils/format'

type Translate = (key: string, named?: Record<string, unknown>) => string

/** formatDuration shows minutes as "2 h 47 min", "2 h" or "47 min". */
export function formatDuration(minutes: number, t: Translate): string {
  const parts = splitDuration(minutes)
  if (parts.hours === 0) {
    return t('duration.minutes', { minutes: parts.minutes })
  }
  if (parts.minutes === 0) {
    return t('duration.hours', { hours: parts.hours })
  }
  return t('duration.hoursMinutes', parts)
}

/** describeRemovedDays lists days a change would remove, for a confirmation. */
export function describeRemovedDays(days: RemovedDay[], t: Translate, locale: string): string[] {
  return days.map((day) => {
    const name = t(`trip.tabs.${day.document}`)
    const label = t('plan.dayNumber', { n: day.position + 1 })
    const date = day.date ? ` · ${formatDayDate(day.date, locale)}` : ''
    const title = day.title ? ` · ${day.title}` : ''
    return `${name}: ${label}${date}${title}`
  })
}

/** STAY_COLORS are the classes that tell stays apart in the nights strip. */
const STAY_COLORS = ['bg-primary', 'bg-secondary', 'bg-accent', 'bg-info', 'bg-success', 'bg-warning'] as const

/** stayColor picks a stay's colour from its place in the list. */
export function stayColor(index: number): string {
  return STAY_COLORS[index % STAY_COLORS.length] ?? 'bg-primary'
}

/**
 * DAY_COLORS tell the days of a document apart. They are read by the map, which
 * draws a day's line and pins in its colour, and by the list of days, which
 * shows the same colour beside each day - without that the colours on the map
 * belong to nobody.
 */
const DAY_COLORS = ['#c2410c', '#0369a1', '#15803d', '#9f1239', '#7c3aed', '#b45309', '#0f766e', '#be185d'] as const

/** dayColor picks a day's colour from its position. */
export function dayColor(index: number): string {
  return DAY_COLORS[index % DAY_COLORS.length] ?? DAY_COLORS[0]
}

/**
 * The choices of the icon dropdowns. They are built here rather than in each
 * component so a category always carries the same name and the same picture,
 * whichever screen offers it.
 */
export function travelModeOptions(t: Translate): IconOption[] {
  return TRAVEL_MODES.map((mode) => ({ value: mode, label: t(`modes.${mode}`), icon: TRAVEL_MODE_ICONS[mode] }))
}

/** placeCategoryOptions lists what a place can be. */
export function placeCategoryOptions(t: Translate): IconOption[] {
  return PLACE_CATEGORIES.map((category) => ({
    value: category, label: t(`categories.${category}`), icon: PLACE_CATEGORY_ICONS[category],
  }))
}

/** activityTypeOptions lists what an activity can be. */
export function activityTypeOptions(t: Translate): IconOption[] {
  return ACTIVITY_TYPES.map((activity) => ({
    value: activity, label: t(`activities.${activity}`), icon: ACTIVITY_ICONS[activity],
  }))
}

/**
 * isVisit tells places and activities - where the trip goes - from the stay
 * marks the stays put into a day.
 */
export function isVisit(item: PlanItem): boolean {
  return item.kind === 'place' || item.kind === 'activity'
}

/** itemIcon names the picture of a place, by its category, or of an activity, by its type. */
export function itemIcon(item: PlanItem): keyof typeof OUTLINE {
  if (item.kind === 'activity' && item.activity_type) {
    return ACTIVITY_ICONS[item.activity_type]
  }
  return PLACE_CATEGORY_ICONS[item.category]
}

/** itemKindLabel says what an element is: its category, or its kind of activity. */
export function itemKindLabel(item: PlanItem, t: Translate): string {
  if (item.kind === 'activity' && item.activity_type) {
    return t(`activities.${item.activity_type}`)
  }
  return t(`categories.${item.category}`)
}

/** costCategoryOptions lists the budget categories a cost can go to. */
export function costCategoryOptions(t: Translate): IconOption[] {
  return COST_CATEGORIES.map((category) => ({
    value: category, label: t(`costCategories.${category}`), icon: COST_CATEGORY_ICONS[category],
  }))
}

/** stopKindOptions lists the kinds of stop along a line, with their pictures. */
export function stopKindOptions(t: Translate): IconOption[] {
  return STOP_KINDS.map((kind) => ({ value: kind, label: t(`stop.kinds.${kind}`), icon: STOP_KIND_ICONS[kind] }))
}

/**
 * stopLabel names a stop beside its activity, as the map and the PDF do: the
 * activity's number and a letter for the stop, "3a", "3b", along the line.
 * Without a number it is the letter alone.
 */
export function stopLabel(number: number | undefined, index: number): string {
  const letter = index < 26 ? String.fromCharCode(97 + index) : `.${index + 1}`
  return number === undefined ? letter : `${number}${letter}`
}

/** stopName is what a stop is called: its own name, or its kind. */
export function stopName(stop: Pick<Stop, 'name' | 'kind'>, t: Translate): string {
  return stop.name || t(`stop.kinds.${stop.kind}`)
}

/** minutesOfDay reads "HH:MM" as minutes since midnight; null when it is no time. */
export function minutesOfDay(value: string | null | undefined): number | null {
  const match = /^(\d{1,2}):(\d{2})/.exec(value ?? '')
  return match ? Number(match[1]) * 60 + Number(match[2]) : null
}

/** timeOfDay writes minutes since midnight as "HH:MM", wrapping past midnight. */
export function timeOfDay(minutes: number): string {
  const inDay = ((minutes % 1440) + 1440) % 1440
  return `${String(Math.floor(inDay / 60)).padStart(2, '0')}:${String(inDay % 60).padStart(2, '0')}`
}

/**
 * minutesBetween is how long a visit from start to end lasts. An end no later
 * than the start is read as the next day's, since a place is not left before it
 * is reached.
 *
 * Arguments:
 *   - start: the time the place is reached, "HH:MM".
 *   - end: the time it is left, "HH:MM".
 *
 * Returns:
 *   - the minutes in between, 1 to 1440; null when either is no time.
 */
export function minutesBetween(start: string, end: string): number | null {
  const from = minutesOfDay(start)
  const to = minutesOfDay(end)
  if (from === null || to === null) {
    return null
  }
  const span = to - from
  return span > 0 ? span : span + 1440
}

/**
 * planTimeLabel says when a place is planned in the fewest words: from and to
 * when both the wished time and the visit's length are known, else whichever
 * of them is.
 *
 * Arguments:
 *   - desired: the wished arrival, "HH:MM", or null.
 *   - visitMinutes: how long the visit lasts; 0 for none.
 *   - t: the translation function, for the length.
 *
 * Returns:
 *   - the label; empty when neither is set.
 */
export function planTimeLabel(desired: string | null, visitMinutes: number, t: Translate): string {
  const start = minutesOfDay(desired)
  if (start !== null && visitMinutes > 0) {
    return `${formatClock(start).time} – ${formatClock(start + visitMinutes).time}`
  }
  if (start !== null) {
    return formatClock(start).time
  }
  return visitMinutes > 0 ? formatDuration(visitMinutes, t) : ''
}
