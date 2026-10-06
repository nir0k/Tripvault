import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import type { Idea, TravelMode, VisaRequirement } from '@/api/types'
import { TRAVEL_MODES, VISA_REQUIREMENTS } from '@/api/types'

// Helpers of the ideas the reader may open - their own and those of the lists
// shared with them: the countries they are in, and the filter the list is
// narrowed by. The whole list is in the browser, so filtering is a
// function of it, and the filter lives in the address, so a narrowed list can
// be opened again or kept as a bookmark.

/**
 * COUNTRY_CODES are the ISO 3166-1 alpha-2 codes a country is chosen from; the
 * names come from the browser in the reader's language, so only the codes are
 * kept here.
 */
export const COUNTRY_CODES: readonly string[] = (
  'AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW ' +
  'BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI ' +
  'FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN ' +
  'IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME ' +
  'MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF ' +
  'PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV ' +
  'SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS XK ' +
  'YE YT ZA ZM ZW'
).split(' ')

/** countryName reads a country's name in the reader's language, or its code when the browser has none. */
export function countryName(code: string, locale: string): string {
  try {
    return new Intl.DisplayNames([locale], { type: 'region' }).of(code) ?? code
  } catch {
    return code
  }
}

/** countryFlag draws a country's flag from its code, as the two regional-indicator letters. */
export function countryFlag(code: string): string {
  return [...code.toUpperCase()].map((letter) => String.fromCodePoint(0x1f1e6 + letter.charCodeAt(0) - 65)).join('')
}

/** MAX_IDEA_DAYS is the longest an idea's trip may take, as the server allows. */
export const MAX_IDEA_DAYS = 30

/** MAX_IDEA_PHOTOS is how many photos an idea keeps, as the server allows. */
export const MAX_IDEA_PHOTOS = 10

/** IdeaSort is the column the list is ordered by. */
export type IdeaSort = 'updated' | 'title' | 'cost' | 'days'

/** IdeaFilter is what the list is narrowed by; an empty field narrows nothing. */
export interface IdeaFilter {
  /** Words looked for in the title, the places, the description and the countries. */
  query: string
  /** Ideas in any of these countries. */
  countries: string[]
  /** Ideas best in any of these months. */
  months: number[]
  /** Ideas whose days overlap this range; null narrows nothing. */
  days: [number, number] | null
  /** Ideas that can cost at most this much in all, in the currency. */
  maxCost: number | null
  currency: string
  /** Ideas wearing any of these tags. */
  tags: string[]
  /** Ideas of any of these people's lists, by the owner's identifier. */
  owners: string[]
  visas: VisaRequirement[]
  /** Ideas reached by any of these ways, alone or mixed. */
  modes: TravelMode[]
  sort: IdeaSort
  /** The order is reversed: the newest first for the changes, the last first for the others. */
  desc: boolean
}

/** emptyFilter narrows nothing, comparing costs in the reader's currency and showing the last changed first. */
export function emptyFilter(currency: string): IdeaFilter {
  return {
    query: '', countries: [], months: [], days: null, maxCost: null, currency, tags: [], owners: [],
    visas: [], modes: [], sort: 'updated', desc: true,
  }
}

/** activeFilters counts the filters that narrow the list, the search and the order left out. */
export function activeFilters(filter: IdeaFilter): number {
  return [filter.countries.length > 0, filter.months.length > 0, filter.days !== null, filter.maxCost !== null,
    filter.tags.length > 0, filter.owners.length > 0, filter.visas.length > 0, filter.modes.length > 0]
    .filter(Boolean).length
}

/**
 * ideaCost reads what an idea's whole trip costs at its least: with the
 * cheapest way of getting there. Null while it is unknown.
 */
function ideaCost(idea: Idea): number | null {
  return idea.cost_min === null ? null : Number(idea.cost_min)
}

/** ideaDays reads the days an idea takes as a range, either end standing for the other; null while not said. */
function ideaDays(idea: Idea): [number, number] | null {
  const least = idea.days_min ?? idea.days_ideal ?? idea.days_max
  const most = idea.days_max ?? idea.days_ideal ?? idea.days_min
  return least === null || most === null ? null : [least, most]
}

/**
 * matchesIdea says whether an idea passes the filter. A filter on something an
 * idea does not say - its months, its days, its cost - leaves the idea out, since
 * nobody can tell it fits; so does a cost filter on an idea in another currency.
 *
 * Arguments:
 *   - idea: the idea.
 *   - filter: what the list is narrowed by.
 *   - locale: the reader's language, which the country names are searched in.
 */
