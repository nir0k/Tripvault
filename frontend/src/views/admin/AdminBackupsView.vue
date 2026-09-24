<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  deleteBackupConfig,
  getBackupRun,
  listArchives,
  listBackupConfigs,
  listBackupRuns,
  restore,
  runBackup,
  waitForRestore,
  type RestoreInput,
} from '@/api/backups'
import type { Archive, BackupConfig, BackupRun, RestoreRun } from '@/api/types'
import AppIcon from '@/components/AppIcon.vue'
import BackupConfigDialog from '@/components/admin/BackupConfigDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const { t, te, locale } = useI18n()

const configs = ref<BackupConfig[]>([])
const runs = ref<BackupRun[]>([])
const nextCursor = ref<string | null>(null)
const loadError = ref('')
const notice = ref('')

const configDialog = useTemplateRef<InstanceType<typeof BackupConfigDialog>>('configDialog')
const editing = ref<BackupConfig | null>(null)

const confirmDialog = useTemplateRef<InstanceType<typeof ConfirmDialog>>('confirmDialog')

const restoreDialog = useTemplateRef<HTMLDialogElement>('restoreDialog')
// restoreSource is where the archive is read from: an SFTP configuration's id,
// or the empty string for this server's own backup directory.
const restoreSource = ref('')
const archives = ref<Archive[]>([])
const archivesLoading = ref(false)
const restoreArchive = ref('')
const restorePassphrase = ref('')
const restoreError = ref('')
const restoring = ref(false)
const restoreProgress = ref<RestoreRun | null>(null)

// The runs still going are polled while they work, so a long backup shows its
// progress instead of an answer that never changes until the page is reloaded.
const pollInterval = 3000
let poll: ReturnType<typeof setInterval> | undefined

const working = computed(() => runs.value.filter((run) => run.status === 'running'))

// remoteConfigs are the places other than this server's disk an archive can be
// restored from; a local configuration writes to that same disk.
const remoteConfigs = computed(() => configs.value.filter((config) => config.destination_type === 'sftp'))

const chosenArchive = computed(() => archives.value.find((archive) => archive.name === restoreArchive.value))

// ownPassphrase is true when the chosen source keeps the passphrase its
// archives are locked with, so nothing needs typing.
const ownPassphrase = computed(() =>
  configs.value.some((config) => config.id === restoreSource.value && config.encrypted))

