<script setup lang="ts">
import { computed } from 'vue'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'

// A choice between a handful of named things, each with a picture.
//
// It is a dropdown rather than a <select> because an <option> holds text and
// nothing else: there is no way to put an icon inside one. The markup follows
// the theme switcher, so the two behave the same way - a click outside or
// Escape folds it.
//
// The summary must carry no tabindex attribute. DaisyUI puts pointer-events:
// none on the first child of an open dropdown that has one, which is how its
// focus-driven variant closes on a second click; on a <details> that stops the
// click reaching the summary at all, so the list never opens. A summary is
// focusable in its own right, so nothing is lost.
const props = withDefaults(defineProps<{
  modelValue: string
  options: readonly IconOption[]
  /** Names the control for screen readers and, unless compact, on screen. */
  label: string
  /** Show only the icon and the name, without the label above it. */
  compact?: boolean
  /** Match the width of the surrounding controls. */
  block?: boolean
  /**
   * Open upwards. A dropdown inside a scrolling dialog is clipped at the box's
   * edge, so a field in the lower part of a long form opens towards the room it
   * has rather than towards the edge.
   */
  up?: boolean
}>(), { compact: false, block: false, up: false })

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

// An option without a picture keeps the column, so the names stay in line.
export interface IconOption {
  value: string
  label: string
  icon?: IconName
}

const { close } = useDropdown('menu')

const chosen = computed(() => props.options.find((option) => option.value === props.modelValue) ?? null)

// choose applies a value and folds the list.
function choose(value: string): void {
  close()
  if (value !== props.modelValue) {
    emit('update:modelValue', value)
  }
}
</script>

<template>
  <details ref="menu" class="dropdown" :class="{ 'w-full': block, 'dropdown-top': up }">
    <summary
      class="btn btn-hover-outline font-normal"
      :class="[compact ? 'btn-sm' : '', block ? 'w-full justify-between' : '']"
      :aria-label="`${label}: ${chosen?.label ?? ''}`"
    >
      <span class="flex min-w-0 items-center gap-2">
        <AppIcon v-if="chosen?.icon" :name="chosen.icon" />
        <span class="truncate">{{ chosen?.label }}</span>
      </span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul
      class="menu dropdown-content z-20 max-h-64 w-56 flex-nowrap overflow-y-auto rounded-box border
             border-base-300 bg-base-100 p-2 shadow-lg"
      :class="up ? 'mb-1' : 'mt-1'"
    >
      <li v-for="option in options" :key="option.value">
        <button
          type="button"
          :class="{ 'menu-active': option.value === modelValue }"
          :aria-pressed="option.value === modelValue"
          @click="choose(option.value)"
        >
          <AppIcon v-if="option.icon" :name="option.icon" />
          <span v-else class="size-5 shrink-0" aria-hidden="true"></span>
          {{ option.label }}
        </button>
      </li>
    </ul>
  </details>
</template>
