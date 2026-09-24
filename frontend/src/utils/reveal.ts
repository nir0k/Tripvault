// How long a card sent to from elsewhere on the page stays marked, so the eye
// finds it after the scroll.
const FLASH_MS = 1600

/**
 * revealElement - scrolls the page to an element and marks it for a moment,
 * which is how a pin on the map or a day in the contents points at what it
 * stands for.
 *
 * Arguments:
 *   - id: the element's id.
 *   - flash: whether to mark it once it is in view.
 *
 * Returns:
 *   - false when no such element is on the page, such as a place the reader
 *     has hidden.
 */
export function revealElement(id: string, flash = true): boolean {
  const element = document.getElementById(id)
  if (!element) {
    return false
  }
  element.scrollIntoView({ behavior: 'smooth', block: 'start' })
  if (flash) {
    element.classList.add('reveal-flash')
    window.setTimeout(() => element.classList.remove('reveal-flash'), FLASH_MS)
  }
  return true
}
