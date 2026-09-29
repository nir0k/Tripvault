<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlaceFields } from '@/api/documents'
import type { CostCategory, CostSplit, PlanItem, TripMember } from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import IconSelect from '@/components/IconSelect.vue'
import { amountCents, centsToAmount, equalShares } from '@/utils/amount'
import { formatMoney, normalizeAmount } from '@/utils/format'
import { costCategoryOptions } from '@/utils/plan'

// The cost of a place or an activity: how much, of what kind and for what, who
// of the trip's members pays it, and how it is shared among them - not at all,
// equally among the members ticked, or in amounts of each one's own that make
// up the whole.
//
// A per-person cost is shared for the whole group, the amount multiplied by the
// travellers, since that is what the payer lays out.
const props = defineProps<{
  currency: string
  travelers: number
  /** The members of the trip, who may pay and share. */
  members: TripMember[]
}>()

const emit = defineEmits<{
  save: [fields: PlaceFields]
}>()

const { t, locale } = useI18n()

const categories = computed(() => costCategoryOptions(t))

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const item = ref<PlanItem | null>(null)
const error = ref('')
const form = reactive({
  amount: '', perPerson: false, category: 'other' as CostCategory, note: '',
  paidBy: '', split: 'none' as CostSplit,
})
// shares holds, by member, whether they share the cost and their own amount.
const shares = reactive<Record<string, { on: boolean; amount: string }>>({})

// people are everybody the form can name: the members, and anybody the place
// already named who has since left the trip.
const people = computed(() => {
  const list = props.members.map((member) => ({ id: member.user.id, name: member.user.display_name }))
  const known = new Set(list.map((person) => person.id))
  const named = [item.value?.paid_by, ...(item.value?.cost_shares ?? []).map((share) => share.user_id)]
  for (const id of named) {
    if (id && !known.has(id)) {
      known.add(id)
      list.push({ id, name: t('place.costForm.formerMember') })
    }
  }
  return list
})

// total is what the payer lays out for the whole group, in hundredths.
const total = computed(() => {
  const cents = amountCents(form.amount) ?? 0
  return form.perPerson ? cents * Math.max(props.travelers, 1) : cents
})
const sharing = computed(() => people.value.filter((person) => shares[person.id]?.on))
// equal gives each member ticked their part of an equal split.
const equal = computed(() => {
  const parts = equalShares(total.value, sharing.value.length)
  return Object.fromEntries(sharing.value.map((person, index) => [person.id, parts[index] ?? 0]))
})
// left is what the individual amounts still leave to assign, negative when they
// come to more than the cost.
const left = computed(() => total.value - sharing.value
  .reduce((sum, person) => sum + (amountCents(shares[person.id]?.amount ?? '') ?? 0), 0))

/** money shows hundredths in the trip's currency. */
function money(cents: number): string {
  return formatMoney(centsToAmount(cents), props.currency, locale.value)
}

/** open shows the form filled with the place's cost. */
function open(target: PlanItem): void {
  item.value = target
  error.value = ''
  Object.assign(form, {
    amount: target.planned_cost_amount ?? '',
    perPerson: target.cost_per_person,
    category: target.cost_category,
    note: target.cost_note,
    paidBy: target.paid_by ?? '',
    split: target.cost_split,
  })
  for (const key of Object.keys(shares)) {
    delete shares[key]
  }
  const stored = new Map(target.cost_shares.map((share) => [share.user_id, share]))
  // A cost not split yet offers everybody, which is what "everyone" means.
  for (const person of people.value) {
    const share = stored.get(person.id)
    shares[person.id] = {
      on: target.cost_split === 'none' ? true : !!share,
      amount: share?.amount ?? '',
    }
  }
  dialog.value?.showModal()
}

/** close hides the form. */
function close(): void {
  dialog.value?.close()
}

/** fail shows why saving did not work, keeping the form open. */
function fail(message: string): void {
  error.value = message
}

// submit checks the split and hands the fields to the page.
function submit(): void {
  error.value = ''
  const split = form.split
  if (split !== 'none') {
    if (!form.paidBy) {
      error.value = t('place.costForm.payerRequired')
      return
    }
    if (sharing.value.length === 0) {
      error.value = t('place.costForm.sharesRequired')
      return
    }
    if (split === 'individuals' && left.value !== 0) {
      error.value = t('place.costForm.sharesMismatch')
      return
    }
  }
  emit('save', {
    planned_cost_amount: normalizeAmount(form.amount),
    cost_per_person: form.perPerson,
    cost_category: form.category,
    cost_note: form.note,
    paid_by: form.paidBy || null,
    cost_split: split,
    cost_shares: split === 'none' ? [] : sharing.value.map((person) => ({
      user_id: person.id,
      amount: split === 'individuals' ? centsToAmount(amountCents(shares[person.id]?.amount ?? '') ?? 0) : null,
    })),
  })
}

