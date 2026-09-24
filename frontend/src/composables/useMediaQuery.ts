import { onBeforeUnmount, ref, type Ref } from 'vue'

/** useMediaQuery tracks whether a CSS media query currently matches. */
export function useMediaQuery(query: string): Ref<boolean> {
  const list = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia(query) : null
  const matches = ref(list?.matches ?? false)

  // onChange follows the query as the window is resized or rotated.
  function onChange(event: MediaQueryListEvent): void {
    matches.value = event.matches
  }

  list?.addEventListener('change', onChange)
  onBeforeUnmount(() => list?.removeEventListener('change', onChange))
  return matches
}
