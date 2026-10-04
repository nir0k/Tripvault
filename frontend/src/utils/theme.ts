import { computed, readonly, ref } from 'vue'
import type { InstanceTheme, Theme, ThemeFile, ThemePalette, ThemeVariant } from '@/api/types'

const STORED_THEME_KEY = 'tripvault.theme'
const STORED_CUSTOM_KEY = 'tripvault.customTheme'
const DARK_QUERY = '(prefers-color-scheme: dark)'

/**
 * THEME_COLOR_NAMES are the colours a palette sets: the interface's own
 * variables without their "--color-" prefix, as the server checks them.
 */
export const THEME_COLOR_NAMES = [
  'base-100', 'base-200', 'base-300', 'base-content',
  'primary', 'primary-content', 'secondary', 'secondary-content',
  'accent', 'accent-content', 'neutral', 'neutral-content',
  'info', 'info-content', 'success', 'success-content',
  'warning', 'warning-content', 'error', 'error-content',
] as const

/** CustomTheme is what the page keeps of the instance's theme it is shown in. */
type CustomTheme = Pick<InstanceTheme, 'id' | 'light' | 'dark'>

const current = ref<Theme>('auto')
const custom = ref<CustomTheme | null>(null)
let listening = false

/** activeTheme is the preference shown now, for controls that display it. */
export const activeTheme = readonly(current)

/** activeCustomThemeId is the instance's theme shown now, null for the built-in one. */
export const activeCustomThemeId = computed(() => custom.value?.id ?? null)

/**
 * lockedVariant is the one palette of a theme that has only one, which is
 * shown whatever the light or dark preference says; null while the
 * preference decides.
 */
export const lockedVariant = computed(() => onlyVariant(custom.value))

/** resolveTheme turns a preference into the palette to show. */
export function resolveTheme(theme: Theme, prefersDark: boolean): ThemeVariant {
  if (theme === 'auto') {
    return prefersDark ? 'dark' : 'light'
  }
  return theme
}

/** onlyVariant names the palette of a theme that has just one, or null. */
export function onlyVariant(theme: Pick<InstanceTheme, 'light' | 'dark'> | null): ThemeVariant | null {
  if (theme?.light && !theme.dark) {
    return 'light'
  }
  if (theme?.dark && !theme.light) {
    return 'dark'
  }
  return null
}

/**
 * resolveVariant picks the palette to show: a theme's only one when it has
 * one, otherwise the one the preference asks for.
 */
export function resolveVariant(
  theme: Theme, prefersDark: boolean, custom: Pick<InstanceTheme, 'light' | 'dark'> | null,
): ThemeVariant {
  return onlyVariant(custom) ?? resolveTheme(theme, prefersDark)
}

/**
 * paletteStyle turns a palette into the variables of an element's style, so
 * the element and everything inside it is drawn in it; no palette is the
 * built-in one, which the element's data-theme gives.
 */
export function paletteStyle(palette: ThemePalette | null): Record<string, string> {
  const style: Record<string, string> = {}
  for (const name of THEME_COLOR_NAMES) {
    const value = palette?.[name]
    if (value) {
      style[`--color-${name}`] = value
    }
  }
  return style
}

/** isTheme reports whether a value is a theme preference. */
function isTheme(value: unknown): value is Theme {
  return value === 'light' || value === 'dark' || value === 'auto'
}

/** isPalette reports whether a stored value is a palette or no palette. */
function isPalette(value: unknown): value is ThemePalette | null {
  return value === null || (typeof value === 'object' && !Array.isArray(value))
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
 * readStoredCustomTheme returns the instance's theme last shown on this
 * browser, so the sign-in page and a read-only link, which have no profile,
 * open in it too; null for the built-in one or anything unreadable.
 */
export function readStoredCustomTheme(): CustomTheme | null {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(STORED_CUSTOM_KEY) ?? 'null')
    if (typeof value !== 'object' || value === null) {
      return null
    }
    const { id, light, dark } = value as Record<string, unknown>
    if (typeof id !== 'string' || !isPalette(light) || !isPalette(dark) || (!light && !dark)) {
      return null
    }
    return { id, light, dark }
  } catch {
    return null
  }
}

/**
 * paint shows the palette the preference and the theme call for: the
 * built-in variant by data-theme, and a theme's colours over it as variables
 * on the root, which outrank the variant's own.
 */
function paint(): void {
  const root = document.documentElement
  const variant = resolveVariant(current.value, window.matchMedia(DARK_QUERY).matches, custom.value)
  root.dataset.theme = variant
  const palette = custom.value?.[variant] ?? null
  for (const name of THEME_COLOR_NAMES) {
    const value = palette?.[name]
    if (value) {
      root.style.setProperty(`--color-${name}`, value)
    } else {
      root.style.removeProperty(`--color-${name}`)
    }
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
  paint()

  if (!listening) {
    listening = true
    window.matchMedia(DARK_QUERY).addEventListener('change', paint)
  }
}

/**
 * applyCustomTheme shows the interface in one of the instance's themes, or
 * in the built-in one for null, and remembers it on this browser like the
 * preference.
 */
export function applyCustomTheme(theme: CustomTheme | null): void {
  custom.value = theme ? { id: theme.id, light: theme.light, dark: theme.dark } : null
  try {
    if (custom.value) {
      localStorage.setItem(STORED_CUSTOM_KEY, JSON.stringify(custom.value))
    } else {
      localStorage.removeItem(STORED_CUSTOM_KEY)
    }
  } catch {
    // The choice still applies to this page.
  }
  paint()
}

/**
 * builtinPalette reads the colours of a built-in variant from the style
 * sheet, the one place they are written, by drawing a hidden element in it.
 */
export function builtinPalette(variant: ThemeVariant): ThemePalette {
  const probe = document.createElement('div')
  probe.dataset.theme = variant
  probe.hidden = true
  document.body.append(probe)
  try {
    const style = getComputedStyle(probe)
    const palette: ThemePalette = {}
    for (const name of THEME_COLOR_NAMES) {
      palette[name] = style.getPropertyValue(`--color-${name}`).trim()
    }
    return palette
  } finally {
    probe.remove()
  }
}

/** themeTemplate is a theme file of the built-in palettes, to start a theme from. */
export function themeTemplate(name: string): ThemeFile {
  return { format: 1, name, light: builtinPalette('light'), dark: builtinPalette('dark') }
}

/** themeFile writes a theme back as the file it was uploaded as. */
export function themeFile(theme: InstanceTheme): ThemeFile {
  return {
    format: 1,
    name: theme.name,
    ...(theme.light ? { light: theme.light } : {}),
    ...(theme.dark ? { dark: theme.dark } : {}),
  }
}

/** themeFilename names a theme's file after the theme, in characters every disk takes. */
export function themeFilename(name: string): string {
  const slug = name.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '')
  return `${slug || 'theme'}.theme.json`
}
