<script setup lang="ts">
import { nextTick, useId, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useCurrencies, describeCurrency as describe, type Currency } from '@/composables/useCurrencies'
import { useDropdown } from '@/composables/useDropdown'

// A currency chosen by typing: the list narrows as the letters arrive, and each
// row reads "€ Euro" - the symbol first, because that is what a person looks for.
//
// The control is an input group: the field's name sits in a plain box on the
// left, and the choice takes the rest of the width, so a form of stacked fields
// still reads as a list of names and values.
const code = defineModel<string>({ required: true })

// The name in the box on the left. A trip's own currency is the usual case, so
// that is the default.
const props = withDefaults(defineProps<{ label?: string }>(), { label: '' })

const { t } = useI18n()

const { close } = useDropdown('menu')
// The box on the left names the control, so the choice points at it rather than
// carrying the name a second time.
const nameId = useId()
const field = useTemplateRef<HTMLInputElement>('field')
const { query, chosen, matches } = useCurrencies(code)

// open clears the previous search so the whole list is there to scroll.
function open(): void {
  query.value = ''
  void nextTick(() => field.value?.focus())
}

// choose applies a currency and folds the list.
function choose(currency: Currency): void {
  close()
  code.value = currency.code
}
</script>

<template>
  <div class="flex w-full">
    <span
      :id="nameId"
      class="flex items-center whitespace-nowrap rounded-s-[var(--radius-field)] border border-e-0
             border-base-300 bg-base-200 px-3 text-sm"
    >
      {{ props.label || t('tripForm.currency') }}
    </span>
    <details ref="menu" class="dropdown min-w-0 flex-1" @toggle="($event.target as HTMLDetailsElement).open && open()">
      <summary
        class="input w-full cursor-pointer items-center justify-between rounded-s-none"
        :aria-labelledby="nameId"
      >
        <span class="truncate">{{ chosen ? describe(chosen) : code || t('tripForm.currency') }}</span>
        <AppIcon name="chevronDown" class="size-3!" />
      </summary>
      <div class="dropdown-content z-20 mt-1 w-full min-w-64 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
        <input
          ref="field"
          v-model="query"
          type="search"
          class="input input-sm w-full"
          :placeholder="t('tripForm.currencySearch')"
          :aria-label="t('tripForm.currencySearch')"
        />
        <ul class="menu mt-1 max-h-64 w-full flex-nowrap overflow-y-auto p-0">
          <li v-for="currency in matches" :key="currency.code">
            <button
              type="button"
              :class="{ 'menu-active': currency.code === code }"
              :aria-pressed="currency.code === code"
              @click="choose(currency)"
            >
              <span class="w-8 shrink-0 text-end font-medium">{{ currency.symbol }}</span>
              <span class="truncate">{{ currency.name }}</span>
              <span class="ms-auto text-xs text-base-content/60">{{ currency.code }}</span>
            </button>
          </li>
        </ul>
        <p v-if="matches.length === 0" class="p-2 text-sm text-base-content/60">{{ t('tripForm.currencyNone') }}</p>
      </div>
    </details>
  </div>
</template>
