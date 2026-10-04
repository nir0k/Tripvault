<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { InstanceTheme, ThemePalette, ThemeVariant } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ThemePreview from '@/components/ThemePreview.vue'
import { useSessionStore } from '@/stores/session'
import { activeCustomThemeId } from '@/utils/theme'

const props = defineProps<{
  /** The instance's themes to choose from, besides the built-in one. */
  themes: InstanceTheme[]
}>()

const emit = defineEmits<{ error: [unknown] }>()

const { t } = useI18n()
const session = useSessionStore()

interface Option {
  id: string | null
  name: string
  theme: InstanceTheme | null
  previews: { variant: ThemeVariant; palette: ThemePalette | null }[]
}

// The built-in theme comes first, shown in both its palettes like every
// theme that has both, and a theme with one palette is shown in that one.
const options = computed<Option[]>(() => [
  {
    id: null,
    name: t('themes.builtin'),
    theme: null,
    previews: [{ variant: 'light', palette: null }, { variant: 'dark', palette: null }],
  },
  ...props.themes.map((theme) => ({
    id: theme.id,
    name: theme.name,
    theme,
    previews: [
      ...(theme.light ? [{ variant: 'light' as const, palette: theme.light }] : []),
      ...(theme.dark ? [{ variant: 'dark' as const, palette: theme.dark }] : []),
    ],
  })),
])

// choose shows the theme at once and saves it to the profile.
function choose(option: Option): void {
  session.setCustomTheme(option.theme).catch((err: unknown) => emit('error', err))
}
</script>

<template>
  <div class="grid gap-3 lg:grid-cols-2" role="radiogroup" :aria-label="t('themes.colourTheme')">
    <!-- A card rather than a button: the preview inside it draws inputs, which
         a button may not hold. It is inert, so the card is the one target. -->
    <div
      v-for="option in options"
      :key="option.id ?? 'builtin'"
      role="radio"
      tabindex="0"
      :aria-checked="option.id === activeCustomThemeId"
      class="cursor-pointer space-y-2 rounded-box border-2 p-3 transition-colors focus-visible:outline-2 focus-visible:outline-primary"
      :class="option.id === activeCustomThemeId ? 'border-primary' : 'border-base-300 hover:border-base-content/30'"
      @click="choose(option)"
      @keydown.enter.prevent="choose(option)"
      @keydown.space.prevent="choose(option)"
    >
      <div class="flex items-center gap-2">
        <span class="min-w-0 flex-1 truncate font-medium">{{ option.name }}</span>
        <span v-if="option.previews.length === 1" class="badge badge-ghost badge-sm">
          {{ t(`themes.only.${option.previews[0]?.variant}`) }}
        </span>
        <AppIcon v-if="option.id === activeCustomThemeId" name="check" class="text-primary" />
      </div>
      <div class="grid gap-2" :class="{ 'sm:grid-cols-2': option.previews.length === 2 }">
        <ThemePreview
          v-for="preview in option.previews"
          :key="preview.variant"
          :palette="preview.palette"
          :variant="preview.variant"
        />
      </div>
    </div>
  </div>
</template>
