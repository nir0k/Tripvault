<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Theme } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'
import { useSessionStore } from '@/stores/session'
import { activeTheme } from '@/utils/theme'

const props = withDefaults(defineProps<{
  align?: 'start' | 'end'
  /** Name the chosen theme next to its icon, for a settings page with room for it. */
  labelled?: boolean
}>(), { align: 'end', labelled: false })

const { t } = useI18n()
const session = useSessionStore()
const { close } = useDropdown('menu')

const themes: { value: Theme; icon: IconName }[] = [
  { value: 'auto', icon: 'circleHalf' },
  { value: 'light', icon: 'sun' },
  { value: 'dark', icon: 'moonStarsFill' },
]

// iconOf returns the picture that stands for a theme preference.
function iconOf(theme: Theme): IconName {
  return themes.find((item) => item.value === theme)?.icon ?? 'circleHalf'
}

// choose applies the theme at once, saves it when signed in and folds the menu.
function choose(theme: Theme): void {
  close()
  void session.setTheme(theme).catch(() => {})
}
</script>

<template>
  <details ref="menu" class="dropdown" :class="{ 'dropdown-end': align === 'end' }">
    <summary
      class="btn btn-ghost btn-sm gap-1 px-2"
      :aria-label="`${t('preferences.theme')}: ${t(`preferences.themes.${activeTheme}`)}`"
      :title="t('preferences.theme')"
    >
      <AppIcon :name="iconOf(activeTheme)" />
      <span v-if="props.labelled">{{ t(`preferences.themes.${activeTheme}`) }}</span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul class="menu dropdown-content z-20 mt-1 w-44 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li v-for="item in themes" :key="item.value">
        <button
          type="button"
          :class="{ 'menu-active': item.value === activeTheme }"
          :aria-pressed="item.value === activeTheme"
          @click="choose(item.value)"
        >
          <AppIcon :name="item.icon" />
          {{ t(`preferences.themes.${item.value}`) }}
        </button>
      </li>
    </ul>
  </details>
</template>
