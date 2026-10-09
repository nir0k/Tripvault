<script setup lang="ts">
import { useLoadGeneration } from '@/composables/useLoadGeneration'
import { computed, ref, useTemplateRef, watch } from 'vue'
import { VueDraggable } from 'vue-draggable-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import * as packingApi from '@/api/packing'
import { listMembers } from '@/api/trips'
import { TAG_COLORS, type PackingCategory, type PackingItem, type PackingList, type TripMember } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { PACKING_ICONS } from '@/components/icons'
import PackingCategoryCard from '@/components/packing/PackingCategoryCard.vue'
import PackingCategoryStyleDialog, { type PackingCategoryStyle } from '@/components/packing/PackingCategoryStyleDialog.vue'
import PackingItemDialog from '@/components/packing/PackingItemDialog.vue'
import PackingTemplateDialog from '@/components/packing/PackingTemplateDialog.vue'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { copyText } from '@/utils/clipboard'
import { packingText, type PackingSection } from '@/utils/packing'

// What to take on the trip: categories the trip names itself, each in its own
// colour and with its icon, and a list of things ticked as they go into the
// bag. The list is packed together, so a tick is everybody's, and an item may
// name the member who brings it. It is printed from here as a checklist with
// every box empty.
//
// Only a plan has a list; a read-only link reads it without who brings what.
const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTripStore()

const list = ref<PackingList | null>(null)
const members = ref<TripMember[]>([])
const loading = ref(false)
const beginLoad = useLoadGeneration()
const busy = ref(false)
const error = ref('')
const exporting = ref(false)

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const itemDialog = useTemplateRef<InstanceType<typeof PackingItemDialog>>('itemDialog')
const styleDialog = useTemplateRef<InstanceType<typeof PackingCategoryStyleDialog>>('styleDialog')
const templateDialog = useTemplateRef<InstanceType<typeof PackingTemplateDialog>>('templateDialog')

const trip = computed(() => store.trip)
// tripId is what the API is asked with; null reads through the link.
const tripId = computed(() => (store.shared ? null : trip.value?.id ?? null))
const canEdit = computed(() => !store.shared && (trip.value?.role === 'owner' || trip.value?.role === 'editor'))

// Packed items can be folded away, and the choice is kept in the address, as
// the report keeps its skipped places.
const hidePacked = computed(() => route.query.packed === 'hide')
function setHidePacked(hide: boolean): void {
  void router.replace({ query: { ...route.query, packed: hide ? 'hide' : undefined } })
}

// The drag library reorders this copy of the categories while dragging.
const categories = ref<PackingCategory[]>([])
watch(list, (value) => {
  categories.value = [...(value?.categories ?? [])]
})

// itemsOf picks the items of one category, or the ones without a category.
function itemsOf(categoryId: string | null): PackingItem[] {
  return (list.value?.items ?? []).filter((item) => item.category_id === categoryId)
}
const uncategorised = computed(() => itemsOf(null))

const bringers = computed(() => Object.fromEntries(members.value.map((member) => [member.user.id, member.user.display_name])))
const progress = computed(() => (list.value && list.value.total > 0 ? Math.round((list.value.packed / list.value.total) * 100) : 0))
// load reads the list and, for a member, who may bring something.
async function load(): Promise<void> {
  const active = beginLoad()
  if (!trip.value || trip.value.kind !== 'plan') {
    return
  }
  loading.value = true
  error.value = ''
  try {
    const [packing, people] = await Promise.all([
      packingApi.getPacking(tripId.value),
      tripId.value === null ? Promise.resolve([]) : listMembers(tripId.value),
    ])
    if (!active()) return
    list.value = packing
    members.value = people
  } catch (err) {
    if (active()) error.value = errorMessage(err, t, te)
  } finally {
    if (active()) loading.value = false
  }
}
watch(() => trip.value?.id, () => void load(), { immediate: true })

/**
 * apply runs a change and shows the list it answers with. A failure is shown
 * and the list read again, so a drag the server refused springs back.
 */
async function apply(change: () => Promise<PackingList>): Promise<boolean> {
  const ownerId = trip.value?.id
  busy.value = true
  error.value = ''
  try {
    const next = await change()
    if (trip.value?.id !== ownerId) return false
    list.value = next
    return true
  } catch (err) {
    if (trip.value?.id !== ownerId) return false
    error.value = errorMessage(err, t, te)
    await load()
    return false
  } finally {
    busy.value = false
  }
}

