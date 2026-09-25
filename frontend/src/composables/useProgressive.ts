import { nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'

// How far below the screen the marker at the end of what is laid out starts the
// next batch, so the tiles are there before the reader scrolls to them.
const AHEAD = '1500px'

/**
 * useProgressive - lays a long list out a batch at a time, the next batch
 * added as the reader scrolls towards the end of what is already on the page,
 * the way a photo library fills in as it is scrolled.
 *
 * A trip of two thousand photographs would otherwise build two thousand tiles
 * before the first one could be seen.
 *
 * Arguments:
 *   - marker: an element placed after the last laid-out entry, shown while
 *     there are more to lay out.
 *   - total: how many entries there are.
 *   - batch: how many entries each batch adds.
 *
 * Returns:
 *   - how many entries to lay out from the start of the list.
 */
export function useProgressive(marker: Ref<Element | null>, total: () => number, batch = 120): Ref<number> {
  const limit = ref(batch)
  let observer: IntersectionObserver | null = null

  // grow adds a batch, and asks again once it is laid out: a batch shorter than
  // the screen leaves the marker in reach, which the observer does not report a
  // second time on its own.
  function grow(): void {
    if (limit.value >= total()) {
      return
    }
    limit.value += batch
    void nextTick(() => {
      if (observer && marker.value) {
        observer.unobserve(marker.value)
        observer.observe(marker.value)
      }
    })
  }

  if (typeof IntersectionObserver === 'undefined') {
    // Nothing to watch the scrolling with: everything is laid out at once.
    watch(total, (count) => {
      limit.value = Math.max(count, batch)
    }, { immediate: true })
    return limit
  }

  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) {
      grow()
    }
  }, { rootMargin: `0px 0px ${AHEAD} 0px` })

  // The marker comes and goes with the list, so it is watched whenever it is there.
  watch(marker, (next, previous) => {
    if (previous) {
      observer?.unobserve(previous)
    }
    if (next) {
      observer?.observe(next)
    }
  }, { immediate: true })

  onBeforeUnmount(() => observer?.disconnect())
  return limit
}
