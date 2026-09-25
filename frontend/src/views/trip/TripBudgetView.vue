<script setup lang="ts">
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as documentsApi from '@/api/documents'
import { getBudget, updateTrip } from '@/api/trips'
import {
  COST_CATEGORIES, type Budget, type BudgetEntry, type BudgetEntryKind, type Expense, type Leg, type PlanItem,
  type Stay, type Transfer, type TripDocument,
} from '@/api/types'
import AppIcon, { type IconName } from '@/components/AppIcon.vue'
import BudgetAmountDialog from '@/components/budget/BudgetAmountDialog.vue'
import BudgetExpenseDialog from '@/components/budget/BudgetExpenseDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PlanLegDialog from '@/components/plan/PlanLegDialog.vue'
import PlanPlaceDialog from '@/components/plan/PlanPlaceDialog.vue'
import PlanStayDialog from '@/components/plan/PlanStayDialog.vue'
import PlanTransferDialog from '@/components/plan/PlanTransferDialog.vue'
import { useTripStore } from '@/stores/trip'
import { errorMessage } from '@/utils/errors'
import { formatDayDate, formatMoney } from '@/utils/format'

// What the plan commits to, against what the trip is willing to spend: the
// totals, the breakdown by category and by day, and every individual cost with
// filters. The figures come from the server, which assembles them from the
// trip's document.
//
// A report's budget adds what was really spent beside every planned amount, and
// measures the overall budget against that. It is read from the report alone,
// so the plan it was copied from may be gone. Its costs are listed but not
// edited here: what was spent is entered in the report, where the story is.
//
// The trip's overall amount is set here, because this is the screen where the
// question "does it fit?" comes up. Every cost is editable here as well, with the
// same forms the plan uses: somebody going through the money should not have to
// go looking for the place that carries it.

const { t, te, locale } = useI18n()
const store = useTripStore()

const budget = ref<Budget | null>(null)
// The plan behind the figures, read so a cost can be edited where it is listed.
const plan = ref<TripDocument | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const filterDay = ref('')
const filterCategory = ref('')
const savedAmount = ref(false)

const amountDialog = useTemplateRef<InstanceType<typeof BudgetAmountDialog>>('amountDialog')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const expenseDialog = useTemplateRef<InstanceType<typeof BudgetExpenseDialog>>('expenseDialog')
const placeDialog = useTemplateRef<InstanceType<typeof PlanPlaceDialog>>('placeDialog')
const stayDialog = useTemplateRef<InstanceType<typeof PlanStayDialog>>('stayDialog')
const transferDialog = useTemplateRef<InstanceType<typeof PlanTransferDialog>>('transferDialog')
const legDialog = useTemplateRef<InstanceType<typeof PlanLegDialog>>('legDialog')

const trip = computed(() => store.trip)
const canEdit = computed(() => trip.value?.role === 'owner' || trip.value?.role === 'editor')
const isReport = computed(() => budget.value?.kind === 'report')
// The cost list is edited only in a plan; a report's costs are entered in the report.
const editCosts = computed(() => canEdit.value && !isReport.value)
// spent is what the overall budget is measured against: the plan's commitments,
// or a report's real spending.
const spent = computed(() => (isReport.value ? budget.value?.actual : budget.value?.planned) ?? null)
const currency = computed(() => budget.value?.currency ?? trip.value?.currency ?? 'EUR')

/** amount shows a decimal string from the API in the trip's currency. */
function amount(value: string | null): string {
  return formatMoney(value, currency.value, locale.value)
}

// overspent is true when the plan already costs more than the trip set aside.
const overspent = computed(() => Number(budget.value?.remaining ?? 0) < 0)
// The bar stops at full width; the line under it says by how much the plan
// overshoots.
const usedWidth = computed(() => Math.min(budget.value?.used_percent ?? 0, 100))