// newCategory is the name typed into the line that adds a category, and
// newStyle the colour and icon chosen for it; without a choice it takes the
// colour the server would give it next and the icon of "other".
const newCategory = ref('')
const newStyle = ref<PackingCategoryStyle | null>(null)
const nextStyle = computed<PackingCategoryStyle>(() => newStyle.value ?? {
  color: TAG_COLORS[(list.value?.categories.length ?? 0) % TAG_COLORS.length] ?? 'gray',
  icon: 'other',
})

// chooseNewStyle opens the window on the style of the category being made.
async function chooseNewStyle(): Promise<void> {
  const style = await styleDialog.value?.choose(nextStyle.value)
  if (style) {
    newStyle.value = style
  }
}

// addCategory adds a category by the name typed, in the style chosen for it.
function addCategory(): void {
  const id = tripId.value
  const name = newCategory.value.trim()
  if (!id || name === '') {
    return
  }
  const style = { ...nextStyle.value }
  newCategory.value = ''
  newStyle.value = null
  void apply(() => packingApi.createCategory(id, { name, ...style }))
}

// addTemplate adds the categories of a template with their things.
function addTemplate(sections: packingApi.PackingAddCategory[]): void {
  const id = tripId.value
  if (id) {
    void apply(() => packingApi.addToPacking(id, sections))
  }
}

// restyle changes a category's colour or icon through the window.
async function restyle(category: PackingCategory): Promise<void> {
  const style = await styleDialog.value?.choose({ color: category.color, icon: category.icon })
  if (style && (style.color !== category.color || style.icon !== category.icon)) {
    void apply(() => packingApi.updateCategory(category.id, style))
  }
}

// onCategoriesSorted stores the order the categories were dragged into.
function onCategoriesSorted(): void {
  const id = tripId.value
  if (id) {
    void apply(() => packingApi.reorderCategories(id, categories.value.map((category) => category.id)))
  }
}

// removeCategory deletes a category after asking, since its items go with it.
async function removeCategory(category: PackingCategory): Promise<void> {
  const count = itemsOf(category.id).length
  const confirmed = await confirmDialog.value?.ask(t('packing.confirmDeleteCategory', { name: category.name }), {
    details: count > 0 ? [t('packing.deleteCategoryItems', count)] : [],
    danger: true,
  })
  if (confirmed) {
    void apply(() => packingApi.deleteCategory(category.id))
  }
}

// addItem adds an item at the end of its category.
function addItem(categoryId: string | null, name: string, quantity: number): void {
  const id = tripId.value
  if (id) {
    void apply(() => packingApi.createItem(id, categoryId, { name, quantity }))
  }
}

// saveItem stores the fields of the dialog, and moves the item when its
// category changed.
async function saveItem(item: PackingItem, fields: packingApi.PackingItemFields, categoryId: string | null): Promise<void> {
  const saved = await apply(async () => {
    const updated = await packingApi.updateItem(item.id, fields)
    if (categoryId === item.category_id) {
      return updated
    }
    return packingApi.moveItem(item.id, categoryId, Number.MAX_SAFE_INTEGER)
  })
  if (saved) {
    itemDialog.value?.close()
  } else {
    itemDialog.value?.fail(error.value)
  }
}

// resetTicks takes every tick off after asking.
async function resetTicks(): Promise<void> {
  const id = tripId.value
  if (id && await confirmDialog.value?.ask(t('packing.confirmReset'))) {
    void apply(() => packingApi.resetPacking(id))
  }
}

// copiedAll says the whole list has just been copied, which its tooltip says for a moment.
const copiedAll = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

// copyAll puts the whole list on the clipboard as text for a chat, under the
// trip's name; a read-only link is told nothing of who brings what.
async function copyAll(): Promise<void> {
  if (!list.value) {
    return
  }
  const sections: PackingSection[] = [
    ...list.value.categories.map((category) => ({ title: category.name, items: itemsOf(category.id) })),
    { title: t('packing.uncategorised'), items: uncategorised.value },
  ]
  const heading = `${trip.value?.title ?? ''} — ${t('trip.tabs.packing')}`
  if (await copyText(packingText(sections, bringers.value, heading))) {
    copiedAll.value = true
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      copiedAll.value = false
    }, 1500)
  }
}

