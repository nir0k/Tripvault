<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from 'vue'
import { VueDraggable, type DraggableEvent } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import type { PackingIcon, PackingItem, TagColor } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { PACKING_ICONS } from '@/components/icons'
import { useDropdownGroup } from '@/composables/useDropdown'
import { copyText } from '@/utils/clipboard'
import { packingText, parseQuickItem } from '@/utils/packing'

// One category of the packing list, in its colour and with its icon: its items,
// each ticked with one tap, and a line at the bottom that adds the next one on
// Enter and stays for the one after. The category is dragged by its heading,
// items between categories by their handle; the list redraws from what the
// server returns.
const props = defineProps<{
  /** The category, or null for the items without one. */
  categoryId: string | null
  title: string
  color: TagColor
  icon: PackingIcon
  items: PackingItem[]
  /** Who brings what, by member identifier. */
  bringers: Record<string, string>
  canEdit: boolean
  /** Packed items are left out; dragging is off while the list is filtered. */
  hidePacked: boolean
  /** The name, colour and icon of a category are changed here; the items without one have none. */
  renamable: boolean
}>()

const emit = defineEmits<{
  toggle: [item: PackingItem, packed: boolean]
  add: [categoryId: string | null, name: string, quantity: number]
  edit: [item: PackingItem]
  remove: [item: PackingItem]
  move: [itemId: string, categoryId: string | null, position: number]
  rename: [name: string]
  restyle: []
  delete: []
}>()

const { t } = useI18n()
const menus = useDropdownGroup()

// The drag library reorders this copy while dragging; it is replaced whenever
// the list changes.
const local = ref<PackingItem[]>([...props.items])
watch(() => props.items, (items) => {
  local.value = [...items]
})
const shown = computed(() => (props.hidePacked ? local.value.filter((item) => !item.packed) : local.value))
const packed = computed(() => props.items.filter((item) => item.packed).length)

// onDrop reports an item dropped into this category or moved within it.
function onDrop(event: DraggableEvent<PackingItem>): void {
  const item = event.data
  if (item) {
    emit('move', item.id, props.categoryId, event.newDraggableIndex ?? event.newIndex ?? 0)
  }
}

// draft is the line that adds an item.
const draft = ref('')
const draftInput = useTemplateRef<HTMLInputElement>('draftInput')

// addItem adds what the line holds and empties it, keeping the focus there
// for the next item.
function addItem(): void {
  const { name, quantity } = parseQuickItem(draft.value)
  if (name === '') {
    return
  }
  emit('add', props.categoryId, name, quantity)
  draft.value = ''
  void nextTick(() => draftInput.value?.focus())
}

// renaming is the name being typed while the category is renamed.
const renaming = ref<string | null>(null)
const renameInput = useTemplateRef<HTMLInputElement>('renameInput')

// startRename turns the title into a field.
function startRename(event: Event): void {
  menus.close(event)
  renaming.value = props.title
  void nextTick(() => renameInput.value?.select())
}

// finishRename stores a changed name; an empty or unchanged one changes nothing.
function finishRename(): void {
  const name = renaming.value?.trim() ?? ''
  renaming.value = null
  if (name !== '' && name !== props.title) {
    emit('rename', name)
  }
}

// copied says the category has just been copied, which its tooltip says for a moment.
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

// copy puts the category on the clipboard as text for a chat: its name and
// its items, packed or not.
async function copy(): Promise<void> {
  if (await copyText(packingText([{ title: props.title, items: props.items }], props.bringers, null))) {
    copied.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copied.value = false
    }, 1500)
  }
}

// choose folds the menu of a row and passes on what was chosen in it.
function choose(event: Event, action: () => void): void {
  menus.close(event)
  action()
}
</script>

