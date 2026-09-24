import type { Media, MediaTarget, TripDocument } from '@/api/types'

// Filing photographs from the gallery of the whole trip, which is the other way
// round from a document: there a day is opened and pictures are added to it,
// here a batch of pictures is taken and the days and places they belong to are
// ticked off. The API replaces a gallery whole rather than taking a difference,
// so what this works out is what each touched gallery should hold afterwards.
//
// Both documents of a trip are fair game: a picture hangs on a day of the plan
// as readily as on a day of the report, and a trip still being planned has no
// report at all.

/** MediaLinkPlace is a day of a document, or a place of one of its days. */
export type MediaLinkPlace = Extract<MediaTarget, 'day' | 'item'>

/** MediaLinkChange is one gallery a batch of pictures is put into, or taken out of. */
export interface MediaLinkChange {
  target: MediaLinkPlace
  targetId: string
  attach: boolean
}

/** MediaLinkUpdate is one gallery of a document as it should be after the change. */
export interface MediaLinkUpdate {
  target: MediaLinkPlace
  targetId: string
  mediaIds: string[]
}

/**
 * mediaLinkUpdates - works out which galleries have to be rewritten to file
 * these pictures where the reader asked, and what each of them holds
 * afterwards. The pictures already hanging there keep their order and what is
 * added goes to the end, the way a gallery grows when pictures are uploaded
 * into it.
 *
 * Arguments:
 *   - documents: the trip's plan and report, whichever of them it has.
 *   - media: the pictures being filed.
 *   - changes: the galleries they are put into, or taken out of.
 *
 * Returns:
 *   - one update per gallery whose contents really change, in the order the
 *     changes were made; a gallery that already holds what was asked for is
 *     left out, so repeating the same choice sends nothing.
 */
export function mediaLinkUpdates(
  documents: TripDocument[],
  media: Media[],
  changes: MediaLinkChange[],
): MediaLinkUpdate[] {
  const ids = media.map((picture) => picture.id)
  const updates: MediaLinkUpdate[] = []
  for (const change of changes) {
    const gallery = galleryOf(documents, change.target, change.targetId)
    if (!gallery) {
      continue
    }
    const current = gallery.map((picture) => picture.id)
    const mediaIds = change.attach
      ? [...current, ...ids.filter((id) => !current.includes(id))]
      : current.filter((id) => !ids.includes(id))
    if (mediaIds.length !== current.length) {
      updates.push({ target: change.target, targetId: change.targetId, mediaIds })
    }
  }
  return updates
}

// galleryOf finds the pictures a day or a place holds, or null when no document
// carries it any more - a day somebody else removed while the page was open.
function galleryOf(documents: TripDocument[], target: MediaLinkPlace, targetId: string): Media[] | null {
  for (const document of documents) {
    if (target === 'day') {
      const day = document.days.find((each) => each.id === targetId)
      if (day) {
        return day.media
      }
      continue
    }
    for (const day of document.days) {
      const item = day.items.find((each) => each.id === targetId)
      if (item) {
        return item.media
      }
    }
  }
  return null
}
