import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/client'
import type { CostCategory, Idea, Media, PackingItem, PackingList, TripDocument } from '@/api/types'
import { amountCents, centsToAmount, equalShares, evaluateFormula, invalidAmount, resolveAmount } from '@/utils/amount'
import { categoryRows } from '@/utils/budget'
import en from '@/i18n/en.json'
import ru from '@/i18n/ru.json'
import { errorMessage } from '@/utils/errors'
import { PACKING_TEMPLATES, packingText, parseQuickItem, templateAdded, templateSections } from '@/utils/packing'
import { addDays, describeUserAgent, formatClock, formatDayDate, formatDistance, formatDateRange, formatElapsed, formatFileSize, formatMoney, formatSpeed, formatTimeOfDay, fromMetres, normalizeAmount, parseTimeOfDay, splitDuration, toMetres } from '@/utils/format'
import { markdownExcerpt, renderMarkdown } from '@/utils/markdown'
import { justifyRows, justifyStrip, previewSize, tileRatio } from '@/utils/justify'
import { minutesBetween, minutesOfDay, planTimeLabel, stopLabel, timeOfDay } from '@/utils/plan'
import { distanceBetween, mediaHint, takenDate } from '@/utils/mediaHints'
import { mediaLinkUpdates } from '@/utils/mediaLinks'
import { decodePolyline, nearestOnLine } from '@/utils/polyline'
import { generatePassword } from '@/utils/password'
import { exifSegment, withExif } from '@/utils/picture'
import { onlyVariant, paletteStyle, resolveTheme, resolveVariant, themeFile, themeFilename } from '@/utils/theme'
import { countryFlag, emptyFilter, filterFromQuery, filterIdeas, filterToQuery, formatDays, formatRange, monthRanges, otherCurrencies, seasonalIdeas } from '@/utils/ideas'
import { DEFAULT_SPEED, MAX_SPEED, MIN_SPEED, SLIDER_STEPS, SPEED_STOPS, positionOf, speedAt, stopPosition, stopSeconds, toblerFactor, walkingSeconds } from '@/utils/trackTime'
import {
  readingLanguage, translatableTexts, translateDocument, translateTrip, translationProgress,
} from '@/utils/translate'

describe('resolveTheme', () => {
  it('follows the system only when asked to', () => {
    expect(resolveTheme('auto', true)).toBe('dark')
    expect(resolveTheme('auto', false)).toBe('light')
    expect(resolveTheme('light', true)).toBe('light')
  })
})

describe('resolveVariant', () => {
  const palette = { primary: '#123456' }

  it('shows a theme with one palette in it, whatever the preference', () => {
    expect(onlyVariant({ light: null, dark: palette })).toBe('dark')
    expect(resolveVariant('light', false, { light: null, dark: palette })).toBe('dark')
    expect(resolveVariant('dark', true, { light: palette, dark: null })).toBe('light')
  })

  it('leaves a theme with both palettes, and no theme, to the preference', () => {
    expect(onlyVariant({ light: palette, dark: palette })).toBeNull()
    expect(resolveVariant('auto', true, { light: palette, dark: palette })).toBe('dark')
    expect(resolveVariant('light', true, null)).toBe('light')
  })
})

describe('theme files', () => {
  it('turns a palette into the variables of a style', () => {
    expect(paletteStyle({ primary: '#123456', unknown: '#000' })).toEqual({ '--color-primary': '#123456' })
    expect(paletteStyle(null)).toEqual({})
  })

  it('writes a theme back as its file, without the palette it lacks', () => {
    const theme = { id: 'x', name: 'Night', light: null, dark: { primary: '#000' }, created_at: '', updated_at: '' }
    expect(themeFile(theme)).toEqual({ format: 1, name: 'Night', dark: { primary: '#000' } })
  })

  it('names the file after the theme', () => {
    expect(themeFilename('Deep Forest!')).toBe('deep-forest.theme.json')
    expect(themeFilename('Тёмный лес')).toBe('тёмный-лес.theme.json')
    expect(themeFilename('!!!')).toBe('theme.theme.json')
  })
})

describe('errorMessage', () => {
  const known = new Set(['errors.invalid_credentials', 'errors.validation.too_short', 'errors.unknown_error'])
  const t = (key: string, named?: Record<string, unknown>) => (named ? `${key} ${JSON.stringify(named)}` : key)
  const te = (key: string) => known.has(key) || key === 'errors.too_many_attempts'

  it('translates a known code', () => {
    expect(errorMessage(new ApiError(401, 'invalid_credentials', ''), t, te)).toBe('errors.invalid_credentials')
  })

  it('describes a validation failure by its reason', () => {
    const error = new ApiError(422, 'validation_failed', '', { field: 'password', reason: 'too_short' })
    expect(errorMessage(error, t, te)).toBe('errors.validation.too_short')
  })

  it('rounds the wait up to whole minutes', () => {
    const error = new ApiError(429, 'too_many_attempts', '', { retry_after: 61 })
    expect(errorMessage(error, t, te)).toBe('errors.too_many_attempts {"minutes":2}')
  })

  it('never shows a raw key for an unknown code', () => {
    expect(errorMessage(new ApiError(500, 'brand_new_code', ''), t, te)).toBe('errors.unknown_error')
    expect(errorMessage(new Error('boom'), t, te)).toBe('errors.unknown_error')
  })
})

describe('generatePassword', () => {
  it('makes passwords of the requested length from the unambiguous alphabet', () => {
    const password = generatePassword(20)
    expect(password).toHaveLength(20)
    expect(password).toMatch(/^[a-km-zA-HJ-NP-Z2-9]+$/)
    expect(generatePassword()).not.toBe(generatePassword())
  })
})

describe('describeUserAgent', () => {
  it('names the browser and the system', () => {
    const firefox = 'Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0'
    expect(describeUserAgent(firefox)).toBe('Firefox · Linux')
    expect(describeUserAgent('curl/8.0')).toBe('curl/8.0')
  })
})

describe('trip formatting', () => {
  it('shows a period without shifting calendar dates', () => {
    // Intl puts thin spaces around the dash.
    expect(formatDateRange('2026-06-20', '2026-06-27', 'en')).toMatch(/^Jun 20\s–\s27, 2026$/)
    expect(formatDateRange(null, null, 'en')).toBe('')
  })

  it('formats amounts in the trip currency', () => {
    expect(formatMoney('1240.50', 'EUR', 'en')).toBe('€1,240.50')
    expect(formatMoney(null, 'EUR', 'en')).toBe('')
  })

  it('accepts a decimal comma and spaces in typed amounts', () => {
    expect(normalizeAmount('1 240,5')).toBe('1240.5')
    expect(normalizeAmount('  ')).toBeNull()
  })

  it('computes an amount typed as a formula', () => {
    expect(normalizeAmount('=120*3+45')).toBe('405')
    expect(normalizeAmount('= (10 + 2,5) * 2 / 3')).toBe('8.33')
  })
})

