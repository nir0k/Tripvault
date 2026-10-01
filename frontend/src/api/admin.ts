import { http } from './client'
import type { AdminUser, ListResponse, MailStatus, PreviewStatus, ServiceStatus, StorageStatus, UserInvitation } from './types'

export interface NewUser {
  email: string
  display_name: string
  password: string
  is_admin: boolean
}

export interface UserChanges {
  display_name?: string
  is_admin?: boolean
  is_active?: boolean
}

/** listUsers returns every account. */
export async function listUsers(): Promise<AdminUser[]> {
  return (await http.get<ListResponse<AdminUser>>('/api/v1/admin/users')).data.items
}

/** createUser creates an account with a temporary password. */
export async function createUser(user: NewUser): Promise<AdminUser> {
  return (await http.post<AdminUser>('/api/v1/admin/users', user)).data
}

/** updateUser renames, promotes, demotes, deactivates or reactivates an account. */
export async function updateUser(id: string, changes: UserChanges): Promise<AdminUser> {
  return (await http.patch<AdminUser>(`/api/v1/admin/users/${encodeURIComponent(id)}`, changes)).data
}

/** resetPassword gives an account a new temporary password. */
export async function resetPassword(id: string, password: string): Promise<void> {
  await http.post(`/api/v1/admin/users/${encodeURIComponent(id)}/reset-password`, { password })
}

/** getStatus reads the service state. */
export async function getStatus(): Promise<ServiceStatus> {
  return (await http.get<ServiceStatus>('/api/v1/admin/status')).data
}

/** updateStorage changes the administrator-controlled allowance for one trip. */
export async function updateStorage(tripQuotaMB: number): Promise<StorageStatus> {
  return (await http.patch<StorageStatus>('/api/v1/admin/storage', { trip_quota_mb: tripQuotaMB })).data
}

/** getPreviewStatus reads how the rendering of photo previews is going. */
export async function getPreviewStatus(): Promise<PreviewStatus> {
  return (await http.get<PreviewStatus>('/api/v1/admin/previews')).data
}

/** setMailEnabled lets or stops the worker delivering queued messages. */
export async function setMailEnabled(enabled: boolean): Promise<MailStatus> {
  return (await http.patch<MailStatus>('/api/v1/admin/mail', { enabled })).data
}

/** setSelfRegistration opens or closes registration; opening it needs mail to be delivered. */
export async function setSelfRegistration(enabled: boolean): Promise<MailStatus> {
  return (await http.patch<MailStatus>('/api/v1/admin/registration', { enabled })).data
}

/** sendTestMail verifies SMTP independently of the delivery switch. */
export async function sendTestMail(email: string): Promise<void> {
  await http.post('/api/v1/admin/mail/test', { email })
}

/** inviteUser emails a one-time account invitation. */
export async function inviteUser(email: string, displayName: string, isAdmin: boolean, locale: string): Promise<UserInvitation> {
  return (await http.post<UserInvitation>('/api/v1/admin/user-invitations', {
    email, display_name: displayName, is_admin: isAdmin, locale,
  })).data
}

/** listUserInvitations returns account invitations without their tokens. */
export async function listUserInvitations(): Promise<UserInvitation[]> {
  return (await http.get<ListResponse<UserInvitation>>('/api/v1/admin/user-invitations')).data.items
}

/** revokeUserInvitation withdraws an unused account invitation. */
export async function revokeUserInvitation(id: string): Promise<void> {
  await http.delete(`/api/v1/admin/user-invitations/${encodeURIComponent(id)}`)
}
