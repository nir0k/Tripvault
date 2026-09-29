<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { PACKING_ICON_KEYS, TAG_COLOR_CHOICES, type PackingIcon, type TagColor } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { PACKING_ICONS } from '@/components/icons'

// How a packing category looks: a colour of the tags' palette and an icon of
// the fixed set, both only telling the categories apart. The window answers
// with the choice rather than saving it, so a category being made and one
// already on the list choose alike.

/** PackingCategoryStyle is what the window chooses. */
export interface PackingCategoryStyle {
  color: TagColor
  icon: PackingIcon
}

const { t } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const color = ref<TagColor>('gray')
const icon = ref<PackingIcon>('other')
let settle: ((style: PackingCategoryStyle | null) => void) | null = null

/**
 * choose opens the window on a category's colour and icon.
 *
 * Returns:
 *   - the colour and icon chosen, or null when the window was closed without saving.
 */
function choose(current: PackingCategoryStyle): Promise<PackingCategoryStyle | null> {
  settle?.(null)
  color.value = current.color
  icon.value = current.icon
  dialog.value?.showModal()
  return new Promise((resolve) => {
    settle = resolve
  })
}

// finish closes the window and hands over the choice, or null.
function finish(style: PackingCategoryStyle | null): void {
  const done = settle
  settle = null
  dialog.value?.close()
  done?.(style)
}

defineExpose({ choose })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle" @close="finish(null)">
    <form class="modal-box flex flex-col gap-4" @submit.prevent="finish({ color, icon })">
      <h2 class="text-lg font-bold">{{ t('packing.styleTitle') }}</h2>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-2">{{ t('packing.color') }}</legend>
        <div role="radiogroup" :aria-label="t('packing.color')" class="flex flex-wrap gap-1">
          <button
            v-for="key in TAG_COLOR_CHOICES"
            :key="key"
            type="button"
            role="radio"
            class="btn btn-ghost btn-sm btn-square"
            :class="{ 'btn-active': key === color }"
            :aria-checked="key === color"
            :aria-label="t(`tags.colors.${key}`)"
            :title="t(`tags.colors.${key}`)"
            @click="color = key"
          >
            <span class="tag-swatch size-5 rounded-full" :class="`tag-${key}`"></span>
          </button>
        </div>
      </fieldset>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-2">{{ t('packing.icon') }}</legend>
        <div role="radiogroup" :aria-label="t('packing.icon')" class="flex flex-wrap gap-2">
          <button
            v-for="key in PACKING_ICON_KEYS"
            :key="key"
            type="button"
            role="radio"
            class="packing-badge btn btn-square btn-sm border-0"
            :class="[`tag-${color}`, key === icon ? 'ring-2 ring-current' : 'opacity-70 hover:opacity-100']"
            :aria-checked="key === icon"
            :aria-label="t(`packing.icons.${key}`)"
            :title="t(`packing.icons.${key}`)"
            @click="icon = key"
          >
            <AppIcon :name="PACKING_ICONS[key]" />
          </button>
        </div>
      </fieldset>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="finish(null)">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
