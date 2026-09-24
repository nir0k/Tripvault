import { createI18n } from 'vue-i18n'
import type en from './en.json'

/** MessageSchema is the shape every dictionary follows; English is the reference. */
export type MessageSchema = typeof en

// Every dictionary in this directory is loaded. Adding a language means adding
// its <code>.json file and its name to LANGUAGE_NAMES - no other code changes.
const modules = import.meta.glob<{ default: MessageSchema }>('./*.json', { eager: true })

const messages: Record<string, MessageSchema> = Object.fromEntries(
  Object.entries(modules).map(([path, module]) => [path.replace(/^\.\/(.+)\.json$/, '$1'), module.default]),
)

/** LANGUAGE_NAMES lists the languages, each in its own name. */
export const LANGUAGE_NAMES: Record<string, string> = {
  en: 'English',
  ru: 'Русский',
}

/** FALLBACK_LOCALE is used when nothing better is known. */
export const FALLBACK_LOCALE = 'en'

/** SUPPORTED_LOCALES are the languages with a dictionary. */
export const SUPPORTED_LOCALES = Object.keys(messages).sort()

const STORED_LOCALE_KEY = 'tripvault.locale'

/**
 * resolveLocale picks the interface language: the profile's choice, then the
 * choice made on a page without an account, then the browser's languages in
 * order, then English.
 */
export function resolveLocale(
  profileLocale: string | null | undefined,
  storedLocale: string | null | undefined,
  browserLanguages: readonly string[],
  supported: readonly string[] = SUPPORTED_LOCALES,
): string {
  for (const candidate of [profileLocale, storedLocale]) {
    if (candidate && supported.includes(candidate)) {
      return candidate
    }
  }
  for (const language of browserLanguages) {
    const base = language.toLowerCase().split('-')[0]
    if (base && supported.includes(base)) {
      return base
    }
  }
  return FALLBACK_LOCALE
}

/** readStoredLocale returns the language chosen without an account, if any. */
export function readStoredLocale(): string | null {
  try {
    return localStorage.getItem(STORED_LOCALE_KEY)
  } catch {
    return null
  }
}

/** storeLocale remembers a language chosen on this browser. */
export function storeLocale(locale: string): void {
  try {
    localStorage.setItem(STORED_LOCALE_KEY, locale)
  } catch {
    // The choice still applies to this page.
  }
}

/**
 * slavicPlural picks the form for "one | few | many" messages in Russian and
 * languages like it: 1, 21 → one; 2–4, 22–24 → few; the rest → many.
 */
export function slavicPlural(choice: number, choicesLength: number): number {
  if (choicesLength < 3) {
    return choice === 1 ? 0 : 1
  }
  const n = Math.abs(choice) % 100
  const last = n % 10
  if (n >= 11 && n <= 14) {
    return 2
  }
  if (last === 1) {
    return 0
  }
  if (last >= 2 && last <= 4) {
    return 1
  }
  return 2
}

export const i18n = createI18n<[MessageSchema], string, false>({
  legacy: false,
  locale: resolveLocale(null, readStoredLocale(), navigator.languages),
  fallbackLocale: FALLBACK_LOCALE,
  messages,
  pluralRules: { ru: slavicPlural },
})

/** applyLocale switches the interface language. */
export function applyLocale(locale: string): void {
  i18n.global.locale.value = locale
  document.documentElement.lang = locale
}
