<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import { formatDateRange } from '@/utils/format'

// The calendar the dates of a trip are picked on, opened from the fields of the
// start or of the end. Opened for the start, a click sets the start and the
// calendar goes on to the end; opened for the end, it asks for the end at once.
// While the end is chosen the period follows the pointer, and a click before
// the start moves the start there instead, so the end can never come before
// it; days more than a year after the start cannot be picked, as the service
// allows no longer trip. Setting the end is the last step, and says so. Dates
// are "YYYY-MM-DD" strings, as the API carries them, and are worked out in UTC
// so no time zone moves a day.
const start = defineModel<string>('start', { required: true })
const end = defineModel<string>('end', { required: true })

const props = defineProps<{
  /** Which date the calendar was opened to pick. */
  phase: 'start' | 'end'
}>()

const emit = defineEmits<{
  /** The end is picked: the period is whole. */
  done: []
}>()

const { t, locale } = useI18n()

/** MAX_DAYS is the longest period a trip may have, counting both ends. */
const MAX_DAYS = 366
const DAY_MS = 24 * 3600 * 1000

// toTime and toDate turn a date string into a UTC midnight and back.
function toTime(value: string): number {
  const [year, month, day] = value.split('-').map(Number)
  return Date.UTC(year ?? 1970, (month ?? 1) - 1, day ?? 1)
}
function toDate(time: number): string {
  return new Date(time).toISOString().slice(0, 10)
}

// hover is the day under the pointer while the end is being chosen.
const hover = ref('')
// month is the first day of the first month shown.
const month = ref(firstOfMonth(start.value || toDate(Date.now())))
// wide shows two months side by side where there is room.
const wide = ref(false)
let query: MediaQueryList | null = null
function onWidth(): void {
  wide.value = query?.matches ?? false
}
onMounted(() => {
  query = window.matchMedia('(min-width: 640px)')
  onWidth()
  query.addEventListener('change', onWidth)
})
onBeforeUnmount(() => query?.removeEventListener('change', onWidth))

// picked is true while a change of the dates comes from a click here, which
// leaves the months where they are; a period set from outside - the form
// filled for a trip - turns the calendar to its start.
let picked = false
watch(start, (value) => {
  if (picked) {
    picked = false
    return
  }
  if (value) {
    month.value = firstOfMonth(value)
  }
})

// choosing is the date a click sets now: the one the calendar was opened for,
// then the end. Without a start there is no end to choose yet.
const choosing = ref<'start' | 'end'>(props.phase === 'end' && start.value ? 'end' : 'start')
watch(() => props.phase, (phase) => {
  choosing.value = phase === 'end' && start.value ? 'end' : 'start'
})

// firstOfMonth names the first day of a date's month.
function firstOfMonth(value: string): string {
  return value.slice(0, 8) + '01'
}

// shiftMonth moves a first-of-month by whole months.
function shiftMonth(value: string, by: number): string {
  const date = new Date(toTime(value))
  date.setUTCMonth(date.getUTCMonth() + by)
  return toDate(date.getTime())
}

// A week starts on Sunday in English and on Monday elsewhere.
const weekStart = computed(() => (locale.value.startsWith('en') ? 0 : 1))

// weekdays are the short names of the days, in the week's order.
const weekdays = computed(() => {
  const format = new Intl.DateTimeFormat(locale.value, { weekday: 'short', timeZone: 'UTC' })
  // 4 January 1970 was a Sunday.
  return Array.from({ length: 7 }, (_, index) => format.format(new Date(((index + weekStart.value) % 7 + 3) * DAY_MS)))
})

interface Month {
  key: string
  title: string
  /** The days in weeks; null where a week holds a day of another month. */
  cells: (string | null)[]
}

// months are the months shown, each laid out in weeks.
const months = computed<Month[]>(() => {
  const title = new Intl.DateTimeFormat(locale.value, { month: 'long', year: 'numeric', timeZone: 'UTC' })
  return Array.from({ length: wide.value ? 2 : 1 }, (_, offset) => {
    const first = shiftMonth(month.value, offset)
    const firstTime = toTime(first)
    const next = toTime(shiftMonth(first, 1))
    const lead = (new Date(firstTime).getUTCDay() - weekStart.value + 7) % 7
    const cells: (string | null)[] = Array.from({ length: lead }, () => null)
    for (let time = firstTime; time < next; time += DAY_MS) {
      cells.push(toDate(time))
    }
    // A month's name is lower case in Russian; only its first letter is raised,
    // so "г." after the year stays as it is.
    const name = title.format(new Date(firstTime))
    return { key: first, title: name.charAt(0).toLocaleUpperCase(locale.value) + name.slice(1), cells }
  })
})

