import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/** VisibleOptions says how long an element counts as visible. */
export interface VisibleOptions {
  /**
   * Stay true once the element has come near the screen; true when not given.
   * When false the flag follows the element in and out of reach.
   */
  once?: boolean
}

/**
 * useVisible - reports whether an element is near the screen.
 *
 * A report with a hundred photographs asks only for those the reader is about
 * to see; the rest are fetched as the page scrolls to them.
 *
 * Arguments:
 *   - element: the element to watch, usually a template ref.
 *   - options: whether the flag stays true once it has turned true.
 *
 * Returns:
 *   - a flag that is true while the element is close to the viewport, or from
 *     the first time it was. Where there is nothing to watch with, everything
 *     counts as visible.
 */
export function useVisible(element: Ref<Element | null>, options: VisibleOptions = {}): Ref<boolean> {
  const once = options.once ?? true
  const visible = ref(typeof IntersectionObserver === 'undefined')
  let observer: IntersectionObserver | null = null

  onMounted(() => {
    if (visible.value || !element.value) {
      return
    }
    observer = new IntersectionObserver((entries) => {
      const near = entries.some((entry) => entry.isIntersecting)
      if (once && !near) {
        return
      }
      visible.value = near
      if (once) {
        observer?.disconnect()
      }
    }, { rootMargin: '300px' })
    observer.observe(element.value)
  })

  onBeforeUnmount(() => observer?.disconnect())
  return visible
}
