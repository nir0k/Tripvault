<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveTranslations } from '@/api/documents'
import { getTrip, updateTrip } from '@/api/trips'
import type { TranslationEntry, Trip } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { LANGUAGE_NAMES, SUPPORTED_LOCALES } from '@/i18n'
import { errorMessage } from '@/utils/errors'

// The languages of a report: the one it is written in, the ones it is
// translated into, and the report's title and summary in each of those. The
// rest of the report is translated on the report itself, in the language chosen
// above it. Taking a language away takes its translations with it, so that is
// asked first.
const props = defineProps<{
  trip: Trip
  canEdit: boolean
}>()

const emit = defineEmits<{
  changed: [trip: Trip]
}>()

const { t, te } = useI18n()

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')
const original = ref('')
const extra = ref<string[]>([])
// titles and summaries hold the trip's own words in each further language.
const titles = reactive<Record<string, string>>({})
const summaries = reactive<Record<string, string>>({})
const saving = ref(false)
const message = ref('')
const error = ref('')

const available = computed(() =>
  SUPPORTED_LOCALES.filter((code) => code !== original.value && !extra.value.includes(code)))

// fill copies the stored languages and translations into the form.
function fill(): void {
  original.value = props.trip.languages[0] ?? ''
  extra.value = props.trip.languages.slice(1)
  for (const code of SUPPORTED_LOCALES) {
    titles[code] = props.trip.translations[code]?.title ?? ''
    summaries[code] = props.trip.translations[code]?.summary ?? ''
  }
}

watch(() => props.trip, fill, { immediate: true })

// setOriginal names the language the report is written in; it cannot be one of
// its translations as well.
function setOriginal(code: string): void {
  original.value = code
  extra.value = extra.value.filter((each) => each !== code)
}

/** languageName names a language in its own words. */
function languageName(code: string): string {
  return LANGUAGE_NAMES[code] ?? code
}

// save stores the languages first, so a language just added can take its
// translations, and then the title and summary of each.
async function save(): Promise<void> {
  const languages = [original.value, ...extra.value]
  const dropped = props.trip.languages.slice(1).filter((code) => !extra.value.includes(code))
  if (dropped.length > 0) {
    const names = dropped.map(languageName).join(', ')
    if (!(await confirmDialog.value?.ask(t('languages.confirmRemove', { languages: names }), { danger: true }))) {
      return
    }
  }

  saving.value = true
  message.value = ''
  error.value = ''
  try {
    if (languages.join(',') !== props.trip.languages.join(',')) {
      await updateTrip(props.trip.id, { languages })
    }
    const documentId = props.trip.report_id
    for (const code of extra.value) {
      const entries: TranslationEntry[] = []
      const stored = props.trip.translations[code] ?? {}
      if ((titles[code] ?? '').trim() !== (stored.title ?? '')) {
        entries.push({ target_type: 'trip', target_id: props.trip.id, field: 'title', value: titles[code] ?? '' })
      }
      if ((summaries[code] ?? '').trim() !== (stored.summary ?? '')) {
        entries.push({ target_type: 'trip', target_id: props.trip.id, field: 'summary', value: summaries[code] ?? '' })
      }
      if (entries.length > 0 && documentId) {
        await saveTranslations(documentId, code, entries)
      }
    }
    emit('changed', await getTrip(props.trip.id))
    message.value = t('settings.saved')
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="card border border-base-300 bg-base-100">
    <form class="card-body gap-4" @submit.prevent="save">
      <h2 class="card-title">{{ t('languages.title') }}</h2>
      <p class="text-sm text-base-content/70">{{ t('languages.hint') }}</p>

      <fieldset :disabled="!canEdit" class="contents">
        <label class="floating-label">
          <span>{{ t('languages.original') }}</span>
          <select
            class="select w-full"
            :value="original"
            :aria-label="t('languages.original')"
            @change="setOriginal(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="code in SUPPORTED_LOCALES" :key="code" :value="code">{{ languageName(code) }}</option>
          </select>
        </label>
        <p class="text-xs text-base-content/60">{{ t('languages.originalHint') }}</p>

        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-base-content/70">{{ t('languages.translations') }}</span>
          <span v-for="code in extra" :key="code" class="badge badge-lg gap-1">
            {{ languageName(code) }}
            <button
              v-if="canEdit"
              type="button"
              class="btn btn-ghost btn-xs btn-circle"
              :aria-label="t('languages.remove', { language: languageName(code) })"
              @click="extra = extra.filter((each) => each !== code)"
            >
              <AppIcon name="close" class="size-3!" />
            </button>
          </span>
          <span v-if="extra.length === 0" class="text-sm text-base-content/50">{{ t('languages.none') }}</span>
          <select
            v-if="canEdit && available.length > 0"
            class="select select-sm w-48"
            :aria-label="t('languages.add')"
            value=""
            @change="extra = [...extra, ($event.target as HTMLSelectElement).value]; ($event.target as HTMLSelectElement).value = ''"
          >
            <option value="" disabled>{{ t('languages.add') }}</option>
            <option v-for="code in available" :key="code" :value="code">{{ languageName(code) }}</option>
          </select>
        </div>

        <fieldset
          v-for="code in extra"
          :key="code"
          class="fieldset gap-3 rounded-box border border-base-300 p-3"
        >
          <legend class="fieldset-legend">{{ t('languages.tripIn', { language: languageName(code) }) }}</legend>
          <label class="floating-label">
            <span>{{ t('tripForm.title') }}</span>
            <input v-model="titles[code]" type="text" maxlength="200" class="input w-full" :placeholder="trip.title" />
          </label>
          <label class="floating-label">
            <span>{{ t('tripForm.summary') }}</span>
            <textarea
              v-model="summaries[code]"
              maxlength="2000"
              rows="3"
              class="textarea w-full"
              :placeholder="trip.summary || t('tripForm.summary')"
            ></textarea>
          </label>
        </fieldset>
      </fieldset>

      <p v-if="message" role="status" class="text-sm text-success">{{ message }}</p>
      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
      <div v-if="canEdit" class="card-actions justify-end">
        <button type="submit" class="btn btn-primary" :disabled="saving">{{ t('common.save') }}</button>
      </div>
    </form>
    <ConfirmDialog ref="confirmDialog" />
  </div>
</template>
