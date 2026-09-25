<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Transfer, TripDocument } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import TransferRow from '@/components/plan/TransferRow.vue'
import { formatMoney } from '@/utils/format'

// The transfers of the document - flights, trains, ferries, shuttles - in the
// order they depart, with what they cost together.
const props = defineProps<{
  document: TripDocument
  currency: string
  travelers: number
  canEdit: boolean
  /** Whether the trip has dates; without them the days cannot show transfers. */
  dated: boolean
}>()

const emit = defineEmits<{
  add: []
  edit: [transfer: Transfer]
  remove: [transfer: Transfer]
}>()

const { t, locale } = useI18n()

// total is what the transfers cost the group, a per-person price counted for
// every traveller as the budget counts it.
const total = computed(() => props.document.transfers.reduce((sum, transfer) => {
  const amount = Number(transfer.planned_cost_amount ?? 0)
  return sum + (transfer.cost_per_person ? amount * Math.max(props.travelers, 1) : amount)
}, 0))
</script>

<template>
  <section class="space-y-4" :aria-label="t('transfer.title')">
    <header class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-xl font-bold">{{ t('transfer.title') }}</h2>
      <button v-if="canEdit" type="button" class="btn btn-primary btn-sm" @click="emit('add')">
        <AppIcon name="plus" />
        {{ t('transfer.add') }}
      </button>
    </header>

    <p v-if="!dated && document.transfers.length > 0" class="text-sm text-base-content/70">{{ t('transfer.noDate') }}</p>

    <ul class="space-y-2">
      <li v-for="transfer in document.transfers" :key="transfer.id" class="flex items-start gap-3 rounded-box border border-base-300 p-3">
        <TransferRow :transfer="transfer" :currency="currency" detailed class="flex-1" />
        <div v-if="canEdit" class="flex gap-1">
          <button type="button" class="btn btn-ghost btn-sm" @click="emit('edit', transfer)">{{ t('plan.edit') }}</button>
          <button type="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('plan.delete')" @click="emit('remove', transfer)">
            <AppIcon name="trash" />
          </button>
        </div>
      </li>
    </ul>
    <p v-if="document.transfers.length === 0" class="rounded-box border border-dashed border-base-300 p-4 text-center text-sm text-base-content/60">
      {{ t('transfer.empty') }}
    </p>

    <footer v-if="total > 0" class="rounded-box bg-base-200 px-4 py-3 text-sm">
      {{ t('transfer.total', { amount: formatMoney(total.toFixed(2), currency, locale) }) }}
    </footer>
  </section>
</template>
