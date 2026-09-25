<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { readShareToken, storeShareToken } from '@/api/client'
import { SHARED_MEDIA_BASE } from '@/api/media'
import AppLogo from '@/components/AppLogo.vue'
import LanguageSelect from '@/components/LanguageSelect.vue'
import ThemeSelect from '@/components/ThemeSelect.vue'
import UnitsSelect from '@/components/UnitsSelect.vue'
import { provideMediaBase } from '@/composables/useMediaUrl'
import { useTripStore } from '@/stores/trip'
import TripLayout from '@/views/trip/TripLayout.vue'

// A trip opened by a read-only link, without an account. The link opens the
// pages a member reads - the same layout, the same document and photo pages -
// with the trip read through the link and every way of changing it left out,
// so the two never drift apart. This page only does what a link needs and an
// account does not: it takes the token, and it stands in for the application's
// menu with a bar of its own.
//
// The token arrives in the fragment of the address, which browsers never send to
// a server. This page takes it into sessionStorage and removes it from the
// address bar at once, so it does not sit in the history or get copied out of it
// by accident; the API client then sends it in a header.

const { t } = useI18n()
const store = useTripStore()

// The pictures of these pages are read through the link, not through an
// account: the token travels in a header either way, but the paths are different.
provideMediaBase(SHARED_MEDIA_BASE)

/**
 * takeToken moves the token from the fragment into this tab's storage and clears
 * the address bar. Without a fragment the token stored earlier is kept, so a
 * reload of the page still works.
 */
function takeToken(): void {
  const fragment = window.location.hash.replace(/^#/, '')
  const found = /(?:^|&)token=([^&]*)/.exec(fragment)?.[1]
  if (!found) {
    return
  }
  storeShareToken(decodeURIComponent(found))
  history.replaceState(null, '', window.location.pathname + window.location.search)
}

// The token is taken and the trip asked for before anything below is drawn, so
// no page of the trip reads anything without the link, nor shows a trip opened
// earlier in this tab.
takeToken()
const hasToken = Boolean(readShareToken())
if (hasToken) {
  void store.loadShared()
}

// A search engine must not keep a link somebody was given in confidence. The
// served page carries an X-Robots-Tag as well; this covers the rest.
const robots = document.createElement('meta')
robots.name = 'robots'
robots.content = 'noindex, nofollow'
onMounted(() => document.head.append(robots))
onBeforeUnmount(() => robots.remove())
</script>

<template>
  <div class="flex min-h-dvh flex-col bg-base-200">
    <header class="border-b border-base-300 bg-base-100">
      <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3 lg:px-8">
        <AppLogo />
        <div class="flex items-center gap-2">
          <LanguageSelect />
          <UnitsSelect />
          <ThemeSelect />
        </div>
      </div>
    </header>

    <!-- isolate keeps the pages below the bar, as the application's own frame does. -->
    <main class="isolate w-full flex-1 p-4 pb-24 lg:p-8">
      <p v-if="!hasToken" role="alert" class="alert alert-error">{{ t('errors.invalid_share_token') }}</p>
      <!-- The layout says itself that the trip is loading, or why it cannot be. -->
      <TripLayout v-else />
    </main>
  </div>
</template>
