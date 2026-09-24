import type { Media, PlanDay, PlanItem, TripDocument } from '@/api/types'
import { isVisit } from '@/utils/plan'

// What a photograph knows about itself, turned into an offer. A camera records
// when and where it took a picture; a report already knows which day is which
// date and where its places are, so the two can be put side by side and the
// obvious suggestion made - without the reader having to look anything up.
//
// A suggestion is only ever made when it says something new: a picture already
// hanging where its metadata points needs no advice.

/** HINT_RADIUS_M is how close a picture must be to a place to be tied to it. */
const HINT_RADIUS_M = 300

/** MediaHint is what a picture suggests about where it belongs. */
export interface MediaHint {
  media: Media
  /** The day the picture was taken on, when that is not the day it hangs in. */
  day: PlanDay | null
  /** The nearest place within HINT_RADIUS_M that it is not tied to yet. */
  item: PlanItem | null
  /** How far that place is, in metres. */
  distanceM: number
}

/**
 * mediaHint - reads what a picture suggests about where it belongs.
 *
 * Arguments:
 *   - media: the stored picture, with whatever its camera recorded.
 *   - document: the report it was uploaded into.
 *   - timezone: the trip's zone, in which the date of a day is read.
 *   - dayId: the day it is already linked to, or null when it hangs elsewhere.
 *   - itemId: the place it is already linked to, or null.
 *
 * Returns:
 *   - the hint, or null when there is nothing worth saying.
 */
export function mediaHint(media: Media, document: TripDocument, timezone: string,
  dayId: string | null = null, itemId: string | null = null): MediaHint | null {
  const takenOn = takenDate(media.taken_at, timezone)
  const day = takenOn ? document.days.find((each) => each.date === takenOn) ?? null : null

  // Places of the day it was taken on read first; without a day, the whole
  // report is fair game, because a picture may still sit next to a place.
  const places = (day ? day.items : document.days.flatMap((each) => each.items))
    .filter((item) => isVisit(item) && item.lat !== null && item.lng !== null)

  let item: PlanItem | null = null
  let distanceM = 0
  if (media.lat !== null && media.lng !== null) {
    for (const place of places) {
      const metres = distanceBetween(media.lat, media.lng, place.lat ?? 0, place.lng ?? 0)
      if (metres <= HINT_RADIUS_M && (item === null || metres < distanceM)) {
        item = place
        distanceM = metres
      }
    }
  }

  const newDay = day !== null && day.id !== dayId
  const newPlace = item !== null && item.id !== itemId
  if (!newDay && !newPlace) {
    return null
  }
  return {
    media,
    day: newDay ? day : null,
    item: newPlace ? item : null,
    distanceM: newPlace ? Math.round(distanceM) : 0,
  }
}

/**
 * takenDate - reads the calendar date a picture was taken on, in the trip's own
 * zone rather than the reader's: a photograph taken late in Reykjavík belongs
 * to the Icelandic day, whoever is looking at it from where.
 *
 * Arguments:
 *   - takenAt: the moment from the picture's metadata, or null.
 *   - timezone: an IANA zone name.
 *
 * Returns:
 *   - the date as YYYY-MM-DD, or null when the picture carries no moment.
 */
export function takenDate(takenAt: string | null, timezone: string): string | null {
  if (!takenAt) {
    return null
  }
  const moment = new Date(takenAt)
  if (Number.isNaN(moment.getTime())) {
    return null
  }
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: timezone || 'UTC',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(moment)
  } catch {
    // An unknown zone name is no reason to lose the date; UTC is the fallback
    // the rest of the service uses as well.
    return moment.toISOString().slice(0, 10)
  }
}

/** EARTH_RADIUS_M is the mean radius used for distances between coordinates. */
const EARTH_RADIUS_M = 6_371_000

/**
 * distanceBetween - measures the distance between two coordinates along the
 * surface of the earth.
 *
 * Arguments:
 *   - fromLat, fromLng: the first point in degrees.
 *   - toLat, toLng: the second point in degrees.
 *
 * Returns:
 *   - the distance in metres.
 */
export function distanceBetween(fromLat: number, fromLng: number, toLat: number, toLng: number): number {
  const radians = Math.PI / 180
  const dLat = (toLat - fromLat) * radians
  const dLng = (toLng - fromLng) * radians
  const a = Math.sin(dLat / 2) ** 2
    + Math.cos(fromLat * radians) * Math.cos(toLat * radians) * Math.sin(dLng / 2) ** 2
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(a)))
}