describe('times of day', () => {
  it('reads a time typed in either clock', () => {
    expect(parseTimeOfDay('14:30')).toBe('14:30')
    expect(parseTimeOfDay('9:05')).toBe('09:05')
    expect(parseTimeOfDay('1430')).toBe('14:30')
    expect(parseTimeOfDay('9.05')).toBe('09:05')
    expect(parseTimeOfDay('7')).toBe('07:00')
    expect(parseTimeOfDay('2:30 pm')).toBe('14:30')
    expect(parseTimeOfDay('2PM')).toBe('14:00')
    expect(parseTimeOfDay('12:15 AM')).toBe('00:15')
    expect(parseTimeOfDay('12 p.m.')).toBe('12:00')
  })

  it('refuses what is not a time of day', () => {
    for (const bad of ['', '24:00', '9:60', '13 pm', '0 am', 'noon', '9:5']) {
      expect(parseTimeOfDay(bad), bad).toBeNull()
    }
  })

  it('shows a stored time in the reader\'s clock', () => {
    expect(formatTimeOfDay('14:30', 'h24')).toBe('14:30')
    expect(formatTimeOfDay('14:30', 'h12')).toBe('2:30 PM')
    expect(formatTimeOfDay('00:05', 'h12')).toBe('12:05 AM')
    expect(formatTimeOfDay(null, 'h12')).toBe('')
  })
})

describe('shared costs', () => {
  it('reads amounts as hundredths, formulas included', () => {
    expect(amountCents('12,5')).toBe(1250)
    expect(amountCents('=10*3')).toBe(3000)
    expect(amountCents('')).toBeNull()
    expect(amountCents('abc')).toBeNull()
    expect(centsToAmount(1205)).toBe('12.05')
    expect(centsToAmount(-50)).toBe('-0.50')
  })

  it('splits equally with the leftover cents on the first', () => {
    expect(equalShares(1000, 3)).toEqual([334, 333, 333])
    expect(equalShares(900, 3)).toEqual([300, 300, 300])
    expect(equalShares(100, 0)).toEqual([])
  })
})

describe('budget by category', () => {
  const row = (category: CostCategory, planned: string, actual = '0.00') => ({ category, planned, actual })

  it('counts tolls in transport and shows them beneath it', () => {
    const rows = categoryRows([
      row('accommodation', '300.00'), row('transport', '100.00', '90.00'), row('fuel', '0.00'), row('tolls', '25.50', '20.00'),
    ])
    expect(rows.map((item) => [item.category, item.planned, item.actual, item.part])).toEqual([
      ['accommodation', '300.00', '0.00', false],
      ['transport', '125.50', '110.00', false],
      ['tolls', '25.50', '20.00', true],
    ])
  })

  it('shows transport for tolls alone and leaves out what carries nothing', () => {
    const rows = categoryRows([row('transport', '0.00'), row('tolls', '12.00'), row('food', '0.00')])
    expect(rows.map((item) => [item.category, item.planned, item.part])).toEqual([
      ['transport', '12.00', false],
      ['tolls', '12.00', true],
    ])
    expect(categoryRows([row('transport', '5.00'), row('tolls', '0.00')]).map((item) => item.category)).toEqual(['transport'])
  })
})

describe('amount formulas', () => {
  it('follows operator precedence and parentheses', () => {
    expect(evaluateFormula('=2+3*4')).toBe(14)
    expect(evaluateFormula('=(2+3)*4')).toBe(20)
    expect(evaluateFormula('=10-4-3')).toBe(3)
    expect(evaluateFormula('=100/4/5')).toBe(5)
    expect(evaluateFormula('=-(3-5)*.5')).toBe(1)
  })

  it('refuses what is not an expression', () => {
    for (const bad of ['=', '=1+', '=(1+2', '=1+2)', '=2*x', '=1/0', '=1..2', '=1 2e3']) {
      expect(evaluateFormula(bad), bad).toBeNull()
    }
  })

  it('leaves plain amounts alone and flags formulas that give no amount', () => {
    expect(resolveAmount('12,50')).toBe('12,50')
    expect(invalidAmount('12,50')).toBe(false)
    expect(invalidAmount('=5-10')).toBe(true)
    expect(invalidAmount('=5*')).toBe(true)
    expect(invalidAmount('=5*2')).toBe(false)
  })

  it('takes a formula without "=" and with the signs of the operator keys', () => {
    expect(resolveAmount('120*3+45')).toBe('405')
    expect(resolveAmount('10 × 3 ÷ 4 − 1,5')).toBe('6')
    expect(resolveAmount('(2+3)×4')).toBe('20')
    expect(resolveAmount('=7-2')).toBe('5')
    expect(invalidAmount('5×')).toBe(true)
    expect(invalidAmount('1 234,50')).toBe(false)
    expect(resolveAmount('-5')).toBe('-5')
  })
})

describe('display preferences', () => {
  it('writes a date in the order the reader asked for', () => {
    expect(formatDayDate('2026-06-22', 'en', false, 'dmy')).toBe('22 Jun')
    expect(formatDayDate('2026-06-22', 'en', false, 'mdy')).toBe('Jun 22')
    expect(formatDayDate('2026-06-22', 'en', true, 'mdy')).toBe('Mon, Jun 22')
    expect(formatDayDate(null, 'en', false, 'dmy')).toBe('')
  })

  it('reads a time on the clock the reader asked for', () => {
    expect(formatClock(14 * 60, 'h24').time).toBe('14:00')
    expect(formatClock(14 * 60, 'h12').time).toBe('2:00 PM')
    // Midnight and noon are twelve on a twelve-hour clock, not zero.
    expect(formatClock(0, 'h12').time).toBe('12:00 AM')
    expect(formatClock(12 * 60 + 5, 'h12').time).toBe('12:05 PM')
    // A schedule that runs past midnight still says how many days it passed.
    expect(formatClock(25 * 60, 'h12')).toEqual({ time: '1:00 AM', dayOffset: 1 })
  })
})

