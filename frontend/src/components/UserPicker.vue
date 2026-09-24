<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TripUser } from '@/api/types'

// search finds people for the typed text; exclude hides people who cannot be
// chosen, such as those already on the trip.
const props = withDefaults(defineProps<{
  search: (query: string) => Promise<TripUser[]>
  exclude?: readonly string[]
}>(), { exclude: () => [] })
const selected = defineModel<TripUser | null>({ default: null })

const { t } = useI18n()

const query = ref('')
const results = ref<TripUser[]>([])
const searching = ref(false)
// searched is set once an answer for the current text arrived, so "nobody
// found" is not shown while the pause before the search is still running.
const searched = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let generation = 0

// Typing clears a previous choice and searches after a short pause. Only the
// newest answer is shown, so a slow early response cannot replace a later one.
watch(query, (text) => {
  if (selected.value && text === label(selected.value)) {
    return
  }
  selected.value = null
  clearTimeout(timer)
  generation++
  searched.value = false
  searching.value = false
  results.value = []
  const trimmed = text.trim()
  if (trimmed.length < 2) {
    return
  }
  timer = setTimeout(async () => {
    const current = ++generation
    searching.value = true
    let found: TripUser[]
    try {
      found = await props.search(trimmed)
    } catch {
      found = []
    }
    if (current === generation) {
      results.value = found.filter((user) => !props.exclude.includes(user.id))
      searching.value = false
      searched.value = true
    }
  }, 300)
})

onBeforeUnmount(() => clearTimeout(timer))

// label renders a person the way the field shows a choice.
function label(user: TripUser): string {
  return `${user.display_name} <${user.email}>`
}

// choose picks one of the results.
function choose(user: TripUser): void {
  selected.value = user
  query.value = label(user)
  results.value = []
}

/** reset empties the field, after the chosen person was used. */
function reset(): void {
  selected.value = null
  query.value = ''
}

defineExpose({ reset })
</script>

<template>
  <div class="relative">
    <label class="input w-full">
      <input v-model="query" type="search" autocomplete="off" :placeholder="t('members.searchPlaceholder')" :aria-label="t('members.searchPlaceholder')" />
      <span v-if="searching" class="loading loading-spinner loading-xs"></span>
    </label>
    <ul v-if="results.length > 0" class="menu absolute z-20 mt-1 w-full rounded-box border border-base-300 bg-base-100 p-1 shadow-lg">
      <li v-for="user in results" :key="user.id">
        <button type="button" class="flex flex-col items-start gap-0" @click="choose(user)">
          <span class="font-medium">{{ user.display_name }}</span>
          <span class="text-sm text-base-content/70">{{ user.email }}</span>
        </button>
      </li>
    </ul>
    <p v-else-if="searched && !selected" class="mt-1 text-sm text-base-content/70">
      {{ t('members.noResults') }}
    </p>
  </div>
</template>