function matchesIdea(idea: Idea, filter: IdeaFilter, locale: string): boolean {
  const query = filter.query.trim().toLocaleLowerCase(locale)
  if (query !== '') {
    const text = [idea.title, idea.description_md, ...idea.places.map((place) => place.name),
      ...idea.countries.map((code) => countryName(code, locale))].join('\n').toLocaleLowerCase(locale)
    if (!query.split(/\s+/).every((word) => text.includes(word))) {
      return false
    }
  }
  if (filter.countries.length > 0 && !idea.countries.some((code) => filter.countries.includes(code))) {
    return false
  }
  if (filter.months.length > 0 && !idea.months.some((month) => filter.months.includes(month))) {
    return false
  }
  if (filter.days !== null) {
    const days = ideaDays(idea)
    if (days === null || days[1] < filter.days[0] || days[0] > filter.days[1]) {
      return false
    }
  }
  if (filter.maxCost !== null) {
    const cost = ideaCost(idea)
    if (idea.currency !== filter.currency || cost === null || cost > filter.maxCost) {
      return false
    }
  }
  if (filter.tags.length > 0 && !idea.tags.some((tag) => filter.tags.includes(tag.id))) {
    return false
  }
  if (filter.owners.length > 0 && !filter.owners.includes(idea.owner.id)) {
    return false
  }
  if (filter.visas.length > 0 && !filter.visas.includes(idea.visa)) {
    return false
  }
  return filter.modes.length === 0
    || idea.transports.some((transport) => transport.modes.some((mode) => filter.modes.includes(mode)))
}

/**
 * filterIdeas narrows the list and puts it in the order asked for. What an
 * order has nothing to go by - a cost or days not said - comes last either way.
 *
 * Returns:
 *   - the ideas that pass, in order.
 */
export function filterIdeas(ideas: Idea[], filter: IdeaFilter, locale: string): Idea[] {
  const passed = ideas.filter((idea) => matchesIdea(idea, filter, locale))
  const sign = filter.desc ? -1 : 1
  // byNumber orders by a figure, the ideas without one last whichever way.
  const byNumber = (value: (idea: Idea) => number | null) => (a: Idea, b: Idea): number => {
    const first = value(a)
    const second = value(b)
    if (first === null || second === null) {
      return first === second ? 0 : first === null ? 1 : -1
    }
    return sign * (first - second)
  }
  switch (filter.sort) {
    case 'title':
      return passed.sort((a, b) => sign * a.title.localeCompare(b.title, locale))
    case 'cost':
      return passed.sort(byNumber(ideaCost))
    case 'days':
      return passed.sort(byNumber((idea) => ideaDays(idea)?.[0] ?? null))
    default:
      return passed.sort((a, b) => sign * a.updated_at.localeCompare(b.updated_at))
  }
}

/**
 * otherCurrencies counts the ideas a cost filter leaves out only for being in
 * another currency, which the page says rather than hiding them silently.
 */
export function otherCurrencies(ideas: Idea[], filter: IdeaFilter, locale: string): number {
  if (filter.maxCost === null) {
    return 0
  }
  const unpriced = { ...filter, maxCost: null }
  return ideas.filter((idea) => idea.currency !== filter.currency && matchesIdea(idea, unpriced, locale)).length
}

// list and number read one field of the address.
function list(value: LocationQuery[string] | undefined): string[] {
  const raw = Array.isArray(value) ? value[0] : value
  return raw ? raw.split(',').filter((item) => item !== '') : []
}
function number(value: LocationQuery[string] | undefined): number | null {
  const [raw] = list(value)
  const parsed = Number(raw)
  return raw !== undefined && Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}

/** filterFromQuery reads a filter out of the address, ignoring what it does not know. */
export function filterFromQuery(query: LocationQuery, currency: string): IdeaFilter {
  const filter = emptyFilter(currency)
  const [text] = list(query.q)
  filter.query = text ?? ''
  filter.countries = list(query.country).map((code) => code.toUpperCase()).filter((code) => COUNTRY_CODES.includes(code))
  filter.months = list(query.month).map(Number).filter((month) => Number.isInteger(month) && month >= 1 && month <= 12)
  const [least, most] = (list(query.days)[0] ?? '').split('-').map(Number)
  if (least !== undefined && most !== undefined && Number.isInteger(least) && Number.isInteger(most)
    && least >= 1 && least <= most && most <= MAX_IDEA_DAYS) {
    filter.days = [least, most]
  }
  filter.maxCost = number(query.cost)
  const [cur] = list(query.cur)
  if (cur && /^[A-Za-z]{3}$/.test(cur)) {
    filter.currency = cur.toUpperCase()
  }
  filter.tags = list(query.tag)
  filter.owners = list(query.owner)
  filter.visas = list(query.visa).filter((visa): visa is VisaRequirement => VISA_REQUIREMENTS.includes(visa as VisaRequirement))
  filter.modes = list(query.mode).filter((mode): mode is TravelMode => TRAVEL_MODES.includes(mode as TravelMode))
  const [sort] = list(query.sort)
  if (sort === 'title' || sort === 'cost' || sort === 'days') {
    filter.sort = sort
    filter.desc = false
  }
  // The order a column starts in is the one people expect of it; the address
  // keeps only that it was turned round.
  if (list(query.rev)[0] === '1') {
    filter.desc = !filter.desc
  }
  return filter
}

