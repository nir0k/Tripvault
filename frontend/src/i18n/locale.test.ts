import { describe, expect, it } from 'vitest'
import { FALLBACK_LOCALE, resolveLocale, slavicPlural } from '@/i18n'

describe('resolveLocale', () => {
  const supported = ['en', 'ru']

  it('prefers the profile, then the stored choice, then the browser', () => {
    expect(resolveLocale('ru', 'en', ['en-US'], supported)).toBe('ru')
    expect(resolveLocale('', 'ru', ['en-US'], supported)).toBe('ru')
    expect(resolveLocale(null, null, ['de-DE', 'ru-RU', 'en'], supported)).toBe('ru')
  })

  it('ignores languages without a dictionary and falls back to English', () => {
    expect(resolveLocale('de', 'fr', ['de-DE'], supported)).toBe(FALLBACK_LOCALE)
  })
})

describe('slavicPlural', () => {
  it('picks one, few and many like Russian does', () => {
    const forms = [1, 2, 5, 11, 12, 21, 22, 25, 111, 0].map((n) => slavicPlural(n, 3))
    expect(forms).toEqual([0, 1, 2, 2, 2, 0, 1, 2, 2, 2])
  })
})
