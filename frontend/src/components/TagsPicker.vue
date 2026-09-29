<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TripTag } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TagPill from '@/components/TagPill.vue'
import { useTagsStore } from '@/stores/tags'
import { tagError } from '@/utils/tags'

// The reader's own tags on a trip or an idea, and the window that puts them on
// it. Tags are the reader's, not the trip's, so anybody who can open the trip
// may tag it - a viewer included - and nobody else sees what they chose. The
// page saves the choice, since a trip and an idea are tagged apart. The choice is
// a dialog rather than a menu: a menu opened from the trip's column would be
// cut off by the column's own edges, while a dialog sits above everything. On
// a phone it opens at the top like every dialog; on a wider screen it is
// placed under its button, where a menu would be, and leaves the page undimmed.
const props = defineProps<{
  /** The tags on what is tagged now. */
  tags: TripTag[]
  /** What is tagged, which the count of each tag's uses follows. */
  kind: 'trip' | 'idea'
  /** save puts exactly these tags on it. */
  save: (ids: string[]) => Promise<void>
}>()

const { t, te } = useI18n()
const store = useTagsStore()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const trigger = useTemplateRef<HTMLButtonElement>('trigger')
const busy = ref(false)
const error = ref('')
const newName = ref('')

const chosen = computed(() => new Set(props.tags.map((tag) => tag.id)))

// The panel's width and the least room it keeps from the edges of the screen.
const PANEL_WIDTH = 288
const MARGIN = 8
// Below this much room under the button the panel opens above it instead.
const MIN_ROOM_BELOW = 240

// anchored says the dialog is placed by its button rather than by the layout
// of a dialog; panel is where.
const anchored = ref(false)
const panel = ref<CSSProperties>({})

// place puts the panel under its button, or above it when the screen has no
// room below, keeping it inside the screen. On a phone it leaves the dialog
// where every dialog opens.
function place(): void {
  anchored.value = window.matchMedia('(min-width: 640px)').matches && trigger.value !== null
  if (!anchored.value || !trigger.value) {
    panel.value = {}
    return
  }
  const rect = trigger.value.getBoundingClientRect()
  const left = Math.max(MARGIN, Math.min(rect.left, window.innerWidth - PANEL_WIDTH - MARGIN))
  const below = window.innerHeight - rect.bottom - MARGIN
  const vertical: CSSProperties = below >= MIN_ROOM_BELOW
    ? { top: `${rect.bottom + 4}px`, maxHeight: `${below - 4}px` }
    : { bottom: `${window.innerHeight - rect.top + 4}px`, maxHeight: `${rect.top - MARGIN - 4}px` }
  panel.value = { position: 'fixed', left: `${left}px`, width: `${PANEL_WIDTH}px`, margin: 0, ...vertical }
}

// open shows the window, reading the reader's tags the first time.
async function open(): Promise<void> {
  error.value = ''
  newName.value = ''
  place()
  window.addEventListener('resize', place)
  dialog.value?.showModal()
  try {
    await store.load()
  } catch (err) {
    error.value = tagError(err, t, te)
  }
}

// closed stops following the size of the screen once the window is gone.
function closed(): void {
  window.removeEventListener('resize', place)
}

onBeforeUnmount(closed)

// put puts the given tags on through the page.
async function put(ids: string[]): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    await props.save(ids)
    return true
  } catch (err) {
    error.value = tagError(err, t, te)
    return false
  } finally {
    busy.value = false
  }
}

// toggle puts a tag on the trip or takes it off at once.
async function toggle(id: string): Promise<void> {
  const on = chosen.value.has(id)
  const ids = on ? [...chosen.value].filter((item) => item !== id) : [...chosen.value, id]
  if (await put(ids)) {
    store.countOn(id, on ? -1 : 1, props.kind)
  }
}

// add makes a tag of the typed name and puts it on the trip; a name the
// reader already has puts that tag on instead of failing.
async function add(): Promise<void> {
  const name = newName.value.trim()
  if (name === '' || busy.value) {
    return
  }
  const existing = store.tags.find((tag) => tag.name.localeCompare(name, undefined, { sensitivity: 'base' }) === 0)
  let id = existing?.id
  if (!id) {
    busy.value = true
    error.value = ''
    try {
      id = (await store.create(name)).id
    } catch (err) {
      error.value = tagError(err, t, te)
      return
    } finally {
      busy.value = false
    }
  }
  if (!chosen.value.has(id) && await put([...chosen.value, id])) {
    store.countOn(id, 1, props.kind)
  }
  newName.value = ''
}

// remove takes a tag off the trip from its pill.
async function remove(id: string): Promise<void> {
  if (!busy.value && await put([...chosen.value].filter((item) => item !== id))) {
    store.countOn(id, -1, props.kind)
  }
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-1">
    <TagPill v-for="tag in tags" :key="tag.id" :tag="tag">
      <button
        type="button"
        class="cursor-pointer opacity-60 hover:opacity-100"
        :aria-label="t('tags.takeOff', { name: tag.name })"
        :disabled="busy"
        @click="remove(tag.id)"
      >
        <AppIcon name="close" class="size-3!" />
      </button>
    </TagPill>
    <button ref="trigger" type="button" class="btn btn-ghost btn-xs gap-1 px-1.5" :title="t('tags.onTrip')" @click="open">
      <AppIcon name="tag" class="size-4!" />
      <span v-if="tags.length === 0">{{ t('tags.addToTrip') }}</span>
      <span v-else class="sr-only">{{ t('tags.onTrip') }}</span>
    </button>

    <dialog ref="dialog" class="modal modal-top sm:modal-middle" :class="{ 'modal-anchored': anchored }" @close="closed">
      <div class="modal-box flex flex-col gap-3" :style="panel">
        <h2 class="text-lg font-bold">{{ t('tags.onTrip') }}</h2>
        <ul v-if="store.tags.length > 0" class="flex flex-col gap-1">
          <li v-for="tag in store.tags" :key="tag.id">
            <label class="label w-full cursor-pointer justify-start gap-2 text-base-content">
              <input
                type="checkbox"
                class="checkbox checkbox-sm"
                :checked="chosen.has(tag.id)"
                :disabled="busy"
                @change="toggle(tag.id)"
              />
              <TagPill :tag="tag" class="min-w-0" />
            </label>
          </li>
        </ul>
        <p v-else-if="store.loaded" class="text-sm text-base-content/70">{{ t('tags.emptyPicker') }}</p>
        <div v-else class="flex justify-center py-2"><span class="loading loading-spinner"></span></div>

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
    </dialog>
  </div>
</template>
