<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { activeTimeFormat } from '@/utils/display'
import { formatTimeOfDay, parseTimeOfDay } from '@/utils/format'

// A time of day typed in the reader's own clock. The browser's time field
// follows the language of the operating system rather than the profile, so a
// reader who chose a 24-hour clock could still be asked for AM and PM; this
// field shows the time the way the profile says and reads it in either clock.
// The model is always "HH:MM", or '' for no time.
const model = defineModel<string>({ required: true })

const props = defineProps<{
  /**
   * The field's name. It is shown while the field is empty together with the
   * form a time is typed in, which a floating label would otherwise hide.
   */
  label?: string
}>()

const emit = defineEmits<{
  /** A new valid time was committed, as a time field's change event says. */
  change: [value: string]
}>()

const { t } = useI18n()
const input = useTemplateRef<HTMLInputElement>('input')

// text is what the field shows: the model in the reader's clock until somebody
// types, then what they typed until they leave the field.
const text = ref(formatTimeOfDay(model.value))
// A time set from outside - a form filled anew, a clock changed in the
// profile - replaces what the field shows, and whatever was wrong with the
// old text goes with it.
watch([model, activeTimeFormat], () => {
  if (parseTimeOfDay(text.value) !== model.value) {
    text.value = formatTimeOfDay(model.value)
    input.value?.setCustomValidity('')
  }
})

const placeholder = computed(() => {
  const form = activeTimeFormat.value === 'h12' ? t('time.placeholder12') : t('time.placeholder24')
  return props.label ? `${props.label} (${form})` : form
})

// read takes what was typed into the model as soon as it is a time, so a form
// sent with Enter carries it, and marks the field invalid while it is not one.
function read(): void {
  const typed = text.value.trim()
  const parsed = typed === '' ? '' : parseTimeOfDay(typed)
  const example = activeTimeFormat.value === 'h12' ? '2:30 PM' : '14:30'
  input.value?.setCustomValidity(parsed === null ? t('time.invalid', { example }) : '')
  if (parsed !== null) {
    model.value = parsed
  }
}

// commit writes the time back in the reader's clock when the field is left,
// and reports a change the way a time field would.
function commit(): void {
  read()
  const parsed = text.value.trim() === '' ? '' : parseTimeOfDay(text.value)
  if (parsed !== null) {
    text.value = formatTimeOfDay(parsed)
    emit('change', parsed)
  }
}
</script>

<template>
  <input
    ref="input"
    v-model="text"
    type="text"
    autocomplete="off"
    :placeholder="placeholder"
    maxlength="10"
    @input="read"
    @change="commit"
  />
</template>