describe('planned times', () => {
  const t = (key: string, named?: Record<string, unknown>) => `${key}:${JSON.stringify(named)}`

  it('reads and writes times of day', () => {
    expect(minutesOfDay('09:30')).toBe(570)
    expect(minutesOfDay('')).toBeNull()
    expect(timeOfDay(570)).toBe('09:30')
    expect(timeOfDay(1500)).toBe('01:00')
  })

  it('measures a visit, past midnight when the end comes first', () => {
    expect(minutesBetween('10:00', '11:30')).toBe(90)
    expect(minutesBetween('23:00', '01:00')).toBe(120)
    expect(minutesBetween('10:00', '10:00')).toBe(1440)
    expect(minutesBetween('', '10:00')).toBeNull()
  })

  it('labels a place by what is known of its time', () => {
    expect(planTimeLabel('10:00', 90, t)).toBe('10:00 – 11:30')
    expect(planTimeLabel('10:00', 0, t)).toBe('10:00')
    expect(planTimeLabel(null, 90, t)).toBe('duration.hoursMinutes:{"hours":1,"minutes":30}')
    expect(planTimeLabel(null, 0, t)).toBe('')
  })
})

describe('schedule formatting', () => {
  it('shows minutes as a time of day and counts days past midnight', () => {
    expect(formatClock(645)).toEqual({ time: '10:45', dayOffset: 0 })
    expect(formatClock(1500)).toEqual({ time: '01:00', dayOffset: 1 })
    expect(splitDuration(167)).toEqual({ hours: 2, minutes: 47 })
  })

  it('shows distances in metres or kilometres', () => {
    expect(formatDistance(800, 'en')).toBe('800 m')
    expect(formatDistance(3450, 'en')).toBe('3.5 km')
    expect(formatDistance(186020, 'en')).toBe('186 km')
    // The length of a recording keeps its tenth, as a watch shows it.
    expect(formatDistance(21397, 'en', 'km', true)).toBe('21.4 km')
    expect(formatDistance(21000, 'en', 'km', true)).toBe('21.0 km')
    expect(formatElapsed(4 * 3600 + 57 * 60 + 51)).toBe('4:57:51')
    expect(formatElapsed(59)).toBe('0:00:59')
  })

  it('shows the size of a file in the unit that reads shortest', () => {
    expect(formatFileSize(512, 'en')).toBe('512 byte')
    expect(formatFileSize(1536, 'en')).toBe('1.5 kB')
    expect(formatFileSize(245 * 1024, 'en')).toBe('245 kB')
    expect(formatFileSize(3.25 * 1024 * 1024, 'en')).toBe('3.3 MB')
  })

  it('shows a distance in miles for a reader who counts in them', () => {
    // Below a tenth of a mile the small unit reads better, exactly as metres do
    // below a kilometre.
    expect(formatDistance(100, 'en', 'mi')).toBe('328 ft')
    expect(formatDistance(3450, 'en', 'mi')).toBe('2.1 mi')
    expect(formatDistance(186020, 'en', 'mi')).toBe('116 mi')
    // The default is unchanged, so a caller that knows nothing of units is safe.
    expect(formatDistance(3450, 'en')).toBe(formatDistance(3450, 'en', 'km'))
  })

  it('reads and writes a typed distance in the reader units', () => {
    expect(toMetres(2.5, 'km')).toBe(2500)
    expect(toMetres(2.5, 'mi')).toBe(4023)
    expect(fromMetres(2500, 'km')).toBe(2.5)
    expect(fromMetres(4023, 'mi')).toBe(2.5)
    // What somebody typed comes back as what they typed, in either unit.
    for (const units of ['km', 'mi'] as const) {
      expect(fromMetres(toMetres(12.3, units), units)).toBe(12.3)
    }
  })

  it('moves calendar dates across months', () => {
    expect(addDays('2026-06-30', 1)).toBe('2026-07-01')
  })
})

describe('markdownExcerpt', () => {
  it('keeps the first sentences as plain text', () => {
    expect(markdownExcerpt('## Trail\n\nA **steep** climb. Views all the way! Then the descent.')).toBe('Trail A steep climb. Views all the way!')
  })

  it('cuts a long sentence at a word', () => {
    const excerpt = markdownExcerpt('word '.repeat(100), 2, 40)
    expect(excerpt.endsWith('…')).toBe(true)
    expect(excerpt.length).toBeLessThanOrEqual(41)
  })

  it('reads nothing into an empty text', () => {
    expect(markdownExcerpt('  ')).toBe('')
  })
})

describe('renderMarkdown', () => {
  it('renders formatting and strips scripts', () => {
    const html = renderMarkdown('**bold** <img src=x onerror=alert(1)> [link](https://example.com)')
    expect(html).toContain('<strong>bold</strong>')
    expect(html).not.toContain('onerror')
    expect(html).toContain('rel="noopener noreferrer nofollow"')
  })

  it('refuses script links', () => {
    expect(renderMarkdown('[x](javascript:alert(1))')).not.toContain('javascript:')
  })
})

describe('justifyRows', () => {
  it('fills every row but the last to the exact width', () => {
    const ratios = [1.5, 0.75, 1.5, 1, 1.5, 1.5, 0.75, 1.5, 1]
    const rows = justifyRows(ratios, 1000, 200, 4)
    expect(rows.flatMap((row) => row.tiles.map((tile) => tile.index))).toEqual(ratios.map((_, index) => index))
    for (const row of rows.slice(0, -1)) {
      const used = row.tiles.reduce((sum, tile) => sum + tile.width, 0) + 4 * (row.tiles.length - 1)
      expect(used).toBe(1000)
      expect(row.tiles.every((tile) => tile.height === row.height)).toBe(true)
      expect(Math.abs(row.height - 200)).toBeLessThan(100)
    }
  })

  it('leaves the last row at the target height', () => {
    const rows = justifyRows([1, 1], 1000, 200, 4)
    expect(rows).toHaveLength(1)
    expect(rows[0]).toEqual({ height: 200, tiles: [{ index: 0, width: 200, height: 200 }, { index: 1, width: 200, height: 200 }] })
  })

  it('keeps the last row no taller than the row above it', () => {
    const rows = justifyRows([1.5, 1.5, 1.5, 1.5, 1], 1000, 240, 4)
    expect(rows).toHaveLength(2)
    expect(rows[1]!.height).toBe(rows[0]!.height)
    expect(rows[0]!.height).toBeLessThan(240)
  })

  it('gives a picture too wide for the page a row of its own', () => {
    const rows = justifyRows([3, 1], 300, 200, 4)
    expect(rows[0]!.tiles).toEqual([{ index: 0, width: 300, height: 100 }])
  })

  it('lays out nothing without a width', () => {
    expect(justifyRows([1, 1], 0, 200, 4)).toEqual([])
  })
})

