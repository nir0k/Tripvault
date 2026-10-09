<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/api/client'
import type { StopFields } from '@/api/documents'
import { parseLink } from '@/api/geo'
import type { ClientConfig, Stop, StopKind } from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import IconSelect from '@/components/IconSelect.vue'
import MapPicker from '@/components/MapPicker.vue'
import TimeInput from '@/components/TimeInput.vue'
import { errorMessage } from '@/utils/errors'
import { formatDistance, normalizeAmount } from '@/utils/format'
import { distanceBetween } from '@/utils/mediaHints'
import { stopKindOptions } from '@/utils/plan'
import { nearestOnLine, type LatLng } from '@/utils/polyline'
import { activeUnits } from '@/utils/units'

// A stop along the line of an activity: what it is, what it is called, a note,
// and where on the line it lies, picked on a small map of the line - a click
// lands on the nearest point of it - or found from coordinates or a map link,
// read by the server as a place's are and put on the line the same way. In a report it also takes when the stop
// was reached and what was really spent there. Its planned cost, who pays it
// and how it is shared are set apart, in the form a place's cost is set in.
const props = defineProps<{
  /** Whether the stop belongs to a report. */
  report: boolean
  /** The map's tiles, or null while they are not known. */
  config: ClientConfig | null
}>()

const emit = defineEmits<{
  save: [fields: StopFields, stop: Stop | null]
}>()

const { t, te, locale } = useI18n()

/** FAR_FROM_LINE is how far, in metres, a point found may lie from the line before the form says so. */
const FAR_FROM_LINE = 500

const kinds = computed(() => stopKindOptions(t))

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const stop = ref<Stop | null>(null)
const line = ref<LatLng[]>([])
// opened counts the openings, so the map is drawn afresh for each.
const opened = ref(0)
const visible = ref(false)
const error = ref('')
// locator is the coordinates or the link typed, and locating what is said of it.
const locator = ref('')
const locating = ref(false)
const located = ref('')
const form = reactive({
  kind: 'food' as StopKind, name: '', note: '', lat: null as number | null, lng: null as number | null,
  time: '', spent: '',
})

/**
 * open shows the form of a stop, or of a new one.
 *
 * Arguments:
 *   - target: the stop, or null for a new one.
 *   - track: the line the stop lies on.
 *   - point: where a new stop was picked, when it was.
 */
function open(target: Stop | null, track: LatLng[], point?: { lat: number; lng: number }): void {
  stop.value = target
  line.value = track
  error.value = ''
  Object.assign(form, {
    kind: target?.kind ?? 'food',
    name: target?.name ?? '',
    note: target?.note_md ?? '',
    lat: target?.lat ?? point?.lat ?? null,
    lng: target?.lng ?? point?.lng ?? null,
    time: target?.actual_time ?? '',
    spent: target?.actual_cost_amount ?? '',
  })
  locator.value = ''
  located.value = ''
  opened.value++
  visible.value = true
  dialog.value?.showModal()
}

/** close hides the form. */
function close(): void {
  dialog.value?.close()
}

/** fail shows why saving did not work, keeping the form open. */
function fail(message: string): void {
  error.value = message
}

// pick puts the stop where the map was clicked, on the line.
function pick(lat: number, lng: number): void {
  form.lat = lat
  form.lng = lng
  located.value = ''
}

// locate reads a position from the coordinates or the link typed and puts the
// stop on the nearest point of the line, saying how far that was when the
// point lies well off the line. A name the link carries names a stop without one.
async function locate(): Promise<void> {
  const text = locator.value.trim()
  if (!text || locating.value) {
    return
  }
  locating.value = true
  located.value = ''
  error.value = ''
  try {
    const found = await parseLink(text, locale.value)
    const [lat, lng] = line.value.length > 0 ? nearestOnLine(line.value, [found.lat, found.lng]) : [found.lat, found.lng]
    form.lat = Number(lat.toFixed(6))
    form.lng = Number(lng.toFixed(6))
    if (found.name && !form.name.trim()) {
      form.name = found.name
    }
    const away = distanceBetween(found.lat, found.lng, lat, lng)
    if (away > FAR_FROM_LINE) {
      located.value = t('stop.farFromTrack', { distance: formatDistance(away, locale.value, activeUnits.value) })
    }
    locator.value = ''
    opened.value++
  } catch (err) {
    error.value = err instanceof ApiError && err.code === 'validation_failed'
      ? t('location.unrecognized')
      : errorMessage(err, t, te)
  } finally {
    locating.value = false
  }
}

// submit hands the fields to the page. A new stop needs its point; a stop
// keeps its own unless it was moved.
function submit(): void {
  if (form.lat === null || form.lng === null) {
    error.value = t('stop.pointRequired')
    return
  }
  const fields: StopFields = { kind: form.kind, name: form.name.trim(), note_md: form.note }
  const moved = !stop.value || stop.value.lat !== form.lat || stop.value.lng !== form.lng
  if (moved) {
    fields.lat = form.lat
    fields.lng = form.lng
  }
  if (props.report) {
    fields.actual_time = form.time || null
    fields.actual_cost_amount = normalizeAmount(form.spent)
  }
  emit('save', fields, stop.value)
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle" @close="visible = false">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ stop ? t('stop.editTitle') : t('stop.newTitle') }}</h2>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="flex flex-col gap-1">
          <span class="sr-only">{{ t('stop.kind') }}</span>
          <IconSelect v-model="form.kind" :options="kinds" :label="t('stop.kind')" block />
        </label>
        <label class="floating-label">
          <span>{{ t('stop.name') }}</span>
          <input v-model="form.name" type="text" maxlength="200" class="input w-full" :placeholder="t('stop.name')" />
        </label>
      </div>

      <label class="floating-label">
        <span>{{ t('stop.note') }}</span>
        <textarea v-model="form.note" rows="3" maxlength="20000" class="textarea w-full" :placeholder="t('stop.note')"></textarea>
      </label>

      <div v-if="report" class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('stop.reachedAt') }}</span>
          <TimeInput v-model="form.time" class="input w-full" :label="t('stop.reachedAt')" />
        </label>
        <label class="floating-label">
          <span>{{ t('stop.spent') }}</span>
          <AmountInput v-model="form.spent" class="input w-full" :placeholder="t('stop.spent')" />
        </label>
      </div>

      <div class="flex flex-col gap-1">
        <span class="text-sm text-base-content/70">{{ t('stop.pointHint') }}</span>
        <div class="join w-full">
          <input
            v-model="locator"
            type="text"
            maxlength="2000"
            class="input join-item w-full"
            :placeholder="t('stop.locator')"
            :aria-label="t('stop.locator')"
            @keydown.enter.prevent="locate"
          />
          <button type="button" class="btn join-item" :disabled="!locator.trim() || locating" @click="locate">
            <span v-if="locating" class="loading loading-spinner loading-xs"></span>
            {{ t('stop.find') }}
          </button>
        </div>
        <p v-if="located" role="status" class="text-sm text-warning">{{ located }}</p>
        <MapPicker
          v-if="visible && config"
          :key="opened"
          :lat="form.lat"
          :lng="form.lng"
          :focus="null"
          :line="line"
          :tile-url="config.map_tile_url"
          :attribution="config.map_attribution"
          @pick="pick"
        />
      </div>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
