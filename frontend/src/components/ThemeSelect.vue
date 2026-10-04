<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Theme } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'
import { useSessionStore } from '@/stores/session'
import { computed } from 'vue'
import { activeTheme, lockedVariant } from '@/utils/theme'

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

// The half circle and the moon are solid shapes that fill their whole box,
// while the sun beside them and the icons of the other selects are outlines
// drawn with a margin; a step smaller, the solid ones look the same size.
const SOLID: IconName[] = ['circleHalf', 'moonStarsFill']

// sizeOf returns the class that evens a theme icon out with the outline ones.
function sizeOf(icon: IconName): string {
  return SOLID.includes(icon) ? 'size-4! m-0.5' : ''
}

// iconOf returns the picture that stands for a theme preference.
function iconOf(theme: Theme): IconName {
  return themes.find((item) => item.value === theme)?.icon ?? 'circleHalf'
}

// shown is the preference the control displays: the one palette of a theme
// that has only one, which no preference changes, or the preference itself.
const shown = computed<Theme>(() => lockedVariant.value ?? activeTheme.value)

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
      :aria-label="`${t('preferences.theme')}: ${t(`preferences.themes.${shown}`)}`"
      :title="t('preferences.theme')"
    >
      <AppIcon :name="iconOf(shown)" :class="sizeOf(iconOf(shown))" />
      <span v-if="props.labelled">{{ t(`preferences.themes.${shown}`) }}</span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul class="menu dropdown-content z-20 mt-1 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li v-if="lockedVariant" class="menu-title text-xs font-normal whitespace-normal">
        {{ t(`themes.locked.${lockedVariant}`) }}
      </li>
      <li v-for="item in themes" :key="item.value" :class="{ 'menu-disabled': lockedVariant }">
        <button
          type="button"
          :disabled="lockedVariant !== null"
          :class="{ 'menu-active': item.value === shown }"
          :aria-pressed="item.value === shown"
          @click="choose(item.value)"
        >
          <AppIcon :name="item.icon" :class="sizeOf(item.icon)" />
          {{ t(`preferences.themes.${item.value}`) }}
        </button>
      </li>
    </ul>
  </details>
</template>