/** filterToQuery writes a filter into the address, leaving out what narrows nothing. */
export function filterToQuery(filter: IdeaFilter, currency: string): LocationQueryRaw {
  const query: LocationQueryRaw = {}
  const join = (items: (string | number)[]): string | undefined => (items.length > 0 ? items.join(',') : undefined)
  query.q = filter.query.trim() || undefined
  query.country = join(filter.countries)
  query.month = join(filter.months)
  query.days = filter.days === null ? undefined : `${filter.days[0]}-${filter.days[1]}`
  query.cost = filter.maxCost === null ? undefined : String(filter.maxCost)
  query.cur = filter.currency !== currency ? filter.currency : undefined
  query.tag = join(filter.tags)
  query.owner = join(filter.owners)
  query.visa = join(filter.visas)
  query.mode = join(filter.modes)
  query.sort = filter.sort !== 'updated' ? filter.sort : undefined
  query.rev = filter.desc !== (filter.sort === 'updated') ? '1' : undefined
  return Object.fromEntries(Object.entries(query).filter(([, value]) => value !== undefined)) as LocationQueryRaw
}

/**
 * monthRanges groups months into runs for reading: 6, 7, 8 and 12, 1 become
 * June-August and December-January, running on over the new year.
 *
 * Returns:
 *   - the runs, each its first and last month; one run of 1 to 12 for all twelve.
 */
export function monthRanges(months: number[]): [number, number][] {
  const set = new Set(months)
  if (set.size === 12) {
    return [[1, 12]]
  }
  const runs: [number, number][] = []
  for (let month = 1; month <= 12; month++) {
    if (set.has(month) && !set.has(month === 1 ? 12 : month - 1)) {
      let end = month
      while (set.has(end === 12 ? 1 : end + 1) && (end === 12 ? 1 : end + 1) !== month) {
        end = end === 12 ? 1 : end + 1
      }
      runs.push([month, end])
    }
  }
  return runs
}

/**
 * IDEA_TRAVEL_MODES are the ways of getting to a place an idea is offered:
 * the ways of travelling far, a cable car or a tram being a part of the trip
 * rather than the way there.
 */
export const IDEA_TRAVEL_MODES: readonly TravelMode[] = ['flight', 'train', 'bus', 'car', 'ferry', 'bike', 'walk', 'other']

/**
 * formatDays reads an idea's days: "5–9 days", "7 days", with the ideal after
 * the range when it says more, such as "5–9 days, ideally 7".
 *
 * Returns:
 *   - the reading, or "" when nothing is said.
 */
export function formatDays(idea: Idea, t: (key: string, named?: Record<string, unknown>, plural?: number) => string): string {
  const { days_min: least, days_max: most, days_ideal: ideal } = idea
  let range = ''
  if (least !== null && most !== null && least !== most) {
    range = t('ideas.daysRange', { from: least, to: most })
  } else if ((least ?? most) !== null) {
    const one = (least ?? most) as number
    range = t('ideas.days', { n: one }, one)
  }
  if (ideal === null || (least === most && least === ideal)) {
    return range || (ideal === null ? '' : t('ideas.days', { n: ideal }, ideal))
  }
  return range ? t('ideas.daysIdeally', { range, n: ideal }) : t('ideas.days', { n: ideal }, ideal)
}

/** formatRange reads an amount that may run from one figure to another, once when they agree. */
export function formatRange(least: string | null, most: string | null, format: (amount: string) => string): string {
  if (least === null) {
    return ''
  }
  return most === null || least === most ? format(least) : `${format(least)} – ${format(most)}`
}

/**
 * formatMonths reads the best months as runs of short month names, such as
 * "Jun–Aug, Dec–Jan".
 *
 * Returns:
 *   - the runs, or "" for all twelve and for none, which the page words itself.
 */
export function formatMonths(months: number[], locale: string): string {
  if (months.length === 0 || months.length === 12) {
    return ''
  }
  const format = new Intl.DateTimeFormat(locale, { month: 'short', timeZone: 'UTC' })
  const name = (month: number): string => format.format(new Date(Date.UTC(2026, month - 1, 1)))
  return monthRanges(months)
    .map(([first, last]) => (first === last ? name(first) : `${name(first)}–${name(last)}`))
    .join(', ')
}

/**
 * seasonalIdeas picks the ideas whose time is now or next: the ones best in
 * this month first, then the ones whose best month comes soonest, counted on
 * past the new year; the last changed first among equals. An idea good all
 * year is good now; one that names no months has no season and is left out.
 *
 * Arguments:
 *   - ideas: the reader's ideas.
 *   - month: the current month, 1 to 12.
 *   - limit: how many to pick.
 *
 * Returns:
 *   - at most limit ideas, the most timely first.
 */
export function seasonalIdeas(ideas: Idea[], month: number, limit: number): Idea[] {
  const wait = (idea: Idea): number => Math.min(...idea.months.map((best) => (best - month + 12) % 12))
  return ideas
    .filter((idea) => idea.months.length > 0)
    .map((idea) => ({ idea, wait: wait(idea) }))
    .sort((a, b) => a.wait - b.wait || b.idea.updated_at.localeCompare(a.idea.updated_at))
    .slice(0, limit)
    .map(({ idea }) => idea)
}
