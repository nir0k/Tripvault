<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LANGUAGE_NAMES } from '@/i18n'

// The languages of a report, as a row of buttons: which one its words are read
// in, or, while editing, which one is being written. The original is marked as
// such; a translation may carry how far it has got.
defineProps<{
  languages: readonly string[]
  modelValue: string
  /** What the buttons choose: the language read, or the one written. */
  label: string
  /** How many fields each translation holds out of how many there are. */
  progress?: Record<string, { done: number; total: number }>
}>()

const emit = defineEmits<{
  'update:modelValue': [code: string]
}>()

const { t } = useI18n()
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 text-sm">
    <span class="text-base-content/70">{{ label }}</span>
    <div class="join" role="group" :aria-label="label">
      <button
        v-for="(code, index) in languages"
        :key="code"
        type="button"
        class="btn btn-sm join-item"
        :class="{ 'btn-active btn-primary': code === modelValue }"
        :aria-pressed="code === modelValue"
        @click="emit('update:modelValue', code)"
      >
        {{ LANGUAGE_NAMES[code] ?? code }}
        <span v-if="index === 0" class="text-xs opacity-70">· {{ t('report.originalLanguage') }}</span>
        <span v-else-if="progress?.[code]" class="text-xs opacity-70">
          · {{ progress[code]!.done }}/{{ progress[code]!.total }}
        </span>
      </button>
    </div>
  </div>
</template>
