<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Idea } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IdeaPicture from '@/components/ideas/IdeaPicture.vue'
import { formatMoney } from '@/utils/format'
import { countryFlag, countryName, formatDays, formatMonths, formatRange } from '@/utils/ideas'

// One idea as a card of the start page: its first photo, its name, where, when
// and for how long, and roughly what it costs. The whole card opens the idea.
const props = defineProps<{ idea: Idea }>()

const { t, locale } = useI18n()

const countries = computed(() => props.idea.countries.map((code) => `${countryFlag(code)} ${countryName(code, locale.value)}`).join(', '))
const months = computed(() => (props.idea.months.length === 12 ? t('ideas.anyTime') : formatMonths(props.idea.months, locale.value)))
const cost = computed(() => formatRange(props.idea.cost_min, props.idea.cost_max,
  (amount) => formatMoney(amount, props.idea.currency, locale.value)))
</script>

<template>
  <RouterLink
    :to="{ name: 'idea', params: { ideaId: idea.id } }"
    class="card overflow-hidden border border-base-300 bg-base-100 transition hover:border-primary/50 hover:shadow-md"
  >
    <figure class="aspect-video bg-base-200">
      <IdeaPicture v-if="idea.photos[0]" :idea-id="idea.id" :photo-id="idea.photos[0].id" class="size-full" />
      <AppIcon v-else name="lightbulb" class="size-8! opacity-30" />
    </figure>
    <div class="card-body gap-1 p-3">
      <h3 class="font-semibold break-words">{{ idea.title }}</h3>
      <p v-if="countries" class="truncate text-sm">{{ countries }}</p>
      <p class="text-sm text-base-content/70">
        {{ months }}<template v-if="formatDays(idea, t)"> · {{ formatDays(idea, t) }}</template>
      </p>
      <p v-if="cost" class="text-sm font-medium">≈ {{ cost }}</p>
    </div>
  </RouterLink>
</template>
