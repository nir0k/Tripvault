<script setup lang="ts">
import { onBeforeUnmount, ref, useTemplateRef, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import SpeedSlider from '@/components/SpeedSlider.vue'
import { formatSpeed } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// The speed one line of a plan is walked at, when it is not the plan's own: a
// steep path or a slow group. It is a slider under the pencil that opened it,
// as a menu would be, but a dialog rather than a menu, which the day's column
// would cut off; on a phone it opens at the top like every dialog. Moving the
// slider and clicking outside keeps the speed, Escape leaves it as it was.

const { t, locale } = useI18n()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const speed = ref(0)
const planSpeed = ref(0)
// asPlan says the panel was closed by "as in the plan", and cancelled that it
// was left with Escape; otherwise closing it keeps the speed on the slider.
let asPlan = false
let cancelled = false
let settle: ((choice: { speed: number | null } | null) => void) | null = null

// The panel's width and the least room it keeps from the edges of the screen.
const PANEL_WIDTH = 320
const MARGIN = 8
// Below this much room under the button the panel opens above it instead.
const MIN_ROOM_BELOW = 160

const anchored = ref(false)
const panel = ref<CSSProperties>({})
let trigger: HTMLElement | null = null

// place puts the panel under the pencil, or above it when the screen has no
// room below, keeping it inside the screen; on a phone it leaves the dialog
// where every dialog opens.
function place(): void {
  anchored.value = window.matchMedia('(min-width: 640px)').matches && trigger !== null
  if (!anchored.value || !trigger) {
    panel.value = {}
    return
  }
  const rect = trigger.getBoundingClientRect()
  const left = Math.max(MARGIN, Math.min(rect.left - PANEL_WIDTH / 2, window.innerWidth - PANEL_WIDTH - MARGIN))
  const below = window.innerHeight - rect.bottom - MARGIN
  const vertical: CSSProperties = below >= MIN_ROOM_BELOW
    ? { top: `${rect.bottom + 4}px`, maxHeight: `${below - 4}px` }
    : { bottom: `${window.innerHeight - rect.top + 4}px`, maxHeight: `${rect.top - MARGIN - 4}px` }
  panel.value = { position: 'fixed', left: `${left}px`, width: `${PANEL_WIDTH}px`, margin: 0, ...vertical }
}

/**
 * choose opens the slider under a button, on a line's speed.
 *
 * Arguments:
 *   - button: the pencil the panel stands under.
 *   - current: the speed the line is timed at now.
 *   - plan: the plan's own speed, which "as in the plan" goes back to.
 *
 * Returns:
 *   - the speed chosen, null inside meaning the plan's; null when the panel
 *     was left with Escape.
 */
function choose(button: HTMLElement | null, current: number, plan: number): Promise<{ speed: number | null } | null> {
  settle?.(null)
  trigger = button
  speed.value = current
  planSpeed.value = plan
  asPlan = false
  cancelled = false
  place()
  window.addEventListener('resize', place)
  dialog.value?.showModal()
  return new Promise((resolve) => {
    settle = resolve
  })
}

// usePlan closes the panel with the plan's speed.
function usePlan(): void {
  asPlan = true
  dialog.value?.close()
}

// cancel notes the panel was left with Escape, which keeps the speed as it was.
function cancel(): void {
  cancelled = true
}

// closed hands over what the panel was closed with, however it was closed.
function closed(): void {
  window.removeEventListener('resize', place)
  const done = settle
  settle = null
  done?.(cancelled ? null : { speed: asPlan ? null : speed.value })
}
onBeforeUnmount(() => window.removeEventListener('resize', place))

defineExpose({ choose })
</script>

<template>
  <dialog
    ref="dialog"
    class="modal modal-top sm:modal-middle"
    :class="{ 'modal-anchored': anchored }"
    @cancel="cancel"
    @close="closed"
  >
    <div class="modal-box flex flex-col gap-2" :style="panel">
      <SpeedSlider v-model="speed" :label="t('track.speed')" />
      <div class="flex items-center justify-between gap-2">
        <span class="text-xs text-base-content/60">{{ t('track.speedHint') }}</span>
        <button type="button" class="btn btn-ghost btn-xs shrink-0" @click="usePlan">
          {{ t('track.speedAsPlan', { speed: formatSpeed(planSpeed, locale, activeUnits) }) }}
        </button>
      </div>
    </div>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
