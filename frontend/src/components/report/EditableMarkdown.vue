<script setup lang="ts">
import { nextTick, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import MarkdownText from '@/components/MarkdownText.vue'

// A piece of a report's prose, read as rendered Markdown and edited in place.
//
// Editing opens on a click and saves when the field loses focus, so writing a
// report is a sequence of clicks and typing rather than of dialogs. Escape puts
// the text back as it was.
//
// While a translation is written the field holds the translation alone, and the
// original it is made from is shown above it; a field with no original has
// nothing to translate and stays out of the way.
const props = defineProps<{
  source: string
  editing: boolean
  placeholder: string
  /** Rows of the field when open; the prose of a day needs more than a caption. */
  rows?: number
  /** The original being translated; undefined while the original is written. */
  original?: string
}>()

const emit = defineEmits<{
  save: [value: string]
}>()

const { t } = useI18n()

const open = ref(false)
const draft = ref('')
const field = useTemplateRef<HTMLTextAreaElement>('field')

// A report switched back to reading closes whatever was open, unsaved.
watch(() => props.editing, (editing) => {
  if (!editing) {
    open.value = false
  }
})

// start opens the field with the current text and puts the cursor in it.
function start(): void {
  if (!props.editing) {
    return
  }
  draft.value = props.source
  open.value = true
  void nextTick(() => field.value?.focus())
}

// finish saves the text, unless nothing changed.
function finish(): void {
  open.value = false
  if (draft.value !== props.source) {
    emit('save', draft.value)
  }
}

// cancel closes the field and keeps the text as it was.
function cancel(): void {
  open.value = false
}
</script>

<template>
  <div v-if="!(editing && original !== undefined && original.trim() === '')" class="space-y-2">
    <div
      v-if="editing && original"
      class="rounded-box border border-dashed border-base-300 px-3 py-2 text-sm text-base-content/70"
    >
      <p class="text-xs font-medium text-base-content/60">{{ t('report.original') }}</p>
      <MarkdownText :source="original" />
    </div>

    <textarea
      v-if="open"
      ref="field"
      v-model="draft"
      class="textarea w-full"
      :rows="rows ?? 4"
      maxlength="20000"
      :placeholder="original !== undefined ? t('report.notTranslated') : placeholder"
      :aria-label="placeholder"
      @blur="finish"
      @keydown.escape.prevent="cancel"
    ></textarea>

    <MarkdownText v-else-if="source" :source="source" :class="{ 'cursor-text': editing }" @click="start" />

    <button
      v-else-if="editing"
      type="button"
      class="btn btn-ghost btn-sm text-base-content/60"
      @click="start"
    >
      {{ original !== undefined ? t('report.addTranslation') : t('report.addText') }}
    </button>
  </div>
</template>
