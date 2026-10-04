<script setup lang="ts">
import { computed, defineAsyncComponent, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { ActivityType, PlaceCategory, PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import IconSelect from '@/components/IconSelect.vue'
import ActivityStops from '@/components/plan/ActivityStops.vue'
import ItemAttachments from '@/components/plan/ItemAttachments.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import ReportTrackLine from '@/components/report/ReportTrackLine.vue'
import { useTripStore } from '@/stores/trip'
import { formatClock, formatMoney, formatTimeOfDay } from '@/utils/format'
import {
  activityTypeOptions, formatDuration, itemIcon, itemKindLabel, placeCategoryOptions, planTimeLabel,
} from '@/utils/plan'

// A card for one element of a day or of the unassigned list. Places carry the
// actions; stay marks can only be switched off for their day.
//
// For somebody who may change the plan the card is also where a place is
// written: its type, its notes in place, and a line of what can be added to it -
// the time, the cost, files such as tickets and, for an activity, the route. The
// full form, behind the menu, keeps the rest.
const props = defineProps<{
  item: PlanItem
  currency: string
  canEdit: boolean
  /** Whether the card sits first or last among the places of its list. */
  first?: boolean
  last?: boolean
  /** The place's number in its day; the unassigned list and stay marks have none. */
  number?: number
  /** The day's colour on the map, which the number is filled with. */
  color?: string
}>()

const emit = defineEmits<{
  locate: []
  edit: []
  update: [fields: PlaceFields]
  editTime: []
  editCost: []
  importTrack: [file: File]
  removeTrack: []
  trackSpeed: [speedKmh: number | null]
  up: []
  down: []
  move: []
  copy: []
  unassign: []
  remove: []
  hideAnchor: []
}>()

const { t, locale } = useI18n()

// The editor of the notes is loaded only once somebody who may write sees a
// card, so a reader of the plan never downloads it.
const RichTextEditor = defineAsyncComponent(() => import('@/components/RichTextEditor.vue'))

const isAnchor = computed(() => props.item.kind === 'stay_anchor')
// editable says whether the card is written in: a place or an activity of
// somebody who may change the plan.
const editable = computed(() => props.canEdit && !isAnchor.value)
const isActivity = computed(() => props.item.kind === 'activity')

// The plan's speed its routes are timed at, unless a route names its own.
const tripStore = useTripStore()
const planSpeed = computed(() => tripStore.trip?.track_speed_kmh ?? null)

// The type is a category for a place and a kind of activity for an activity.
const typeOptions = computed(() => (isActivity.value ? activityTypeOptions(t) : placeCategoryOptions(t)))
const typeValue = computed(() => (isActivity.value ? props.item.activity_type ?? 'other' : props.item.category))

// setType stores the type chosen on the card.
function setType(value: string): void {
  emit('update', isActivity.value ? { activity_type: value as ActivityType } : { category: value as PlaceCategory })
}

const timeLabel = computed(() => planTimeLabel(props.item.desired_time, props.item.visit_minutes, t))

const trackField = useTemplateRef<HTMLInputElement>('trackField')
const attachments = useTemplateRef<InstanceType<typeof ItemAttachments>>('attachments')

// onTrackPicked hands the chosen route over and empties the field, so the same
// file can be chosen again after it was removed.
function onTrackPicked(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) {
    emit('importTrack', file)
  }
  input.value = ''
}

// A morning mark is left at its departure; everything else is reached at its arrival.
const time = computed(() => {
  const schedule = props.item.schedule
  if (!schedule) {
    return null
  }
  const minutes = props.item.anchor === 'morning' ? schedule.departure_minutes : schedule.arrival_minutes
  return formatClock(minutes)
})

const cost = computed(() => {
  const amount = formatMoney(props.item.planned_cost_amount, props.currency, locale.value)
  return amount && props.item.cost_per_person ? `${amount} ${t('place.perPersonShort')}` : amount
})

const menuButton = useTemplateRef<HTMLElement>('menuButton')

// A place with no coordinates is nowhere on the map, so it does not offer to
// take the reader there.
const canLocate = computed(() => props.item.lat !== null && props.item.lng !== null)

// coordinates are the place's position as it is copied: latitude first, the
// way every map application reads a pasted pair.
const coordinates = computed(() => (canLocate.value ? `${props.item.lat}, ${props.item.lng}` : ''))
const googleMapsURL = computed(() => (canLocate.value
  ? `https://www.google.com/maps/search/?api=1&query=${props.item.lat},${props.item.lng}`
  : ''))

// copied says the pair has just been put on the clipboard, which the button's
// tooltip reports for a moment.
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

// copyCoordinates puts the place's position on the clipboard, where the
// browser allows it.
async function copyCoordinates(): Promise<void> {
  if (!coordinates.value) {
    return
  }
  try {
    await navigator.clipboard.writeText(coordinates.value)
  } catch {
    // Without clipboard access nothing is copied, and the tooltip says nothing.
    return
  }
  copied.value = true
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    copied.value = false
  }, 1500)
}