describe('justifyStrip', () => {
  it('lays out every picture at the target height when they all fit', () => {
    const strip = justifyStrip([1, 1], 1000, 200, 4, Infinity, 1)
    expect(strip?.shown).toBe(2)
    expect(strip?.row.tiles).toEqual([{ index: 0, width: 200, height: 200 }, { index: 1, width: 200, height: 200 }])
  })

  it('fills the width with the pictures that fit and a tile for the rest', () => {
    const strip = justifyStrip([1.5, 1.5, 1.5, 1.5, 1.5, 1.5], 1000, 200, 4, Infinity, 1)!
    expect(strip.shown).toBeGreaterThan(0)
    expect(strip.shown).toBeLessThan(6)
    expect(strip.row.tiles).toHaveLength(strip.shown + 1)
    expect(strip.row.tiles.at(-1)!.index).toBe(strip.shown)
    const used = strip.row.tiles.reduce((sum, tile) => sum + tile.width, 0) + 4 * strip.shown
    expect(used).toBe(1000)
  })

  it('keeps to the limit without stretching the row', () => {
    const strip = justifyStrip([1, 1, 1, 1], 1000, 200, 4, 2, 1)!
    expect(strip.shown).toBe(2)
    expect(strip.row.tiles).toHaveLength(3)
    expect(strip.row.height).toBe(200)
  })

  it('shows one picture however narrow the page is', () => {
    const strip = justifyStrip([3, 1], 300, 200, 4, Infinity, 1)!
    expect(strip.shown).toBe(1)
    expect(strip.row.tiles).toHaveLength(2)
  })

  it('lays out nothing without a width or pictures', () => {
    expect(justifyStrip([1], 0, 200, 4, Infinity, 1)).toBeNull()
    expect(justifyStrip([], 1000, 200, 4, Infinity, 1)).toBeNull()
  })
})

describe('tileRatio and previewSize', () => {
  it('bounds the proportions and falls back to a square', () => {
    expect(tileRatio(0, 0)).toBe(1)
    expect(tileRatio(4000, 3000)).toBeCloseTo(4 / 3)
    expect(tileRatio(10000, 1000)).toBe(3)
    expect(tileRatio(1000, 10000)).toBe(0.5)
  })

  it('picks the narrowest preview covering the tile on the screen', () => {
    expect(previewSize(200, 1)).toBe(320)
    expect(previewSize(300, 2)).toBe(640)
    expect(previewSize(500, 2)).toBe(1280)
    expect(previewSize(2000, 3)).toBe(1280)
  })
})

describe('decodePolyline', () => {
  it('reads the reference example of the format', () => {
    expect(decodePolyline('_p~iF~ps|U_ulLnnqC_mqNvxq`@')).toEqual([[38.5, -120.2], [40.7, -120.95], [43.252, -126.453]])
    expect(decodePolyline('')).toEqual([])
  })
})

