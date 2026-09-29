<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { EditorContent, useEditor } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import { Placeholder } from '@tiptap/extensions'
import { Markdown } from '@tiptap/markdown'

// A text written in place, formatted as it is typed and kept as Markdown, so
// the report, the PDF and every reader render it the way they always did.
//
// It knows only what a note about a place needs - bold, italics, both kinds of
// list and links - and has no toolbar: the keyboard does it (Ctrl+B, Ctrl+I),
// and so does Markdown typed as it is meant ("**", "*", "- ", "1. ",
// "[text](address)"). A pasted address over selected words links them.
//
// The text is handed up once, when the editor loses the focus, rather than on
// every key, so writing a sentence does not save it word by word.
const props = defineProps<{
  modelValue: string
  placeholder: string
  label: string
}>()

const emit = defineEmits<{
  /** The text as Markdown, when it changed while the editor had the focus. */
  commit: [markdown: string]
}>()

const editor = useEditor({
  content: props.modelValue,
  contentType: 'markdown',
  extensions: [
    StarterKit.configure({
      blockquote: false,
      code: false,
      codeBlock: false,
      heading: false,
      horizontalRule: false,
      strike: false,
      underline: false,
      link: {
        openOnClick: false,
        autolink: true,
        linkOnPaste: true,
        markdownLinks: true,
        defaultProtocol: 'https',
        protocols: ['http', 'https', 'mailto'],
      },
    }),
    Markdown,
    Placeholder.configure({ placeholder: () => props.placeholder }),
  ],
  editorProps: {
    attributes: {
      class: 'markdown min-h-6 text-sm leading-relaxed outline-none',
      'aria-label': props.label,
      'aria-multiline': 'true',
    },
  },
  onBlur: ({ editor: current }) => {
    const markdown = current.isEmpty ? '' : current.getMarkdown().trim()
    if (markdown !== props.modelValue.trim()) {
      emit('commit', markdown)
    }
  },
})

// A text changed elsewhere - the full form, another reader - replaces what the
// editor shows, unless somebody is writing in it.
watch(() => props.modelValue, (value) => {
  const current = editor.value
  if (current && !current.isFocused && current.getMarkdown().trim() !== value.trim()) {
    current.commands.setContent(value, { contentType: 'markdown', emitUpdate: false })
  }
})

onBeforeUnmount(() => editor.value?.destroy())
</script>

<template>
  <EditorContent :editor="editor" class="rich-text rounded-field px-1 py-0.5 hover:bg-base-200/60 focus-within:bg-base-200/60" />
</template>
