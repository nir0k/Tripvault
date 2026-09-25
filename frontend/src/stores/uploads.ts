import { defineStore } from 'pinia'
import { computed, reactive, ref, watch } from 'vue'
import { uploadMedia } from '@/api/media'
import type { Media } from '@/api/types'
import { fileChecksum, shrinkPicture } from '@/utils/picture'

// Every photograph on its way up, whichever page it was chosen on. The queue
// belongs to the application rather than to the panel a file was dropped on, so
// an upload carries on while its reader moves to another page, and one window
// in the corner (UploadPanel) tells how far it has got.
//
// Files go up one after another, each in a request of its own, so each has a
// bar of its own; large ones are shrunk here first - a phone's photograph is far
// larger than anything a report shows, and the service would only store the
// difference.

/** UploadState is where one file stands. */
export type UploadState = 'waiting' | 'uploading' | 'done' | 'failed' | 'cancelled'

/** UploadSettings are the choices made where the files were picked. */
export interface UploadSettings {
  /** Upload the files as private. */
  private: boolean
  /** Reduce large pictures before sending them. */
  shrink: boolean
}

/** Batch is the files picked together, and who wants to hear when they are stored. */
interface Batch {
  onUploaded: ((media: Media[]) => void) | null
  /** Files stored since the page last heard. */
  stored: Media[]
}

/** Upload is one file in the queue. */
export interface Upload {
  id: number
  tripId: string
  file: File
  settings: UploadSettings
  batch: Batch
  state: UploadState
  /** The share of the file sent so far, 0 to 1. */
  share: number
  /** Why it failed, as the API or the network said it, for the panel to word. */
  failure: unknown
}

/** UploadHandle lets the page that started a batch stop listening to it. */
export interface UploadHandle {
  /** detach stops the page hearing of the batch; the files still go up. */
  detach: () => void
}

/** AUTO_CLOSE_MS is how long the panel stays once everything went up. */
const AUTO_CLOSE_MS = 4000

