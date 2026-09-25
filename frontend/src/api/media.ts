import { http, UPLOAD_TIMEOUT_MS } from './client'
import type { ListResponse, Media, MediaTarget } from './types'

// The files of a trip. Their bytes are not fetched here: a picture travels with
// an access or a share token in a header, so it cannot be handed to an <img>
// as an address and is loaded by useMediaUrl instead. What this module builds
// are the paths that composable asks for.

/** MEDIA_SIZES are the widths previews are served at. */
export const MEDIA_SIZES = [160, 320, 640, 1280, 1920] as const

/** MediaSize is one of the widths a preview is served at. */
export type MediaSize = (typeof MEDIA_SIZES)[number]

/** The base path of the two ways a file is reached: signed in, or by a link. */
export const MEDIA_BASE = '/api/v1/media'
export const SHARED_MEDIA_BASE = '/api/v1/shared/media'

/**
 * mediaFilePath builds the address of the stored file itself.
 *
 * Arguments:
 *   - base: MEDIA_BASE in the application, SHARED_MEDIA_BASE behind a link.
 *   - id: the file.
 */
export function mediaFilePath(base: string, id: string): string {
  return `${base}/${encodeURIComponent(id)}`
}

/**
 * mediaThumbnailPath builds the address of a preview.
 *
 * Arguments:
 *   - base: MEDIA_BASE in the application, SHARED_MEDIA_BASE behind a link.
 *   - id: the file.
 *   - size: one of MEDIA_SIZES.
 */
export function mediaThumbnailPath(base: string, id: string, size: MediaSize): string {
  return `${mediaFilePath(base, id)}/thumbnail?size=${size}`
}

/** listMedia returns every file of a trip in the order they were taken. */
export async function listMedia(tripId: string): Promise<Media[]> {
  return (await http.get<ListResponse<Media>>(`/api/v1/trips/${encodeURIComponent(tripId)}/media`)).data.items
}

/** UploadOptions carry the privacy of an upload and follow its progress. */
export interface UploadOptions {
  private?: boolean
  /**
   * sourceChecksum is the SHA-256, in hex, of the original a shrunk file was
   * made from, so the server knows a copy of it whatever bytes shrinking gave.
   */
  sourceChecksum?: string
  /** onProgress reports the share of the file that has been sent, 0 to 1. */
  onProgress?: (share: number) => void
  signal?: AbortSignal
}

/**
 * uploadMedia stores one file against a trip.
 *
 * One file per request on purpose: a browser reports the progress of a request,
 * not of a part, so a gallery of files uploaded together can only show a bar
 * each if each has its own request.
 */
export async function uploadMedia(tripId: string, file: File, options: UploadOptions = {}): Promise<Media> {
  const form = new FormData()
  if (options.private) {
    form.append('private', 'true')
  }
  if (options.sourceChecksum) {
    form.append('source_checksum', options.sourceChecksum)
  }
  form.append('file', file, file.name)

  const response = await http.post<ListResponse<Media>>(
    `/api/v1/trips/${encodeURIComponent(tripId)}/media`,
    form,
    {
      signal: options.signal,
      timeout: UPLOAD_TIMEOUT_MS,
      onUploadProgress: (event) => {
        if (options.onProgress && event.total) {
          options.onProgress(event.loaded / event.total)
        }
      },
    },
  )
  const stored = response.data.items[0]
  if (!stored) {
    throw new Error('the upload stored nothing')
  }
  return stored
}

/** updateMedia changes whether a file is private. */
export async function updateMedia(id: string, changes: { is_private: boolean }): Promise<Media> {
  return (await http.patch<Media>(`/api/v1/media/${encodeURIComponent(id)}`, changes)).data
}

/** deleteMedia removes a file from the trip, from every gallery and from the disk. */
export async function deleteMedia(id: string): Promise<void> {
  await http.delete(`/api/v1/media/${encodeURIComponent(id)}`)
}

/**
 * setMediaLinks makes a trip, a day or a place show exactly these files, in
 * this order. The whole gallery is sent, not a difference, so repeating the
 * call changes nothing.
 *
 * favoriteMediaIds names which of them the report shows for that day or place;
 * leaving it out keeps the ones already marked, so a reorder need not repeat
 * them.
 */
export async function setMediaLinks(target: MediaTarget, targetId: string, mediaIds: string[],
  favoriteMediaIds?: string[]): Promise<void> {
  await http.put('/api/v1/media-links', {
    target_type: target,
    target_id: targetId,
    media_ids: mediaIds,
    ...(favoriteMediaIds ? { favorite_media_ids: favoriteMediaIds } : {}),
  })
}

/** MEDIA_FAVORITE_LIMIT is how many pictures a day or a place shows in the report. */
export const MEDIA_FAVORITE_LIMIT = 7

/**
 * setMediaFavorites marks files as the pictures the report shows, or stops
 * marking them. The gallery of a trip names files rather than the places they
 * hang in, so the mark lands wherever in the report they are shown.
 */
export async function setMediaFavorites(mediaIds: string[], favorite: boolean): Promise<void> {
  await http.post('/api/v1/media-favorites', { media_ids: mediaIds, favorite })
}
