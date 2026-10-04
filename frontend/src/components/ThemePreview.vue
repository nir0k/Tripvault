<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ThemePalette, ThemeVariant } from '@/api/types'
import { paletteStyle } from '@/utils/theme'

const props = defineProps<{
  /** The palette to show; null shows the built-in one of the variant. */
  palette: ThemePalette | null
  variant: ThemeVariant
}>()

const { t } = useI18n()

// The colours the strip shows: the page's three backgrounds and its text,
// then the colours things are marked with. The "-content" colours are seen
// on the buttons and badges below rather than as squares of their own.
const SWATCHES = [
  'base-100', 'base-200', 'base-300', 'base-content', 'primary', 'secondary', 'accent', 'neutral',
  'info', 'success', 'warning', 'error',
] as const

// The element takes the variant's built-in palette by its data-theme and the
// theme's colours over it as its own variables, so everything inside is drawn
// in them whatever the page around it is shown in.
const style = computed(() => paletteStyle(props.palette))
</script>

<template>
  <div
    :data-theme="variant"
    :style="style"
    class="overflow-hidden rounded-box border border-base-300 bg-base-100 text-base-content"
    inert
  >
    <div class="flex">
      <span
        v-for="name in SWATCHES"
        :key="name"
        class="h-4 flex-1"
        :style="{ background: `var(--color-${name})` }"
        :title="name"
      ></span>
    </div>
    <div class="flex items-center justify-between gap-2 bg-base-200 px-3 py-2">
      <span class="truncate text-sm font-semibold">{{ t('themes.preview.title') }}</span>
      <span class="badge badge-primary badge-sm">{{ t('themes.preview.badge') }}</span>
    </div>
    <div class="space-y-2 p-3">
      <p class="text-sm">
        {{ t('themes.preview.text') }}
        <span class="text-base-content/60">{{ t('themes.preview.muted') }}</span>
      </p>
      <div class="flex flex-wrap gap-1">
        <span class="btn btn-primary btn-xs">{{ t('themes.preview.primary') }}</span>
        <span class="btn btn-secondary btn-xs">{{ t('themes.preview.secondary') }}</span>
        <span class="btn btn-accent btn-xs">{{ t('themes.preview.accent') }}</span>
        <span class="btn btn-neutral btn-xs">{{ t('themes.preview.neutral') }}</span>
      </div>
      <div class="flex flex-wrap gap-1">
        <span class="badge badge-info badge-sm">{{ t('themes.preview.info') }}</span>
        <span class="badge badge-success badge-sm">{{ t('themes.preview.success') }}</span>
        <span class="badge badge-warning badge-sm">{{ t('themes.preview.warning') }}</span>
        <span class="badge badge-error badge-sm">{{ t('themes.preview.error') }}</span>
      </div>
      <div class="flex items-center gap-2">
        <input type="text" class="input input-xs min-w-0 flex-1" :value="t('themes.preview.input')" readonly tabindex="-1" />
        <input type="checkbox" class="toggle toggle-primary toggle-xs" checked tabindex="-1" />
      </div>
      <p class="rounded-field border border-base-300 bg-base-300/40 px-2 py-1 text-xs">
        <span class="text-error">{{ t('themes.preview.errorText') }}</span>
        ·
        <span class="text-success">{{ t('themes.preview.successText') }}</span>
      </p>
    </div>
  </div>
</template>
