<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/AppIcon.vue'
import BackToTop from '@/components/BackToTop.vue'

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
</script>

<template>
  <div class="fixed end-4 bottom-20 z-20 flex flex-col items-center gap-2 lg:end-6 lg:bottom-6">
    <BackToTop inline />

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
