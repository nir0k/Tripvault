import { computed, inject, type ComputedRef, type InjectionKey } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { readingLanguage } from '@/utils/translate'

// Which language a report's own words are shown in. It starts at the reader's
// interface language when the report has it and at the original otherwise, and
// a reader who picks another keeps it in the address (?lang=), so a reload or a
// shared address opens the report the same way. The interface around the words
// stays in the reader's language either way.

/**
 * useContentLanguage - the language a report is read and written in.
 *
 * Arguments:
 *   - languages: the report's languages, the original first.
 *
 * Returns:
 *   - lang: the language shown.
 *   - original: the report's original language.
 *   - choose: switches the language, keeping the choice in the address.
 */
export function useContentLanguage(languages: () => readonly string[]): {
  lang: ComputedRef<string>
  original: ComputedRef<string>
  choose: (code: string) => void
} {
  const { locale } = useI18n()
  const route = useRoute()
  const router = useRouter()

  const requested = computed(() => (typeof route.query.lang === 'string' ? route.query.lang : locale.value))
  const lang = computed(() => readingLanguage(languages(), requested.value))
  const original = computed(() => languages()[0] ?? '')

  function choose(code: string): void {
    // The reader's own language needs no mark in the address.
    const lang = code === readingLanguage(languages(), locale.value) ? undefined : code
    void router.replace({ query: { ...route.query, lang } })
  }

  return { lang, original, choose }
}

/**
 * ReportTextEditing tells the editors of a report which text they open with.
 * While a translation is written, an editor opens with the translation alone
 * - empty where there is none - and shows the original beside it; otherwise
 * it opens with the text shown.
 */
export interface ReportTextEditing {
  /** translating is true while the language written is not the original. */
  translating: boolean
  /** source is what an editor of a field opens with. */
  source: (id: string, field: string, shown: string) => string
  /** original is the text the translator works from, or undefined while writing the original. */
  original: (id: string, field: string) => string | undefined
}

/** reportTextKey is how the report page hands ReportTextEditing to its editors. */
export const reportTextKey: InjectionKey<ComputedRef<ReportTextEditing>> = Symbol('reportText')

const writingTheOriginal: ReportTextEditing = {
  translating: false,
  source: (_id, _field, shown) => shown,
  original: () => undefined,
}

/** useReportText reads what the report page says about the text being written. */
export function useReportText(): ComputedRef<ReportTextEditing> {
  return inject(reportTextKey, computed(() => writingTheOriginal))
}
