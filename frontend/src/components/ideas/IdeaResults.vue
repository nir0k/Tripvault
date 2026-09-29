<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { LocationQuery } from 'vue-router'
import type { Idea } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IdeaPicture from '@/components/ideas/IdeaPicture.vue'
import { formatMoney } from '@/utils/format'
import { countryFlag, formatRange } from '@/utils/ideas'

// What the list's search and filters found, beside the idea opened from it:
// each idea by its picture, its name, its flags and its cost, the open one
// marked, and a way back to the whole table with the same filter. Every link
// keeps the filter, so the reader walks the results without losing them.
defineProps<{
  ideas: Idea[]
  currentId: string
  /** The list's search and filters, as its address carries them. */
  query: LocationQuery
}>()

const emit = defineEmits<{
  /** An idea was chosen, which folds the panel on a phone. */
  chosen: []
}>()

const { t, locale } = useI18n()

// cost reads an idea's whole cost for the list.
function cost(idea: Idea): string {
  return formatRange(idea.cost_min, idea.cost_max, (amount) => formatMoney(amount, idea.currency, locale.value))
}
</script>

<template>
  <nav class="flex flex-col gap-2" :aria-label="t('ideas.results')">
    <div class="flex items-center justify-between gap-2">
      <span class="text-sm font-semibold">{{ t('ideas.resultsCount', { n: ideas.length }) }}</span>
      <RouterLink :to="{ name: 'ideas', query }" class="btn btn-ghost btn-xs">
        <AppIcon name="expand" class="size-3.5!" />
        {{ t('ideas.expandResults') }}
      </RouterLink>
    </div>
    <ul class="flex flex-col gap-1">
      <li v-for="idea in ideas" :key="idea.id">
        <RouterLink
          :to="{ name: 'idea', params: { ideaId: idea.id }, query }"
          class="flex items-center gap-2 rounded-field p-1.5 hover:bg-base-200"
          :class="{ 'bg-primary/10 ring-1 ring-primary/40': idea.id === currentId }"
          :aria-current="idea.id === currentId ? 'page' : undefined"
          @click="emit('chosen')"
        >
          <IdeaPicture
            v-if="idea.photos[0]"
            :idea-id="idea.id"
            :photo-id="idea.photos[0].id"
            class="size-10 shrink-0 rounded-field"
          />
          <span v-else class="flex size-10 shrink-0 items-center justify-center rounded-field bg-base-200" aria-hidden="true">
            <AppIcon name="lightbulb" class="size-4! opacity-40" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{{ idea.title }}</span>
            <span class="block truncate text-xs text-base-content/60">
              <span aria-hidden="true">{{ idea.countries.map(countryFlag).join(' ') }}</span>
              {{ cost(idea) }}
            </span>
          </span>
        </RouterLink>
      </li>
    </ul>
  </nav>
</template>
