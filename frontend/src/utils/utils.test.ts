import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/client'
import type { Media, TripDocument } from '@/api/types'
import { amountCents, centsToAmount, equalShares, evaluateFormula, invalidAmount, resolveAmount } from '@/utils/amount'
import { errorMessage } from '@/utils/errors'
import { addDays, describeUserAgent, formatClock, formatDayDate, formatDistance, formatDateRange, formatMoney, formatTimeOfDay, fromMetres, normalizeAmount, parseTimeOfDay, splitDuration, toMetres } from '@/utils/format'
import { markdownExcerpt, renderMarkdown } from '@/utils/markdown'
import { justifyRows, justifyStrip, previewSize, tileRatio } from '@/utils/justify'
import { minutesBetween, minutesOfDay, planTimeLabel, timeOfDay } from '@/utils/plan'
import { distanceBetween, mediaHint, takenDate } from '@/utils/mediaHints'
import { mediaLinkUpdates } from '@/utils/mediaLinks'
import { decodePolyline } from '@/utils/polyline'
import { generatePassword } from '@/utils/password'
import { exifSegment, withExif } from '@/utils/picture'
import { resolveTheme } from '@/utils/theme'
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
    story_md: '', actual_time: null, actual_end_time: null, rating: null, actual_cost_amount: null, difficulty: null, source_item_id: null, track: null,
    media: [], cover_media_id: null,
  })
  const day = (id: string, position: number, date: string, items: ReturnType<typeof place>[]) => ({
    id, position, date, title: '', notes_md: '', start_time: '09:00', default_mode: null, timezone: null,
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
      actual_cost_amount: null, note: 'bus', route_preference: 'fastest' as const, via: [], route_pinned: false,
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
