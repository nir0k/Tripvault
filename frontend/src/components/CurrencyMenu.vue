<script setup lang="ts">
import { nextTick, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { useCurrencies, type Currency } from '@/composables/useCurrencies'
import { useDropdown } from '@/composables/useDropdown'

// A currency as a compact dropdown, built like the theme, language and distance
// controls it sits beside on the profile: a small button naming the choice,
// opening on a list the typed letters narrow. The trip form keeps the wide
// CurrencySelect, whose label box suits a column of fields.
const code = defineModel<string>({ required: true })

const props = withDefaults(defineProps<{
  align?: 'start' | 'end'
  /** What the control chooses, read by assistive technology and on hover. */
  label: string
}>(), { align: 'end' })

const { t } = useI18n()
const { close } = useDropdown('menu')
const field = useTemplateRef<HTMLInputElement>('field')
const { query, chosen, matches } = useCurrencies(code)

// open clears the previous search and puts the cursor in it.
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
  <details
    ref="menu"
    class="dropdown"
    :class="{ 'dropdown-end': props.align === 'end' }"
    @toggle="($event.target as HTMLDetailsElement).open && open()"
  >
    <summary
      class="btn btn-ghost btn-sm gap-1 px-2"
      :aria-label="`${props.label}: ${chosen?.name ?? code}`"
      :title="props.label"
    >
      <AppIcon name="wallet" />
      <span>{{ chosen && chosen.symbol !== chosen.code ? `${chosen.symbol} ${chosen.code}` : code }}</span>
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <div class="dropdown-content z-20 mt-1 w-72 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
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
</template>
