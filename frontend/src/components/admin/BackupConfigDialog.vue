<script setup lang="ts">
import { computed, reactive, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { createBackupConfig, updateBackupConfig, type BackupConfigInput } from '@/api/backups'
import type { BackupConfig } from '@/api/types'
import { errorMessage } from '@/utils/errors'

const props = defineProps<{ config: BackupConfig | null }>()
const emit = defineEmits<{ saved: [BackupConfig]; closed: [] }>()

const { t, te } = useI18n()

// keeping says which of the two ways of keeping archives the form is showing.
// The API takes them as two numbers of which at most one may be set; a reader
// thinks of it as one choice.
type Keeping = 'count' | 'days' | 'everything'

const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const error = ref('')
const busy = ref(false)

const form = reactive({
  destination: 'local',
  host: '',
  port: '',
  path: '',
  user: '',
  hostKey: '',
  password: '',
  privateKey: '',
  keyPassphrase: '',
  encrypted: false,
  passphrase: '',
  scheduled: false,
  schedule: '0 3 * * *',
  keeping: 'count' as Keeping,
  retainCount: 5,
  retainDays: 30,
  enabled: true,
})

const isSFTP = computed(() => form.destination === 'sftp')
const editing = computed(() => props.config !== null)

// holds says whether the configuration being edited already keeps a credential,
// which is what lets the form offer "leave it as it is" instead of showing it.
function holds(name: string): boolean {
  return props.config?.stored_secrets.includes(name) ?? false
}

// fill puts a configuration into the form, or the defaults of a new one.
function fill(config: BackupConfig | null): void {
  error.value = ''
  form.password = ''
  form.privateKey = ''
  form.keyPassphrase = ''
  form.passphrase = ''

  if (!config) {
    form.destination = 'local'
    form.host = ''
    form.port = ''
    form.path = ''
    form.user = ''
    form.hostKey = ''
    form.encrypted = false
    form.scheduled = false
    form.schedule = '0 3 * * *'
    form.keeping = 'count'
    form.retainCount = 5
    form.retainDays = 30
    form.enabled = true
    return
  }

  form.destination = config.destination_type
  form.host = config.destination_params.host ?? ''
  form.port = config.destination_params.port ?? ''
  form.path = config.destination_params.path ?? ''
  form.user = config.destination_params.user ?? ''
  form.hostKey = config.destination_params.host_key ?? ''
  form.encrypted = config.encrypted
  form.scheduled = config.schedule_cron !== ''
  form.schedule = config.schedule_cron || '0 3 * * *'
  form.enabled = config.enabled

  if (config.retain_count > 0) {
    form.keeping = 'count'
    form.retainCount = config.retain_count
  } else if (config.retain_days > 0) {
    form.keeping = 'days'
    form.retainDays = config.retain_days
  } else {
    form.keeping = 'everything'
  }
}

watch(() => props.config, fill, { immediate: true })

/** open shows the dialog, filled with whatever configuration was given. */
function open(): void {
  fill(props.config)
  dialog.value?.showModal()
}

// close hides the dialog and tells the page it is gone.
function close(): void {
  dialog.value?.close()
  emit('closed')
}

// secrets collects only the credentials the form was actually given. A field
// left empty means "keep what is stored" rather than "remove it", which is why
// nothing empty is sent.
function secrets(): Record<string, string> {
  const given: Record<string, string> = {}
  if (isSFTP.value) {
    if (form.password) {
      given.password = form.password
    }
    if (form.privateKey) {
      given.private_key = form.privateKey
    }
    if (form.keyPassphrase) {
      given.key_passphrase = form.keyPassphrase
    }
  }
  if (form.encrypted && form.passphrase) {
    given.passphrase = form.passphrase
  }
  return given
}

// params collects what the destination needs, and no credential.
function params(): Record<string, string> {
  if (!isSFTP.value) {
    return {}
  }
  const destination: Record<string, string> = { host: form.host.trim(), user: form.user.trim() }
  if (form.port.trim()) {
    destination.port = form.port.trim()
  }
  if (form.path.trim()) {
    destination.path = form.path.trim()
  }
  if (form.hostKey.trim()) {
    destination.host_key = form.hostKey.trim()
  }
  return destination
}

// submit stores the configuration and hands it back to the page.
async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    const input: BackupConfigInput = {
      destination_type: form.destination,
      destination_params: params(),
      secrets: secrets(),
      encrypted: form.encrypted,
      schedule_cron: form.scheduled ? form.schedule.trim() : '',
      retain_count: form.keeping === 'count' ? Number(form.retainCount) : 0,
      retain_days: form.keeping === 'days' ? Number(form.retainDays) : 0,
      enabled: form.enabled,
    }
    const saved = props.config
      ? await updateBackupConfig(props.config.id, input)
      : await createBackupConfig(input)
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
  <dialog ref="dialog" class="modal modal-bottom sm:modal-middle">
    <form class="modal-box flex max-h-[90vh] flex-col gap-3 overflow-y-auto sm:max-w-2xl" @submit.prevent="submit">
      <h2 class="text-lg font-bold">{{ editing ? t('backups.editTitle') : t('backups.createTitle') }}</h2>

      <label class="floating-label">
        <span>{{ t('backups.destination') }}</span>
        <select v-model="form.destination" class="select w-full">
          <option value="local">{{ t('backups.destinations.local') }}</option>
          <option value="sftp">{{ t('backups.destinations.sftp') }}</option>
        </select>
      </label>
      <p v-if="!isSFTP" class="text-sm text-base-content/70">{{ t('backups.localHint') }}</p>

      <template v-if="isSFTP">
        <div class="grid gap-3 sm:grid-cols-[2fr_1fr]">
          <label class="floating-label">
            <span>{{ t('backups.host') }}</span>
            <input v-model="form.host" type="text" required class="input w-full" :placeholder="t('backups.host')" />
          </label>
          <label class="floating-label">
            <span>{{ t('backups.port') }}</span>
            <input v-model="form.port" type="text" inputmode="numeric" class="input w-full" placeholder="22" />
          </label>
        </div>
        <label class="floating-label">
          <span>{{ t('backups.user') }}</span>
          <input v-model="form.user" type="text" required autocomplete="off" class="input w-full" :placeholder="t('backups.user')" />
        </label>
        <label class="floating-label">
          <span>{{ t('backups.path') }}</span>
          <input v-model="form.path" type="text" class="input w-full" placeholder="tripvault-backups" />
        </label>
        <label class="floating-label">
          <span>{{ t('backups.password') }}</span>
          <input
            v-model="form.password"
            type="password"
            autocomplete="new-password"
            class="input w-full"
            :placeholder="holds('password') ? t('backups.secretKept') : t('backups.password')"
          />
        </label>
        <label class="form-control">
          <span class="label-text">{{ t('backups.privateKey') }}</span>
          <textarea
            v-model="form.privateKey"
            rows="3"
            class="textarea w-full font-mono text-xs"
            :placeholder="holds('private_key') ? t('backups.secretKept') : t('backups.privateKeyHint')"
          ></textarea>
        </label>
        <label class="floating-label">
          <span>{{ t('backups.keyPassphrase') }}</span>
          <input
            v-model="form.keyPassphrase"
            type="password"
            autocomplete="new-password"
            class="input w-full"
            :placeholder="holds('key_passphrase') ? t('backups.secretKept') : t('backups.keyPassphrase')"
          />
        </label>
        <label class="form-control">
          <span class="label-text">{{ t('backups.hostKey') }}</span>
          <textarea v-model="form.hostKey" rows="2" class="textarea w-full font-mono text-xs" :placeholder="t('backups.hostKeyHint')"></textarea>
        </label>
      </template>

      <label class="label cursor-pointer justify-start gap-3">
        <input v-model="form.encrypted" type="checkbox" class="checkbox" />
        <span>{{ t('backups.encrypt') }}</span>
      </label>
      <template v-if="form.encrypted">
        <label class="floating-label">
          <span>{{ t('backups.passphrase') }}</span>
          <input
            v-model="form.passphrase"
            type="password"
            autocomplete="new-password"
            class="input w-full"
            :placeholder="holds('passphrase') ? t('backups.secretKept') : t('backups.passphrase')"
          />
        </label>
        <p class="text-sm text-warning">{{ t('backups.passphraseWarning') }}</p>
      </template>

      <label class="label cursor-pointer justify-start gap-3">
        <input v-model="form.scheduled" type="checkbox" class="checkbox" />
        <span>{{ t('backups.onSchedule') }}</span>
      </label>
      <template v-if="form.scheduled">
        <label class="floating-label">
          <span>{{ t('backups.schedule') }}</span>
          <input v-model="form.schedule" type="text" required class="input w-full font-mono" placeholder="0 3 * * *" />
        </label>
        <p class="text-sm text-base-content/70">{{ t('backups.scheduleHint') }}</p>
      </template>

      <label class="floating-label">
        <span>{{ t('backups.keeping') }}</span>
        <select v-model="form.keeping" class="select w-full">
          <option value="count">{{ t('backups.keepByCount') }}</option>
          <option value="days">{{ t('backups.keepByAge') }}</option>
          <option value="everything">{{ t('backups.keepEverything') }}</option>
        </select>
      </label>
      <label v-if="form.keeping === 'count'" class="floating-label">
        <span>{{ t('backups.retainCount') }}</span>
        <input v-model.number="form.retainCount" type="number" min="1" max="365" required class="input w-full" />
      </label>
      <label v-else-if="form.keeping === 'days'" class="floating-label">
        <span>{{ t('backups.retainDays') }}</span>
        <input v-model.number="form.retainDays" type="number" min="1" max="3650" required class="input w-full" />
      </label>

      <label class="label cursor-pointer justify-start gap-3">
        <input v-model="form.enabled" type="checkbox" class="checkbox" />
        <span>{{ t('backups.enabled') }}</span>
      </label>

      <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" @click="close">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="busy">
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
          {{ t('common.save') }}
        </button>
      </div>
    </form>
    <form method="dialog" class="modal-backdrop">
      <button type="submit" @click="emit('closed')">{{ t('common.close') }}</button>
    </form>
  </dialog>
</template>
