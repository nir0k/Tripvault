import { http } from './client'
import type { AdminUser, ListResponse, PreviewStatus, ServiceStatus } from './types'

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

/** getPreviewStatus reads how the rendering of photo previews is going. */
export async function getPreviewStatus(): Promise<PreviewStatus> {
  return (await http.get<PreviewStatus>('/api/v1/admin/previews')).data
}
