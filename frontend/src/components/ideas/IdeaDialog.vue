<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { addIdeaPhoto, createIdea, deleteIdeaPhoto, updateIdea } from '@/api/ideas'
import {
  IDEA_COSTS, VISA_REQUIREMENTS, type Idea, type IdeaCost, type IdeaFields, type TravelMode, type VisaRequirement,
} from '@/api/types'
import AmountInput from '@/components/AmountInput.vue'
import AppIcon from '@/components/AppIcon.vue'
import CurrencySelect from '@/components/CurrencySelect.vue'
import { TRAVEL_MODE_ICONS } from '@/components/icons'
import CountrySelect from '@/components/ideas/CountrySelect.vue'
import DaysRangeSlider from '@/components/ideas/DaysRangeSlider.vue'
import IdeaPicture from '@/components/ideas/IdeaPicture.vue'
import MonthPicker from '@/components/ideas/MonthPicker.vue'
import LabeledSelect, { type LabeledOption } from '@/components/LabeledSelect.vue'
import LocationField, { type LocationModel } from '@/components/LocationField.vue'
import { useSessionStore } from '@/stores/session'
import { errorMessage } from '@/utils/errors'
import { normalizeAmount } from '@/utils/format'
import { IDEA_TRAVEL_MODES, MAX_IDEA_DAYS, MAX_IDEA_PHOTOS } from '@/utils/ideas'
import { shrinkPicture } from '@/utils/picture'

// The whole of an idea in one form: where - the countries and any number of
// places, one open at a time - when and for how long, whether a visa is
// needed, the ways of getting there, each an alternative with its own cost and
// time, roughly what the rest costs, and up to ten photos. The form saves
// every field at once, then takes away the photos removed and uploads the new
// ones, each shrunk first as a trip's are; the tags are put on from the idea's
// page.

const emit = defineEmits<{ saved: [idea: Idea] }>()

const { t, te } = useI18n()
const session = useSessionStore()

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const editing = ref<Idea | null>(null)
const busy = ref(false)
const error = ref('')

/** TransportForm is one way of getting there as the form edits it. */
interface TransportForm {
  /** A way mixing several ways of travelling offers them all to pick from. */
  mixed: boolean
  modes: TravelMode[]
  cost: string
  hours: string | number
  minutes: string | number
}

/** IdeaForm is an idea as the form edits it: amounts and times as typed. */
interface IdeaForm {
  title: string
  countries: string[]
  places: LocationModel[]
  months: number[]
  daysMin: number
  daysMax: number
  daysIdeal: string | number
  description: string
  currency: string
  costs: Record<IdeaCost, string>
  visa: VisaRequirement
  transports: TransportForm[]
}

// A new idea starts as a week away, with no visa to get.
const DEFAULT_DAYS = 7

const form = ref<IdeaForm>(emptyForm())
// openPlace is the one place shown whole; the others read as their address.
const openPlace = ref(0)

/** PendingPhoto is a picture chosen for the idea, uploaded when it is saved. */
interface PendingPhoto {
  file: File
  url: string
}
// kept are the idea's photos still on it, pending the ones to upload.
const kept = ref<Idea['photos']>([])
const pending = ref<PendingPhoto[]>([])
const photoField = useTemplateRef<HTMLInputElement>('photoField')
const photoCount = computed(() => kept.value.length + pending.value.length)

// forgetPending lets go of the previews of pictures not uploaded.
function forgetPending(): void {
  pending.value.forEach((photo) => URL.revokeObjectURL(photo.url))
  pending.value = []
}
onBeforeUnmount(forgetPending)

// pickPhotos adds the chosen pictures, as many as still fit.
function pickPhotos(event: Event): void {
  const input = event.target as HTMLInputElement
  const room = MAX_IDEA_PHOTOS - photoCount.value
  for (const file of Array.from(input.files ?? []).slice(0, Math.max(room, 0))) {
    pending.value.push({ file, url: URL.createObjectURL(file) })
  }
  input.value = ''
}

// removePending takes a chosen picture back before it was uploaded.
function removePending(index: number): void {
  const [photo] = pending.value.splice(index, 1)
  if (photo) {
    URL.revokeObjectURL(photo.url)
  }
}

// placeLabel reads a folded place: its address, or its point.
function placeLabel(place: LocationModel, index: number): string {
  if (place.address.trim() !== '') {
    return place.address
  }
  return place.lat ? `${place.lat}, ${place.lng}` : t('ideas.placeNumber', { n: index + 1 })
}

// addPlace adds a place and opens it, folding the one open before.
function addPlace(): void {
  form.value.places.push({ address: '', lat: '', lng: '' })
  openPlace.value = form.value.places.length - 1
}