// clear takes the cost away, and with it who pays and who shares it.
function clear(): void {
  emit('save', {
    planned_cost_amount: null, cost_per_person: false, cost_note: '', paid_by: null, cost_split: 'none', cost_shares: [],
  })
}

defineExpose({ open, close, fail })
</script>

<template>
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90dvh] flex-col gap-3 overflow-y-auto" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ t('place.costForm.title') }}</h2>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="floating-label">
          <span>{{ t('place.costForm.amount') }}</span>
          <AmountInput v-model="form.amount" class="input w-full" :placeholder="t('place.costForm.amount')" />
        </label>
        <label class="flex flex-col gap-1">
          <span class="sr-only">{{ t('place.costForm.category') }}</span>
          <IconSelect v-model="form.category" :options="categories" :label="t('place.costForm.category')" block />
        </label>
        <label class="label cursor-pointer justify-start gap-2 sm:col-span-2">
          <input v-model="form.perPerson" type="checkbox" class="checkbox" />
          <span>{{ t('place.perPerson') }}</span>
        </label>
      </div>

      <label class="floating-label">
        <span>{{ t('place.costForm.note') }}</span>
        <input v-model="form.note" type="text" maxlength="500" class="input w-full" :placeholder="t('place.costForm.note')" />
      </label>

      <label class="flex flex-col gap-1">
        <span class="label">{{ t('place.costForm.paidBy') }}</span>
        <select v-model="form.paidBy" class="select w-full">
          <option value="">{{ t('place.costForm.nobody') }}</option>
          <option v-for="person in people" :key="person.id" :value="person.id">{{ person.name }}</option>
        </select>
      </label>

      <fieldset class="fieldset gap-2">
        <legend class="fieldset-legend">{{ t('place.costForm.split') }}</legend>
        <div class="join" role="radiogroup" :aria-label="t('place.costForm.split')">
          <input v-model="form.split" type="radio" value="none" class="btn btn-sm join-item" :aria-label="t('place.costForm.splits.none')" />
          <input v-model="form.split" type="radio" value="everyone" class="btn btn-sm join-item" :aria-label="t('place.costForm.splits.everyone')" />
          <input v-model="form.split" type="radio" value="individuals" class="btn btn-sm join-item" :aria-label="t('place.costForm.splits.individuals')" />
        </div>

        <ul v-if="form.split !== 'none'" class="flex flex-col gap-1">
          <li v-for="person in people" :key="person.id" class="flex items-center gap-3">
            <label class="label min-w-0 flex-1 cursor-pointer justify-start gap-2">
              <input v-model="shares[person.id]!.on" type="checkbox" class="checkbox checkbox-sm" />
              <span class="truncate text-base-content">{{ person.name }}</span>
            </label>
            <span v-if="form.split === 'everyone' && shares[person.id]?.on" class="text-sm tabular-nums text-base-content/70">
              {{ money(equal[person.id] ?? 0) }}
            </span>
            <AmountInput
              v-else-if="form.split === 'individuals' && shares[person.id]?.on"
              v-model="shares[person.id]!.amount"
              class="input input-sm w-32"
              :placeholder="t('place.costForm.share')"
              :aria-label="t('place.costForm.shareOf', { name: person.name })"
            />
          </li>
        </ul>
        <p v-if="form.split === 'individuals'" class="text-sm" :class="left === 0 ? 'text-success' : 'text-warning'">
          {{ left >= 0 ? t('place.costForm.left', { amount: money(left) }) : t('place.costForm.over', { amount: money(-left) }) }}
        </p>
        <p v-if="form.split !== 'none' && form.perPerson" class="text-xs text-base-content/60">
          {{ t('place.costForm.perPersonHint', { amount: money(total) }) }}
        </p>
      </fieldset>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button
          v-if="item && item.planned_cost_amount !== null"
          type="button"
          class="btn btn-ghost text-error me-auto"
          @click="clear"
        >
          {{ t('place.costForm.clear') }}
        </button>
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary">{{ t('common.save') }}</button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
