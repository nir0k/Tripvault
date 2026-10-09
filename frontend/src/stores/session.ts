import { defineStore } from 'pinia'
import { useTripStore } from '@/stores/trip'
import { computed, ref } from 'vue'
import * as authApi from '@/api/auth'
import { readRefreshToken, refreshSession } from '@/api/client'
import * as meApi from '@/api/me'
import { listThemes } from '@/api/themes'
import type { DateFormat, InstanceTheme, Theme, TimeFormat, Units, User } from '@/api/types'
import { applyLocale, readStoredLocale, resolveLocale, storeLocale } from '@/i18n'
import { applyDateFormat, applyTimeFormat } from '@/utils/display'
import { applyCustomTheme, applyTheme } from '@/utils/theme'
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
    if (user.value?.id !== next?.id) useTripStore().reset()
    user.value = next
    if (next) {
      applyLocale(resolveLocale(next.locale, readStoredLocale(), navigator.languages))
      applyTheme(next.theme)
      void syncCustomTheme(next.theme_id)
      applyUnits(next.units)
      applyDateFormat(next.date_format)
      applyTimeFormat(next.time_format)
    }
  }

  /**
   * syncCustomTheme shows the instance's theme the account chose. The page
   * keeps showing the copy this browser remembers until the palettes are
   * read, so an administrator's new file reaches the person on the next load,
   * and a theme deleted meanwhile gives way to the built-in one.
   */
  async function syncCustomTheme(themeId: string | null): Promise<void> {
    if (!themeId) {
      applyCustomTheme(null)
      return
    }
    try {
      const theme = (await listThemes()).find((item) => item.id === themeId)
      if (user.value?.theme_id === themeId) {
        applyCustomTheme(theme ?? null)
      }
    } catch {
      // A temporary password or an outage leaves the remembered copy shown.
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
    setUser(null)
  }

  /** forget drops the account locally when the server ended the session. */
  function forget(): void {
    setUser(null)
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

  /**
   * setCustomTheme shows one of the instance's themes at once, or the
   * built-in one for null, and saves the choice like the theme preference.
   */
  async function setCustomTheme(theme: InstanceTheme | null): Promise<void> {
    applyCustomTheme(theme)
    if (user.value && !user.value.must_change_password) {
      user.value = await meApi.updateMe({ theme_id: theme?.id ?? null })
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
    setUser, restore, login, logout, forget, reload, setLocale, setTheme, setCustomTheme, setUnits,
    setDateFormat, setTimeFormat,
  }
})
