import { readonly, ref } from 'vue'
import type { Theme } from '@/api/types'

const STORED_THEME_KEY = 'tripvault.theme'
const DARK_QUERY = '(prefers-color-scheme: dark)'

const current = ref<Theme>('auto')
let listening = false

/** activeTheme is the preference shown now, for controls that display it. */
export const activeTheme = readonly(current)

/** resolveTheme turns a preference into the palette to show. */
export function resolveTheme(theme: Theme, prefersDark: boolean): 'light' | 'dark' {
  if (theme === 'auto') {
    return prefersDark ? 'dark' : 'light'
  }
  return theme
}

/** isTheme reports whether a value is a theme preference. */
function isTheme(value: unknown): value is Theme {
  return value === 'light' || value === 'dark' || value === 'auto'
}

/** readStoredTheme returns the theme chosen on this browser, or "auto". */
export function readStoredTheme(): Theme {
  try {
    const value = localStorage.getItem(STORED_THEME_KEY)
    return isTheme(value) ? value : 'auto'
  } catch {
    return 'auto'
  }
}

/**
 * applyTheme shows a theme and remembers it on this browser, so the sign-in
 * page opens in the palette the person last used. "auto" follows the operating
 * system, including when it changes while the page is open.
 */
export function applyTheme(theme: Theme): void {
  current.value = theme
  try {
    localStorage.setItem(STORED_THEME_KEY, theme)
  } catch {
    // The choice still applies to this page.
  }

  const media = window.matchMedia(DARK_QUERY)
  document.documentElement.dataset.theme = resolveTheme(theme, media.matches)

  if (!listening) {
    listening = true
    media.addEventListener('change', (event) => {
      if (current.value === 'auto') {
        document.documentElement.dataset.theme = resolveTheme('auto', event.matches)
      }
    })
  }
}