// exportList saves the checklist to print, every box empty: a full-size PDF,
// a compact one fitted on one page, or the compact one as a picture.
async function exportList(format: 'pdf' | 'compact' | 'jpeg'): Promise<void> {
  if (exporting.value) {
    return
  }
  // The menu closes when it loses focus.
  if (window.document.activeElement instanceof HTMLElement) {
    window.document.activeElement.blur()
  }
  exporting.value = true
  error.value = ''
  try {
    if (format === 'jpeg') {
      await packingApi.downloadPackingImage(tripId.value, locale.value)
    } else {
      await packingApi.downloadPackingPDF(tripId.value, locale.value, format === 'compact')
    }
  } catch (err) {
    // A failure that is not the server's is the picture failing to be drawn
    // in this browser, which the PDF does not depend on.
    error.value = format === 'jpeg' && !(err instanceof ApiError) ? t('packing.jpegFailed') : errorMessage(err, t, te)
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <p v-if="trip && trip.kind !== 'plan'" class="text-base-content/70">{{ t('packing.planOnly') }}</p>
    <div v-else-if="loading && !list" class="flex justify-center"><span class="loading loading-spinner"></span></div>

    <template v-else-if="list">
      <header class="flex flex-wrap items-center gap-3">
        <div class="min-w-48 flex-1">
          <p class="text-sm text-base-content/70">{{ t('packing.packedOf', { packed: list.packed, total: list.total }) }}</p>
          <progress class="progress progress-success w-full" :value="progress" max="100" :aria-label="t('packing.progress')"></progress>
        </div>
        <label class="label cursor-pointer gap-2 text-sm">
          <input
            type="checkbox"
            class="toggle toggle-sm"
            :checked="hidePacked"
            @change="setHidePacked(($event.target as HTMLInputElement).checked)"
          />
          {{ t('packing.hidePacked') }}
        </label>
        <!-- The actions on the whole list are icons, each named by its tooltip. -->
        <div class="flex items-center gap-1">
          <span class="tooltip tooltip-bottom" :data-tip="copiedAll ? t('packing.copied') : t('packing.copyAll')">
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              :aria-label="t('packing.copyAll')"
              :disabled="list.total === 0"
              @click="copyAll"
            >
              <AppIcon :name="copiedAll ? 'check' : 'clipboardCopy'" />
            </button>
          </span>
          <div class="dropdown dropdown-end">
            <span class="tooltip tooltip-bottom" :data-tip="t('packing.download')">
              <div
                tabindex="0"
                role="button"
                class="btn btn-ghost btn-sm btn-square"
                :class="{ 'btn-disabled': exporting }"
                :aria-label="t('packing.download')"
              >
                <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
                <AppIcon v-else name="filePdf" />
              </div>
            </span>
            <ul tabindex="0" class="menu dropdown-content z-10 w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
              <li>
                <button type="button" @click="exportList('pdf')"><AppIcon name="filePdf" />{{ t('packing.pdf') }}</button>
              </li>
              <li>
                <button type="button" @click="exportList('compact')">
                  <AppIcon name="filePdf" />
                  <span class="flex flex-col items-start">
                    {{ t('packing.pdfCompact') }}
                    <span class="text-xs text-base-content/60">{{ t('packing.compactHint') }}</span>
                  </span>
                </button>
              </li>
              <li>
                <button type="button" @click="exportList('jpeg')">
                  <AppIcon name="image" />
                  <span class="flex flex-col items-start">
                    {{ t('packing.jpeg') }}
                    <span class="text-xs text-base-content/60">{{ t('packing.compactHint') }}</span>
                  </span>
                </button>
              </li>
            </ul>
          </div>
          <span v-if="canEdit && list.packed > 0" class="tooltip tooltip-bottom" :data-tip="t('packing.reset')">
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              :aria-label="t('packing.reset')"
              :disabled="busy"
              @click="resetTicks"
            >
              <AppIcon name="eraser" />
            </button>
          </span>
        </div>
      </header>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

      <div v-if="list.categories.length === 0 && list.items.length === 0" class="rounded-box border border-dashed border-base-300 p-4 text-sm">
        <p class="text-base-content/70">{{ canEdit ? t('packing.emptyEditor') : t('packing.empty') }}</p>
        <button v-if="canEdit" type="button" class="btn btn-sm mt-3" :disabled="busy" @click="templateDialog?.open()">
          <AppIcon name="packLuggage" />
          {{ t('packing.fromTemplate') }}
        </button>
      </div>

      <!-- A category is dragged by its whole heading; the fields and buttons in
           it are filtered out of the drag, and a finger must rest a moment
           before a drag starts, so the page still scrolls under it. -->
      <VueDraggable
        v-model="categories"
        :disabled="!canEdit"
        handle=".category-handle"
        filter="input, button, summary, .dropdown-content"
        :prevent-on-filter="false"
        :delay="200"
        :delay-on-touch-only="true"
        :animation="150"
        ghost-class="opacity-40"
        class="grid gap-4 md:grid-cols-2"
        @update="onCategoriesSorted"
      >
        <PackingCategoryCard
          v-for="category in categories"
          :key="category.id"
          :category-id="category.id"
          :title="category.name"
          :color="category.color"
          :icon="category.icon"
          :items="itemsOf(category.id)"
          :bringers="bringers"
          :can-edit="canEdit"
          :hide-packed="hidePacked"
          renamable
          @toggle="(item, packed) => apply(() => packingApi.updateItem(item.id, { packed }))"
          @add="addItem"
          @edit="(item) => itemDialog?.open(item)"
          @remove="(item) => apply(() => packingApi.deleteItem(item.id))"
          @move="(itemId, categoryId, position) => apply(() => packingApi.moveItem(itemId, categoryId, position))"
          @rename="(name) => apply(() => packingApi.updateCategory(category.id, { name }))"
          @restyle="restyle(category)"
          @delete="removeCategory(category)"
        />
      </VueDraggable>

      <!-- The items without a category: shown once there are some, or while
           there is no category to put a first item in. -->
      <PackingCategoryCard
        v-if="uncategorised.length > 0 || (canEdit && list.categories.length === 0)"
        :category-id="null"
        :title="t('packing.uncategorised')"
        color="gray"
        icon="other"
        :items="uncategorised"
        :bringers="bringers"
        :can-edit="canEdit"
        :hide-packed="hidePacked"
        :renamable="false"
        @toggle="(item, packed) => apply(() => packingApi.updateItem(item.id, { packed }))"
        @add="addItem"
        @edit="(item) => itemDialog?.open(item)"
        @remove="(item) => apply(() => packingApi.deleteItem(item.id))"
        @move="(itemId, categoryId, position) => apply(() => packingApi.moveItem(itemId, categoryId, position))"
      />

      <form v-if="canEdit" class="flex max-w-md items-center gap-2" @submit.prevent="addCategory()">
        <button
          type="button"
          class="packing-badge btn btn-sm btn-square border-0"
          :class="`tag-${nextStyle.color}`"
          :aria-label="t('packing.newStyle')"
          :title="t('packing.newStyle')"
          @click="chooseNewStyle"
        >
          <AppIcon :name="PACKING_ICONS[nextStyle.icon]" />
        </button>
        <input
          v-model="newCategory"
          type="text"
          maxlength="100"
          class="input input-sm min-w-0 flex-1"
          :placeholder="t('packing.newCategory')"
          :aria-label="t('packing.newCategory')"
        />
        <button type="submit" class="btn btn-sm" :disabled="busy || newCategory.trim() === ''">
          <AppIcon name="plus" />
          {{ t('packing.addCategory') }}
        </button>
        <button type="button" class="btn btn-sm btn-ghost" :disabled="busy" @click="templateDialog?.open()">
          <AppIcon name="packLuggage" />
          {{ t('packing.fromTemplate') }}
        </button>
      </form>
    </template>

    <p v-else-if="error" role="alert" class="text-error">{{ error }}</p>

    <PackingItemDialog ref="itemDialog" :categories="list?.categories ?? []" :members="members" @save="saveItem" />
    <PackingCategoryStyleDialog ref="styleDialog" />
    <PackingTemplateDialog ref="templateDialog" :list="list" :busy="busy" @add="addTemplate" />
    <ConfirmDialog ref="confirmDialog" />
  </div>
</template>
