<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { IdeaChange } from '@/api/types'
import { formatDateTime } from '@/utils/format'

// The history of a list of ideas, or of one idea: who did what, and when, the
// newest first. Read for a whole list it names each idea, linked while it is
// still there and struck through under the title it had once it is deleted.
withDefaults(defineProps<{
  changes: IdeaChange[]
  /** Whether each entry names its idea, as the history of a whole list does. */
  withTitles?: boolean
}>(), { withTitles: false })

const { t, locale } = useI18n()
</script>

<template>
  <p v-if="changes.length === 0" class="text-sm text-base-content/60">{{ t('ideas.history.empty') }}</p>
  <ol v-else class="flex flex-col divide-y divide-base-300">
    <li v-for="change in changes" :key="change.id" class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-2 text-sm">
      <span class="font-medium">{{ change.user?.display_name ?? t('ideas.deletedUser') }}</span>
      <span class="text-base-content/70">{{ t(`ideas.history.actions.${change.action}`) }}</span>
      <template v-if="withTitles">
        <RouterLink v-if="change.idea_id" :to="{ name: 'idea', params: { ideaId: change.idea_id } }" class="link">
          {{ change.idea_title }}
        </RouterLink>
        <span v-else class="line-through opacity-70">{{ change.idea_title }}</span>
      </template>
      <time class="ms-auto text-xs whitespace-nowrap text-base-content/60" :datetime="change.created_at">
        {{ formatDateTime(change.created_at, locale) }}
      </time>
    </li>
  </ol>
</template>