describe('media hints', () => {
  const picture = (id: string, takenAt: string | null, lat: number | null, lng: number | null) => ({
    id, trip_id: 't', original_name: `${id}.jpg`, mime: 'image/jpeg', size: 1, width: 4, height: 3,
    taken_at: takenAt, lat, lng, is_private: false, is_favorite: false, status: 'ready' as const,
    created_at: '2026-06-21T00:00:00Z',
  })
  const place = (id: string, name: string, lat: number | null, lng: number | null) => ({
    id, day_id: 'd2', position: 0, kind: 'place' as const, anchor: null, stay_id: null, name, activity_type: null,
    category: 'sight' as const, lat, lng, address: '', osm_ref: '', description_md: '', url: '',
    desired_time: null, visit_minutes: 0, is_optional: false, booking_ref: '', planned_cost_amount: null,
    cost_per_person: false, cost_category: 'other' as const,
    cost_note: '', paid_by: null, cost_split: 'none' as const, cost_shares: [], schedule: null, status: 'visited' as const,
    story_md: '', actual_time: null, actual_end_time: null, rating: null, actual_cost_amount: null, difficulty: null, source_item_id: null, track: null, attachments: [],
    media: [], cover_media_id: null,
  })
  const day = (id: string, position: number, date: string, items: ReturnType<typeof place>[]) => ({
    id, position, date, title: '', notes_md: '', highlight: '', start_time: '09:00', default_mode: null, timezone: null,
    morning_anchor: true, evening_anchor: true, no_overnight: false, items, legs: [],
    summary: {
      visit_minutes: 0, travel_minutes: 0, distance_m: 0, by_mode: [], unknown_travel: false,
      pending_legs: 0, estimated_legs: 0, end_minutes: 0, planned_cost: '0', actual_cost: '0',
    },
    media: [], cover_media_id: null,
  })
  // Skógafoss and a waterfall a few hundred metres away, on the second day.
  const skogafoss = place('p1', 'Skógafoss', 63.532, -19.511)
  const document = {
    id: 'doc', trip_id: 't', kind: 'report' as const, source_document_id: null, intro_md: '', summary_md: '',
    days: [day('d1', 0, '2026-06-20', []), day('d2', 1, '2026-06-21', [skogafoss])],
    unassigned: [], stays: [], transfers: [], expenses: [], nights: [],
    stay_summary: { nights: 0, cost: '0', average_per_night: null }, pending_legs: 0, estimated_legs: 0,
    totals: null, translations: {},
    created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z',
  }

  describe('translation', () => {
    const leg = {
      id: 'l1', from_item_id: 'x', to_item_id: 'p1', mode: 'walk' as const, distance_m: null, duration_s: null,
      calculated_distance_m: null, calculated_duration_s: null, manual_distance: false, manual_duration: false,
      geometry: '', source: 'pending' as const, error: null, calculated_at: null, planned_cost_amount: null,
      actual_cost_amount: null, note: 'bus', route_preference: 'fastest' as const, via: [], route_pinned: false, segments: [], tickets: [],
    }
    const report: TripDocument = {
      ...document,
      intro_md: 'intro',
      days: [{ ...day('d1', 0, '2026-06-20', [{ ...skogafoss, story_md: 'wet' }]), title: 'day', legs: [leg] }],
      translations: { de: { doc: { intro_md: 'intro (de)' }, p1: { story_md: 'wet (de)' }, d1: { title: '' } } },
    }

    it('reads a report in the reader\'s language when it has it, and in the original otherwise', () => {
      expect(readingLanguage(['en', 'de'], 'de')).toBe('de')
      expect(readingLanguage(['en', 'de'], 'ru')).toBe('en')
      expect(readingLanguage([], 'ru')).toBe('')
    })

    it('puts the translation over the original, field by field', () => {
      const shown = translateDocument(report, 'de')
      expect(shown.intro_md).toBe('intro (de)')
      expect(shown.days[0]?.items[0]?.story_md).toBe('wet (de)')
      // An empty translation and one nobody wrote both leave the original.
      expect(shown.days[0]?.title).toBe('day')
      expect(shown.days[0]?.items[0]?.name).toBe('Skógafoss')
      expect(shown.days[0]?.legs[0]?.note).toBe('bus')
      // The original is untouched, and a language without translations is it.
      expect(report.intro_md).toBe('intro')
      expect(translateDocument(report, 'en')).toBe(report)
    })

    it('translates the ends of a transfer and lists them for translation', () => {
      const flight = {
        id: 'f1', kind: 'flight' as const, name: 'FI 204', from_name: 'Keflavík', from_address: '', from_lat: null,
        from_lng: null, to_name: 'Copenhagen', to_address: '', to_lat: null, to_lng: null,
        departure_date: '2026-06-20', departure_time: null, arrival_date: null, arrival_time: null, booking_ref: '',
        url: '', notes_md: '', planned_cost_amount: null, cost_per_person: false, actual_cost_amount: null,
        source_transfer_id: null,
      }
      const withFlight: TripDocument = { ...report, transfers: [flight], translations: { de: { f1: { to_name: 'Kopenhagen' } } } }
      expect(translateDocument(withFlight, 'de').transfers[0]).toMatchObject({ from_name: 'Keflavík', to_name: 'Kopenhagen', name: 'FI 204' })
      expect(translatableTexts(withFlight).map((text) => `${text.id}.${text.field}`)).toEqual(
        expect.arrayContaining(['f1.from_name', 'f1.to_name']))
    })

    it('translates the story of the night, and only the evening\'s', () => {
      const mark = (id: string, anchor: 'morning' | 'evening', story: string) => ({
        ...skogafoss, id, kind: 'stay_anchor' as const, anchor, stay_id: 's1', story_md: story,
      })
      const nights: TripDocument = {
        ...report,
        days: [{ ...day('d1', 0, '2026-06-20', []), items: [mark('m1', 'morning', 'dawn'), mark('e1', 'evening', 'aurora')] }],
        translations: { de: { e1: { story_md: 'Polarlicht' } } },
      }
      expect(translateDocument(nights, 'de').days[0]?.items[1]?.story_md).toBe('Polarlicht')
      const listed = translatableTexts(nights).map((text) => `${text.id}.${text.field}`)
      expect(listed).toContain('e1.story_md')
      expect(listed).not.toContain('m1.story_md')
    })

    it('translates the words of the stops along a line', () => {
      const stop = {
        id: 's1', kind: 'food' as const, name: 'Café', note_md: 'Soup', lat: 63.5, lng: -19.5, distance_m: 1200,
        grades_to: null, actual_time: null, planned_cost_amount: null, actual_cost_amount: null, cost_per_person: false,
        cost_category: 'food' as const, cost_note: '', paid_by: null, cost_split: 'none' as const, cost_shares: [],
      }
      const track = {
        id: 'tr', original_name: 'hike.gpx', format: 'gpx' as const, distance_m: 7000, point_count: 2, ascent_m: null,
        descent_m: null, geometry: '', grades: null, speed_kmh: null, started_at: null, ended_at: null, stops: [stop],
      }
      const withStop: TripDocument = {
        ...report,
        days: [{ ...day('d1', 0, '2026-06-20', []), items: [{ ...skogafoss, kind: 'activity' as const, track }] }],
        translations: { de: { s1: { name: 'Kneipe' } } },
      }
      const shown = translateDocument(withStop, 'de').days[0]?.items[0]?.track?.stops[0]
      expect(shown).toMatchObject({ name: 'Kneipe', note_md: 'Soup' })
      expect(translatableTexts(withStop).map((text) => `${text.id}.${text.field}`)).toEqual(
        expect.arrayContaining(['s1.name', 's1.note_md']))
    })

    it('counts what is left to translate', () => {
      // intro, day title, place name, story and leg note hold words.
      expect(translationProgress(report, 'de')).toEqual({ done: 2, total: 5 })
      expect(translatableTexts(report).map((text) => `${text.id}.${text.field}`)).toContain('l1.note')
    })

    it('translates a trip\'s own title and summary', () => {
      const trip = { title: 'Iceland', summary: 'short', translations: { de: { title: 'Island' } } }
      expect(translateTrip(trip, 'de')).toMatchObject({ title: 'Island', summary: 'short' })
      expect(translateTrip(trip, 'en').title).toBe('Iceland')
    })
  })

  it('reads the date a picture was taken in the trip\'s own zone', () => {
    // Just before midnight UTC is still the same day in Reykjavík, and already
    // the next one in Tokyo.
    expect(takenDate('2026-06-21T23:30:00Z', 'Atlantic/Reykjavik')).toBe('2026-06-21')
    expect(takenDate('2026-06-21T23:30:00Z', 'Asia/Tokyo')).toBe('2026-06-22')
    expect(takenDate(null, 'UTC')).toBeNull()
  })

  it('measures the distance between two coordinates', () => {
    expect(Math.round(distanceBetween(63.532, -19.511, 63.532, -19.511))).toBe(0)
    // A tenth of a degree of latitude is about eleven kilometres.
    expect(Math.round(distanceBetween(63.5, -19.5, 63.6, -19.5) / 100)).toBe(111)
  })

  it('offers the day a picture was taken on and the place it was taken next to', () => {
    const hint = mediaHint(picture('m1', '2026-06-21T14:00:00Z', 63.5325, -19.5112), document,
      'Atlantic/Reykjavik', 'd1', null)
    expect(hint?.day?.id).toBe('d2')
    expect(hint?.item?.name).toBe('Skógafoss')
    expect(hint?.distanceM).toBeLessThan(300)
  })

  it('says nothing when the picture already hangs where it belongs', () => {
    expect(mediaHint(picture('m2', '2026-06-21T14:00:00Z', 63.5325, -19.5112), document,
      'Atlantic/Reykjavik', 'd2', 'p1')).toBeNull()
    // A picture without metadata, and one taken far from everything, are quiet too.
    expect(mediaHint(picture('m3', null, null, null), document, 'Atlantic/Reykjavik', 'd2', null)).toBeNull()
    expect(mediaHint(picture('m4', '2026-06-21T14:00:00Z', 64.9, -18.1), document,
      'Atlantic/Reykjavik', 'd2', null)).toBeNull()
  })
})