<template>
  <section class="packing-card rounded-box border p-3" :class="`tag-${color}`">
    <!-- The whole heading is what a category is dragged by; its fields and
         buttons are left out of the drag by the list's filter. -->
    <header
      class="mb-2 flex items-center gap-2"
      :class="{ 'category-handle cursor-grab active:cursor-grabbing': canEdit && renamable }"
      :title="canEdit && renamable ? t('packing.dragCategory') : undefined"
    >
      <span v-if="canEdit && renamable" class="text-base-content/40 select-none" aria-hidden="true">⋮⋮</span>
      <button
        v-if="canEdit && renamable"
        type="button"
        class="packing-badge btn btn-sm btn-square border-0"
        :aria-label="t('packing.styleOf', { name: title })"
        :title="t('packing.styleOf', { name: title })"
        @click="emit('restyle')"
      >
        <AppIcon :name="PACKING_ICONS[icon]" />
      </button>
      <span v-else class="packing-badge flex size-8 shrink-0 items-center justify-center rounded-field" aria-hidden="true">
        <AppIcon :name="PACKING_ICONS[icon]" />
      </span>
      <input
        v-if="renaming !== null"
        ref="renameInput"
        v-model="renaming"
        type="text"
        maxlength="100"
        class="input input-sm min-w-0 flex-1"
        :aria-label="t('packing.categoryName')"
        @keydown.enter.prevent="finishRename"
        @keydown.escape.prevent="renaming = null"
        @blur="finishRename"
      />
      <h2 v-else class="min-w-0 flex-1 font-semibold break-words">{{ title }}</h2>
      <span class="badge badge-ghost badge-sm tabular-nums">{{ packed }}/{{ items.length }}</span>
      <span v-if="items.length > 0" class="tooltip" :data-tip="copied ? t('packing.copied') : t('packing.copyCategory')">
        <button
          type="button"
          class="btn btn-ghost btn-sm btn-square"
          :aria-label="t('packing.copyCategoryOf', { name: title })"
          @click="copy"
        >
          <AppIcon :name="copied ? 'check' : 'clipboardCopy'" class="size-4!" />
        </button>
      </span>
      <details v-if="canEdit && renamable" class="dropdown dropdown-end">
        <summary class="btn btn-ghost btn-sm btn-square" :aria-label="t('packing.categoryActions', { name: title })">
          <AppIcon name="dots" />
        </summary>
        <ul class="menu dropdown-content z-20 mt-1 w-48 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
          <li><button type="button" @click="startRename">{{ t('packing.rename') }}</button></li>
          <li><button type="button" @click="choose($event, () => emit('restyle'))">{{ t('packing.style') }}</button></li>
          <li><button type="button" class="text-error" @click="choose($event, () => emit('delete'))">{{ t('packing.deleteCategory') }}</button></li>
        </ul>
      </details>
    </header>

    <VueDraggable
      v-model="local"
      :group="{ name: 'packing', pull: true, put: true }"
      :disabled="!canEdit || hidePacked"
      handle=".item-handle"
      :animation="150"
      ghost-class="opacity-40"
      class="flex min-h-8 flex-col"
      @add="onDrop"
      @update="onDrop"
    >
      <div
        v-for="item in local"
        v-show="!hidePacked || !item.packed"
        :key="item.id"
        class="group flex items-start gap-2 rounded-field px-1 py-1.5 hover:bg-base-200"
      >
        <span v-if="canEdit && !hidePacked" class="item-handle mt-0.5 cursor-grab text-base-content/30" aria-hidden="true">⋮⋮</span>
        <input
          type="checkbox"
          class="checkbox checkbox-sm checkbox-success mt-0.5"
          :checked="item.packed"
          :disabled="!canEdit"
          :aria-label="t('packing.markPacked', { name: item.name })"
          @change="emit('toggle', item, ($event.target as HTMLInputElement).checked)"
        />
        <div class="min-w-0 flex-1">
          <p class="break-words" :class="{ 'text-base-content/50 line-through': item.packed }">
            {{ item.name }}
            <span v-if="item.quantity > 1" class="ms-1 text-sm text-base-content/60 tabular-nums">×{{ item.quantity }}</span>
          </p>
          <p v-if="(item.bringer_id && bringers[item.bringer_id]) || item.note" class="text-xs text-base-content/60">
            <span v-if="item.bringer_id && bringers[item.bringer_id]" class="font-medium">{{ bringers[item.bringer_id] }}</span>
            <span v-if="item.bringer_id && bringers[item.bringer_id] && item.note" aria-hidden="true"> · </span>
            <span v-if="item.note">{{ item.note }}</span>
          </p>
        </div>
        <details v-if="canEdit" class="dropdown dropdown-end">
          <summary class="btn btn-ghost btn-xs btn-square" :aria-label="t('packing.itemActions', { name: item.name })">
            <AppIcon name="dots" />
          </summary>
          <ul class="menu dropdown-content z-20 mt-1 w-44 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
            <li><button type="button" @click="choose($event, () => emit('edit', item))">{{ t('packing.edit') }}</button></li>
            <li><button type="button" class="text-error" @click="choose($event, () => emit('remove', item))">{{ t('packing.delete') }}</button></li>
          </ul>
        </details>
      </div>
    </VueDraggable>
    <p v-if="items.length > 0 && shown.length === 0" class="px-1 py-1.5 text-sm text-base-content/60">{{ t('packing.allPacked') }}</p>

    <form v-if="canEdit" class="mt-1 flex items-center gap-2" @submit.prevent="addItem">
      <AppIcon name="plus" class="ms-1 text-base-content/40" />
      <input
        ref="draftInput"
        v-model="draft"
        type="text"
        maxlength="210"
        class="input input-sm input-ghost min-w-0 flex-1"
        :placeholder="t('packing.addItem')"
        :aria-label="t('packing.addItemTo', { name: title })"
      />
    </form>
  </section>
</template>
