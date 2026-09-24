import { inject, onBeforeUnmount, provide, ref, watch, type InjectionKey, type Ref } from 'vue'
import { http } from '@/api/client'
import { MEDIA_BASE } from '@/api/media'

// A stored file needs a token, which travels in a header, so it cannot be given
// to an <img> as a plain address. Each picture is fetched with the header set
// and shown as an object URL, which owns memory until it is revoked. The HTTP
// cache still does its work: the answer carries an ETag, so a picture asked for
// twice is sent once, and the second time the server only confirms the reader
// may still see it.

/** mediaBaseKey carries which of the two media paths a page reads through. */
const mediaBaseKey: InjectionKey<string> = Symbol('mediaBase')

/**
 * provideMediaBase - tells the pictures below this component where to read
 * files from: the application's own path, or the one a read-only link opens.
 *
 * Arguments:
 *   - base: MEDIA_BASE or SHARED_MEDIA_BASE.
 */
export function provideMediaBase(base: string): void {
  provide(mediaBaseKey, base)
}

/**
 * useMediaBase - returns the media path of the current page.
 *
 * Returns:
 *   - the provided base, or the application's own outside a link.
 */
export function useMediaBase(): string {
  return inject(mediaBaseKey, MEDIA_BASE)
}

/**
 * MediaPriority - where a picture stands in the loading queue: `high` for the
 * one a reader has opened, `normal` for what is on the page, `low` for what is
 * fetched ahead of being asked for.
 */
export type MediaPriority = 'high' | 'normal' | 'low'

const priorityRank: Record<MediaPriority, number> = { high: 0, normal: 1, low: 2 }

// How many pictures are fetched at once. It matches the previews the server
// renders at a time: more in flight would only wait there, in an order nobody
// chose, instead of here in the order of the page.
const parallelLoads = 4

/** QueuedLoad is one fetch waiting for a free slot. */
interface QueuedLoad {
  rank: number
  element: Element | null
  seq: number
  start: () => void
}

const waiting: QueuedLoad[] = []
let active = 0
let enqueued = 0

// comesFirst orders two waiting loads: by priority, then by where their
// pictures sit on the page, so a gallery fills from its first picture to its
// last, and by arrival where there is no picture on the page to compare.
function comesFirst(a: QueuedLoad, b: QueuedLoad): boolean {
  if (a.rank !== b.rank) {
    return a.rank < b.rank
  }
  if (a.element?.isConnected && b.element?.isConnected && a.element !== b.element) {
    return (a.element.compareDocumentPosition(b.element) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
  }
  return a.seq < b.seq
}

// pump starts the first waiting loads while there are free slots.
function pump(): void {
  while (active < parallelLoads && waiting.length > 0) {
    let next = 0
    for (let i = 1; i < waiting.length; i++) {
      if (comesFirst(waiting[i]!, waiting[next]!)) {
        next = i
      }
    }
    const [load] = waiting.splice(next, 1)
    active++
    load!.start()
  }
}

/** QueueCancelled is how a load taken out of the queue before it started ends. */
class QueueCancelled extends Error {}

/**
 * queueMediaLoad fetches a stored file once a slot is free.
 *
 * Arguments:
 *   - path: the address of the file or preview.
 *   - priority: its place in the queue.
 *   - element: the picture on the page, which orders loads of one priority.
 *
 * Returns:
 *   - the fetched bytes, and a cancel that drops the load if it has not
 *     started yet; a load already on the wire is left to finish into the cache.
 */
function queueMediaLoad(path: string, priority: MediaPriority, element: Element | null): {
  done: Promise<Blob>
  cancel: () => void
} {
  let entry: QueuedLoad | null = null
  let reject: (reason: unknown) => void = () => {}
  const done = new Promise<Blob>((resolve, fail) => {
    reject = fail
    entry = {
      rank: priorityRank[priority],
      element,
      seq: enqueued++,
      start: () => {
        http.get<Blob>(path, { responseType: 'blob' })
          .then((response) => resolve(response.data), fail)
          .finally(() => {
            active--
            pump()
          })
      },
    }
    waiting.push(entry)
  })
  pump()

  function cancel(): void {
    const at = entry ? waiting.indexOf(entry) : -1
    if (at >= 0) {
      waiting.splice(at, 1)
      reject(new QueueCancelled())
    }
  }
  return { done, cancel }
}

/**
 * prefetchMedia - fetches a file ahead of it being shown, at the back of the
 * queue, so that it comes from the browser's cache when it is.
 *
 * Arguments:
 *   - path: the address of the file or preview.
 */
export function prefetchMedia(path: string): void {
  queueMediaLoad(path, 'low', null).done.catch(() => {})
}

/**
 * loadMediaUrl - fetches a stored file outside a component, for a picture drawn
 * by code that is not Vue's, such as a popup on the map. The URL is the
 * caller's to revoke once the picture is gone.
 *
 * Arguments:
 *   - path: the address of the file or preview.
 *   - priority: its place in the queue; high, since somebody asked to see it.
 *
 * Returns:
 *   - the object URL once the file has arrived, and a cancel that drops the
 *     load if it has not started yet.
 */
export function loadMediaUrl(path: string, priority: MediaPriority = 'high'): {
  done: Promise<string>
  cancel: () => void
} {
  const load = queueMediaLoad(path, priority, null)
  return { done: load.done.then((data) => URL.createObjectURL(data)), cancel: load.cancel }
}

/** MediaUrlOptions place a picture in the loading queue. */
export interface MediaUrlOptions {
  /** The picture on the page, whose position orders the queue. */
  element?: Ref<Element | null>
  /** The queue it waits in; normal when not given. */
  priority?: MediaPriority
}

/**
 * useMediaUrl - loads a stored file and returns a URL an <img> can show.
 *
 * The value is null until the file has arrived, and null again while a new
 * address is loading, so a picture that has just changed shows nothing rather
 * than briefly showing the one before it. Loads share one queue, so a page of
 * pictures fills in the order of the page rather than in whatever order the
 * answers happen to come back.
 *
 * Arguments:
 *   - path: a reactive address of the file or preview, or null for nothing.
 *   - options: the picture's place in the loading queue.
 *
 * Returns:
 *   - the object URL, and a flag that is true while a picture is being fetched.
 */
export function useMediaUrl(path: Ref<string | null>, options: MediaUrlOptions = {}): {
  url: Ref<string | null>
  loading: Ref<boolean>
} {
  const url = ref<string | null>(null)
  const loading = ref(false)
  let current: string | null = null
  let generation = 0
  let cancel: (() => void) | null = null

  // release frees the memory an object URL holds.
  function release(): void {
    if (current) {
      URL.revokeObjectURL(current)
      current = null
    }
  }

  watch(path, async (next) => {
    const wanted = ++generation
    cancel?.()
    cancel = null
    release()
    url.value = null
    if (!next) {
      loading.value = false
      return
    }
    loading.value = true
    const load = queueMediaLoad(next, options.priority ?? 'normal', options.element?.value ?? null)
    cancel = load.cancel
    try {
      const data = await load.done
      if (wanted !== generation) {
        return
      }
      current = URL.createObjectURL(data)
      url.value = current
    } catch {
      // A picture that will not load is not worth an error message: the
      // placeholder in its place says the same thing more quietly.
    } finally {
      if (wanted === generation) {
        loading.value = false
        cancel = null
      }
    }
  }, { immediate: true })

  onBeforeUnmount(() => {
    cancel?.()
    release()
  })
  return { url, loading }
}
