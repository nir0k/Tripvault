<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'

// The "+" in the gap between two elements of a day, which asks what goes there.
// It stays out of sight until the pointer is over its row, so a day does not
// bristle with buttons; a touch screen has no pointer to hover and always
// shows it. At the end of a day it is a pill with words, always in sight, since
// that is where a day is most often added to.
defineProps<{
  /** Show the button whether or not its row is hovered, as in an empty day. */
  visible?: boolean
  /** A pill with words, always shown, for the end of a day. */
  pill?: boolean
}>()

const emit = defineEmits<{
  pick: [kind: 'place' | 'activity']
}>()

const { t } = useI18n()

// pick reports the choice and closes the menu, which DaisyUI keeps open for as
// long as something inside it has the focus.
function pick(kind: 'place' | 'activity'): void {
  ;(document.activeElement as HTMLElement | null)?.blur()
  emit('pick', kind)
}
</script>

<template>
  <div
    class="dropdown transition-opacity focus-within:opacity-100 pointer-coarse:opacity-100"
    :class="[visible || pill ? 'opacity-100' : 'opacity-0 group-hover:opacity-100', { 'dropdown-center': pill }]"
  >
    <div
      v-if="pill"
      tabindex="0"
      role="button"
      class="btn btn-sm btn-primary btn-soft rounded-full px-4"
    >
      <AppIcon name="plus" class="size-5!" />
      {{ t('plan.quickAdd.empty') }}
    </div>
    <div
      v-else
      tabindex="0"
      role="button"
      class="btn btn-circle btn-xs btn-primary btn-soft"
      :aria-label="t('plan.quickAdd.insert')"
      :title="t('plan.quickAdd.insert')"
    >
      <AppIcon name="plus" class="size-4!" />
    </div>
    <ul tabindex="0" class="menu dropdown-content z-20 w-44 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li>
        <button type="button" @click="pick('place')">
          <AppIcon name="mapPin" />
          {{ t('place.kinds.place') }}
        </button>
      </li>
      <li>
        <button type="button" @click="pick('activity')">
          <AppIcon name="activity" />
          {{ t('place.kinds.activity') }}
        </button>
      </li>
    </ul>
  </div>
</template>
