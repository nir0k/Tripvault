<script setup lang="ts">
import { nextTick, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { TAG_COLOR_CHOICES, type Tag, type TagColor } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import TagPill from '@/components/TagPill.vue'
import { useTagsStore } from '@/stores/tags'
import { tagError } from '@/utils/tags'

// The window a person manages their own tags in: every tag in its colour with
// the number of trips wearing it, renamed in place, recoloured or deleted. A
// tag gets its colour when it is made and keeps it until it is changed here. A tag is put on a trip from
// the trip's own page; here it can also be made in advance.
const { t, te } = useI18n()
const store = useTagsStore()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const renameInput = useTemplateRef<HTMLInputElement[]>('renameInput')
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const newName = ref('')
// editing is the tag being renamed, and draft the name it is getting.
const editing = ref<string | null>(null)
const draft = ref('')
// colouring is the tag whose palette is open.
const colouring = ref<string | null>(null)

/** open shows the window with the list read afresh. */
async function open(): Promise<void> {
  error.value = ''
  newName.value = ''
  editing.value = null
  colouring.value = null
  dialog.value?.showModal()
  loading.value = true
  try {
    await store.load(true)
  } catch (err) {
    error.value = tagError(err, t, te)
  } finally {
    loading.value = false
  }
}

// run carries out one change, showing why it failed and reporting success.
async function run(action: () => Promise<unknown>): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    await action()
    return true
  } catch (err) {
    error.value = tagError(err, t, te)
    return false
  } finally {
    busy.value = false
  }
}

// add makes a new tag from the field at the bottom.
async function add(): Promise<void> {
  const name = newName.value.trim()
  if (name === '' || busy.value) {
    return
  }
  if (await run(() => store.create(name))) {
    newName.value = ''
  }
}

// edit turns a tag's name into a field.
function edit(tag: Tag): void {
  colouring.value = null
  editing.value = tag.id
  draft.value = tag.name
  error.value = ''
  void nextTick(() => renameInput.value?.[0]?.focus())
}

// save renames the tag being edited; an unchanged or empty name just stops.
async function save(tag: Tag): Promise<void> {
  if (editing.value !== tag.id || busy.value) {
    return
  }
  const name = draft.value.trim()
  if (name === '' || name === tag.name) {
    editing.value = null
    return
  }
  if (await run(() => store.update(tag.id, { name }))) {
    editing.value = null
  }
}

// togglePalette opens or folds the palette of a tag.
function togglePalette(tag: Tag): void {
  editing.value = null
  colouring.value = colouring.value === tag.id ? null : tag.id
}

// recolour gives a tag another colour of the palette and folds the palette.
async function recolour(tag: Tag, color: TagColor): Promise<void> {
  if (color === tag.color) {
    colouring.value = null
    return
  }
  if (await run(() => store.update(tag.id, { color }))) {
    colouring.value = null
  }
}

// usage says what wears a tag: its trips and its ideas, or nothing.
function usage(tag: Tag): string {
  const parts = [
    ...(tag.trip_count > 0 ? [t('tags.tripCount', tag.trip_count)] : []),
    ...(tag.idea_count > 0 ? [t('tags.ideaCount', tag.idea_count)] : []),
  ]
  return parts.length > 0 ? parts.join(', ') : t('tags.unused')
}

// remove deletes a tag once the person confirms, saying how many trips lose it.
async function remove(tag: Tag): Promise<void> {
  const uses = tag.trip_count + tag.idea_count
  const question = uses > 0
    ? t('tags.confirmDeleteUsed', { name: tag.name, n: uses }, uses)
    : t('tags.confirmDelete', { name: tag.name })
  if (!(await confirmDialog.value?.ask(question, { danger: true }))) {
    return
  }
  await run(() => store.remove(tag.id))
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <div class="modal-box flex flex-col gap-3">
      <h2 class="text-lg font-bold">{{ t('tags.title') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('tags.hint') }}</p>

      <div v-if="loading" class="flex justify-center py-4"><span class="loading loading-spinner"></span></div>
      <p v-else-if="store.tags.length === 0" class="py-2 text-sm text-base-content/70">{{ t('tags.empty') }}</p>
      <ul v-else class="flex flex-col gap-1">
        <li v-for="tag in store.tags" :key="tag.id" class="flex flex-wrap items-center gap-2">
          <button
            type="button"
            class="btn btn-ghost btn-sm btn-square"
            :aria-label="t('tags.colorOf', { name: tag.name })"
            :aria-expanded="colouring === tag.id"
            :title="t('tags.colorOf', { name: tag.name })"
            :disabled="busy"
            @click="togglePalette(tag)"
          >
            <span class="tag-swatch size-4 rounded-full" :class="`tag-${tag.color}`"></span>
          </button>
          <form v-if="editing === tag.id" class="flex flex-1 items-center gap-2" @submit.prevent="save(tag)">
            <input
              ref="renameInput"
              v-model="draft"
              type="text"
              maxlength="50"
              class="input input-sm min-w-0 flex-1"
              :aria-label="t('tags.rename', { name: tag.name })"
              @keydown.esc.prevent.stop="editing = null"
            />
            <button type="submit" class="btn btn-sm btn-primary" :disabled="busy">{{ t('common.save') }}</button>
            <button type="button" class="btn btn-sm btn-ghost" @click="editing = null">{{ t('common.cancel') }}</button>
          </form>
          <template v-else>
            <TagPill :tag="tag" class="min-w-0" />
            <span class="flex-1 text-sm text-base-content/60">{{ usage(tag) }}</span>
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              :aria-label="t('tags.rename', { name: tag.name })"
              :title="t('tags.rename', { name: tag.name })"
              :disabled="busy"
              @click="edit(tag)"
            >
              <AppIcon name="pencil" />
            </button>
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square text-error"
              :aria-label="t('tags.delete', { name: tag.name })"
              :title="t('tags.delete', { name: tag.name })"
              :disabled="busy"
              @click="remove(tag)"
            >
              <AppIcon name="trash" />
            </button>
          </template>
          <div
            v-if="colouring === tag.id"
            role="radiogroup"
            :aria-label="t('tags.colorOf', { name: tag.name })"
            class="flex basis-full flex-wrap gap-1 ps-10"
          >
            <button
              v-for="color in TAG_COLOR_CHOICES"
              :key="color"
              type="button"
              role="radio"
              class="btn btn-ghost btn-sm btn-square"
              :class="{ 'btn-active': color === tag.color }"
              :aria-checked="color === tag.color"
              :aria-label="t(`tags.colors.${color}`)"
              :title="t(`tags.colors.${color}`)"
              :disabled="busy"
              @click="recolour(tag, color)"
            >
              <span class="tag-swatch size-5 rounded-full" :class="`tag-${color}`"></span>
            </button>
          </div>
        </li>
      </ul>

      <form class="flex items-center gap-2" @submit.prevent="add">
        <input
          v-model="newName"
          type="text"
          maxlength="50"
          class="input input-sm min-w-0 flex-1"
          :placeholder="t('tags.newPlaceholder')"
          :aria-label="t('tags.newPlaceholder')"
        />
        <button type="submit" class="btn btn-sm" :disabled="busy || newName.trim() === ''">
          <AppIcon name="plus" />
          {{ t('tags.add') }}
        </button>
      </form>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action mt-2">
        <form method="dialog">
          <button type="submit" class="btn">{{ t('common.close') }}</button>
        </form>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
    <ConfirmDialog ref="confirmDialog" />
  </dialog>
</template>