describe('media links', () => {
  // Only the galleries are read here, so the documents carry their days and
  // nothing else.
  const picture = (id: string) => ({ id }) as Media
  const documents = [{
    days: [
      { id: 'd1', media: [picture('m1')], items: [{ id: 'p1', media: [picture('m2')] }] },
      { id: 'd2', media: [], items: [] },
    ],
  }, {
    days: [{ id: 'd3', media: [], items: [] }],
  }] as unknown as TripDocument[]

  it('adds pictures to the end of a gallery and keeps what it holds', () => {
    expect(mediaLinkUpdates(documents, [picture('m1'), picture('m2')], [{ target: 'day', targetId: 'd1', attach: true }]))
      .toEqual([{ target: 'day', targetId: 'd1', mediaIds: ['m1', 'm2'] }])
  })

  it('finds a place inside the day it belongs to', () => {
    expect(mediaLinkUpdates(documents, [picture('m3')], [{ target: 'item', targetId: 'p1', attach: true }]))
      .toEqual([{ target: 'item', targetId: 'p1', mediaIds: ['m2', 'm3'] }])
  })

  it('takes pictures out of a gallery', () => {
    expect(mediaLinkUpdates(documents, [picture('m1')], [{ target: 'day', targetId: 'd1', attach: false }]))
      .toEqual([{ target: 'day', targetId: 'd1', mediaIds: [] }])
  })

  it('sends nothing for a gallery that already says what was asked', () => {
    expect(mediaLinkUpdates(documents, [picture('m1')], [{ target: 'day', targetId: 'd1', attach: true }])).toEqual([])
    expect(mediaLinkUpdates(documents, [picture('m1')], [{ target: 'day', targetId: 'd2', attach: false }])).toEqual([])
  })

  it('finds a day of the other document as well', () => {
    expect(mediaLinkUpdates(documents, [picture('m1')], [{ target: 'day', targetId: 'd3', attach: true }]))
      .toEqual([{ target: 'day', targetId: 'd3', mediaIds: ['m1'] }])
  })

  it('ignores a day or place no document carries any more', () => {
    expect(mediaLinkUpdates(documents, [picture('m1')], [
      { target: 'day', targetId: 'gone', attach: true },
      { target: 'item', targetId: 'gone', attach: true },
    ])).toEqual([])
  })
})

describe('EXIF of a shrunk picture', () => {
  // A JPEG start marker, an APP1 EXIF segment whose first directory holds an
  // orientation of 6 in little-endian order, and the start of the picture data.
  const tiff = [
    0x49, 0x49, 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00, // "II", 42, first directory at 8
    0x01, 0x00, // one entry
    0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00, // orientation SHORT 6
    0x00, 0x00, 0x00, 0x00, // no next directory
  ]
  const payload = [0x45, 0x78, 0x69, 0x66, 0x00, 0x00, ...tiff]
  const app1 = [0xff, 0xe1, 0x00, payload.length + 2, ...payload]
  const original = new Uint8Array([0xff, 0xd8, ...app1, 0xff, 0xda, 0x00, 0x02])

  it('copies the segment and marks the copy upright', () => {
    const segment = exifSegment(original)
    expect(segment).not.toBeNull()
    expect(segment!.length).toBe(app1.length)
    // The orientation value sits 8 bytes into its entry, after the header.
    const orientationAt = 4 + 6 + 10 + 8
    expect(segment![orientationAt]).toBe(1)
    // The original is left as it was.
    expect(original[2 + orientationAt]).toBe(6)
  })

  it('puts the segment right after the start marker', () => {
    const canvasJpeg = new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x02, 0xff, 0xd9])
    const joined = withExif(canvasJpeg, exifSegment(original)!)
    expect([...joined.subarray(0, 4)]).toEqual([0xff, 0xd8, 0xff, 0xe1])
    expect([...joined.subarray(joined.length - 6)]).toEqual([0xff, 0xe0, 0x00, 0x02, 0xff, 0xd9])
    expect(exifSegment(joined)).not.toBeNull()
  })

  it('finds nothing in a file without EXIF or that is not a JPEG', () => {
    expect(exifSegment(new Uint8Array([0xff, 0xd8, 0xff, 0xda, 0x00, 0x02]))).toBeNull()
    expect(exifSegment(new Uint8Array([0x89, 0x50, 0x4e, 0x47]))).toBeNull()
  })
})

describe('parseQuickItem', () => {
  it('reads a count at the end of the line as the quantity', () => {
    expect(parseQuickItem('Socks x3')).toEqual({ name: 'Socks', quantity: 3 })
    expect(parseQuickItem(' Носки ×2 ')).toEqual({ name: 'Носки', quantity: 2 })
    expect(parseQuickItem('Batteries *12')).toEqual({ name: 'Batteries', quantity: 12 })
  })

  it('keeps a name that only looks like a count', () => {
    expect(parseQuickItem('Tent')).toEqual({ name: 'Tent', quantity: 1 })
    expect(parseQuickItem('x3')).toEqual({ name: 'x3', quantity: 1 })
    expect(parseQuickItem('Box x0')).toEqual({ name: 'Box x0', quantity: 1 })
  })
})

describe('walkingSeconds', () => {
  // grades builds the slopes of a line: metres at each percent from -50.
  function grades(runs: Record<number, number>): number[] {
    const buckets = new Array<number>(101).fill(0)
    for (const [grade, metres] of Object.entries(runs)) {
      buckets[Number(grade) + 50] = metres
    }
    return buckets
  }

  it('walks the flat at the speed chosen', () => {
    expect(walkingSeconds({ distance_m: 4700, grades: null }, 4.7)).toBeCloseTo(3600)
    expect(walkingSeconds({ distance_m: 4700, grades: grades({ 0: 4700 }) }, 4.7)).toBeCloseTo(3600)
  })

  it('climbs slower and descends a gentle slope faster', () => {
    const flat = walkingSeconds({ distance_m: 1000, grades: grades({ 0: 1000 }) }, 4)
    expect(walkingSeconds({ distance_m: 1000, grades: grades({ 20: 1000 }) }, 4)).toBeGreaterThan(flat * 1.8)
    expect(walkingSeconds({ distance_m: 1000, grades: grades({ [-5]: 1000 }) }, 4)).toBeLessThan(flat)
    expect(walkingSeconds({ distance_m: 1000, grades: grades({ [-30]: 1000 }) }, 4)).toBeGreaterThan(flat)
    expect(toblerFactor(0)).toBeCloseTo(1)
  })

  it('walks the metres the slopes leave out as flat', () => {
    const counted = walkingSeconds({ distance_m: 1000, grades: grades({ 0: 1000 }) }, 5)
    expect(walkingSeconds({ distance_m: 2000, grades: grades({ 0: 1000 }) }, 5)).toBeCloseTo(counted * 2)
  })
})

