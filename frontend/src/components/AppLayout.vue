<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import AppLogo from '@/components/AppLogo.vue'
import AppSearch from '@/components/AppSearch.vue'
import LanguageDialog from '@/components/LanguageDialog.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import UserAvatar from '@/components/UserAvatar.vue'
import { useDropdown } from '@/composables/useDropdown'
import { useSessionStore } from '@/stores/session'

interface NavItem {
  name: string
  label: string
  icon: IconName
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const session = useSessionStore()
const userMenu = useDropdown('accountMenu')
const adminDropdown = useDropdown('adminMenu')
const languageDialog = useTemplateRef<InstanceType<typeof LanguageDialog>>('languageDialog')

// Plans and reports are separate trips in separate sections: a journey being
// prepared, and one written up afterwards. The start page shows a few of each.
const mainItems = computed<NavItem[]>(() => [
  { name: 'home', label: t('nav.home'), icon: 'home' },
  { name: 'trips', label: t('nav.trips'), icon: 'map' },
  { name: 'reports', label: t('nav.reports'), icon: 'report' },
])
const adminItems = computed<NavItem[]>(() =>
  session.isAdmin
    ? [
        { name: 'admin-users', label: t('nav.users'), icon: 'users' },
        { name: 'admin-backups', label: t('nav.backups'), icon: 'archive' },
        { name: 'admin-status', label: t('nav.status'), icon: 'status' },
      ]
    : [],
)

// The bottom bar has room for a few destinations only: administration is one
// entry there, opening on the accounts page.
const dockItems = computed<NavItem[]>(() => [
  ...mainItems.value,
  ...(session.isAdmin ? [{ name: 'admin-users', label: t('nav.administration'), icon: 'users' as const }] : []),
  { name: 'profile', label: t('nav.profile'), icon: 'user' },
])

const inAdmin = computed(() => String(route.name).startsWith('admin-'))

// isActive marks a destination as current, counting every administration page
// as the administration entry of the bottom bar, and every page of a plan or a
// report as its own list.
function isActive(name: string): boolean {
  if (name === 'admin-users' && inAdmin.value) {
    return true
  }
  if ((name === 'trips' || name === 'reports') && route.meta.kind !== undefined) {
    return route.meta.kind === (name === 'reports' ? 'report' : 'plan')
  }
  return route.name === name
}

// openLanguages folds the account menu and asks for the language in a window of
// its own: a list of languages does not belong in a menu, and every one of them
// has to be readable in its own name.
function openLanguages(): void {
  userMenu.close()
  languageDialog.value?.open()
}

// signOut ends the session and returns to the sign-in page.
async function signOut(): Promise<void> {
  userMenu.close()
  await session.logout()
  await router.push({ name: 'login' })
}
</script>

<template>
  <div class="flex min-h-dvh flex-col bg-base-100">
    <header class="navbar sticky top-0 z-30 gap-2 border-b border-base-300 bg-base-200 px-4">
      <div class="flex min-w-0 flex-1 items-center gap-1">
        <RouterLink :to="{ name: 'home' }" class="mr-2 shrink-0">
          <AppLogo />
        </RouterLink>

        <nav class="hidden items-center gap-1 lg:flex" :aria-label="t('nav.main')">
          <RouterLink
            v-for="item in mainItems"
            :key="item.name"
            :to="{ name: item.name }"
            class="btn btn-ghost btn-sm"
            :class="{ 'bg-primary text-primary-content': isActive(item.name) }"
          >
            <AppIcon :name="item.icon" />
            {{ item.label }}
          </RouterLink>

          <details v-if="adminItems.length > 0" ref="adminMenu" class="dropdown">
            <summary class="btn btn-ghost btn-sm" :class="{ 'bg-primary text-primary-content': inAdmin }">
              <AppIcon name="users" />
              {{ t('nav.administration') }}
              <AppIcon name="chevronDown" class="size-3!" />
            </summary>
            <ul class="menu dropdown-content z-30 mt-1 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
              <li v-for="item in adminItems" :key="item.name">
                <RouterLink
                  :to="{ name: item.name }"
                  :class="{ 'menu-active': route.name === item.name }"
                  @click="adminDropdown.close()"
                >
                  <AppIcon :name="item.icon" />
                  {{ item.label }}
                </RouterLink>
              </li>
            </ul>
          </details>
        </nav>
      </div>

      <div class="flex min-w-0 shrink-0 items-center gap-1">
        <AppSearch />
        <ThemeSelect />

        <details ref="accountMenu" class="dropdown dropdown-end">
          <summary class="btn btn-ghost btn-sm btn-square" :aria-label="t('nav.userMenu')">
            <UserAvatar
              v-if="session.user"
              :user-id="session.user.id"
              :has-avatar="session.user.has_avatar"
              :updated-at="session.user.avatar_updated_at"
            />
          </summary>
          <div class="dropdown-content z-30 mt-1 w-60 space-y-2 rounded-box border border-base-300 bg-base-100 p-3 shadow-lg">
            <p class="truncate px-1 font-semibold">{{ session.user?.display_name }}</p>
            <RouterLink :to="{ name: 'profile' }" class="btn btn-ghost btn-sm w-full justify-start" @click="userMenu.close()">
              <AppIcon name="user" />
              {{ t('nav.profile') }}
            </RouterLink>
            <button type="button" class="btn btn-ghost btn-sm w-full justify-start" @click="openLanguages">
              <AppIcon name="globe" />
              {{ t('preferences.language') }}
            </button>
            <button type="button" class="btn btn-ghost btn-sm w-full justify-start" @click="signOut">
              <AppIcon name="logout" />
              {{ t('nav.signOut') }}
            </button>
          </div>
        </details>
      </div>
    </header>

    <!-- isolate keeps the page below the navbar: a map paints its own layers
         with z-indexes of its own, which would otherwise cover the bar. -->
    <main class="isolate mx-auto w-full flex-1 p-4 pb-24 lg:p-8" :class="route.meta.wide ? 'max-w-none' : 'max-w-6xl'">
      <RouterView />
    </main>

    <nav class="dock dock-sm border-t border-base-300 bg-base-200 lg:hidden" :aria-label="t('nav.main')">
      <RouterLink
        v-for="item in dockItems"
        :key="item.name"
        :to="{ name: item.name }"
        :class="{ 'dock-active': isActive(item.name) }"
      >
        <AppIcon :name="item.icon" />
        <span class="dock-label">{{ item.label }}</span>
      </RouterLink>
    </nav>

    <!-- Outside the account menu: a dialog inside it would be hidden with it. -->
    <LanguageDialog ref="languageDialog" />
  </div>
</template>
