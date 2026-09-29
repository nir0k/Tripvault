<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { updateTrip } from '@/api/trips'
import type { PlanState, Trip, TripStatus } from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import { useDropdown } from '@/composables/useDropdown'
import { errorMessage } from '@/utils/errors'

// The status of a plan, as a badge. A plan runs from upcoming through ongoing
// to overdue by its dates alone; only a person closes it, as completed or
// cancelled, and so for an editor the badge opens the menu that does. A report
// has no status and shows nothing.
const props = withDefaults(defineProps<{
  trip: Trip
  /** Let the reader close or reopen the plan from the badge. */
  editable?: boolean
}>(), { editable: false })

const emit = defineEmits<{ updated: [trip: Trip] }>()

const { t, te } = useI18n()
const { close } = useDropdown('menu')

const STATUS_CLASSES: Record<TripStatus, string> = {
  ongoing: 'badge-success',
  upcoming: 'badge-info',
  overdue: 'badge-warning',
  completed: 'badge-neutral',
  cancelled: 'badge-error',
}

interface Choice {
  state: PlanState | null
  label: string
  icon: IconName
}

const saving = ref(false)
const error = ref('')

const status = computed(() => props.trip.status)
const closed = computed(() => status.value === 'completed' || status.value === 'cancelled')
const canChange = computed(() => props.editable && props.trip.kind === 'plan')

// choices offers what the plan can become: an open plan is completed or
// cancelled, a closed one goes over to the other state or opens again.
const choices = computed<Choice[]>(() => {
  const all: Choice[] = [
    { state: 'completed', label: t('trips.state.complete'), icon: 'check' },
    { state: 'cancelled', label: t('trips.state.cancel'), icon: 'cross' },
  ]
  const offered = all.filter((choice) => choice.state !== status.value)
  if (closed.value) {
    offered.push({ state: null, label: t('trips.state.reopen'), icon: 'arrowLeft' })
  }
  return offered
})

// choose saves the new state and hands the trip the server confirmed on; the
// menu stays open with the reason when the change is refused.
async function choose(state: PlanState | null): Promise<void> {
  if (saving.value) {
    return
  }
  saving.value = true
  error.value = ''
  try {
    emit('updated', await updateTrip(props.trip.id, { state }))
    close()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <details v-if="status && canChange" ref="menu" class="dropdown">
    <summary
      class="badge badge-sm cursor-pointer gap-1"
      :class="STATUS_CLASSES[status]"
      :aria-label="`${t('trips.state.label')}: ${t(`trips.status.${status}`)}`"
      :title="t('trips.state.label')"
    >
      {{ t(`trips.status.${status}`) }}
      <AppIcon name="chevronDown" class="size-3!" />
    </summary>
    <ul class="menu dropdown-content z-20 mt-1 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
      <li v-for="choice in choices" :key="choice.state ?? 'open'">
        <button type="button" :disabled="saving" @click="choose(choice.state)">
          <AppIcon :name="choice.icon" />
          {{ choice.label }}
        </button>
      </li>
      <li v-if="error" role="alert" class="px-3 py-1 text-sm text-error">{{ error }}</li>
    </ul>
  </details>
  <span v-else-if="status" class="badge badge-sm" :class="STATUS_CLASSES[status]">{{ t(`trips.status.${status}`) }}</span>
</template>
