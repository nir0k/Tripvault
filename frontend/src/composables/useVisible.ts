import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/**
 * useVisible - reports whether an element has come near the screen, and stays
 * true once it has.
 *
 * A report with a hundred photographs asks only for those the reader is about
 * to see; the rest are fetched as the page scrolls to them.
 *
 * Arguments:
 *   - element: the element to watch, usually a template ref.
 *
 * Returns:
 *   - a flag that turns true when the element is close to the viewport. Where
 *     there is nothing to watch with, everything counts as visible.
 */
export function useVisible(element: Ref<Element | null>): Ref<boolean> {
  const visible = ref(typeof IntersectionObserver === 'undefined')
  let observer: IntersectionObserver | null = null

  onMounted(() => {
    if (visible.value || !element.value) {
      return
    }
    observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        visible.value = true
        observer?.disconnect()
      }
    }, { rootMargin: '300px' })
    observer.observe(element.value)
  })

  onBeforeUnmount(() => observer?.disconnect())
  return visible
}
