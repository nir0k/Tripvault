import { computed, ref, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

// The currencies a person chooses from, and the search that narrows them. The
// codes, the names and the symbols all come from Intl, so the names follow the
// interface language and nothing has to be shipped or kept up to date.

/** Currency is one currency as the choosers show it. */
export interface Currency {
  code: string
  name: string
  symbol: string
}

// The list is capped: nobody scrolls three hundred currencies, and the point of
// the search is to type the first letters of the one wanted.
const MAX_SHOWN = 30

// displayNames builds the translator of currency names, or nothing where the
// browser has none.
function displayNames(language: string): Intl.DisplayNames | null {
  try {
    return new Intl.DisplayNames([language], { type: 'currency' })
  } catch {
    return null
  }
}

// symbolOf reads a currency's short symbol, falling back to its code.
function symbolOf(value: string, language: string): string {
  try {
    const parts = new Intl.NumberFormat(language, {
      style: 'currency', currency: value, currencyDisplay: 'narrowSymbol',
    }).formatToParts(0)
    return parts.find((part) => part.type === 'currency')?.value ?? value
  } catch {
    return value
  }
}

/** describeCurrency renders a currency the way a field and a list show it. */
export function describeCurrency(currency: Currency): string {
  return currency.symbol === currency.code
    ? `${currency.code} · ${currency.name}`
    : `${currency.symbol} ${currency.name} · ${currency.code}`
}

/**
 * useCurrencies describes every currency the browser knows in the reader's
 * language and narrows them by what is typed into query.
 *
 * code is the chosen currency: it stays in the list even where the browser
 * cannot list currencies at all.
 */
export function useCurrencies(code: Ref<string>) {
  const { locale } = useI18n()
  const query = ref('')

  const currencies = computed<Currency[]>(() => {
    let codes: string[]
    try {
      codes = Intl.supportedValuesOf('currency')
    } catch {
      codes = [code.value].filter(Boolean)
    }
    const names = displayNames(locale.value)
    return codes.map((value) => ({ code: value, name: names?.of(value) ?? value, symbol: symbolOf(value, locale.value) }))
  })

  const chosen = computed(() => currencies.value.find((currency) => currency.code === code.value) ?? null)

  const matches = computed(() => {
    const text = query.value.trim().toLowerCase()
    const found = text === ''
      ? currencies.value
      : currencies.value.filter((currency) =>
        currency.code.toLowerCase().startsWith(text)
        || currency.name.toLowerCase().includes(text)
        || currency.symbol.toLowerCase() === text)
    return found.slice(0, MAX_SHOWN)
  })

  return { query, chosen, matches }
}
