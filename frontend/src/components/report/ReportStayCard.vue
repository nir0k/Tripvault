<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Media, PlanItem, Stay } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MediaGallery from '@/components/media/MediaGallery.vue'
import MediaUploader from '@/components/media/MediaUploader.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import EditableMarkdown from '@/components/report/EditableMarkdown.vue'
import { useReportText } from '@/composables/useContentLanguage'
import { formatMoney } from '@/utils/format'

// Where a day of a report ended: the stay of the night, after the day's last
// place, as the report's PDF ends a day's chapter. The night is the day's
// evening stay mark, which carries the story of that night - a story of each
// night rather than of the stay, which may last a week. While nobody told one,
// a reader sees the stay's own notes instead, as in the PDF. The night keeps
// its pictures as a place does - the report shows its favourites - but has no
// cover of its own.
const props = defineProps<{
  /** The day's evening stay mark. */
  night: PlanItem
  /** The stay the night is spent at. */
  stay: Stay
  currency: string
  editing: boolean
  /** The trip the night belongs to, which is what pictures are uploaded against. */
  tripId?: string
}>()

const emit = defineEmits<{
  story: [story: string]
  uploaded: [media: Media[]]
  privacy: [media: Media, isPrivate: boolean]
  favoriteMedia: [media: Media, isFavorite: boolean]
  unlinkMedia: [media: Media]
  removeMedia: [media: Media]
}>()

const { t, locale } = useI18n()
const text = useReportText()

const cost = computed(() => formatMoney(props.stay.actual_cost_amount ?? props.stay.planned_cost_amount,
  props.currency, locale.value))
</script>

<template>
  <article
    :id="`item-${night.id}`"
    class="scroll-mt-20 space-y-2 rounded-box border border-base-300 bg-base-200/50 p-3"
  >
    <header class="min-w-0 space-y-1">
      <p class="text-xs font-semibold tracking-wide text-primary uppercase">{{ t('report.night') }}</p>
      <h4 class="flex items-center gap-2 font-medium break-words">
        <AppIcon name="stay" class="size-4! shrink-0 opacity-70" />
        {{ stay.name }}
      </h4>
      <p class="flex flex-wrap items-center gap-2 text-sm text-base-content/70">
        <span>{{ t(`stayKinds.${stay.kind}`) }}</span>
        <span v-if="cost">{{ cost }}</span>
        <span v-if="stay.address" class="break-words">{{ stay.address }}</span>
      </p>
    </header>

    <EditableMarkdown
      v-if="editing || night.story_md"
      :source="text.source(night.id, 'story_md', night.story_md)"
      :original="text.original(night.id, 'story_md')"
      :editing="editing"
      :placeholder="t('report.nightPlaceholder')"
      :rows="3"
      @save="(story) => emit('story', story)"
    />
    <MarkdownText v-else-if="stay.notes_md" :source="stay.notes_md" />

    <MediaGallery
      v-if="night.media.length > 0"
      :items="night.media"
      :can-edit="editing"
      :can-cover="false"
      :trip-id="tripId"
      prefer-favorites
      :can-favorite="editing"
      @privacy="(media, isPrivate) => emit('privacy', media, isPrivate)"
      @favorite="(media, isFavorite) => emit('favoriteMedia', media, isFavorite)"
      @unlink="(media) => emit('unlinkMedia', media)"
      @remove="(media) => emit('removeMedia', media)"
    />
    <MediaUploader v-if="editing && tripId" :trip-id="tripId" small @uploaded="(media) => emit('uploaded', media)" />
  </article>
</template>
