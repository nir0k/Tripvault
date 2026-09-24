<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { downloadTrack } from '@/api/documents'
import { SHARED_MEDIA_BASE } from '@/api/media'
import type { Track } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { useMediaBase } from '@/composables/useMediaUrl'
import { errorMessage } from '@/utils/errors'
import { formatDistance, formatHeight } from '@/utils/format'
import { activeUnits } from '@/utils/units'

// The line a day, a place or an activity was really travelled, imported from a
// watch or a phone. It is one quiet row of figures - how far, how much up, how
// much down - each told by its icon rather than a word, with the file one click
// away; the map above does the actual showing.
defineProps<{
  track: Track | null
  editing: boolean
  /** What importing a file here does, shown while there is none. */
  hint: string
}>()

const emit = defineEmits<{
  import: [file: File]
  remove: []
}>()

const { t, te, locale } = useI18n()
const field = useTemplateRef<HTMLInputElement>('field')
const dragging = ref(false)
const downloading = ref(false)
const error = ref('')
// A read-only link reads the file through its own path, like its pictures.
const shared = useMediaBase() === SHARED_MEDIA_BASE

// onPick hands the chosen file over and empties the field, so the same file can
// be imported again after it was removed.
function onPick(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) {
    emit('import', file)
  }
  input.value = ''
}

// onDrop takes a file dropped on the row.
function onDrop(event: DragEvent): void {
  dragging.value = false
  const file = event.dataTransfer?.files?.[0]
  if (file) {
    emit('import', file)
  }
}

// download saves the file the track was imported from.
async function download(track: Track): Promise<void> {
  downloading.value = true
  error.value = ''
  try {
    await downloadTrack(track, shared)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    downloading.value = false
  }
}
</script>

<template>
  <div
    v-if="track || editing"
    class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-box px-3 py-2 text-sm"
    :class="dragging ? 'bg-primary/10' : 'bg-base-200'"
    @dragover.prevent="editing && (dragging = true)"
    @dragleave="dragging = false"
    @drop.prevent="editing && onDrop($event)"
  >
    <AppIcon name="map" class="size-4! opacity-70" />
    <template v-if="track">
      <span class="flex items-center gap-1" :title="t('track.distance')">
        <AppIcon name="ruler" class="size-4! opacity-70" />
        <span class="sr-only">{{ t('track.distance') }}</span>
        {{ formatDistance(track.distance_m, locale, activeUnits) }}
      </span>
      <span v-if="track.ascent_m !== null" class="flex items-center gap-1" :title="t('track.ascent')">
        <AppIcon name="ascent" class="size-4! text-success" />
        <span class="sr-only">{{ t('track.ascent') }}</span>
        {{ formatHeight(track.ascent_m, locale, activeUnits) }}
      </span>
      <span v-if="track.descent_m !== null" class="flex items-center gap-1" :title="t('track.descent')">
        <AppIcon name="descent" class="size-4! text-warning" />
        <span class="sr-only">{{ t('track.descent') }}</span>
        {{ formatHeight(track.descent_m, locale, activeUnits) }}
      </span>
      <span class="min-w-0 flex-1"></span>
      <button
        type="button"
        class="btn btn-ghost btn-xs btn-square"
        :disabled="downloading"
        :aria-label="t('track.download', { name: track.original_name || track.format.toUpperCase() })"
        :title="t('track.download', { name: track.original_name || track.format.toUpperCase() })"
        @click="download(track)"
      >
        <span v-if="downloading" class="loading loading-spinner loading-xs"></span>
        <AppIcon v-else name="download" class="size-4!" />
      </button>
      <button v-if="editing" type="button" class="btn btn-ghost btn-xs" @click="field?.click()">
        {{ t('track.replace') }}
      </button>
      <button v-if="editing" type="button" class="btn btn-ghost btn-xs text-error" @click="emit('remove')">
        {{ t('track.remove') }}
      </button>
    </template>
    <template v-else>
      <span class="min-w-0 flex-1 truncate opacity-70">{{ hint }}</span>
      <button type="button" class="btn btn-xs btn-hover-outline" @click="field?.click()">
        {{ t('track.import') }}
      </button>
    </template>
    <p v-if="error" role="alert" class="w-full text-error">{{ error }}</p>
    <input ref="field" type="file" accept=".gpx,.kml,application/gpx+xml" class="hidden" @change="onPick" />
  </div>
</template>