// onCardClick takes the map to this place. A click on something that does a job
// of its own is left to it: the menu and its button, a link, the drag handle.
// The menu's whole column counts as the menu, because on a phone taking the
// reader to the map swaps the list away, and a finger that lands just beside
// the small button must not do that.
function onCardClick(event: MouseEvent): void {
  const target = event.target as HTMLElement | null
  if (canLocate.value
    && !target?.closest('a, button, summary, input, label, [role="button"], [contenteditable], .dropdown, .drag-handle, [data-card-menu]')) {
    emit('locate')
  }
}

// host is the site a link points at, which reads better than the whole address.
const host = computed(() => {
  try {
    return new URL(props.item.url).host.replace(/^www\./, '')
  } catch {
    return props.item.url
  }
})
</script>

<template>
  <div
    :data-card-id="item.id"
    class="flex items-start gap-3 rounded-box border bg-base-100 p-3 transition-shadow"
    :class="[
      isAnchor ? 'border-dashed border-base-300 bg-base-200/60' : 'border-base-300',
      { 'opacity-60': item.is_optional, 'cursor-pointer': canLocate },
    ]"
    @click="onCardClick"
  >
    <span v-if="canEdit && !isAnchor" class="drag-handle mt-1 hidden cursor-grab text-base-content/40 lg:block" aria-hidden="true">⋮⋮</span>

    <div class="flex w-12 shrink-0 flex-col items-start text-sm tabular-nums">
      <span
        v-if="number !== undefined"
        class="badge badge-sm border-0 font-semibold text-white"
        :class="{ 'badge-neutral': !color }"
        :style="color ? { backgroundColor: color } : undefined"
      >{{ number }}</span>
      <template v-if="time">
        <span class="font-semibold">{{ time.time }}</span>
        <span v-if="time.dayOffset > 0" class="text-xs text-base-content/70">+{{ time.dayOffset }}</span>
      </template>
    </div>

    <div class="min-w-0 flex-1">
      <p class="flex flex-wrap items-center gap-2">
        <span v-if="isAnchor" class="badge badge-ghost badge-sm">{{ t(`plan.anchor.${item.anchor}`) }}</span>
        <button
          v-if="canLocate"
          type="button"
          class="text-start font-medium break-words hover:underline"
          :title="t('map.locate')"
          @click="emit('locate')"
        >
          {{ item.name }}
        </button>
        <span v-else class="font-medium break-words">{{ item.name }}</span>
        <span v-if="item.is_optional" class="badge badge-outline badge-sm">{{ t('place.optional') }}</span>
        <!-- Taking the place elsewhere: into Google Maps, or its position onto
             the clipboard. Without coordinates both are there but greyed out,
             so the reader learns why rather than wondering where they went. -->
        <span class="flex items-center">
          <span class="tooltip" :data-tip="canLocate ? t('place.openInGoogleMaps') : t('place.noCoordinates')">
            <a
              v-if="canLocate"
              :href="googleMapsURL"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-ghost btn-xs btn-square"
              :aria-label="t('place.openInGoogleMaps')"
            >
              <AppIcon name="mapPin" class="size-4!" />
            </a>
            <span v-else class="btn btn-ghost btn-xs btn-square btn-disabled" role="img" :aria-label="t('place.noCoordinates')">
              <AppIcon name="mapPin" class="size-4!" />
            </span>
          </span>
          <span
            class="tooltip"
            :data-tip="!canLocate ? t('place.noCoordinates') : copied ? t('place.coordinatesCopied') : t('place.copyCoordinates')"
          >
            <button
              type="button"
              class="btn btn-ghost btn-xs btn-square"
              :class="{ 'btn-disabled': !canLocate }"
              :aria-disabled="!canLocate"
              :aria-label="t('place.copyCoordinates')"
              @click="copyCoordinates"
            >
              <AppIcon :name="copied ? 'check' : 'copy'" class="size-4!" />
            </button>
          </span>
        </span>
      </p>
      <p v-if="!isAnchor && !editable" class="flex flex-wrap items-center gap-x-2 text-sm text-base-content/70">
        <span class="flex items-center gap-1">
          <AppIcon :name="itemIcon(item)" class="size-4!" />
          {{ itemKindLabel(item, t) }}
        </span>
        <span v-if="item.visit_minutes > 0">· {{ formatDuration(item.visit_minutes, t) }}</span>
        <span v-if="item.desired_time">· {{ t('place.desiredAt', { time: formatTimeOfDay(item.desired_time) }) }}</span>
        <span v-if="cost">· {{ cost }}</span>
      </p>
      <p v-if="item.address" class="truncate text-sm text-base-content/60">{{ item.address }}</p>
      <p v-if="!isAnchor && (item.booking_ref || item.url)" class="flex flex-wrap items-center gap-x-3 text-sm">
        <span v-if="item.booking_ref" class="text-base-content/60">
          {{ t('place.bookingRef') }}: {{ item.booking_ref }}
        </span>
        <a
          v-if="item.url"
          :href="item.url"
          target="_blank"
          rel="noopener noreferrer"
          class="link link-primary truncate"
        >{{ host }}</a>
      </p>
      <p v-if="item.schedule?.late" class="mt-1">
        <span class="badge badge-warning badge-sm h-auto py-0.5">{{ t('place.late', { time: formatTimeOfDay(item.desired_time) }) }}</span>
      </p>
      <div v-if="editable" class="mt-1 flex flex-col items-start gap-1">
        <IconSelect
          :model-value="typeValue"
          :options="typeOptions"
          :label="isActivity ? t('place.activityType') : t('place.category')"
          compact
          @update:model-value="setType"
        />
        <RichTextEditor
          class="w-full"
          :model-value="item.description_md"
          :placeholder="t('place.notesPlaceholder')"
          :label="t('place.description')"
          @commit="(markdown) => emit('update', { description_md: markdown })"
        />
        <div class="-ms-2 flex flex-wrap items-center gap-1 text-sm">
          <button type="button" class="btn btn-ghost btn-xs" @click="emit('editTime')">
            <AppIcon name="clock" class="size-4!" />
            {{ timeLabel || t('place.addTime') }}
          </button>
          <button type="button" class="btn btn-ghost btn-xs" @click="emit('editCost')">
            <AppIcon name="dollar" class="size-4!" />
            {{ cost || t('place.addCost') }}
          </button>
          <button type="button" class="btn btn-ghost btn-xs" @click="attachments?.pick()">
            <AppIcon name="paperclip" class="size-4!" />
            {{ t('attachment.add') }}
          </button>
          <button v-if="isActivity && !item.track" type="button" class="btn btn-ghost btn-xs" @click="trackField?.click()">
            <AppIcon name="upload" class="size-4!" />
            {{ t('track.add') }}
          </button>
        </div>
        <input ref="trackField" type="file" accept=".gpx,.kml,application/gpx+xml" class="hidden" @change="onTrackPicked" />
      </div>
      <MarkdownText v-else-if="item.description_md" :source="item.description_md" class="mt-1" />
      <ReportTrackLine
        v-if="item.track"
        :track="item.track"
        :editing="editable"
        hint=""
        :plan-speed="planSpeed"
        class="mt-2"
        @import="(file) => emit('importTrack', file)"
        @remove="emit('removeTrack')"
        @speed="(speed) => emit('trackSpeed', speed)"
      />
      <ActivityStops
        v-if="item.track"
        :item="item"
        :editing="editable"
        :report="false"
        :number="number"
        class="mt-1"
      />
      <ItemAttachments
        v-if="!isAnchor"
        ref="attachments"
        :item-id="item.id"
        :attachments="item.attachments"
        :editing="editable"
        class="mt-2"
      />
    </div>

    <!-- The menu's column reaches the card's edges, so a tap beside the button
         opens the menu rather than the map. -->
    <div
      v-if="canEdit"
      data-card-menu
      class="-my-3 -ms-2 -me-3 self-stretch py-3 ps-2 pe-3"
      @click.self="menuButton?.focus()"
    >
      <div class="dropdown dropdown-end">
        <div
          ref="menuButton"
          tabindex="0"
          role="button"
          class="btn btn-ghost btn-sm btn-square pointer-coarse:btn-md"
          :aria-label="t('plan.itemActions', { name: item.name })"
        >
          <AppIcon name="dots" />
        </div>
        <ul tabindex="0" class="menu dropdown-content z-20 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
          <template v-if="isAnchor">
            <li><button type="button" @click="emit('hideAnchor')">{{ t('plan.hideAnchor') }}</button></li>
          </template>
          <template v-else>
            <li><button type="button" @click="emit('edit')">{{ t('plan.edit') }}</button></li>
            <li v-if="!first"><button type="button" @click="emit('up')">{{ t('plan.moveUp') }}</button></li>
            <li v-if="!last"><button type="button" @click="emit('down')">{{ t('plan.moveDown') }}</button></li>
            <li><button type="button" @click="emit('move')">{{ item.day_id ? t('plan.moveToDay') : t('plan.assignDay') }}</button></li>
            <li><button type="button" @click="emit('copy')">{{ t('plan.copyToDay') }}</button></li>
            <li v-if="item.day_id"><button type="button" @click="emit('unassign')">{{ t('plan.unassign') }}</button></li>
            <li><button type="button" class="text-error" @click="emit('remove')">{{ t('plan.delete') }}</button></li>
          </template>
        </ul>
      </div>
    </div>
  </div>
</template>