describe('stops along a line', () => {
  it('puts a point on the nearest stretch of a line', () => {
    const line: [number, number][] = [[64, -21], [64.1, -21], [64.1, -20.9]]
    const [lat, lng] = nearestOnLine(line, [64.05, -20.98])
    expect(lat).toBeCloseTo(64.05, 6)
    expect(lng).toBeCloseTo(-21, 6)
    // Beyond the end it is the end.
    expect(nearestOnLine(line, [64.2, -20.5])).toEqual([64.1, -20.9])
    // There and back, the way out.
    const back: [number, number][] = [[64, -21], [64.1, -21], [64, -21]]
    expect(nearestOnLine(back, [64.02, -21])[0]).toBeCloseTo(64.02, 6)
  })

  it('labels a stop by its activity and a letter', () => {
    expect(stopLabel(3, 0)).toBe('3a')
    expect(stopLabel(3, 1)).toBe('3b')
    expect(stopLabel(undefined, 2)).toBe('c')
  })

  it('times the way to a stop by the slopes up to it', () => {
    expect(stopSeconds({ distance_m: 4700, grades_to: null }, 4.7)).toBeCloseTo(3600, 0)
    const climbing = Array.from({ length: 101 }, (_, index) => (index === 60 ? 4700 : 0))
    expect(stopSeconds({ distance_m: 4700, grades_to: climbing }, 4.7)).toBeGreaterThan(3600)
  })
})

describe('speed scale', () => {
  it('puts the marks an equal step apart and the ends at the bounds', () => {
    expect(speedAt(0)).toBe(MIN_SPEED)
    expect(speedAt(SLIDER_STEPS)).toBe(MAX_SPEED)
    SPEED_STOPS.forEach((stop, index) => {
      expect(positionOf(stop)).toBe(stopPosition(index))
      expect(speedAt(stopPosition(index))).toBe(stop)
    })
    expect(positionOf(DEFAULT_SPEED)).toBe(stopPosition(SPEED_STOPS.indexOf(DEFAULT_SPEED)))
  })

  it('draws the thumb onto a mark nearby and leaves it free between them', () => {
    const mark = stopPosition(SPEED_STOPS.indexOf(4.7))
    expect(speedAt(mark + 10)).toBe(4.7)
    expect(speedAt(mark - 10)).toBe(4.7)
    expect(speedAt(mark + 50)).toBe(5.1)
    expect(positionOf(5.1)).toBe(mark + 50)
    expect(speedAt(-5)).toBe(MIN_SPEED)
  })

  it('shows a speed in the reader\'s units', () => {
    expect(formatSpeed(4.7, 'en', 'km')).toBe('4.7 km/h')
    expect(formatSpeed(16.09344, 'en', 'mi')).toBe('10 mph')
    expect(formatSpeed(6.5, 'en', 'km', false)).toBe('6.5')
  })
})

