import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import type { Trip } from '@/api/types'
import { useTripStore } from '@/stores/trip'
import { useMediaUrl } from '@/composables/useMediaUrl'

const api = vi.hoisted(() => ({ trip: vi.fn(), image: vi.fn() }))
vi.mock('@/api/trips', () => ({ getTrip: api.trip }))
vi.mock('@/api/client', () => ({ http: { get: api.image } }))

/** deferred controls when an API response reaches a component. */
function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void; reject: (error: unknown) => void } {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail })
  return { promise, resolve, reject }
}

afterEach(() => { vi.clearAllMocks(); vi.unstubAllGlobals() })

describe('trip navigation', () => {
  it('keeps the newest trip and loading state when earlier responses arrive late', async () => {
    setActivePinia(createPinia())
    const a = deferred<Trip>(), b = deferred<Trip>()
    api.trip.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const store = useTripStore()
    const first = store.load('a'), second = store.load('b')
    a.resolve({ id: 'a' } as Trip)
    await first
    expect(store.trip).toBeNull()
    expect(store.loading).toBe(true)
    b.resolve({ id: 'b' } as Trip)
    await second
    expect(store.trip?.id).toBe('b')
    expect(store.loading).toBe(false)
  })

  it('does not restore private trip data after an account reset', async () => {
    setActivePinia(createPinia())
    const answer = deferred<Trip>()
    api.trip.mockReturnValueOnce(answer.promise)
    const store = useTripStore(), pending = store.load('a')
    store.reset()
    answer.resolve({ id: 'a' } as Trip)
    await pending
    expect(store.trip).toBeNull()
  })
})

describe('image disposal', () => {
  it('does not allocate a blob URL when an active request finishes after unmount', async () => {
    const answer = deferred<{ data: Blob }>()
    api.image.mockReturnValueOnce(answer.promise)
    const create = vi.fn(() => 'blob:test'), revoke = vi.fn()
    vi.stubGlobal('URL', class extends URL { static createObjectURL = create; static revokeObjectURL = revoke })
    const app = createApp({ setup() { useMediaUrl(ref('/image')); return () => h('div') } })
    const root = document.createElement('div')
    app.mount(root)
    app.unmount()
    answer.resolve({ data: new Blob(['photo']) })
    await answer.promise
    await nextTick()
    expect(create).not.toHaveBeenCalled()
  })
})
