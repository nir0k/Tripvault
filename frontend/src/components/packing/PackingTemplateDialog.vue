<script setup lang="ts">
import { computed, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PackingAddCategory } from '@/api/packing'
import type { PackingList } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { PACKING_ICONS } from '@/components/icons'
import { PACKING_TEMPLATES, templateAdded, templateSections, type PackingTemplate } from '@/utils/packing'

// The ready lists an editor adds to a plan's packing list: each template shows
// what it brings, in the reader's words, and one the list already holds whole
// is marked as added. The window hands the chosen template over rather than
// saving it, like the window of a category's look.

const props = defineProps<{
  list: PackingList | null
  busy: boolean
}>()

const emit = defineEmits<{
  add: [categories: PackingAddCategory[]]
}>()

const { t, locale } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')

// Every template in the reader's words, with whether the list holds it whole.
const templates = computed(() => PACKING_TEMPLATES.map((template: PackingTemplate) => {
  const sections = templateSections(template, t)
  return {
    template,
    sections,
    count: sections.reduce((sum, section) => sum + section.items.length, 0),
    added: props.list !== null && templateAdded(sections, props.list, locale.value),
  }
}))

// open shows the window.
function open(): void {
  dialog.value?.showModal()
}

// add hands a template over and closes the window.
function add(sections: PackingAddCategory[]): void {
  emit('add', sections)
  dialog.value?.close()
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <div class="modal-box flex flex-col gap-4">
      <h2 class="text-lg font-bold">{{ t('packing.templatesTitle') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('packing.templatesHint') }}</p>

      <ul class="flex flex-col gap-3">
        <li
          v-for="entry in templates"
          :key="entry.template.key"
          class="flex items-start gap-3 rounded-box border border-base-300 p-3"
        >
          <span
            class="packing-badge flex size-8 shrink-0 items-center justify-center rounded-field"
            :class="`tag-${entry.template.categories[0]?.color ?? 'gray'}`"
            aria-hidden="true"
          >
            <AppIcon :name="PACKING_ICONS[entry.template.icon]" />
          </span>
          <div class="min-w-0 flex-1">
            <p class="font-semibold">
              {{ t(`packing.templates.${entry.template.key}`) }}
              <span class="text-sm font-normal text-base-content/60">· {{ t('packing.templateItemCount', entry.count) }}</span>
            </p>
            <!-- A template of one category lists its things; one of several
                 names each category before them. -->
            <p v-for="section in entry.sections" :key="section.name" class="text-sm text-base-content/70">
              <span v-if="entry.sections.length > 1" class="font-medium text-base-content">{{ section.name }}: </span>
              {{ section.items.map((item) => (item.quantity ? `${item.name} ×${item.quantity}` : item.name)).join(', ') }}
            </p>
          </div>
          <span v-if="entry.added" class="badge badge-ghost shrink-0 gap-1">
            <AppIcon name="check" class="size-3.5!" />
            {{ t('packing.templateAdded') }}
          </span>
          <button
            v-else
            type="button"
            class="btn btn-sm shrink-0"
            :disabled="busy"
            :aria-label="t('packing.templateAddName', { name: t(`packing.templates.${entry.template.key}`) })"
            @click="add(entry.sections)"
          >
            <AppIcon name="plus" />
            {{ t('packing.templateAdd') }}
          </button>
        </li>
      </ul>

      <div class="modal-action">
        <form method="dialog">
          <button type="submit" class="btn btn-ghost">{{ t('common.close') }}</button>
        </form>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
