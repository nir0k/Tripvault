<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Units } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'
import { useSessionStore } from '@/stores/session'
import { activeUnits } from '@/utils/units'

// Which units distances are shown in. It sits beside the language and the theme
// because it is the same kind of choice: about the reader, not about the trip.
const props = withDefaults(defineProps<{
  align?: 'start' | 'end'
  /** Name the chosen units next to the icon, for a settings page with room. */
  labelled?: boolean
}>(), { align: 'end', labelled: false })

const { t } = useI18n()
const session = useSessionStore()
const { close } = useDropdown('menu')

const units: Units[] = ['km', 'mi']

// choose applies the units at once, saves them when signed in and folds the menu.
function choose(value: Units): void {
  close()
  void session.setUnits(value).catch(() => {})
}
</script>

<template>
  <details ref="menu" class="dropdown" :class="{ 'dropdown-end': align === 'end' }">
    <summary
      class="btn btn-ghost btn-sm gap-1 px-2"
      :aria-label="`${t('preferences.units')}: ${t(`preferences.unitNames.${activeUnits}`)}`"
      :title="t('preferences.units')"
    >
      <AppIcon name="ruler" />
      <span v-if="props.labelled">{{ t(`preferences.unitNames.${activeUnits}`) }}</span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul class="menu dropdown-content z-20 mt-1 w-44 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li v-for="value in units" :key="value">
        <button
          type="button"
          :class="{ 'menu-active': value === activeUnits }"
          :aria-pressed="value === activeUnits"
          @click="choose(value)"
        >
          {{ t(`preferences.unitNames.${value}`) }}
        </button>
      </li>
    </ul>
  </details>
</template>
