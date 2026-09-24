import { http, UPLOAD_TIMEOUT_MS } from './client'
import type { DateFormat, ListResponse, SessionItem, Theme, TimeFormat, Units, User } from './types'

export interface ProfileChanges {
  display_name?: string
  locale?: string
  theme?: Theme
  units?: Units
  date_format?: DateFormat
  time_format?: TimeFormat
  default_currency?: string
}

/** getMe reads the signed-in account. */
export async function getMe(): Promise<User> {
  return (await http.get<User>('/api/v1/me')).data
}

/** updateMe changes only the given settings of the signed-in account. */
export async function updateMe(changes: ProfileChanges): Promise<User> {
  return (await http.patch<User>('/api/v1/me', changes)).data
}

/**
 * avatarPath builds the address of an account's picture. The bytes travel with
 * an access token in a header, so the address is asked for by useMediaUrl
 * rather than handed to an <img>; the moment the picture changed is part of it,
 * so a new one is fetched instead of the copy the browser holds.
 */
export function avatarPath(userId: string, updatedAt: string | null): string {
  const version = updatedAt ? `?v=${encodeURIComponent(updatedAt)}` : ''
  return `/api/v1/users/${encodeURIComponent(userId)}/avatar${version}`
}

/** setAvatar stores the picture the signed-in account is shown by. */
export async function setAvatar(file: Blob, name = 'avatar.jpg'): Promise<User> {
  const form = new FormData()
  form.append('file', file, name)
  return (await http.put<User>('/api/v1/me/avatar', form, { timeout: UPLOAD_TIMEOUT_MS })).data
}

/** deleteAvatar leaves the account with the placeholder everybody starts with. */
export async function deleteAvatar(): Promise<User> {
  return (await http.delete<User>('/api/v1/me/avatar')).data
}

/**
 * deleteAccount deletes the signed-in account with the trips it owns, their
 * photographs and its sessions. There is no undo.
 */
export async function deleteAccount(): Promise<void> {
  await http.delete('/api/v1/me')
}

/** changePassword replaces the signed-in account's password. */
export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  await http.post('/api/v1/me/password', { current_password: currentPassword, new_password: newPassword })
}

/** listSessions returns the signed-in account's live sessions. */
export async function listSessions(): Promise<SessionItem[]> {
  return (await http.get<ListResponse<SessionItem>>('/api/v1/me/sessions')).data.items
}

/** revokeSession ends one of the signed-in account's sessions. */
export async function revokeSession(id: string): Promise<void> {
  await http.delete(`/api/v1/me/sessions/${encodeURIComponent(id)}`)
}
