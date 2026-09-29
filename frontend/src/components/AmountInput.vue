<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { invalidAmount, isFormula, resolveAmount } from '@/utils/amount'

// A text field for an amount of money that also takes a formula such as
// "120*3+45". The formula is computed when the field is left; one that does
// not compute keeps the form from being sent, as a required field would.
//
// A phone shows a number pad for the field, which has no operators, so on a
// touch screen a row of operator keys floats by the field while it has the
// focus. The attributes the field is given - its classes among them - go to
// the input, not to the row.
defineOptions({ inheritAttrs: false })

const model = defineModel<string>({ required: true })

const { t } = useI18n()
const input = useTemplateRef<HTMLInputElement>('input')
const keys = useTemplateRef<HTMLElement>('keys')

const OPERATORS = ['+', '−', '×', '÷', '(', ')']
// The gap between the field and the row, and the least margin from the edges
// of the screen.
const GAP = 4
const MARGIN = 8
// How long after a key is pressed a blur of the field is taken for the press.
const PRESS_WINDOW_MS = 400

// Only a touch screen gets the keys: a keyboard has every sign already.
const touch = window.matchMedia('(pointer: coarse)').matches
const open = ref(false)
// host is where the row is put: a modal dialog sits in the top layer, above
// anything in the body, so a field inside one keeps its row inside it.
const host = ref<HTMLElement | null>(null)
const place = ref({ top: 0, left: 0 })
let pressedAt = 0

// The field is invalid while it holds a formula that does not compute. It is
// checked on every change, including a form filled anew for another record.
watch([model, input], ([value, element]) => {
  element?.setCustomValidity(invalidAmount(value) ? t('amount.invalidFormula') : '')
}, { immediate: true, flush: 'post' })

// compute replaces a formula with its result.
function compute(): void {
  if (isFormula(model.value) && !invalidAmount(model.value)) {
    model.value = resolveAmount(model.value)
  }
}

// position puts the row under the field, or above it when the on-screen
// keyboard leaves no room below, and keeps it inside the screen.
function position(): void {
  const field = input.value
  if (!field || !open.value) {
    return
  }
  const rect = field.getBoundingClientRect()
  const height = keys.value?.offsetHeight ?? 0
  const width = keys.value?.offsetWidth ?? 0
  const viewport = window.visualViewport
  const bottom = viewport ? viewport.offsetTop + viewport.height : window.innerHeight
  const below = rect.bottom + GAP
  place.value = {
    top: below + height <= bottom - MARGIN ? below : Math.max(MARGIN, rect.top - height - GAP),
    left: Math.max(MARGIN, Math.min(rect.left, window.innerWidth - width - MARGIN)),
  }
}

// follow moves the row with the field while the page, a dialog or the visible
// part of the screen scrolls or changes size.
function follow(on: boolean): void {
  const method = on ? 'addEventListener' : 'removeEventListener'
  window[method]('scroll', position, true)
  window[method]('resize', position)
  window.visualViewport?.[method]('resize', position)
  window.visualViewport?.[method]('scroll', position)
}

// show opens the row when the field takes the focus on a touch screen.
function show(): void {
  if (!touch || !input.value) {
    return
  }
  host.value = input.value.closest('dialog') ?? document.body
  open.value = true
  follow(true)
  void nextTick(position)
}

// hide folds the row away.
function hide(): void {
  if (open.value) {
    open.value = false
    follow(false)
  }
}

// leave computes the formula once the person leaves the field. A blur that
// comes of pressing one of the keys is not leaving: the field takes the focus
// back and the row stays.
function leave(): void {
  if (Date.now() - pressedAt < PRESS_WINDOW_MS) {
    input.value?.focus()
    return
  }
  compute()
  hide()
}

// press types an operator where the caret is, or computes the formula on "=".
// It acts on the press itself and cancels it, so the field keeps the focus and
// the number pad stays up.
function press(event: PointerEvent, key: string): void {
  event.preventDefault()
  pressedAt = Date.now()
  const field = input.value
  if (!field) {
    return
  }
  if (key === '=') {
    compute()
  } else {
    const start = field.selectionStart ?? model.value.length
    const end = field.selectionEnd ?? start
    model.value = model.value.slice(0, start) + key + model.value.slice(end)
    void nextTick(() => field.setSelectionRange(start + key.length, start + key.length))
  }
  field.focus()
}

onBeforeUnmount(hide)
</script>

<template>
  <input
    ref="input"
    v-bind="$attrs"
    v-model="model"
    type="text"
    inputmode="decimal"
    autocomplete="off"
    :title="t('amount.formulaHint')"
    @focus="show"
    @blur="leave"
  />
  <Teleport v-if="open && host" :to="host">
    <div
      ref="keys"
      role="toolbar"
      :aria-label="t('amount.keys')"
      class="join fixed z-50 border border-base-300 bg-base-100 shadow-lg"
      :style="{ top: `${place.top}px`, left: `${place.left}px` }"
    >
      <button
        v-for="key in OPERATORS"
        :key="key"
        type="button"
        tabindex="-1"
        class="btn btn-sm join-item min-w-10 text-base"
        @pointerdown="press($event, key)"
      >
        {{ key }}
      </button>
      <button
        type="button"
        tabindex="-1"
        class="btn btn-sm btn-primary join-item min-w-10 text-base"
        @pointerdown="press($event, '=')"
      >
        =
      </button>
    </div>
  </Teleport>
</template>
