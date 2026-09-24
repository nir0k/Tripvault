<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'
import { useLanguages } from '@/composables/useLanguages'
import { LANGUAGE_NAMES } from '@/i18n'
import { useSessionStore } from '@/stores/session'

// The interface language, as a dropdown built like the theme and the distance
// controls beside it: the same kind of choice, about the reader rather than
// about the trip. The languages are grouped the way the window in the
// navigation bar groups them - the likely ones first, the rest below.
const props = withDefaults(defineProps<{
  align?: 'start' | 'end'
  /** Name the chosen language next to the icon, for a settings page with room. */
  labelled?: boolean
}>(), { align: 'end', labelled: false })

const { t, locale } = useI18n()
const session = useSessionStore()
const { close } = useDropdown('menu')
const { recommended, rest } = useLanguages()

// choose applies the language at once, saves it when signed in and folds the
// menu. Saving may fail without taking the choice away from this browser.
function choose(code: string): void {
  close()
  void session.setLocale(code).catch(() => {})
}
</script>

<template>
  <details ref="menu" class="dropdown" :class="{ 'dropdown-end': align === 'end' }">
    <summary
      class="btn btn-ghost btn-sm gap-1 px-2"
      :aria-label="`${t('preferences.language')}: ${LANGUAGE_NAMES[locale] ?? locale}`"
      :title="t('preferences.language')"
    >
      <AppIcon name="globe" />
      <span v-if="props.labelled">{{ LANGUAGE_NAMES[locale] ?? locale }}</span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul class="menu dropdown-content z-20 mt-1 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li class="menu-title">{{ t('preferences.languageRecommended') }}</li>
      <li v-for="language in recommended" :key="language.code">
        <button
          type="button"
          :class="{ 'menu-active': language.code === locale }"
          :aria-pressed="language.code === locale"
          @click="choose(language.code)"
        >
          {{ language.name }}
        </button>
      </li>
      <template v-if="rest.length > 0">
        <li class="menu-title">{{ t('preferences.languageAll') }}</li>
        <li v-for="language in rest" :key="language.code">
          <button
            type="button"
            :class="{ 'menu-active': language.code === locale }"
            :aria-pressed="language.code === locale"
            @click="choose(language.code)"
          >
            {{ language.name }}
          </button>
        </li>
      </template>
    </ul>
  </details>
</template>