// The rows of the day table, followed by what belongs to no single day.
const dayRows = computed(() => budget.value?.days ?? [])
const untiedRows = computed(() => {
  const current = budget.value
  if (!current) {
    return []
  }
  return [
    { label: t('stay.title'), amount: current.stays },
    { label: t('transfer.title'), amount: current.transfers },
    { label: t('budget.wholeTrip'), amount: current.untied },
  ].filter((row) => Number(row.amount) !== 0)
})

// Only the categories that carry something; the rest would be rows of zeroes.
const categoryRows = computed(() => (budget.value?.categories ?? [])
  .filter((row) => Number(row.planned) !== 0 || Number(row.actual) !== 0))

// entries are the costs left by the day and category filters. Without a day
// chosen the whole trip is shown: the costs of its days and those of no day,
// such as the stays and the places still waiting for one.
const entries = computed(() => (budget.value?.entries ?? []).filter((entry) =>
  (filterDay.value === '' || entry.day_id === filterDay.value)
  && (filterCategory.value === '' || entry.category === filterCategory.value)))

const filtered = computed(() => filterDay.value !== '' || filterCategory.value !== '')
const filteredTotal = computed(() =>
  entries.value.filter((entry) => !entry.unassigned).reduce((sum, entry) => sum + Number(entry.amount), 0))
const filteredActual = computed(() =>
  entries.value.reduce((sum, entry) => sum + Number(entry.actual_amount ?? 0), 0))

// ENTRY_ICONS shows at a glance what carries each cost.
const ENTRY_ICONS: Record<BudgetEntryKind, IconName> = {
  place: 'map', stay: 'stay', transfer: 'modeFlight', leg: 'transport', expense: 'wallet',
}

/** dayNumber labels the day a cost belongs to, or the trip as a whole. */
function dayNumber(dayId: string | null): string {
  const day = budget.value?.days.find((row) => row.day_id === dayId)
  if (!day) {
    return ''
  }
  const number = t('plan.dayNumber', { n: day.position + 1 })
  const date = formatDayDate(day.date, locale.value)
  return date ? `${number} · ${date}` : number
}

// load reads the budget of the open trip, and the plan the figures come from.
async function load(): Promise<void> {
  const tripId = trip.value?.id
  if (!tripId) {
    return
  }
  loading.value = true
  error.value = ''
  try {
    const next = await getBudget(tripId)
    apply(next)
    plan.value = next.document_id ? await documentsApi.getDocument(next.document_id) : null
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    loading.value = false
  }
}

// apply shows a budget the server returned.
function apply(next: Budget): void {
  budget.value = next
}

// The budget is read again for another trip.
watch(() => trip.value?.id, () => void load(), { immediate: true })

/**
 * saveAmount stores the trip's overall budget from the amount form and reads
 * the figures again, so the remainder and the bar follow at once. An unchanged
 * amount only closes the form.
 *
 * Arguments:
 *   - next: the amount to set, or null to leave the trip without a budget.
 */
async function saveAmount(next: string | null): Promise<void> {
  const current = trip.value
  if (!current) {
    return
  }
  if (next === (budget.value?.budget_amount ?? null)) {
    amountDialog.value?.close()
    return
  }
  busy.value = true
  error.value = ''
  savedAmount.value = false
  try {
    store.set(await updateTrip(current.id, { budget_amount: next }))
    apply(await getBudget(current.id))
    savedAmount.value = true
    amountDialog.value?.close()
  } catch (err) {
    amountDialog.value?.fail(errorMessage(err, t, te))
  } finally {
    busy.value = false
  }
}

/**
 * change applies one edit of the plan. The call answers with the whole document,
 * which is kept, and the figures are read again on top of it.
 *
 * Arguments:
 *   - call: the change to perform.
 *
 * Returns:
 *   - true when it went through; the message is in error otherwise.
 */
