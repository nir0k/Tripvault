<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'

// The report's own controls, kept within reach while it is read: a column of
// round buttons in the lower right corner. From the top down - back to the
// start, which appears once the reader has scrolled away from it; hiding what
// was skipped; and the pencil that switches editing on and off, for those who
// may edit. On a phone the column sits above the bottom navigation bar.
defineProps<{
  editing: boolean
  canEdit: boolean
  hideSkipped: boolean
}>()

const emit = defineEmits<{
  toggleEdit: []
  toggleSkipped: []
}>()

const { t } = useI18n()

// A button that is off keeps the usual face with an orange edge and glyph, so it
// stands out from the page and the photographs scrolling under it; one that is
// on is orange all over.
const IDLE = 'border-primary text-primary'
const ACTIVE = 'btn-primary border-primary'

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
  <div class="fixed end-4 bottom-20 z-20 flex flex-col items-center gap-2 lg:end-6 lg:bottom-6">
    <Transition
      enter-from-class="opacity-0 translate-y-2"
      leave-to-class="opacity-0 translate-y-2"
      enter-active-class="transition duration-200"
      leave-active-class="transition duration-200"
    >
      <button
        v-if="scrolled"
        type="button"
        class="btn btn-circle border-2 shadow-lg"
        :class="IDLE"
        :aria-label="t('report.toTop')"
        :title="t('report.toTop')"
        @click="toTop"
      >
        <AppIcon name="chevronUp" />
      </button>
    </Transition>

    <button
      type="button"
      class="btn btn-circle border-2 shadow-lg"
      :class="hideSkipped ? ACTIVE : IDLE"
      :aria-label="hideSkipped ? t('report.showSkipped') : t('report.hideSkipped')"
      :title="hideSkipped ? t('report.showSkipped') : t('report.hideSkipped')"
      :aria-pressed="hideSkipped"
      @click="emit('toggleSkipped')"
    >
      <AppIcon :name="hideSkipped ? 'eyeSlash' : 'eye'" />
    </button>

    <button
      v-if="canEdit"
      type="button"
      class="btn btn-circle border-2 shadow-lg"
      :class="editing ? ACTIVE : IDLE"
      :aria-label="editing ? t('report.stopEditing') : t('report.edit')"
      :title="editing ? t('report.stopEditing') : t('report.edit')"
      :aria-pressed="editing"
      @click="emit('toggleEdit')"
    >
      <AppIcon name="pencil" />
    </button>
  </div>
</template>
