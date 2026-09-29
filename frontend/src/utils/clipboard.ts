// Putting text on the clipboard. The clipboard API is there only on a secure
// page - https or localhost - so a page opened over plain http falls back to
// copying from a hidden field, which every browser still allows on a click.

/**
 * copyText puts text on the clipboard.
 *
 * Returns:
 *   - true when the text was copied, false when the browser allowed neither way.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // The fallback below may still work.
  }
  const field = document.createElement('textarea')
  field.value = text
  field.setAttribute('readonly', '')
  field.style.position = 'fixed'
  field.style.opacity = '0'
  document.body.appendChild(field)
  field.select()
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    field.remove()
  }
}
