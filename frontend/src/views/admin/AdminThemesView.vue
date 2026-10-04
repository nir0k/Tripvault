<script setup lang="ts">
import { onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError } from '@/api/client'
import { createTheme, deleteTheme, listThemes, replaceTheme } from '@/api/themes'
import type { InstanceTheme, ThemeFile, ThemeVariant } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import ThemePreview from '@/components/ThemePreview.vue'
import { useSessionStore } from '@/stores/session'
import { saveBlob } from '@/utils/download'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'
import { activeCustomThemeId, applyCustomTheme, themeFile, themeFilename, themeTemplate } from '@/utils/theme'

const { t, te, locale } = useI18n()
const session = useSessionStore()

const themes = ref<InstanceTheme[]>([])
const error = ref('')
const notice = ref('')
const busy = ref(false)

const fileInput = useTemplateRef<HTMLInputElement>('fileInput')
const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

// replacing is the theme the next chosen file replaces; null adds a new one.
const replacing = ref<InstanceTheme | null>(null)

const VARIANTS: ThemeVariant[] = ['light', 'dark']

// load reads every theme of the instance.
async function load(): Promise<void> {
  try {
    themes.value = await listThemes()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

// download hands a theme file to the browser's own download.
function download(file: ThemeFile): void {
  const body = new Blob([`${JSON.stringify(file, null, 2)}\n`], { type: 'application/json' })
  saveBlob(body, themeFilename(file.name))
}

// downloadTemplate saves the built-in palettes as a theme file to start from.
function downloadTemplate(): void {
  download(themeTemplate(t('themes.templateName')))
}

// pick opens the file chooser, to add a theme or to replace one.
function pick(theme: InstanceTheme | null): void {
  replacing.value = theme
  error.value = ''
  notice.value = ''
  fileInput.value?.click()
}

/**
 * describeError says what is wrong with an uploaded file. A refused colour is
 * named by its place in the file - "dark.primary" - since the administrator
 * has to find it there; everything else reads like any other error.
 */
function describeError(err: unknown): string {
  if (err instanceof ApiError && err.code === 'validation_failed' && typeof err.details.field === 'string') {
    const reason = String(err.details.reason ?? '')
    return t('themes.invalidFile', {
      field: err.details.field,
      reason: te(`themes.reasons.${reason}`) ? t(`themes.reasons.${reason}`) : errorMessage(err, t, te),
    })
  }
  if (err instanceof ApiError && err.code === 'already_exists') {
    return t('themes.nameTaken')
  }
  return errorMessage(err, t, te)
}

// upload reads the chosen file and sends it as a new theme or in place of one.
async function upload(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const chosen = input.files?.[0]
  input.value = ''
  if (!chosen) {
    return
  }
  let file: ThemeFile
  try {
    file = JSON.parse(await chosen.text()) as ThemeFile
  } catch {
    error.value = t('themes.notJson')
    return
  }
  busy.value = true
  try {
    const target = replacing.value
    if (target) {
      const replaced = await replaceTheme(target.id, file)
      // The administrator reading the interface in this theme sees the new
      // colours at once, as everybody else will on their next load.
      if (activeCustomThemeId.value === replaced.id) {
        applyCustomTheme(replaced)
      }
      notice.value = t('themes.replaced', { name: replaced.name })
    } else {
      const created = await createTheme(file)
      notice.value = t('themes.uploaded', { name: created.name })
    }
    await load()
  } catch (err) {
    error.value = describeError(err)
  } finally {
    busy.value = false
  }
}

// remove deletes a theme after confirmation; its readers go back to the
// built-in theme.
async function remove(theme: InstanceTheme): Promise<void> {
  const confirmed = await confirmDialog.value?.ask(t('themes.deleteQuestion', { name: theme.name }), { danger: true })
  if (!confirmed) {
    return
  }
  error.value = ''
  notice.value = ''
  try {
    await deleteTheme(theme.id)
    if (activeCustomThemeId.value === theme.id) {
      await session.reload()
    }
    await load()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

onMounted(load)
</script>

<template>
  <section class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('themes.title') }}</h1>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-hover-outline" @click="downloadTemplate">
          <AppIcon name="download" />
          {{ t('themes.downloadTemplate') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="pick(null)">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          <AppIcon v-else name="upload" />
          {{ t('themes.upload') }}
        </button>
      </div>
    </div>

    <p class="text-sm text-base-content/70">{{ t('themes.hint') }}</p>

    <input ref="fileInput" type="file" accept=".json,application/json" class="hidden" @change="upload" />

    <p v-if="error" role="alert" class="text-error">{{ error }}</p>
    <div v-if="notice" role="status" class="alert alert-success">
      <span>{{ notice }}</span>
      <button type="button" class="btn btn-ghost btn-sm" @click="notice = ''">{{ t('common.close') }}</button>
    </div>

    <p v-if="!themes.length" class="rounded-box border border-dashed border-base-300 p-6 text-center text-base-content/70">
      {{ t('themes.empty') }}
    </p>

    <ul class="space-y-4">
      <li v-for="theme in themes" :key="theme.id" class="card border border-base-300 bg-base-100">
        <div class="card-body gap-3">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="card-title min-w-0 flex-1 truncate">{{ theme.name }}</h2>
            <span v-for="variant in VARIANTS.filter((item) => theme[item])" :key="variant" class="badge badge-ghost badge-sm">
              {{ t(`preferences.themes.${variant}`) }}
            </span>
            <span class="text-xs text-base-content/60">
              {{ t('themes.updated', { time: formatDateTime(theme.updated_at, locale) }) }}
            </span>
            <div class="dropdown dropdown-end">
              <div tabindex="0" role="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('themes.actions', { name: theme.name })">
                <AppIcon name="dots" />
              </div>
              <ul tabindex="0" class="menu dropdown-content z-10 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
                <li>
                  <button type="button" @click="download(themeFile(theme))">
                    <AppIcon name="download" />
                    {{ t('themes.download') }}
                  </button>
                </li>
                <li>
                  <button type="button" :disabled="busy" @click="pick(theme)">
                    <AppIcon name="upload" />
                    {{ t('themes.replace') }}
                  </button>
                </li>
                <li>
                  <button type="button" class="text-error" @click="remove(theme)">
                    <AppIcon name="trash" />
                    {{ t('themes.delete') }}
                  </button>
                </li>
              </ul>
            </div>
          </div>
          <div class="grid gap-3 md:grid-cols-2">
            <template v-for="variant in VARIANTS" :key="variant">
              <ThemePreview v-if="theme[variant]" :palette="theme[variant]" :variant="variant" />
            </template>
          </div>
        </div>
      </li>
    </ul>

    <ConfirmDialog ref="confirmDialog" />
  </section>
</template>
