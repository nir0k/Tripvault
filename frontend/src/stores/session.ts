import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as authApi from '@/api/auth'
import { readRefreshToken, refreshSession } from '@/api/client'
import * as meApi from '@/api/me'
import type { DateFormat, Theme, TimeFormat, Units, User } from '@/api/types'
import { applyLocale, readStoredLocale, resolveLocale, storeLocale } from '@/i18n'
import { applyDateFormat, applyTimeFormat } from '@/utils/display'
import { applyTheme } from '@/utils/theme'
import { applyUnits } from '@/utils/units'

/** useSessionStore holds the signed-in account and its interface preferences. */
export const useSessionStore = defineStore('session', () => {
  const user = ref<User | null>(null)
  let restoring: Promise<void> | null = null

  const signedIn = computed(() => user.value !== null)
  const isAdmin = computed(() => user.value?.is_admin === true)
  const mustChangePassword = computed(() => user.value?.must_change_password === true)

  /**
   * setUser stores the account and applies how it reads the interface: its
   * language, theme, units and the way it writes dates and times.
   */
  function setUser(next: User | null): void {
    user.value = next
    if (next) {
      applyLocale(resolveLocale(next.locale, readStoredLocale(), navigator.languages))
      applyTheme(next.theme)
      applyUnits(next.units)
      applyDateFormat(next.date_format)
      applyTimeFormat(next.time_format)
    }
  }

  /**
   * restore signs back in from the stored refresh token once per page load.
   * Every navigation awaits it, so a reload lands on the page it was on.
   */
  function restore(): Promise<void> {
    if (!restoring) {
      restoring = (async () => {
        if (!readRefreshToken()) {
          return
        }
        try {
          const session = await refreshSession()
          setUser(session?.user ?? null)
        } catch {
          // A temporary API outage must not erase the refresh token. The next
          // reload or authenticated request can retry once the server returns.
          setUser(null)
        }
      })()
    }
    return restoring
  }

  /** login signs in with a password. */
  async function login(email: string, password: string): Promise<User> {
    const session = await authApi.login(email, password)
    setUser(session.user)
    return session.user
  }

  /** logout ends the session and forgets the account. */
  async function logout(): Promise<void> {
    await authApi.logout()
    user.value = null
  }

  /** forget drops the account locally when the server ended the session. */
  function forget(): void {
    user.value = null
  }

  /** reload reads the account again, after something changed it. */
  async function reload(): Promise<void> {
    setUser(await meApi.getMe())
  }

  /**
   * setLocale switches the language at once and, when signed in, saves it to
   * the profile so it follows the person to another device.
   */
  async function setLocale(locale: string): Promise<void> {
    applyLocale(locale)
    storeLocale(locale)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ locale })
    }
  }

  /** setTheme switches the theme at once and saves it like the language. */
  async function setTheme(theme: Theme): Promise<void> {
    applyTheme(theme)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ theme })
    }
  }

  /** setUnits switches the distance units at once and saves them like the theme. */
  async function setUnits(units: Units): Promise<void> {
    applyUnits(units)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ units })
    }
  }

  /** setDateFormat switches the date order at once and saves it like the units. */
  async function setDateFormat(format: DateFormat): Promise<void> {
    applyDateFormat(format)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ date_format: format })
    }
  }

  /** setTimeFormat switches the clock at once and saves it like the units. */
  async function setTimeFormat(format: TimeFormat): Promise<void> {
    applyTimeFormat(format)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ time_format: format })
    }
  }

  return {
    user, signedIn, isAdmin, mustChangePassword,
    setUser, restore, login, logout, forget, reload, setLocale, setTheme, setUnits,
    setDateFormat, setTimeFormat,
  }
})