describe('ideas', () => {
  // idea builds an idea with only what a test cares about.
  function idea(fields: Partial<Idea>): Idea {
    return {
      id: fields.title ?? 'x', title: 'x', countries: [], places: [], photos: [], months: [],
      days_min: null, days_max: null, days_ideal: null, description_md: '', currency: 'EUR',
      costs: { stay: null, food: null, other: null }, transports: [], transport_min: null, transport_max: null,
      cost_min: null, cost_max: null, visa: 'not_needed', tags: [], created_at: '', updated_at: '2026-01-01T00:00:00Z',
      ...fields,
    }
  }
  const ideas = [
    idea({ title: 'Westfjords', countries: ['IS'], months: [6, 7, 8], days_min: 5, days_max: 7, cost_min: '1200.00',
      cost_max: '1500.00', transport_min: '400.00', costs: { stay: '800.00', food: null, other: null },
      transports: [{ modes: ['car', 'flight'], cost: '400.00', minutes: null }],
      places: [{ name: 'Ísafjörður', lat: null, lng: null }], tags: [{ id: 't1', name: 'nature', color: 'green' }] }),
    idea({ title: 'Baltic tour', countries: ['EE', 'LV', 'LT'], months: [5, 9], days_ideal: 10, cost_min: '900.00',
      transports: [{ modes: ['train'], cost: null, minutes: 600 }], updated_at: '2026-03-01T00:00:00Z' }),
    idea({ title: 'Tokyo', countries: ['JP'], months: [4], days_min: 12, days_max: 20, currency: 'JPY',
      cost_min: '300000.00', visa: 'on_arrival', description_md: 'Cherry **blossom**' }),
  ]
  const titles = (list: Idea[]): string[] => list.map((item) => item.title)

  it('narrows by every field and leaves out what an idea does not say', () => {
    const base = emptyFilter('EUR')
    expect(titles(filterIdeas(ideas, { ...base, query: 'blossom' }, 'en'))).toEqual(['Tokyo'])
    expect(titles(filterIdeas(ideas, { ...base, query: 'latvia' }, 'en'))).toEqual(['Baltic tour'])
    expect(titles(filterIdeas(ideas, { ...base, query: 'ísafj' }, 'en'))).toEqual(['Westfjords'])
    expect(titles(filterIdeas(ideas, { ...base, countries: ['LT', 'JP'] }, 'en'))).toEqual(['Baltic tour', 'Tokyo'])
    expect(titles(filterIdeas(ideas, { ...base, months: [7, 9] }, 'en'))).toEqual(['Baltic tour', 'Westfjords'])
    expect(titles(filterIdeas(ideas, { ...base, days: [7, 10] }, 'en'))).toEqual(['Baltic tour', 'Westfjords'])
    expect(titles(filterIdeas(ideas, { ...base, days: [21, 30] }, 'en'))).toEqual([])
    expect(titles(filterIdeas(ideas, { ...base, maxCost: 1000 }, 'en'))).toEqual(['Baltic tour'])
    expect(titles(filterIdeas(ideas, { ...base, tags: ['t1'] }, 'en'))).toEqual(['Westfjords'])
    expect(titles(filterIdeas(ideas, { ...base, visas: ['on_arrival'] }, 'en'))).toEqual(['Tokyo'])
    expect(titles(filterIdeas(ideas, { ...base, modes: ['train', 'flight'] }, 'en'))).toEqual(['Baltic tour', 'Westfjords'])
    expect(otherCurrencies(ideas, { ...base, maxCost: 1000 }, 'en')).toBe(1)
  })

  it('sorts either way, the ideas without the figure last', () => {
    const base = emptyFilter('EUR')
    expect(titles(filterIdeas(ideas, { ...base, sort: 'title', desc: false }, 'en'))).toEqual(['Baltic tour', 'Tokyo', 'Westfjords'])
    expect(titles(filterIdeas(ideas, { ...base, sort: 'title', desc: true }, 'en'))).toEqual(['Westfjords', 'Tokyo', 'Baltic tour'])
    expect(titles(filterIdeas(ideas, { ...base, sort: 'days', desc: false }, 'en'))).toEqual(['Westfjords', 'Baltic tour', 'Tokyo'])
    expect(titles(filterIdeas(ideas, { ...base, sort: 'cost', desc: false }, 'en'))).toEqual(['Baltic tour', 'Westfjords', 'Tokyo'])
    expect(titles(filterIdeas(ideas, base, 'en'))[0]).toBe('Baltic tour')
  })

  it('keeps the filter in the address and reads it back', () => {
    const filter = { ...emptyFilter('EUR'), query: 'fjord', countries: ['IS'], months: [6, 7], days: [5, 9] as [number, number],
      maxCost: 900, currency: 'USD', visas: ['needed' as const], modes: ['car' as const],
      sort: 'cost' as const, desc: true }
    const query = filterToQuery(filter, 'EUR')
    expect(filterFromQuery(query as Record<string, string>, 'EUR')).toEqual(filter)
    expect(filterToQuery(emptyFilter('EUR'), 'EUR')).toEqual({})
    expect(filterFromQuery({ month: '13,2', country: 'zz,fr', visa: 'maybe', days: '9-3' }, 'EUR')).toMatchObject({
      months: [2], countries: ['FR'], visas: [], days: null })
  })

  it('picks the ideas whose season is now or next, over the new year', () => {
    const season = [
      idea({ title: 'Winter', months: [1, 2] }), idea({ title: 'Autumn', months: [10] }),
      idea({ title: 'Now', months: [9] }), idea({ title: 'Always', months: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12] }),
      idea({ title: 'Someday', months: [] }), idea({ title: 'Summer', months: [7, 8] }),
    ]
    expect(titles(seasonalIdeas(season, 9, 5))).toEqual(['Now', 'Always', 'Autumn', 'Winter', 'Summer'].slice(0, 5))
    expect(titles(seasonalIdeas(season, 9, 2))).toHaveLength(2)
  })

  it('reads months as runs over the new year, days with the ideal, ranges and flags', () => {
    expect(monthRanges([6, 7, 8])).toEqual([[6, 8]])
    expect(monthRanges([12, 1, 2, 5])).toEqual([[5, 5], [12, 2]])
    expect(monthRanges([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12])).toEqual([[1, 12]])
    expect(countryFlag('is')).toBe('🇮🇸')
    const t = (key: string, named?: Record<string, unknown>): string => `${key}:${JSON.stringify(named)}`
    expect(formatDays(idea({ days_min: 5, days_max: 9, days_ideal: 7 }), t)).toContain('ideas.daysIdeally')
    expect(formatDays(idea({ days_min: 7, days_max: 7, days_ideal: 7 }), t)).toBe('ideas.days:{"n":7}')
    expect(formatRange('10', '20', (amount) => `€${amount}`)).toBe('€10 – €20')
    expect(formatRange('10', '10', (amount) => `€${amount}`)).toBe('€10')
  })
})

describe('packingText', () => {
  it('writes a list for a chat: a heading, categories and plain lines', () => {
    const item = (name: string, fields: Partial<PackingItem> = {}): PackingItem => ({
      id: name, category_id: null, name, quantity: 1, note: '', packed: false, bringer_id: null, position: 0, ...fields,
    })
    const text = packingText([
      { title: 'Documents', items: [item('Passports', { quantity: 3, packed: true }), item('Tickets', { note: 'printed', bringer_id: 'u1' })] },
      { title: 'Empty', items: [] },
      { title: 'Clothes', items: [item('Shoes')] },
    ], { u1: 'Ada' }, 'Lisbon — Packing list')
    expect(text).toBe('Lisbon — Packing list\n\nDocuments\n- Passports ×3\n- Tickets — printed (Ada)\n\nClothes\n- Shoes')
  })
})

describe('packing templates', () => {
  const hiking = PACKING_TEMPLATES.find((template) => template.key === 'hiking')!
  const sections = templateSections(hiking, (key) => key.split('.').pop()!)

  it('writes a template in the reader\'s words, a pair of categories in one colour', () => {
    expect(sections.map((section) => [section.name, section.icon, section.color])).toEqual([
      ['hikingGear', 'gear', 'green'],
      ['hikingClothes', 'clothes', 'green'],
    ])
    expect(sections[1]?.items).toContainEqual({ name: 'hikingSocks', quantity: 3, note: 'hikingSocks' })
    expect(sections[0]?.items).toContainEqual({ name: 'backpack' })
  })

  it('says a template is added once the list holds all of it, in any case', () => {
    const list: PackingList = { categories: [], items: [], packed: 0, total: 0 }
    expect(templateAdded(sections, list, 'en')).toBe(false)
    sections.forEach((section, index) => {
      list.categories.push({ id: `c${index}`, name: section.name.toUpperCase(), color: 'green', icon: 'gear', position: index })
      for (const item of section.items) {
        list.items.push({ id: item.name, category_id: `c${index}`, name: ` ${item.name} `, quantity: 1, note: '', packed: false, bringer_id: null, position: 0 })
      }
    })
    expect(templateAdded(sections, list, 'en')).toBe(true)
    list.items.pop()
    expect(templateAdded(sections, list, 'en')).toBe(false)
  })

  it('has every name in the dictionaries', () => {
    for (const locale of [en, ru]) {
      for (const template of PACKING_TEMPLATES) {
        expect(locale.packing.templates).toHaveProperty(template.key)
        for (const category of template.categories) {
          expect(locale.packing.templateCategories).toHaveProperty(category.key)
          for (const item of category.items) {
            expect(locale.packing.templateItems).toHaveProperty(item.key)
            if (item.note) {
              expect(locale.packing.templateNotes).toHaveProperty(item.key)
            }
          }
        }
      }
    }
  })
})
