import { computed, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { LANGUAGE_NAMES, SUPPORTED_LOCALES } from '@/i18n'

// The languages offered, in the two groups every language picker shows: the
// ones this reader is likely to want - the language in use and the one the
// browser asks for - and the rest underneath. Only languages the build carries
// a dictionary for are offered, because any other would leave the interface in
// English without saying why.
//
// Both the window in the navigation bar and the control in the profile read
// this, so the two never disagree about what is recommended.

/** RECOMMENDED_COUNT is how many languages are offered above the rest. */
const RECOMMENDED_COUNT = 2

/** Language is one choice in a language picker. */
export interface Language {
  code: string
  name: string
}

/**
 * useLanguages - groups the languages of this build for a picker.
 *
 * Returns:
 *   - all: every language with a dictionary, in its own name.
 *   - recommended: the language in use and the one the browser asks for.
 *   - rest: the languages left over, which is none while the build carries no
 *     more dictionaries than the picker recommends.
 */
export function useLanguages(): {
  all: ComputedRef<Language[]>
  recommended: ComputedRef<Language[]>
  rest: ComputedRef<Language[]>
} {
  const { locale } = useI18n()

  const all = computed<Language[]>(() =>
    SUPPORTED_LOCALES.map((code) => ({ code, name: LANGUAGE_NAMES[code] ?? code })))

  const recommended = computed<Language[]>(() => {
    const codes = [locale.value]
    for (const language of navigator.languages ?? []) {
      const base = language.toLowerCase().split('-')[0]
      if (base && SUPPORTED_LOCALES.includes(base) && !codes.includes(base)) {
        codes.push(base)
      }
    }
    return codes
      .slice(0, RECOMMENDED_COUNT)
      .map((code) => all.value.find((language) => language.code === code))
      .filter((language): language is Language => language !== undefined)
  })

  const rest = computed<Language[]>(() =>
    all.value.filter((language) => !recommended.value.some((each) => each.code === language.code)))

  return { all, recommended, rest }
}