// choosingEnd is true while a click sets the end.
const choosingEnd = computed(() => choosing.value === 'end' && start.value !== '')

// last is the far end of the period shown: while the end is chosen, the day
// under the pointer when it could end the trip; otherwise the end.
const last = computed(() => {
  if (choosingEnd.value && hover.value >= start.value && !tooFar(hover.value)) {
    return hover.value
  }
  return end.value
})

// tooFar marks days too long after the start to end a trip.
function tooFar(day: string): boolean {
  return choosingEnd.value && (toTime(day) - toTime(start.value)) / DAY_MS + 1 > MAX_DAYS
}

// inPeriod marks the days between the start and the far end, both included.
function inPeriod(day: string): boolean {
  return start.value !== '' && last.value !== '' && day >= start.value && day <= last.value
}

// pick sets the start, or the end once the start is set; a day before the
// start becomes the new start, and a new click after a whole period starts over.
function pick(day: string): void {
  if (!choosingEnd.value || day < start.value) {
    picked = true
    start.value = day
    // An end that no longer fits the new start goes; one that does is kept.
    if (end.value && (end.value < day || (toTime(end.value) - toTime(day)) / DAY_MS + 1 > MAX_DAYS)) {
      end.value = ''
    }
    choosing.value = 'end'
    return
  }
  if (!tooFar(day)) {
    end.value = day
    hover.value = ''
    emit('done')
  }
}

// period says what a click sets now, and the period while there is one.
const period = computed(() => {
  const hint = choosingEnd.value ? t('dateRange.pickEnd') : t('dateRange.pickStart')
  if (!start.value || !last.value) {
    return hint
  }
  const days = (toTime(last.value) - toTime(start.value)) / DAY_MS + 1
  return `${hint}: ${formatDateRange(start.value, last.value, locale.value)} · ${t('trips.days', days)}`
})

// dayLabel names a day in full for a screen reader.
function dayLabel(day: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'full', timeZone: 'UTC' }).format(new Date(toTime(day)))
}
</script>

<template>
  <div class="flex flex-col gap-2 rounded-box border border-base-300 p-3">
    <div class="flex items-center justify-between gap-2">
      <button
        type="button"
        class="btn btn-ghost btn-sm btn-square"
        :aria-label="t('dateRange.previous')"
        @click="month = shiftMonth(month, -1)"
      >
        <AppIcon name="chevronLeft" />
      </button>
      <p class="text-sm font-medium" aria-live="polite">{{ period }}</p>
      <button
        type="button"
        class="btn btn-ghost btn-sm btn-square"
        :aria-label="t('dateRange.next')"
        @click="month = shiftMonth(month, 1)"
      >
        <AppIcon name="chevronRight" />
      </button>
    </div>

    <div class="grid gap-4" :class="wide ? 'grid-cols-2' : ''" @mouseleave="hover = ''">
      <div v-for="shown in months" :key="shown.key" class="flex flex-col gap-1">
        <p class="text-center text-sm font-semibold">{{ shown.title }}</p>
        <div class="grid grid-cols-7 text-center text-xs text-base-content/60">
          <span v-for="name in weekdays" :key="name" class="py-1">{{ name }}</span>
        </div>
        <div class="grid grid-cols-7 gap-y-0.5">
          <template v-for="(day, index) in shown.cells" :key="day ?? `lead-${index}`">
            <span v-if="day === null"></span>
            <button
              v-else
              type="button"
              class="h-9 text-sm tabular-nums transition-colors disabled:cursor-not-allowed disabled:opacity-30"
              :class="[
                day === start || day === last
                  ? 'rounded-field bg-primary font-semibold text-primary-content'
                  : inPeriod(day) ? 'bg-primary/15' : 'rounded-field hover:bg-base-200',
              ]"
              :disabled="tooFar(day)"
              :aria-label="dayLabel(day)"
              :aria-pressed="day === start || day === end || inPeriod(day)"
              @click="pick(day)"
              @mouseenter="hover = day"
              @focus="hover = day"
            >
              {{ Number(day.slice(8)) }}
            </button>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
