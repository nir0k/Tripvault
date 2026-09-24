<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanItem } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import MarkdownText from '@/components/MarkdownText.vue'
import { formatClock, formatMoney } from '@/utils/format'
import { formatDuration, itemIcon, itemKindLabel } from '@/utils/plan'

// A card for one element of a day or of the unassigned list. Places carry the
// actions; stay marks can only be switched off for their day.
const props = defineProps<{
  item: PlanItem
  currency: string
  canEdit: boolean
  /** Whether the card sits first or last among the places of its list. */
  first?: boolean
  last?: boolean
  /** The place's number in its day; the unassigned list and stay marks have none. */
  number?: number
}>()

const emit = defineEmits<{
  locate: []
  edit: []
  up: []
  down: []
  move: []
  copy: []
  unassign: []
  remove: []
  hideAnchor: []
}>()

const { t, locale } = useI18n()

const isAnchor = computed(() => props.item.kind === 'stay_anchor')

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

// A place with no coordinates is nowhere on the map, so it does not offer to
// take the reader there.
const canLocate = computed(() => props.item.lat !== null && props.item.lng !== null)

// onCardClick takes the map to this place. A click on something that does a job
// of its own is left to it: the menu and its button, a link, the drag handle.
function onCardClick(event: MouseEvent): void {
  const target = event.target as HTMLElement | null
  if (canLocate.value && !target?.closest('a, button, summary, input, label, [role="button"], .drag-handle')) {
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
      <span v-if="number !== undefined" class="badge badge-neutral badge-sm">{{ number }}</span>
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
      </p>
      <p v-if="!isAnchor" class="flex flex-wrap items-center gap-x-2 text-sm text-base-content/70">
        <span class="flex items-center gap-1">
          <AppIcon :name="itemIcon(item)" class="size-4!" />
          {{ itemKindLabel(item, t) }}
        </span>
        <span v-if="item.visit_minutes > 0">· {{ formatDuration(item.visit_minutes, t) }}</span>
        <span v-if="item.desired_time">· {{ t('place.desiredAt', { time: item.desired_time }) }}</span>
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
        <span class="badge badge-warning badge-sm h-auto py-0.5">{{ t('place.late', { time: item.desired_time }) }}</span>
      </p>
      <MarkdownText v-if="item.description_md" :source="item.description_md" class="mt-1" />
    </div>

    <div v-if="canEdit" class="dropdown dropdown-end">
      <div tabindex="0" role="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('plan.itemActions', { name: item.name })">
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
</template>