// load reads the configurations and the first page of the history.
async function load(): Promise<void> {
  loadError.value = ''
  try {
    const [stored, history] = await Promise.all([listBackupConfigs(), listBackupRuns()])
    configs.value = stored
    runs.value = history.items
    nextCursor.value = history.next_cursor
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// loadMore appends the next page of the history.
async function loadMore(): Promise<void> {
  if (!nextCursor.value) {
    return
  }
  try {
    const page = await listBackupRuns(nextCursor.value)
    runs.value = [...runs.value, ...page.items]
    nextCursor.value = page.next_cursor
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// refreshWorking updates the runs that are still going, one request each.
async function refreshWorking(): Promise<void> {
  const inFlight = working.value
  if (inFlight.length === 0) {
    return
  }
  const updated = await Promise.all(inFlight.map(async (run) => {
    try {
      return await getBackupRun(run.id)
    } catch {
      // A run that cannot be read right now is left as it was: the next tick
      // asks again, and a failure here is not worth an error message.
      return run
    }
  }))

  const byID = new Map(updated.map((run) => [run.id, run]))
  runs.value = runs.value.map((run) => byID.get(run.id) ?? run)
  // A run that has just finished changes what the configurations show next to
  // them, so the list is read again once.
  if (updated.some((run) => run.status !== 'running')) {
    configs.value = await listBackupConfigs()
  }
}

// openCreate shows the form for a new configuration.
function openCreate(): void {
  editing.value = null
  configDialog.value?.open()
}

// openEdit shows the form for an existing configuration.
function openEdit(config: BackupConfig): void {
  editing.value = config
  configDialog.value?.open()
}

// onSaved refreshes the list after the dialog stored a configuration.
async function onSaved(): Promise<void> {
  editing.value = null
  await load()
}

// withdraw stops a configuration, leaving the archives it wrote alone.
async function withdraw(config: BackupConfig): Promise<void> {
  const confirmed = await confirmDialog.value?.ask(t('backups.withdrawQuestion'))
  if (!confirmed) {
    return
  }
  try {
    await deleteBackupConfig(config.id)
    await load()
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// start runs a configuration now and puts the run at the top of the history.
async function start(config: BackupConfig): Promise<void> {
  loadError.value = ''
  try {
    const run = await runBackup(config.id)
    runs.value = [run, ...runs.value]
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// openRestore shows the form that replaces everything from an archive,
// starting with the archives on this server's own disk.
function openRestore(): void {
  restoreSource.value = ''
  restorePassphrase.value = ''
  restoreError.value = ''
  restoreProgress.value = null
  restoreDialog.value?.showModal()
  void loadArchives()
}

// loadArchives reads what the chosen source holds and picks the newest.
async function loadArchives(): Promise<void> {
  archives.value = []
  restoreArchive.value = ''
  restoreError.value = ''
  archivesLoading.value = true
  try {
    archives.value = await listArchives(restoreSource.value || null)
    restoreArchive.value = archives.value[0]?.name ?? ''
  } catch (err) {
    restoreError.value = errorMessage(err, t, te)
  } finally {
    archivesLoading.value = false
  }
}

// confirmRestore asks the reader to type out what a restore does.
async function confirmRestore(): Promise<boolean> {
  return (await confirmDialog.value?.ask(t('backups.restoreQuestion'), {
    danger: true,
    confirmWord: t('backups.restoreWord'),
  })) ?? false
}

// runRestore starts a restore and follows it to its end, reporting each stage.
async function runRestore(input: RestoreInput, onProgress?: (run: RestoreRun) => void): Promise<void> {
  const result = await waitForRestore(await restore(input), onProgress)
  notice.value = t('backups.restored', { trips: result.trips, files: result.files })
  await load()
}

// submitRestore replaces the instance from the chosen archive.
async function submitRestore(): Promise<void> {
  if (!restoreArchive.value || !(await confirmRestore())) {
    return
  }
  restoring.value = true
  restoreError.value = ''
  try {
    await runRestore({
      configId: restoreSource.value || null,
      archive: restoreArchive.value,
      passphrase: restorePassphrase.value,
    }, (run) => { restoreProgress.value = run })
    restoreDialog.value?.close()
  } catch (err) {
    restoreError.value = errorMessage(err, t, te)
  } finally {
    restoring.value = false
  }
}

// restoreHeld replaces the instance from the archive a run wrote, read back
// from where its configuration put it.
async function restoreHeld(run: BackupRun): Promise<void> {
  if (!(await confirmRestore())) {
    return
  }
  loadError.value = ''
  try {
    await runRestore({ configId: run.backup_config_id, archive: run.artifact })
  } catch (err) {
    loadError.value = errorMessage(err, t, te)
  }
}

// describeDestination says where a configuration writes, in one line.
function describeDestination(config: BackupConfig): string {
  if (config.destination_type !== 'sftp') {
    return t('backups.destinations.local')
  }
  const { host = '', user = '', path = '' } = config.destination_params
  return `${user}@${host}${path ? `:${path}` : ''}`
}

// describeKeeping says how long a configuration keeps its archives.
function describeKeeping(config: BackupConfig): string {
  if (config.retain_count > 0) {
    return t('backups.keepsCount', { count: config.retain_count })
  }
  if (config.retain_days > 0) {
    return t('backups.keepsDays', { days: config.retain_days })
  }
  return t('backups.keepsEverything')
}

// formatSize turns a byte count into something a person reads.
function formatSize(bytes: number | null): string {
  if (bytes === null) {
    return ''
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(value < 10 && unit > 0 ? 1 : 0)} ${units[unit]}`
}

// percentOf is how far a run has got through its files, as a percentage.
function percentOf(run: BackupRun): number {
  const total = run.progress?.bytes_total ?? 0
  if (!run.progress || total === 0) {
    return 0
  }
  return Math.min(100, Math.round((run.progress.bytes_done / total) * 100))
}

onMounted(async () => {
  await load()
  poll = setInterval(refreshWorking, pollInterval)
})

onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <section class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">{{ t('backups.title') }}</h1>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-ghost" @click="openRestore">
          <AppIcon name="archive" />
          {{ t('backups.restore') }}
        </button>
        <button type="button" class="btn btn-primary" @click="openCreate">
          <AppIcon name="plus" />
          {{ t('backups.create') }}
        </button>
      </div>
    </div>

    <p class="text-sm text-base-content/70">{{ t('backups.intro') }}</p>
    <p v-if="loadError" role="alert" class="text-error">{{ loadError }}</p>
    <div v-if="notice" role="status" class="alert alert-success">
      <span>{{ notice }}</span>
      <button type="button" class="btn btn-ghost btn-sm" @click="notice = ''">{{ t('common.close') }}</button>
    </div>

    <ul v-if="configs.length > 0" class="divide-y divide-base-300 rounded-box border border-base-300">
      <li v-for="config in configs" :key="config.id" class="flex flex-wrap items-center gap-3 p-4">
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2 font-medium">
            <span class="truncate">{{ describeDestination(config) }}</span>
            <span v-if="config.encrypted" class="badge badge-primary badge-sm">{{ t('backups.encrypted') }}</span>
            <span v-if="!config.enabled" class="badge badge-ghost badge-sm">{{ t('backups.disabled') }}</span>
          </p>
          <p class="text-sm text-base-content/70">
            {{ config.schedule_cron ? t('backups.schedules', { schedule: config.schedule_cron }) : t('backups.onRequest') }}
            · {{ describeKeeping(config) }}
          </p>
          <p v-if="config.next_run_at" class="text-sm text-base-content/70">
            {{ t('backups.nextRun', { time: formatDateTime(config.next_run_at, locale) }) }}
          </p>
        </div>

        <button type="button" class="btn btn-sm" @click="start(config)">{{ t('backups.runNow') }}</button>
        <div class="dropdown dropdown-end">
          <div tabindex="0" role="button" class="btn btn-ghost btn-sm btn-square" :aria-label="t('backups.actions')">
            <AppIcon name="dots" />
          </div>
          <ul tabindex="0" class="menu dropdown-content z-10 w-48 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
            <li><button type="button" @click="openEdit(config)">{{ t('backups.edit') }}</button></li>
            <li><button type="button" class="text-error" @click="withdraw(config)">{{ t('backups.withdraw') }}</button></li>
          </ul>
        </div>
      </li>
    </ul>
    <p v-else class="rounded-box border border-dashed border-base-300 p-6 text-center text-base-content/70">
      {{ t('backups.none') }}
    </p>

    <div class="space-y-3">
      <h2 class="text-xl font-semibold">{{ t('backups.history') }}</h2>
      <ul v-if="runs.length > 0" class="divide-y divide-base-300 rounded-box border border-base-300">
        <li v-for="run in runs" :key="run.id" class="flex flex-wrap items-center gap-3 p-4">
          <div class="min-w-0 flex-1">
            <p class="flex flex-wrap items-center gap-2">
              <span
                class="badge badge-sm" :class="{
                  'badge-success': run.status === 'success',
                  'badge-error': run.status === 'failed',
                  'badge-info': run.status === 'running',
                }"
              >{{ t(`backups.status.${run.status}`) }}</span>
              <span class="truncate text-sm">{{ formatDateTime(run.started_at, locale) }}</span>
              <span v-if="run.size_bytes !== null" class="text-sm text-base-content/70">{{ formatSize(run.size_bytes) }}</span>
            </p>
            <p v-if="run.artifact" class="truncate font-mono text-xs text-base-content/70">{{ run.artifact }}</p>
            <p v-if="run.error_message" class="text-sm text-error">{{ run.error_message }}</p>

            <template v-if="run.status === 'running'">
              <p v-if="run.progress" class="text-sm text-base-content/70">
                {{ t(`backups.stages.${run.progress.stage}`) }}
                · {{ t('backups.filesDone', { done: run.progress.files_done, total: run.progress.files_total }) }}
              </p>
              <progress v-if="run.progress" class="progress progress-primary w-full" :value="percentOf(run)" max="100"></progress>
              <p v-else class="text-sm text-base-content/70">{{ t('backups.interrupted') }}</p>
            </template>
          </div>

          <button v-if="run.archive_held" type="button" class="btn btn-ghost btn-sm text-error" @click="restoreHeld(run)">
            {{ t('backups.restoreThis') }}
          </button>
        </li>
      </ul>
      <p v-else class="rounded-box border border-dashed border-base-300 p-6 text-center text-base-content/70">
        {{ t('backups.noRuns') }}
      </p>
      <button v-if="nextCursor" type="button" class="btn btn-ghost btn-sm" @click="loadMore">{{ t('backups.more') }}</button>
    </div>

    <BackupConfigDialog ref="configDialog" :config="editing" @saved="onSaved" @closed="editing = null" />

    <ConfirmDialog ref="confirmDialog" />

    <dialog ref="restoreDialog" class="modal modal-bottom sm:modal-middle">
      <form class="modal-box flex flex-col gap-3" @submit.prevent="submitRestore">
        <h2 class="text-lg font-bold">{{ t('backups.restoreTitle') }}</h2>
        <p class="text-sm text-warning">{{ t('backups.restoreWarning') }}</p>

        <label class="form-control">
          <span class="label-text">{{ t('backups.restoreSource') }}</span>
          <select v-model="restoreSource" class="select w-full" :disabled="restoring" @change="loadArchives">
            <option value="">{{ t('backups.sourceDisk') }}</option>
            <option v-for="config in remoteConfigs" :key="config.id" :value="config.id">
              {{ describeDestination(config) }}
            </option>
          </select>
        </label>

        <label class="form-control">
          <span class="label-text">{{ t('backups.archive') }}</span>
          <select v-model="restoreArchive" required class="select w-full" :disabled="restoring || archives.length === 0">
            <option v-for="archive in archives" :key="archive.name" :value="archive.name">
              {{ formatDateTime(archive.written_at, locale) }} · {{ formatSize(archive.size_bytes) }}{{ archive.encrypted ? ` · ${t('backups.encrypted')}` : '' }}
            </option>
          </select>
          <span v-if="archivesLoading" class="text-sm text-base-content/70">{{ t('backups.archivesLoading') }}</span>
          <span v-else-if="archives.length === 0 && !restoreError" class="text-sm text-base-content/70">{{ t('backups.noArchives') }}</span>
          <span v-else-if="chosenArchive" class="truncate font-mono text-xs text-base-content/70">{{ chosenArchive.name }}</span>
        </label>

        <label v-if="chosenArchive?.encrypted" class="floating-label">
          <span>{{ t('backups.passphrase') }}</span>
          <input
            v-model="restorePassphrase"
            type="password"
            autocomplete="off"
            class="input w-full"
            :required="!ownPassphrase"
            :placeholder="ownPassphrase ? t('backups.passphraseOwn') : t('backups.passphrase')"
          />
        </label>

        <p class="text-sm text-base-content/70">{{ t('backups.restoreElsewhere') }}</p>

        <p v-if="restoreError" role="alert" class="text-sm text-error">{{ restoreError }}</p>
        <p v-else-if="restoreProgress" role="status" class="text-sm text-base-content/70">
          {{ t(`backups.restoreStages.${restoreProgress.stage}`) }}
        </p>

        <div class="modal-action">
          <button type="button" class="btn btn-ghost" @click="restoreDialog?.close()">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-error" :disabled="restoring || !chosenArchive">
            <span v-if="restoring" class="loading loading-spinner loading-sm"></span>
            {{ t('backups.restore') }}
          </button>
        </div>
      </form>
      <form method="dialog" class="modal-backdrop">
        <button type="submit">{{ t('common.close') }}</button>
      </form>
    </dialog>
  </section>
</template>
