<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlanDay } from '@/api/types'
import { formatDayDate } from '@/utils/format'
import { dayColor, isVisit } from '@/utils/plan'
import { revealElement } from '@/utils/reveal'

// The contents of a report: its days, each in the colour the map draws it in,
// the way the plan lists them. A report is read as one long page, so a day here
// scrolls to its section rather than opening it, and the day being read is the
// one marked. A column on wide screens, a row that scrolls sideways on phones.
const props = defineProps<{
  days: PlanDay[]
}>()

const { t, locale } = useI18n()

// How far below the top of the window a section's heading may sit and still be
// the one being read: below the sticky header, with some room to spare.
const READING_LINE = 120

const current = ref(0)
let frame = 0

// follow marks the last day whose section has reached the reading line. At the
// bottom of the page the last day is the one being read, even when it is too
// short to ever reach that line.
function follow(): void {
  frame = 0
  const bottom = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2
  const last = props.days[props.days.length - 1]
  if (bottom && last && window.scrollY > 0) {
    current.value = last.position
    return
  }
  let reading = 0
  for (const day of props.days) {
    const section = document.getElementById(sectionId(day))
    if (section && section.getBoundingClientRect().top <= READING_LINE) {
      reading = day.position
    }
  }
  current.value = reading
}

// onScroll follows the page at most once a frame.
function onScroll(): void {
  if (!frame) {
    frame = window.requestAnimationFrame(follow)
  }
}

// sectionId is the id of a day's section in the report.
function sectionId(day: PlanDay): string {
  return `day-${day.position + 1}`
}

// placeCount counts a day's places and activities, leaving out its stay marks.
function placeCount(day: PlanDay): number {
  return day.items.filter(isVisit).length
}

onMounted(() => {
  window.addEventListener('scroll', onScroll, { passive: true })
  follow()
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
  if (frame) {
    window.cancelAnimationFrame(frame)
  }
})
watch(() => props.days, onScroll)
</script>

<template>
  <nav :aria-label="t('report.contents')">
    <ul class="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 lg:mx-0 lg:flex-col lg:overflow-visible lg:px-0 lg:pb-0">
      <li v-for="day in days" :key="day.id" class="shrink-0">
        <button
          type="button"
          class="btn btn-sm h-auto min-h-8 w-full flex-nowrap justify-start gap-2 px-2 py-1 font-normal"
          :class="current === day.position ? 'btn-primary' : 'btn-ghost'"
          :aria-current="current === day.position ? 'location' : undefined"
          @click="revealElement(sectionId(day), false)"
        >
          <span
            class="size-2.5 shrink-0 rounded-full ring-1 ring-base-100"
            :style="{ backgroundColor: dayColor(day.position) }"
            aria-hidden="true"
          ></span>
          <span class="font-semibold tabular-nums lg:w-5 lg:text-right">{{ day.position + 1 }}</span>
          <span class="max-w-40 min-w-0 flex-1 truncate text-left lg:max-w-none">
            {{ day.title || formatDayDate(day.date, locale) || t('plan.dayNumber', { n: day.position + 1 }) }}
          </span>
          <span v-if="placeCount(day) > 0" class="badge badge-xs">{{ placeCount(day) }}</span>
        </button>
      </li>
    </ul>
  </nav>
</template>