async function change(call: () => Promise<TripDocument>): Promise<boolean> {
  const tripId = trip.value?.id
  if (!tripId) {
    return false
  }
  busy.value = true
  error.value = ''
  try {
    plan.value = await call()
    apply(await getBudget(tripId))
    return true
  } catch (err) {
    error.value = errorMessage(err, t, te)
    return false
  } finally {
    busy.value = false
  }
}

/** findPlace looks up a place of the plan, on a day or among the ideas. */
function findPlace(id: string): PlanItem | null {
  for (const day of plan.value?.days ?? []) {
    const found = day.items.find((item) => item.id === id)
    if (found) {
      return found
    }
  }
  return plan.value?.unassigned.find((item) => item.id === id) ?? null
}

/** findLeg looks up a leg of the plan by its identifier. */
function findLeg(id: string): Leg | null {
  for (const day of plan.value?.days ?? []) {
    const found = day.legs.find((leg) => leg.id === id)
    if (found) {
      return found
    }
  }
  return null
}

// edit opens the form of whatever carries the cost: the plan's own place, stay
// and leg forms, and the expense form for a cost that stands on its own.
function edit(entry: BudgetEntry): void {
  switch (entry.kind) {
    case 'place': {
      const item = findPlace(entry.id)
      if (item) {
        placeDialog.value?.open(item)
      }
      return
    }
    case 'stay': {
      const stay = plan.value?.stays.find((row) => row.id === entry.id)
      if (stay) {
        stayDialog.value?.open(stay)
      }
      return
    }
    case 'transfer': {
      const transfer = plan.value?.transfers.find((row) => row.id === entry.id)
      if (transfer) {
        transferDialog.value?.open(transfer)
      }
      return
    }
    case 'leg': {
      const leg = findLeg(entry.id)
      if (leg) {
        legDialog.value?.open(leg)
      }
      return
    }
    default: {
      const expense = plan.value?.expenses.find((row) => row.id === entry.id)
      if (expense) {
        expenseDialog.value?.open(expense)
      }
    }
  }
}

// remove takes a cost out of the plan, after confirmation. A place, a stay, a
// transfer and an expense are deleted; a leg only loses its amount, because
// legs are not records of their own - the server keeps one between every pair
// of neighbouring elements of a day and would put a deleted one straight back.
async function remove(entry: BudgetEntry): Promise<void> {
  const name = entry.label || amount(entry.amount)
  const questions: Record<BudgetEntryKind, string> = {
    place: 'plan.confirmDeletePlace',
    stay: 'stay.confirmDelete',
    transfer: 'transfer.confirmDelete',
    leg: 'budget.confirmClearLegCost',
    expense: 'budget.confirmDeleteExpense',
  }
  const question = t(questions[entry.kind], { name })
  if (!(await confirmDialog.value?.ask(question, { danger: true }))) {
    return
  }
  switch (entry.kind) {
    case 'place':
      await change(() => documentsApi.deletePlace(entry.id))
      return
    case 'stay':
      await change(() => documentsApi.deleteStay(entry.id))
      return
    case 'transfer':
      await change(() => documentsApi.deleteTransfer(entry.id))
      return
    case 'leg':
      await change(() => documentsApi.updateLeg(entry.id, { planned_cost_amount: null }))
      return
    default:
      await change(() => documentsApi.deleteExpense(entry.id))
  }
}

// saveExpense creates or changes an expense from the expense form.
async function saveExpense(fields: documentsApi.ExpenseFields, expense: Expense | null): Promise<void> {
  const documentId = budget.value?.document_id
  if (!documentId) {
    return
  }
  const ok = await change(() =>
    expense ? documentsApi.updateExpense(expense.id, fields) : documentsApi.createExpense(documentId, fields))
  if (ok) {
    expenseDialog.value?.close()
  } else {
    expenseDialog.value?.fail(error.value)
    error.value = ''
  }
}

