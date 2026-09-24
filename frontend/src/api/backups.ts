import { ApiError, http } from './client'
import type { Archive, BackupConfig, BackupRun, ListResponse, PageResponse, RestoreRun } from './types'

/**
 * BackupConfigInput is a configuration as the form sends it.
 *
 * A credential left out of `secrets` keeps whatever is stored, so the form never
 * has to show a password in order to keep it; an empty value removes it.
 */
export interface BackupConfigInput {
  destination_type: string
  destination_params: Record<string, string>
  secrets?: Record<string, string>
  encrypted: boolean
  schedule_cron: string
  retain_count: number
  retain_days: number
  enabled: boolean
}

/** listBackupConfigs returns the instance's backup configurations. */
export async function listBackupConfigs(): Promise<BackupConfig[]> {
  return (await http.get<ListResponse<BackupConfig>>('/api/v1/admin/backup-configs')).data.items
}

/** createBackupConfig stores a new configuration. */
export async function createBackupConfig(input: BackupConfigInput): Promise<BackupConfig> {
  return (await http.post<BackupConfig>('/api/v1/admin/backup-configs', input)).data
}

/** updateBackupConfig replaces a configuration's settings. */
export async function updateBackupConfig(id: string, input: BackupConfigInput): Promise<BackupConfig> {
  return (await http.put<BackupConfig>(`/api/v1/admin/backup-configs/${encodeURIComponent(id)}`, input)).data
}

/** deleteBackupConfig withdraws a configuration, leaving its archives alone. */
export async function deleteBackupConfig(id: string): Promise<void> {
  await http.delete(`/api/v1/admin/backup-configs/${encodeURIComponent(id)}`)
}

/**
 * runBackup starts a backup now. The answer is the run, still going: follow it
 * with getBackupRun until it says how it ended.
 */
export async function runBackup(id: string): Promise<BackupRun> {
  return (await http.post<BackupRun>(`/api/v1/admin/backup-configs/${encodeURIComponent(id)}/run`)).data
}

/** listBackupRuns returns one page of the history, most recent first. */
export async function listBackupRuns(cursor?: string): Promise<PageResponse<BackupRun>> {
  return (await http.get<PageResponse<BackupRun>>('/api/v1/admin/backup-runs', { params: { cursor } })).data
}

/** getBackupRun reads one run, with its progress while it is still going. */
export async function getBackupRun(id: string): Promise<BackupRun> {
  return (await http.get<BackupRun>(`/api/v1/admin/backup-runs/${encodeURIComponent(id)}`)).data
}

/**
 * listArchives reads what a destination holds, newest first. Archives never
 * pass through the browser: a restore is made from one of these.
 *
 * Arguments:
 *   - configId: the configuration whose destination to read, or null for this
 *     server's own backup directory.
 */
export async function listArchives(configId: string | null): Promise<Archive[]> {
  return (await http.get<ListResponse<Archive>>('/api/v1/admin/archives', {
    params: configId ? { config_id: configId } : {},
  })).data.items
}

/** RestoreInput names the archive to replace everything on the instance from. */
export interface RestoreInput {
  /** The configuration whose destination holds it, or null for this server's disk. */
  configId: string | null
  archive: string
  /** Typed only for an archive whose configuration does not hold its passphrase. */
  passphrase?: string
}

/** restore replaces everything on the instance from an archive a destination holds. */
export async function restore(input: RestoreInput): Promise<RestoreRun> {
  return (await http.post<RestoreRun>('/api/v1/admin/restore', {
    ...(input.configId ? { config_id: input.configId } : {}),
    archive: input.archive,
    ...(input.passphrase ? { passphrase: input.passphrase } : {}),
    confirm: 'replace-everything',
  })).data
}

/** getRestoreRun reads progress without relying on the pre-restore session. */
async function getRestoreRun(id: string): Promise<RestoreRun> {
  return (await http.get<RestoreRun>(`/api/v1/restore-runs/${encodeURIComponent(id)}`)).data
}

/** waitForRestore follows a run until it succeeds or reports its stable error. */
export async function waitForRestore(
  initial: RestoreRun,
  onProgress?: (run: RestoreRun) => void,
): Promise<RestoreRun> {
  let run = initial
  onProgress?.(run)
  while (run.status === 'queued' || run.status === 'running') {
    await new Promise((resolve) => window.setTimeout(resolve, 1000))
    run = await getRestoreRun(run.id)
    onProgress?.(run)
  }
  if (run.status === 'failed') {
    throw new ApiError(422, run.error_code || 'restore_failed', run.error_message)
  }
  return run
}