// removePlace takes a place away, or empties the last one.
function removePlace(index: number): void {
  if (form.value.places.length > 1) {
    form.value.places.splice(index, 1)
    openPlace.value = Math.min(openPlace.value, form.value.places.length - 1)
  } else {
    form.value.places[0] = { address: '', lat: '', lng: '' }
  }
}

// emptyForm starts an idea in the reader's currency, a week long, with nothing else said.
function emptyForm(): IdeaForm {
  return {
    title: '', countries: [], places: [{ address: '', lat: '', lng: '' }], months: [],
    daysMin: DEFAULT_DAYS, daysMax: DEFAULT_DAYS, daysIdeal: DEFAULT_DAYS, description: '',
    currency: session.user?.default_currency ?? 'EUR', costs: { stay: '', food: '', other: '' },
    visa: 'not_needed', transports: [],
  }
}

/** open shows the form for a new idea, or filled with one to change. */
function open(idea: Idea | null = null): void {
  editing.value = idea
  error.value = ''
  form.value = idea
    ? {
        title: idea.title, countries: [...idea.countries],
        places: idea.places.length > 0
          ? idea.places.map((place) => ({ address: place.name, lat: place.lat?.toString() ?? '', lng: place.lng?.toString() ?? '' }))
          : [{ address: '', lat: '', lng: '' }],
        months: [...idea.months],
        daysMin: idea.days_min ?? idea.days_ideal ?? DEFAULT_DAYS,
        daysMax: idea.days_max ?? idea.days_ideal ?? DEFAULT_DAYS,
        daysIdeal: idea.days_ideal ?? '', description: idea.description_md, currency: idea.currency,
        costs: { stay: idea.costs.stay ?? '', food: idea.costs.food ?? '', other: idea.costs.other ?? '' },
        visa: idea.visa,
        transports: idea.transports.map((transport) => ({
          mixed: transport.modes.length > 1, modes: [...transport.modes], cost: transport.cost ?? '',
          hours: transport.minutes === null ? '' : Math.floor(transport.minutes / 60),
          minutes: transport.minutes === null ? '' : transport.minutes % 60,
        })),
      }
    : emptyForm()
  // A new idea opens its first place; a saved one opens folded, its places read at a glance.
  openPlace.value = idea && idea.places.length > 0 ? -1 : 0
  kept.value = idea ? [...idea.photos] : []
  forgetPending()
  dialog.value?.showModal()
}

const visaOptions = computed<LabeledOption<VisaRequirement>[]>(() =>
  VISA_REQUIREMENTS.map((visa) => ({ value: visa, label: t(`ideas.visas.${visa}`) })))

/** TransportChoice is what the menu that adds a way of getting there offers. */
type TransportChoice = TravelMode | 'mixed'
const transportOptions = computed<LabeledOption<TransportChoice>[]>(() => [
  ...IDEA_TRAVEL_MODES.map((mode) => ({ value: mode as TransportChoice, label: t(`modes.${mode}`), icon: TRAVEL_MODE_ICONS[mode] })),
  { value: 'mixed', label: t('ideas.mixed'), icon: 'transport' },
])

// addTransport adds a way of getting there: one way of travelling, or a mixed
// one whose ways are picked on its row.
function addTransport(choice: TransportChoice): void {
  form.value.transports.push({
    mixed: choice === 'mixed', modes: choice === 'mixed' ? [] : [choice], cost: '', hours: '', minutes: '',
  })
}

// toggleMode switches a way of travelling of a mixed way on or off.
function toggleMode(transport: TransportForm, mode: TravelMode): void {
  transport.modes = transport.modes.includes(mode)
    ? transport.modes.filter((item) => item !== mode)
    : [...transport.modes, mode]
}

// number reads a number typed, or null when the field is empty. A number field
// hands its value over as a number once something is typed.
function number(value: string | number): number | null {
  return String(value).trim() === '' ? null : Number(value)
}

// coordinate reads a coordinate a location field holds, or null.
function coordinate(value: string): number | null {
  return value.trim() === '' ? null : Number(value.replace(',', '.'))
}

// fields gathers the form into what the server stores.
function fields(): IdeaFields {
  const value = form.value
  return {
    title: value.title, countries: value.countries,
    places: value.places.map((place) => ({ name: place.address, lat: coordinate(place.lat), lng: coordinate(place.lng) })),
    months: value.months, days_min: value.daysMin, days_max: value.daysMax, days_ideal: number(value.daysIdeal),
    description_md: value.description, currency: value.currency.toUpperCase(),
    costs: {
      stay: normalizeAmount(value.costs.stay), food: normalizeAmount(value.costs.food),
      other: normalizeAmount(value.costs.other),
    },
    visa: value.visa,
    transports: value.transports.map((transport) => {
      const hours = number(transport.hours)
      const minutes = number(transport.minutes)
      return {
        modes: transport.modes, cost: normalizeAmount(transport.cost),
        minutes: hours === null && minutes === null ? null : (hours ?? 0) * 60 + (minutes ?? 0),
      }
    }),
  }
}

