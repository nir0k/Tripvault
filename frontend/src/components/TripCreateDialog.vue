<script setup lang="ts">
import { computed, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { createTrip } from '@/api/trips'
import type { DocumentKind } from '@/api/types'
import TripFields, { type TripFormModel } from '@/components/TripFields.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'
import { browserTimeZone, normalizeAmount } from '@/utils/format'
import { tripRoute } from '@/utils/tripRoutes'

const { t, te } = useI18n()
const router = useRouter()
const session = useSessionStore()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const kind = ref<DocumentKind>('plan')
const form = ref<TripFormModel>(emptyForm())
const error = ref('')
const busy = ref(false)

const title = computed(() => (kind.value === 'plan' ? t('tripForm.createPlanTitle') : t('tripForm.createReportTitle')))

// emptyForm starts a trip with the reader's default currency and time zone.
function emptyForm(): TripFormModel {
  return {
    title: '',
    summary: '',
    startDate: '',
    endDate: '',
    timezone: browserTimeZone(),
    currency: session.user?.default_currency ?? 'EUR',
    travelers: 1,
    budget: '',
  }
}

/** open shows the wizard for a new plan, or for a report written from scratch. */
function open(next: DocumentKind): void {
  kind.value = next
  form.value = emptyForm()
  error.value = ''
  dialog.value?.showModal()
}

// submit creates the plan or the report and opens it.
async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    const trip = await createTrip({
      title: form.value.title,
      start_date: form.value.startDate,
      end_date: form.value.endDate,
      timezone: form.value.timezone,
      currency: form.value.currency.toUpperCase(),
      travelers: form.value.travelers,
      budget_amount: normalizeAmount(form.value.budget),
      kind: kind.value,
    })
    dialog.value?.close()
    await router.push(tripRoute(trip))
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex flex-col gap-3" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ title }}</h2>
      <TripFields v-model="form" />
      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="busy">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('tripForm.create') }}
        </button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
