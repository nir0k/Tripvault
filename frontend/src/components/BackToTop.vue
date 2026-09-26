<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'

// A round button back to the start of a long page. It appears once the reader
// has scrolled further than the first screen. Standing alone it takes the
// lower right corner itself, above the bottom navigation on a phone; inside a
// column of other floating buttons (inline) it leaves the placing to them.
const props = withDefaults(defineProps<{
  /** Sit in a column the parent places, instead of in the corner. */
  inline?: boolean
  /**
   * Pixels at the bottom of the window to keep clear of, where a bar such as a
   * selection covers the page; the button stays in its corner when that is higher.
   */
  clearance?: number
}>(), { inline: false, clearance: 0 })

const { t } = useI18n()

// scrolled says the reader is further down than the first screen, which is when
// a way back to the top earns its place.
const scrolled = ref(false)

// onScroll follows how far the page is scrolled.
function onScroll(): void {
  scrolled.value = window.scrollY > window.innerHeight * 0.6
}

// toTop returns to the start of the page, smoothly unless the reader asked
// their system for less motion.
function toTop(): void {
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  window.scrollTo({ top: 0, behavior: reduced ? 'auto' : 'smooth' })
}

onMounted(() => {
  onScroll()
  window.addEventListener('scroll', onScroll, { passive: true })
})
onBeforeUnmount(() => window.removeEventListener('scroll', onScroll))
</script>

<template>
  <Transition
    enter-from-class="opacity-0 translate-y-2"
    leave-to-class="opacity-0 translate-y-2"
    enter-active-class="transition duration-200"
    leave-active-class="transition duration-200"
  >
    <button
      v-if="scrolled"
      type="button"
      class="btn btn-circle border-2 border-primary text-primary shadow-lg"
      :class="props.inline ? '' : 'fixed end-4 z-20 transition-[bottom] [--corner:5rem] lg:end-6 lg:[--corner:1.5rem]'"
      :style="props.inline ? undefined : { bottom: `max(var(--corner), ${props.clearance}px)` }"
      :aria-label="t('report.toTop')"
      :title="t('report.toTop')"
      @click="toTop"
    >
      <AppIcon name="chevronUp" />
    </button>
  </Transition>
</template>