// submit saves the idea, then its photos: the ones taken off go, the new ones
// are shrunk and uploaded one by one; the idea the last answer names goes to
// the page.
async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    let saved = editing.value ? await updateIdea(editing.value.id, fields()) : await createIdea(fields())
    editing.value = saved
    const keptIds = new Set(kept.value.map((photo) => photo.id))
    for (const photo of saved.photos.filter((item) => !keptIds.has(item.id))) {
      saved = await deleteIdeaPhoto(saved.id, photo.id)
    }
    while (pending.value.length > 0) {
      const next = pending.value[0]!
      saved = await addIdeaPhoto(saved.id, await shrinkPicture(next.file))
      URL.revokeObjectURL(next.url)
      pending.value.shift()
    }
    kept.value = [...saved.photos]
    dialog.value?.close()
    emit('saved', saved)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="modal modal-top sm:modal-middle">
    <form class="modal-box flex max-w-2xl flex-col gap-4" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('ideas.editTitle') : t('ideas.newTitle') }}</h2>

      <label class="floating-label">
        <span>{{ t('ideas.title') }}</span>
        <input v-model="form.title" type="text" required maxlength="200" class="input w-full" :placeholder="t('ideas.title')" />
      </label>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-1">{{ t('ideas.where') }}</legend>
        <CountrySelect v-model="form.countries" :label="t('ideas.countries')" />
        <div v-for="(place, index) in form.places" :key="index" class="flex items-start gap-1">
          <div v-if="openPlace === index" class="min-w-0 flex-1">
            <LocationField v-model="form.places[index]!" :focus="null" :legend="t('ideas.placeNumber', { n: index + 1 })" />
          </div>
          <button
            v-else
            type="button"
            class="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-field border border-base-300 px-3 py-2 text-start hover:bg-base-200"
            :aria-label="t('ideas.editPlace', { name: placeLabel(place, index) })"
            @click="openPlace = index"
          >
            <AppIcon name="map" class="size-4! shrink-0 opacity-60" />
            <span class="truncate">{{ placeLabel(place, index) }}</span>
            <AppIcon name="chevronDown" class="ms-auto size-3! shrink-0 opacity-60" />
          </button>
          <button
            v-if="form.places.length > 1 || place.address || place.lat"
            type="button"
            class="btn btn-ghost btn-sm btn-square mt-1 text-error"
            :aria-label="t('ideas.removePlace', { n: index + 1 })"
            :title="t('ideas.removePlace', { n: index + 1 })"
            @click="removePlace(index)"
          >
            <AppIcon name="trash" />
          </button>
        </div>
        <button
          v-if="form.places.length < 20"
          type="button"
          class="btn btn-ghost btn-sm self-start"
          @click="addPlace"
        >
          <AppIcon name="plus" />
          {{ t('ideas.addPlace') }}
        </button>
      </fieldset>

      <fieldset class="flex flex-col gap-3">
        <legend class="label mb-1">{{ t('ideas.when') }}</legend>
        <MonthPicker v-model="form.months" :label="t('ideas.months')" />
        <div class="grid grid-cols-[minmax(0,1fr)_7rem] items-center gap-4">
          <DaysRangeSlider v-model:from="form.daysMin" v-model:to="form.daysMax" :label="t('ideas.duration')" />
          <label class="floating-label">
            <span>{{ t('ideas.daysIdeal') }}</span>
            <input
              v-model="form.daysIdeal"
              type="number"
              :min="form.daysMin"
              :max="Math.min(form.daysMax, MAX_IDEA_DAYS)"
              class="input w-full"
              :placeholder="t('ideas.daysIdeal')"
            />
          </label>
        </div>
      </fieldset>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-1">{{ t('ideas.gettingThere') }}</legend>
        <p class="text-xs text-base-content/60">{{ t('ideas.transportHint') }}</p>
        <div
          v-for="(transport, index) in form.transports"
          :key="index"
          class="flex flex-col gap-2 rounded-box border border-base-300 p-2"
        >
          <div class="flex items-center gap-2">
            <span v-if="!transport.mixed" class="flex flex-1 items-center gap-2 font-medium">
              <AppIcon :name="TRAVEL_MODE_ICONS[transport.modes[0]!]" class="size-4!" />
              {{ t(`modes.${transport.modes[0]}`) }}
            </span>
            <div v-else class="flex flex-1 flex-wrap gap-1">
              <span class="me-1 self-center text-sm font-medium">{{ t('ideas.mixed') }}:</span>
              <button
                v-for="mode in IDEA_TRAVEL_MODES"
                :key="mode"
                type="button"
                class="btn btn-xs"
                :class="transport.modes.includes(mode) ? 'btn-primary' : 'btn-ghost border-base-300'"
                :aria-pressed="transport.modes.includes(mode)"
                @click="toggleMode(transport, mode)"
              >
                <AppIcon :name="TRAVEL_MODE_ICONS[mode]" class="size-3.5!" />
                {{ t(`modes.${mode}`) }}
              </button>
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-square text-error"
              :aria-label="t('ideas.removeTransport')"
              :title="t('ideas.removeTransport')"
              @click="form.transports.splice(index, 1)"
            >
              <AppIcon name="trash" />
            </button>
          </div>
          <div class="grid grid-cols-[minmax(0,1fr)_5rem_5rem] gap-2">
            <label class="floating-label">
              <span>{{ t('ideas.transportCost') }}</span>
              <AmountInput v-model="transport.cost" class="input input-sm w-full" :placeholder="t('ideas.transportCost')" />
            </label>
            <label class="floating-label">
              <span>{{ t('ideas.hours') }}</span>
              <input v-model="transport.hours" type="number" min="0" max="168" class="input input-sm w-full" :placeholder="t('ideas.hours')" />
            </label>
            <label class="floating-label">
              <span>{{ t('ideas.minutes') }}</span>
              <input v-model="transport.minutes" type="number" min="0" max="59" class="input input-sm w-full" :placeholder="t('ideas.minutes')" />
            </label>
          </div>
        </div>
        <LabeledSelect
          v-if="form.transports.length < 10"
          :model-value="null"
          :label="t('ideas.addTransport')"
          :options="transportOptions"
          :placeholder="t('ideas.chooseTransport')"
          @choose="addTransport"
        />
        <LabeledSelect v-model="form.visa" :label="t('ideas.visa')" :options="visaOptions" />
      </fieldset>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-1">{{ t('ideas.cost') }}</legend>
        <p class="text-xs text-base-content/60">{{ t('ideas.costHint') }}</p>
        <CurrencySelect v-model="form.currency" />
        <div class="grid grid-cols-3 gap-2">
          <label v-for="part in IDEA_COSTS" :key="part" class="floating-label">
            <span>{{ t(`ideas.costs.${part}`) }}</span>
            <AmountInput v-model="form.costs[part]" class="input w-full" :placeholder="t(`ideas.costs.${part}`)" />
          </label>
        </div>
      </fieldset>

      <fieldset class="flex flex-col gap-2">
        <legend class="label mb-1">{{ t('ideas.photos') }} ({{ photoCount }}/{{ MAX_IDEA_PHOTOS }})</legend>
        <div class="flex flex-wrap gap-2">
          <div v-for="(photo, index) in kept" :key="photo.id" class="relative size-20">
            <IdeaPicture v-if="editing" :idea-id="editing.id" :photo-id="photo.id" class="size-20 rounded-box" />
            <button
              type="button"
              class="btn btn-circle btn-xs absolute -end-1 -top-1"
              :aria-label="t('ideas.removePhoto', { n: index + 1 })"
              :title="t('ideas.removePhoto', { n: index + 1 })"
              @click="kept.splice(index, 1)"
            >
              <AppIcon name="close" class="size-3!" />
            </button>
          </div>
          <div v-for="(photo, index) in pending" :key="photo.url" class="relative size-20">
            <img :src="photo.url" alt="" class="size-20 rounded-box object-cover opacity-80" />
            <button
              type="button"
              class="btn btn-circle btn-xs absolute -end-1 -top-1"
              :aria-label="t('ideas.removePhoto', { n: kept.length + index + 1 })"
              :title="t('ideas.removePhoto', { n: kept.length + index + 1 })"
              @click="removePending(index)"
            >
              <AppIcon name="close" class="size-3!" />
            </button>
          </div>
          <button
            v-if="photoCount < MAX_IDEA_PHOTOS"
            type="button"
            class="flex size-20 cursor-pointer flex-col items-center justify-center gap-1 rounded-box border border-dashed border-base-300 text-xs text-base-content/60 hover:bg-base-200"
            @click="photoField?.click()"
          >
            <AppIcon name="plus" />
            {{ t('ideas.addPhotos') }}
          </button>
        </div>
        <input ref="photoField" type="file" accept="image/*" multiple class="hidden" @change="pickPhotos" />
      </fieldset>

      <label class="flex flex-col gap-1">
        <span class="label">{{ t('ideas.description') }}</span>
        <textarea
          v-model="form.description"
          rows="4"
          maxlength="20000"
          class="textarea w-full"
          :placeholder="t('ideas.descriptionHint')"
        ></textarea>
      </label>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="dialog?.close()">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="busy">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('common.save') }}
        </button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
