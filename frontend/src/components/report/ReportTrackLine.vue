<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { downloadTrack } from '@/api/documents'
import { SHARED_MEDIA_BASE } from '@/api/media'
import type { Track } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TrackSpeedDialog from '@/components/plan/TrackSpeedDialog.vue'
import { useMediaBase } from '@/composables/useMediaUrl'
import { errorMessage } from '@/utils/errors'
import { formatDistance, formatElapsed, formatHeight, formatSpeed } from '@/utils/format'
import { formatDuration } from '@/utils/plan'
import { walkingSeconds } from '@/utils/trackTime'
import { activeUnits } from '@/utils/units'

// The line a place or an activity was really travelled in a report, or is meant
// to be travelled in a plan, imported from a watch, a phone or an outdoor app. It is one quiet row of figures - how far, how long, how
// much up, how much down - each told by its icon rather than a word, with the
// file one click away; the map above does the actual showing. How long is the
// whole recording, pauses included, as a watch's total time; a route drawn
// ahead carries no time and shows none. In a plan the row also tells how long
// the route takes to walk, worked out from its slopes at the line's own speed
// or the plan's, which somebody who may change the plan sets with the pencil.
const props = withDefaults(defineProps<{
  track: Track | null
  editing: boolean
  /** What importing a file here does, shown while there is none. */
  hint: string
  /** The plan's speed on the flat; null in a report, which shows no estimate. */
  planSpeed?: number | null
}>(), { planSpeed: null })

const emit = defineEmits<{
  import: [file: File]
  remove: []
  /** A speed of the line's own, or null for the plan's again. */
  speed: [speedKmh: number | null]
}>()

const { t, te, locale } = useI18n()

// elapsed is how many seconds the recording took, or null without its times.
const elapsed = computed(() => {
  const track = props.track
  if (!track?.started_at || !track.ended_at) {
    return null
  }
  return (Date.parse(track.ended_at) - Date.parse(track.started_at)) / 1000
})
// speed is what the line is timed at, and estimate how long it takes then, in
// minutes; both null in a report.
const speed = computed(() => (props.planSpeed === null ? null : props.track?.speed_kmh ?? props.planSpeed))
const estimate = computed(() => {
  if (!props.track || speed.value === null) {
    return null
  }
  return Math.max(1, Math.round(walkingSeconds(props.track, speed.value) / 60))
})
const speedDialog = useTemplateRef<InstanceType<typeof TrackSpeedDialog>>('speedDialog')
const speedButton = useTemplateRef<HTMLButtonElement>('speedButton')

// chooseSpeed opens the scale on the line's speed and passes on a change.
async function chooseSpeed(): Promise<void> {
  if (speed.value === null || props.planSpeed === null) {
    return
  }
  const choice = await speedDialog.value?.choose(speedButton.value, speed.value, props.planSpeed)
  if (choice && choice.speed !== (props.track?.speed_kmh ?? null)) {
    emit('speed', choice.speed)
  }
}

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
        <AppIcon name="distance" class="size-4! opacity-70" />
        <span class="sr-only">{{ t('track.distance') }}</span>
        {{ formatDistance(track.distance_m, locale, activeUnits, true) }}
      </span>
      <span v-if="estimate !== null && speed !== null" class="flex items-center gap-1" :title="t('track.estimate')">
        <AppIcon name="clock" class="size-4! opacity-70" />
        <span class="sr-only">{{ t('track.estimate') }}</span>
        ≈ {{ formatDuration(estimate, t) }}
        <span :class="track.speed_kmh === null ? 'text-base-content/60' : ''">
          · {{ formatSpeed(speed, locale, activeUnits) }}
        </span>
        <button
          v-if="editing"
          ref="speedButton"
          type="button"
          class="btn btn-ghost btn-xs btn-square"
          :aria-label="t('track.changeSpeed')"
          :title="t('track.changeSpeed')"
          @click="chooseSpeed"
        >
          <AppIcon name="pencil" class="size-3.5!" />
        </button>
      </span>
      <span v-if="elapsed !== null" class="flex items-center gap-1" :title="t('track.time')">
        <AppIcon name="clock" class="size-4! opacity-70" />
        <span class="sr-only">{{ t('track.time') }}</span>
        {{ formatElapsed(elapsed) }}
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
    <TrackSpeedDialog v-if="planSpeed !== null" ref="speedDialog" />
  </div>
</template>
