<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { COUNTRY_CODES, countryFlag, countryName } from '@/utils/ideas'

// Countries chosen by typing a few letters of their name, in the reader's
// language, or of their code. The chosen ones are pills in the order they were
// chosen, the first being the main one; the matches are listed under the field
// rather than in a menu, which a dialog would cut off.
const countries = defineModel<string[]>({ required: true })

defineProps<{
  /** What the field chooses, read out and shown as its placeholder. */
  label: string
}>()

const { t, locale } = useI18n()
const query = ref('')

// named pairs every code with its name, sorted by name for the reader.
const named = computed(() => COUNTRY_CODES
  .map((code) => ({ code, name: countryName(code, locale.value) }))
  .sort((a, b) => a.name.localeCompare(b.name, locale.value)))

// matches are the countries not chosen yet whose name or code starts with what
// was typed, a word of the name counting too, so "korea" finds both Koreas.
const matches = computed(() => {
  const text = query.value.trim().toLocaleLowerCase(locale.value)
  if (text === '') {
    return []
  }
  return named.value.filter(({ code, name }) => {
    if (countries.value.includes(code)) {
      return false
    }
    const lower = name.toLocaleLowerCase(locale.value)
    return code.toLowerCase() === text || lower.startsWith(text) || lower.split(/[\s-]+/).some((word) => word.startsWith(text))
  }).slice(0, 8)
})

// add chooses a country and empties the field for the next one.
function add(code: string): void {
  countries.value = [...countries.value, code]
  query.value = ''
}

// remove takes a country off the choice.
function remove(code: string): void {
  countries.value = countries.value.filter((item) => item !== code)
}

// addFirst chooses the first match on Enter, so a country is chosen from the keyboard.
function addFirst(): void {
  const [first] = matches.value
  if (first) {
    add(first.code)
  }
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div v-if="countries.length > 0" class="flex flex-wrap gap-1">
      <span v-for="code in countries" :key="code" class="badge badge-ghost gap-1">
        <span aria-hidden="true">{{ countryFlag(code) }}</span>
        {{ countryName(code, locale) }}
        <button
          type="button"
          class="cursor-pointer opacity-60 hover:opacity-100"
          :aria-label="t('ideas.removeCountry', { name: countryName(code, locale) })"
          @click="remove(code)"
        >
          <AppIcon name="close" class="size-3!" />
        </button>
      </span>
    </div>
    <input
      v-model="query"
      type="search"
      class="input w-full"
      :placeholder="label"
      :aria-label="label"
      @keydown.enter.prevent="addFirst"
    />
    <ul v-if="matches.length > 0" class="menu w-full rounded-box border border-base-300 bg-base-100 p-1">
      <li v-for="match in matches" :key="match.code">
        <button type="button" @click="add(match.code)">
          <span aria-hidden="true">{{ countryFlag(match.code) }}</span>
          {{ match.name }}
        </button>
      </li>
    </ul>
  </div>
</template>