/** useUploadsStore holds the queue of files being uploaded. */
export const useUploadsStore = defineStore('uploads', () => {
  const items = reactive<Upload[]>([])
  const visible = ref(false)
  const collapsed = ref(false)
  let nextId = 1
  let running = false
  let current: { upload: Upload; controller: AbortController } | null = null
  let closeTimer: ReturnType<typeof setTimeout> | null = null

  const total = computed(() => items.filter((item) => item.state !== 'cancelled').length)
  const done = computed(() => items.filter((item) => item.state === 'done').length)
  const failed = computed(() => items.filter((item) => item.state === 'failed').length)
  const busy = computed(() => items.some((item) => item.state === 'waiting' || item.state === 'uploading'))
  // The share of the whole queue that is through, counting the file on its way
  // by how much of it has gone.
  const progress = computed(() => {
    const counted = items.filter((item) => item.state !== 'cancelled')
    if (counted.length === 0) {
      return 0
    }
    const sent = counted.reduce((sum, item) => {
      if (item.state === 'done' || item.state === 'failed') {
        return sum + 1
      }
      return sum + (item.state === 'uploading' ? item.share : 0)
    }, 0)
    return sent / counted.length
  })

  // A reader who closes the tab mid-upload loses the rest, so the browser asks
  // first. Moving between pages of the application loses nothing.
  function onBeforeUnload(event: BeforeUnloadEvent): void {
    event.preventDefault()
  }
  watch(busy, (now) => {
    if (now) {
      window.addEventListener('beforeunload', onBeforeUnload)
    } else {
      window.removeEventListener('beforeunload', onBeforeUnload)
    }
  })

  // Once nothing is left to send the panel folds away on its own, unless
  // something failed: then it stays, so the reason can be read.
  watch([busy, failed], ([now, failures]) => {
    cancelAutoClose()
    if (!now && failures === 0 && items.length > 0) {
      closeTimer = setTimeout(close, AUTO_CLOSE_MS)
    }
  })

  // cancelAutoClose keeps the panel up.
  function cancelAutoClose(): void {
    if (closeTimer) {
      clearTimeout(closeTimer)
      closeTimer = null
    }
  }

  /**
   * enqueue adds files to the queue and shows the panel.
   *
   * Arguments:
   *   - tripId: the trip the files are stored against.
   *   - files: the files picked or dropped.
   *   - settings: privacy and shrinking, as chosen where they were picked.
   *   - onUploaded: told of the files stored, once the batch has settled.
   *
   * Returns:
   *   - a handle that stops onUploaded being called, for a page that goes away.
   */
  function enqueue(tripId: string, files: File[], settings: UploadSettings,
    onUploaded: (media: Media[]) => void): UploadHandle {
    const batch: Batch = { onUploaded, stored: [] }
    for (const file of files) {
      items.push({
        id: nextId++, tripId, file, settings: { ...settings }, batch,
        state: 'waiting', share: 0, failure: null,
      })
    }
    if (files.length > 0) {
      cancelAutoClose()
      visible.value = true
      collapsed.value = false
      void run()
    }
    return { detach: () => { batch.onUploaded = null } }
  }

  // run sends waiting files one at a time until none is left.
  async function run(): Promise<void> {
    if (running) {
      return
    }
    running = true
    try {
      for (;;) {
        const upload = items.find((item) => item.state === 'waiting')
        if (!upload) {
          break
        }
        await send(upload)
        settle(upload.batch)
      }
    } finally {
      running = false
    }
  }

  // send uploads one file, recording how it went on the file itself.
  async function send(upload: Upload): Promise<void> {
    const controller = new AbortController()
    current = { upload, controller }
    upload.state = 'uploading'
    upload.share = 0
    upload.failure = null
    try {
      // The sum is taken of the chosen file, before shrinking, so a copy of it
      // is refused however it was shrunk the time before.
      const sourceChecksum = await fileChecksum(upload.file)
      const prepared = upload.settings.shrink ? await shrinkPicture(upload.file) : upload.file
      if (controller.signal.aborted) {
        return
      }
      const stored = await uploadMedia(upload.tripId, prepared, {
        private: upload.settings.private,
        sourceChecksum: sourceChecksum ?? undefined,
        signal: controller.signal,
        onProgress: (share) => {
          upload.share = share
        },
      })
      upload.share = 1
      upload.state = 'done'
      upload.batch.stored.push(stored)
    } catch (err) {
      if (!controller.signal.aborted) {
        upload.state = 'failed'
        upload.failure = err
      }
    } finally {
      current = null
    }
  }

  // settle tells a batch's page what was stored once none of its files is left
  // to send. A file retried later settles its batch again on its own.
  function settle(batch: Batch): void {
    const pending = items.some((item) => item.batch === batch &&
      (item.state === 'waiting' || item.state === 'uploading'))
    if (pending || batch.stored.length === 0) {
      return
    }
    const stored = batch.stored.splice(0, batch.stored.length)
    batch.onUploaded?.(stored)
  }

  /** retry puts a failed file back in the queue. */
  function retry(upload: Upload): void {
    if (upload.state !== 'failed') {
      return
    }
    upload.state = 'waiting'
    upload.failure = null
    void run()
  }

  /** cancel stops one file, whether it is waiting or on its way. */
  function cancel(upload: Upload): void {
    if (upload.state === 'uploading' && current?.upload === upload) {
      current.controller.abort()
    } else if (upload.state !== 'waiting') {
      return
    }
    upload.state = 'cancelled'
    settle(upload.batch)
  }

  /** cancelAll stops every file not yet sent. */
  function cancelAll(): void {
    for (const upload of items) {
      cancel(upload)
    }
  }

  /** close puts the panel away and forgets the settled files. */
  function close(): void {
    if (busy.value) {
      return
    }
    cancelAutoClose()
    visible.value = false
    items.splice(0, items.length)
  }

  /** reset drops everything, for a reader who signs out. */
  function reset(): void {
    cancelAll()
    cancelAutoClose()
    visible.value = false
    items.splice(0, items.length)
  }

  return {
    items, visible, collapsed, total, done, failed, busy, progress,
    enqueue, retry, cancel, cancelAll, close, reset,
  }
})
