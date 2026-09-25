<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Transfer } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import { TRANSFER_KIND_ICONS } from '@/components/icons'
import MarkdownText from '@/components/MarkdownText.vue'
import { formatDayDate, formatMoney, formatTimeOfDay } from '@/utils/format'

// One booked journey: how it travels, from where to where, when it leaves and
// arrives and what it costs. The list of transfers shows it with its booking
// and notes; a day shows it briefly, beside the places it does not take part in.
const props = defineProps<{
  transfer: Transfer
  currency: string
  /** Shows the booking, the link and the notes as well. */
  detailed?: boolean
  /**
   * The date of the day showing the transfer; an end on another date names its
   * date, one on this date only its time.
   */
  date?: string | null
}>()

const { t, locale } = useI18n()

const arrivalDate = computed(() => props.transfer.arrival_date ?? props.transfer.departure_date)

// when reads one end of the journey: its date, unless it is the date of the
// day showing it, and its time in the reader's clock.
function when(date: string, time: string | null): string {
  const clock = formatTimeOfDay(time)
  if (props.date === date) {
    return clock
  }
  const day = formatDayDate(date, locale.value)
  return clock ? `${day}, ${clock}` : day
}

const departs = computed(() => when(props.transfer.departure_date, props.transfer.departure_time))
const arrives = computed(() => {
  if (arrivalDate.value !== props.transfer.departure_date) {
    return when(arrivalDate.value, props.transfer.arrival_time)
  }
  // An arrival on the day of departure needs no date of its own: the
  // departure already said it.
  return formatTimeOfDay(props.transfer.arrival_time)
})

const cost = computed(() => {
  const amount = formatMoney(props.transfer.planned_cost_amount, props.currency, locale.value)
  return amount && props.transfer.cost_per_person ? `${amount} ${t('place.perPersonShort')}` : amount
})

// host is the site a link points at, which reads better than the whole address.
const host = computed(() => {
  try {
    return new URL(props.transfer.url).host.replace(/^www\./, '')
  } catch {
    return props.transfer.url
  }
})
</script>

<template>
  <div class="flex min-w-0 items-start gap-3">
    <AppIcon :name="TRANSFER_KIND_ICONS[transfer.kind]" class="mt-0.5 size-5! shrink-0 opacity-70" />
    <div class="min-w-0 flex-1 space-y-0.5">
      <p class="flex flex-wrap items-center gap-x-2">
        <span class="font-medium break-words">{{ transfer.from_name }} → {{ transfer.to_name }}</span>
        <span class="badge badge-ghost badge-sm">{{ t(`transferKinds.${transfer.kind}`) }}</span>
        <span v-if="transfer.name" class="text-sm text-base-content/70">{{ transfer.name }}</span>
      </p>
      <p class="flex flex-wrap gap-x-2 text-sm text-base-content/70">
        <span v-if="departs">{{ t('transfer.departs', { time: departs }) }}</span>
        <span v-if="arrives">· {{ t('transfer.arrives', { time: arrives }) }}</span>
        <span v-if="cost">· {{ cost }}</span>
      </p>
      <template v-if="detailed">
        <p v-if="transfer.booking_ref || transfer.url" class="flex flex-wrap items-center gap-x-3 text-sm">
          <span v-if="transfer.booking_ref" class="text-base-content/60">
            {{ t('transfer.bookingRef') }}: {{ transfer.booking_ref }}
          </span>
          <a
            v-if="transfer.url"
            :href="transfer.url"
            target="_blank"
            rel="noopener noreferrer nofollow"
            class="link link-primary truncate"
          >{{ host }}</a>
        </p>
        <MarkdownText v-if="transfer.notes_md" :source="transfer.notes_md" />
      </template>
    </div>
  </div>
</template>