// savePlace stores a place edited from the cost list.
async function savePlace(fields: documentsApi.PlaceFields, item: PlanItem | null): Promise<void> {
  if (!item) {
    return
  }
  if (await change(() => documentsApi.updatePlace(item.id, fields))) {
    placeDialog.value?.close()
  } else {
    placeDialog.value?.fail(error.value)
    error.value = ''
  }
}

// saveStay stores a stay edited from the cost list.
async function saveStay(fields: documentsApi.StayFields, stay: Stay | null): Promise<void> {
  if (!stay) {
    return
  }
  if (await change(() => documentsApi.updateStay(stay.id, fields))) {
    stayDialog.value?.close()
  } else {
    stayDialog.value?.fail(error.value)
    error.value = ''
  }
}

// saveTransfer stores a transfer edited from the cost list.
async function saveTransfer(fields: documentsApi.TransferFields, transfer: Transfer | null): Promise<void> {
  if (!transfer) {
    return
  }
  if (await change(() => documentsApi.updateTransfer(transfer.id, fields))) {
    transferDialog.value?.close()
  } else {
    transferDialog.value?.fail(error.value)
    error.value = ''
  }
}

// saveLeg stores the typed values of a leg edited from the cost list.
async function saveLeg(leg: Leg, changes: documentsApi.LegChanges): Promise<void> {
  if (await change(() => documentsApi.updateLeg(leg.id, changes))) {
    legDialog.value?.close()
  } else {
    legDialog.value?.fail(error.value)
    error.value = ''
  }
}
</script>

