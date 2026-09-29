<script setup lang="ts" generic="T extends string">
import { computed, useId } from 'vue'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'

// A choice among a few named options, drawn as the currency is chosen: the
// field's name in a plain box on the left, the choice in the rest of the width,
// and the options in a menu under it, each with an icon when it has one. With
// no value it is a button that offers the options to add, such as a way of
// getting there.

/** LabeledOption is one option of the choice. */
export interface LabeledOption<V extends string> {
  value: V
  label: string
  icon?: IconName
}

const model = defineModel<T | null>({ default: null })

const props = defineProps<{
  /** The name in the box on the left. */
  label: string
  options: LabeledOption<T>[]
  /** What the field shows while nothing is chosen. */
  placeholder?: string
}>()

const emit = defineEmits<{
  /** An option was chosen, even the one already chosen. */
  choose: [value: T]
}>()

const { close } = useDropdown('menu')
const nameId = useId()
const chosen = computed(() => props.options.find((option) => option.value === model.value) ?? null)

// choose applies an option and folds the menu.
function choose(value: T): void {
  close()
  model.value = value
  emit('choose', value)
}
</script>

<template>
  <div class="flex w-full">
    <span
      :id="nameId"
      class="flex items-center whitespace-nowrap rounded-s-[var(--radius-field)] border border-e-0
             border-base-300 bg-base-200 px-3 text-sm"
    >
      {{ label }}
    </span>
    <details ref="menu" class="dropdown min-w-0 flex-1">
      <summary class="input w-full cursor-pointer items-center justify-between rounded-s-none" :aria-labelledby="nameId">
        <span class="flex min-w-0 items-center gap-2 truncate" :class="{ 'text-base-content/60': !chosen }">
          <AppIcon v-if="chosen?.icon" :name="chosen.icon" class="size-4!" />
          {{ chosen?.label ?? placeholder ?? '' }}
        </span>
        <AppIcon name="chevronDown" class="size-3!" />
      </summary>
      <ul class="menu dropdown-content z-20 mt-1 max-h-72 w-full min-w-56 flex-nowrap overflow-y-auto rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
        <li v-for="option in options" :key="option.value">
          <button
            type="button"
            :class="{ 'menu-active': option.value === model }"
            :aria-pressed="option.value === model"
            @click="choose(option.value)"
          >
            <AppIcon v-if="option.icon" :name="option.icon" class="size-4!" />
            {{ option.label }}
          </button>
        </li>
      </ul>
    </details>
  </div>
</template>
