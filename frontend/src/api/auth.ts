import axios from 'axios'
import { clearSession, readRefreshToken, storeSession, toApiError } from './client'
import type { SessionResponse } from './types'

/** login signs in and keeps the session's tokens. */
export async function login(email: string, password: string): Promise<SessionResponse> {
  try {
    const { data } = await axios.post<SessionResponse>('/api/v1/auth/login', { email, password })
    storeSession(data)
    return data
  } catch (error) {
    throw toApiError(error)
  }
}

/** logout ends the session on the server, then forgets it locally regardless. */
export async function logout(): Promise<void> {
  const token = readRefreshToken()
  clearSession()
  if (!token) {
    return
  }
  try {
    await axios.post('/api/v1/auth/logout', { refresh_token: token })
  } catch {
    // The local session is gone either way; the server's expires on its own.
  }
}