<template>
  <div class="space-y-8">
    <p v-if="error" role="alert" class="text-error">{{ error }}</p>
    <div v-if="loading" class="flex justify-center"><span class="loading loading-spinner"></span></div>

    <template v-else-if="budget">
      <section class="space-y-4" :aria-label="t('budget.totals')">
        <div class="stats stats-vertical w-full border border-base-300 sm:stats-horizontal">
          <div class="stat">
            <div class="stat-title">{{ t('budget.planned') }}</div>
            <div class="stat-value text-2xl">{{ amount(budget.planned) }}</div>
            <div v-if="!isReport" class="stat-desc">{{ t('budget.perPerson', { amount: amount(budget.per_person) }) }}</div>
          </div>
          <div v-if="isReport" class="stat">
            <div class="stat-title">{{ t('budget.actual') }}</div>
            <div class="stat-value text-2xl">{{ amount(budget.actual) }}</div>
            <div class="stat-desc">{{ t('budget.perPerson', { amount: amount(budget.per_person) }) }}</div>
          </div>
          <div class="stat">
            <div class="stat-title">{{ t('trip.budget') }}</div>
            <div class="stat-value text-2xl">{{ amount(budget.budget_amount) || t('trip.noBudget') }}</div>
            <div class="stat-desc">{{ t('trip.travelers', budget.travelers) }}</div>
          </div>
          <div v-if="budget.remaining !== null" class="stat">
            <div class="stat-title">{{ overspent ? t('budget.over') : t('budget.remaining') }}</div>
            <div class="stat-value text-2xl" :class="overspent ? 'text-error' : 'text-success'">
              {{ amount(overspent ? String(-Number(budget.remaining)) : budget.remaining) }}
            </div>
          </div>
        </div>

        <div v-if="budget.used_percent !== null" class="space-y-1">
          <progress
            class="progress w-full"
            :class="overspent ? 'progress-error' : 'progress-primary'"
            :value="usedWidth"
            max="100"
            :aria-label="t('budget.usedLabel')"
          ></progress>
          <p class="text-sm" :class="overspent ? 'text-error' : 'text-base-content/70'">
            {{ t(isReport ? 'budget.spentOfBudget' : 'budget.usedOfBudget', {
              percent: budget.used_percent,
              planned: amount(spent),
              budget: amount(budget.budget_amount),
            }) }}
          </p>
        </div>

        <div v-if="canEdit" class="flex flex-wrap items-center gap-2">
          <button
            type="button"
            class="btn btn-hover-outline"
            :disabled="busy"
            @click="amountDialog?.open(budget.budget_amount)"
          >
            <AppIcon name="wallet" />
            {{ t('budget.setBudget') }}
          </button>
          <span v-if="savedAmount" class="text-sm text-success">{{ t('settings.saved') }}</span>
        </div>

        <p v-if="Number(budget.unassigned) !== 0" class="text-sm">
          <span class="badge h-auto border-none bg-base-200 py-0.5">
            {{ t('budget.unassignedTotal', { amount: amount(budget.unassigned) }) }}
          </span>
        </p>
        <p v-if="!budget.document_id" class="text-sm text-base-content/70">{{ t('budget.noPlan') }}</p>
        <p v-if="isReport && canEdit" class="text-sm text-base-content/70">{{ t('budget.reportHint') }}</p>
      </section>

      <div class="grid gap-6 lg:grid-cols-2">
        <section v-if="categoryRows.length > 0" class="space-y-3">
          <h2 class="text-xl font-bold">{{ t('budget.byCategory') }}</h2>
          <div class="overflow-x-auto">
            <table class="table table-zebra table-sm">
              <thead>
                <tr>
                  <th>{{ t('budget.category') }}</th>
                  <th class="text-end">{{ t('budget.planned') }}</th>
                  <th v-if="isReport" class="text-end">{{ t('budget.actual') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in categoryRows" :key="row.category">
                  <td>{{ t(`costCategories.${row.category}`) }}</td>
                  <td class="text-end whitespace-nowrap">{{ amount(row.planned) }}</td>
                  <td v-if="isReport" class="text-end whitespace-nowrap">{{ amount(row.actual) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section v-if="dayRows.length > 0 || untiedRows.length > 0" class="space-y-3">
          <h2 class="text-xl font-bold">{{ t('budget.byDay') }}</h2>
          <div class="overflow-x-auto">
            <table class="table table-zebra table-sm">
              <thead>
                <tr>
                  <th>{{ t('plan.days') }}</th>
                  <th class="text-end">{{ t('budget.planned') }}</th>
                  <th v-if="isReport" class="text-end">{{ t('budget.actual') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in dayRows" :key="row.day_id">
                  <td class="whitespace-nowrap">{{ dayNumber(row.day_id) }}</td>
                  <td class="text-end">{{ amount(row.planned) }}</td>
                  <td v-if="isReport" class="text-end">{{ amount(row.actual) }}</td>
                </tr>
                <tr v-for="row in untiedRows" :key="row.label" class="text-base-content/80">
                  <td class="whitespace-nowrap">{{ row.label }}</td>
                  <td class="text-end">{{ amount(row.amount) }}</td>
                  <td v-if="isReport"></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <section class="space-y-3">
        <header class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="text-xl font-bold">{{ t('budget.costs') }}</h2>
          <button
            v-if="editCosts"
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="!budget.document_id || busy"
            @click="expenseDialog?.open(null)"
          >
            <AppIcon name="plus" />
            {{ t('budget.addExpense') }}
          </button>
        </header>

        <div class="flex flex-wrap gap-3">
          <label class="select w-full sm:w-64">
            <span class="label">{{ t('budget.filterDay') }}</span>
            <select v-model="filterDay">
              <option value="">{{ t('budget.wholeTrip') }}</option>
              <option v-for="row in budget.days" :key="row.day_id" :value="row.day_id">{{ dayNumber(row.day_id) }}</option>
            </select>
          </label>
          <label class="select w-full sm:w-56">
            <span class="label">{{ t('budget.filterCategory') }}</span>
            <select v-model="filterCategory">
              <option value="">{{ t('budget.allCategories') }}</option>
              <option v-for="category in COST_CATEGORIES" :key="category" :value="category">
                {{ t(`costCategories.${category}`) }}
              </option>
            </select>
          </label>
        </div>

        <div v-if="entries.length > 0" class="overflow-x-auto">
          <table class="table table-zebra table-sm">
            <thead>
              <tr>
                <th>{{ t('budget.cost') }}</th>
                <th>{{ t('budget.category') }}</th>
                <th>{{ t('plan.days') }}</th>
                <th class="text-end">{{ isReport ? t('budget.planned') : t('budget.amount') }}</th>
                <th v-if="isReport" class="text-end">{{ t('budget.actual') }}</th>
                <th v-if="editCosts"><span class="sr-only">{{ t('budget.expenseActions') }}</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="entry in entries" :key="entry.kind + entry.id">
                <td>
                  <span class="flex items-center gap-2">
                    <span class="text-base-content/60" :title="t(`budget.kinds.${entry.kind}`)">
                      <AppIcon :name="ENTRY_ICONS[entry.kind]" />
                    </span>
                    <span class="break-words">{{ entry.label || t(`budget.kinds.${entry.kind}`) }}</span>
                  </span>
                  <span class="flex flex-wrap gap-1 ps-7">
                    <span v-if="entry.is_optional" class="badge badge-ghost badge-sm">{{ t('place.optional') }}</span>
                    <span v-if="entry.unassigned" class="badge badge-ghost badge-sm">{{ t('plan.unassigned') }}</span>
                  </span>
                </td>
                <td class="whitespace-nowrap">{{ t(`costCategories.${entry.category}`) }}</td>
                <td class="whitespace-nowrap">{{ entry.day_id ? dayNumber(entry.day_id) : t('budget.wholeTrip') }}</td>
                <td class="text-end whitespace-nowrap">
                  {{ amount(entry.amount) }}
                  <span v-if="entry.per_person" class="block text-xs text-base-content/60">
                    {{ amount(entry.unit_amount) }} {{ t('place.perPersonShort') }}
                  </span>
                </td>
                <td v-if="isReport" class="text-end whitespace-nowrap">
                  {{ entry.actual_amount === null ? '—' : amount(entry.actual_amount) }}
                </td>
                <td v-if="editCosts" class="text-end">
                  <span class="join">
                    <button
                      type="button"
                      class="btn btn-ghost btn-xs join-item"
                      :disabled="busy"
                      @click="edit(entry)"
                    >
                      {{ t('plan.edit') }}
                    </button>
                    <button
                      type="button"
                      class="btn btn-ghost btn-xs join-item text-error"
                      :disabled="busy"
                      :aria-label="t('plan.delete')"
                      @click="remove(entry)"
                    >
                      <AppIcon name="trash" />
                    </button>
                  </span>
                </td>
              </tr>
            </tbody>
            <tfoot v-if="filtered">
              <tr>
                <th colspan="3">{{ t('budget.filteredTotal') }}</th>
                <th class="text-end">{{ amount(filteredTotal.toFixed(2)) }}</th>
                <th v-if="isReport" class="text-end">{{ amount(filteredActual.toFixed(2)) }}</th>
                <th v-if="editCosts"></th>
              </tr>
            </tfoot>
          </table>
        </div>
        <p v-else class="text-sm text-base-content/70">
          {{ filtered ? t('budget.noneMatch') : t('budget.noCosts') }}
        </p>
      </section>

      <BudgetAmountDialog ref="amountDialog" @save="saveAmount" />
      <BudgetExpenseDialog ref="expenseDialog" :days="budget.days" @save="saveExpense" />
      <PlanPlaceDialog ref="placeDialog" :focus="null" @save="savePlace" />
      <PlanStayDialog ref="stayDialog" :focus="null" @save="saveStay" />
      <PlanTransferDialog ref="transferDialog" :focus="null" @save="saveTransfer" />
      <PlanLegDialog ref="legDialog" @save="saveLeg" />
      <ConfirmDialog ref="confirmDialog" />
    </template>
  </div>
</template>
