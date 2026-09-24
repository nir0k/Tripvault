<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createShareLink, listShareLinks, revokeShareLink } from '@/api/trips'
import type { CreatedShareLink, ShareLink } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

// The read-only links of a trip. Only the owner may see or change them: editing
// a trip does not decide who gets to look at it.
//
// A created link's token is shown once and never again, so the address is kept on
// screen until the person dismisses it, with a button to copy it.
const props = defineProps<{
  tripId: string
  isOwner: boolean
}>()

const { t, te, locale } = useI18n()

const links = ref<ShareLink[]>([])
const created = ref<CreatedShareLink | null>(null)
const error = ref('')
const busy = ref(false)
const copied = ref(false)
const form = reactive({ label: '', includePrivateMedia: false, expires: '' })

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

// today bounds the date picker: a link that has already expired is refused.
const today = computed(() => new Date().toISOString().slice(0, 10))

/**
 * createdURL is the address to hand out. It is built from this page's own origin
 * rather than the server's guess at its public address, which is what the person
 * reading it actually reached.
 */
const createdURL = computed(() => (created.value ? `${window.location.origin}/s#token=${created.value.token}` : ''))

/**
 * expiryTimestamp turns a chosen date into the end of that day in the reader's
 * own zone, so a link picked for the 1st works through the whole 1st.
 */
function expiryTimestamp(date: string): string | null {
  if (!date) {
    return null
  }
  return new Date(`${date}T23:59:59`).toISOString()
}

// load reads the trip's live links.
async function load(): Promise<void> {
  if (!props.isOwner) {
    return
  }
  error.value = ''
  try {
    links.value = await listShareLinks(props.tripId)
  } catch (err) {
    error.value = errorMessage(err, t, te)
  }
}

watch(() => props.tripId, () => {
  created.value = null
  void load()
}, { immediate: true })

// create mints a link and keeps its address on screen.
async function create(): Promise<void> {
  busy.value = true
  error.value = ''
  copied.value = false
  try {
    created.value = await createShareLink(props.tripId, {
      label: form.label,
      include_private_media: form.includePrivateMedia,
      expires_at: expiryTimestamp(form.expires),
    })
    Object.assign(form, { label: '', includePrivateMedia: false, expires: '' })
    await load()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}

// copy puts the new address on the clipboard, where the browser allows it.
async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(createdURL.value)
    copied.value = true
  } catch {
    // Without clipboard access the address is still on screen to select.
    copied.value = false
  }
}

// revoke stops a link working, after confirmation.
async function revoke(link: ShareLink): Promise<void> {
  const name = link.label || t('share.unlabelled')
  if (!(await confirmDialog.value?.ask(t('share.confirmRevoke', { name }), { danger: true }))) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    await revokeShareLink(link.id)
    if (created.value?.id === link.id) {
      created.value = null
    }
    await load()
  } catch (err) {
    error.value = errorMessage(err, t, te)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card border border-base-300 bg-base-100">
    <div class="card-body gap-4">
      <p v-if="!isOwner" class="text-sm text-base-content/70">{{ t('share.onlyOwner') }}</p>

      <template v-else>
        <p class="text-sm text-base-content/70">{{ t('share.hint') }}</p>
        <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

        <div v-if="created" class="alert alert-success flex-col items-start gap-2">
          <p class="font-medium">{{ t('share.createdTitle') }}</p>
          <p class="text-sm">{{ t('share.createdHint') }}</p>
          <code class="w-full break-all rounded bg-base-100 px-2 py-1 text-xs text-base-content">{{ createdURL }}</code>
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-sm" @click="copy">{{ t('share.copy') }}</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="created = null">{{ t('common.close') }}</button>
            <span v-if="copied" class="text-sm">{{ t('share.copied') }}</span>
          </div>
        </div>

        <ul v-if="links.length > 0" class="divide-y divide-base-300">
          <li v-for="link in links" :key="link.id" class="flex flex-wrap items-center gap-3 py-3">
            <div class="min-w-0 flex-1 space-y-1">
              <p class="truncate font-medium">{{ link.label || t('share.unlabelled') }}</p>
              <p class="flex flex-wrap items-center gap-1 text-sm text-base-content/70">
                <span v-if="link.include_private_media" class="badge badge-warning badge-sm">
                  {{ t('share.privateMediaShort') }}
                </span>
                <span>{{
                  link.expires_at
                    ? t('share.expiresOn', { time: formatDateTime(link.expires_at, locale) })
                    : t('share.neverExpires')
                }}</span>
              </p>
              <p class="text-sm text-base-content/60">
                {{ link.use_count === 0
                  ? t('share.neverOpened')
                  : t('share.opened', { n: link.use_count, time: formatDateTime(link.last_used_at, locale) }) }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              :disabled="busy"
              @click="revoke(link)"
            >
              <AppIcon name="trash" />
              {{ t('share.revoke') }}
            </button>
          </li>
        </ul>
        <p v-else class="text-sm text-base-content/60">{{ t('share.empty') }}</p>

        <form class="grid gap-3 border-t border-base-300 pt-4" @submit.prevent="create">
          <h3 class="font-medium">{{ t('share.newTitle') }}</h3>
          <label class="floating-label">
            <span>{{ t('share.label') }}</span>
            <input
              v-model="form.label"
              type="text"
              maxlength="200"
              class="input w-full"
              :placeholder="t('share.labelPlaceholder')"
            />
          </label>
          <label class="floating-label">
            <span>{{ t('share.expires') }}</span>
            <input v-model="form.expires" type="date" class="input w-full" :min="today" />
          </label>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="form.includePrivateMedia" type="checkbox" class="checkbox checkbox-sm" />
            <span>{{ t('share.privateMedia') }}</span>
          </label>
          <div class="flex justify-end">
            <button type="submit" class="btn btn-primary" :disabled="busy">
              <AppIcon name="plus" />
              {{ t('share.create') }}
            </button>
          </div>
        </form>
      </template>

      <ConfirmDialog ref="confirmDialog" />
    </div>
  </div>
</template>
