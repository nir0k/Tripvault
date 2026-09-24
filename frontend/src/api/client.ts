import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import type { ErrorBody, SessionResponse } from './types'

// The refresh token is kept across reloads; the access token lives in memory
// only and is obtained again from the refresh token when the page loads.
const REFRESH_TOKEN_KEY = 'tripvault.refresh_token'

// The token of a read-only link is the whole credential, so it is kept in
// sessionStorage: it dies with the tab, never reaches another one and never goes
// back into the address bar after the first load.
const SHARE_TOKEN_KEY = 'tripvault.share_token'

let accessToken: string | null = null
let refreshing: Promise<SessionResponse | null> | null = null
let onSessionEnded: () => void = () => {}

/** ApiError is a failed API call reduced to what the interface needs. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: Record<string, unknown>

  constructor(status: number, code: string, message: string, details: Record<string, unknown> = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

/** http is the one axios instance every API module uses. */
export const http = axios.create({ timeout: 20_000 })

/**
 * PDF_TIMEOUT_MS is how long a report's PDF may take. It is built on the
 * server, photographs and all, which the first time can take far longer than
 * any other answer; the server allows it the same five minutes.
 */
export const PDF_TIMEOUT_MS = 300_000

/**
 * UPLOAD_TIMEOUT_MS is how long a file upload may take. A photograph of the
 * largest allowed size from a slow connection needs minutes rather than
 * seconds; the server allows an upload the same ten minutes.
 */
export const UPLOAD_TIMEOUT_MS = 600_000

/** readRefreshToken returns the stored refresh token, if any. */
export function readRefreshToken(): string | null {
  try {
    return localStorage.getItem(REFRESH_TOKEN_KEY)
  } catch {
    return null
  }
}

/** storeSession keeps the tokens of a fresh session. */
export function storeSession(session: SessionResponse): void {
  accessToken = session.access_token
  try {
    localStorage.setItem(REFRESH_TOKEN_KEY, session.refresh_token)
  } catch {
    // Without storage the session still works until the page is reloaded.
  }
}

/** clearSession forgets both tokens. */
export function clearSession(): void {
  accessToken = null
  try {
    localStorage.removeItem(REFRESH_TOKEN_KEY)
  } catch {
    // Nothing stored, nothing to remove.
  }
}

/** readShareToken returns the share token of this tab, if it holds one. */
export function readShareToken(): string | null {
  try {
    return sessionStorage.getItem(SHARE_TOKEN_KEY)
  } catch {
    return null
  }
}

/** storeShareToken keeps a share token for the rest of this tab's life. */
export function storeShareToken(token: string): void {
  try {
    sessionStorage.setItem(SHARE_TOKEN_KEY, token)
  } catch {
    // Without storage the link still works until the page is reloaded.
  }
}

/** onSessionEnd registers what happens when a session cannot be refreshed. */
export function onSessionEnd(handler: () => void): void {
  onSessionEnded = handler
}

// REFRESH_LOCK names the lock every tab of this origin takes to refresh.
const REFRESH_LOCK = 'tripvault.refresh'

/**
 * refreshSession exchanges the stored refresh token for a new pair.
 *
 * A refresh token works once, and every tab reads the same stored one. Callers
 * in one tab share one request, and tabs take turns through a lock, so a tab
 * reads the token only after the one before it has stored its replacement.
 */
export function refreshSession(): Promise<SessionResponse | null> {
  if (refreshing) {
    return refreshing
  }
  // Locks exist only in a secure context; over plain http inside a household
  // network the tabs go without, and exchangeRefreshToken copes on its own.
  const locks = typeof navigator !== 'undefined' ? navigator.locks : undefined
  refreshing = (locks ? locks.request(REFRESH_LOCK, exchangeRefreshToken) : exchangeRefreshToken())
    .finally(() => {
      refreshing = null
    })
  return refreshing
}

/**
 * exchangeRefreshToken sends the stored refresh token and keeps what comes back.
 *
 * Returns:
 *   - the new session, or null when there is none to continue.
 */
async function exchangeRefreshToken(): Promise<SessionResponse | null> {
  const token = readRefreshToken()
  if (!token) {
    return null
  }
  try {
    const response = await axios.post<SessionResponse>('/api/v1/auth/refresh', { refresh_token: token })
    storeSession(response.data)
    return response.data
  } catch (error: unknown) {
    // Only a refused token ends the session. A transient server or network
    // failure is reported to the caller without destroying a usable token.
    if (!(error instanceof AxiosError) || error.response?.status !== 401) {
      throw toApiError(error)
    }
    // Another tab may have refreshed with the same token a moment earlier and
    // stored its replacement meanwhile; the session goes on with that one.
    const replaced = readRefreshToken()
    if (replaced && replaced !== token) {
      return exchangeRefreshToken()
    }
    clearSession()
    return null
  }
}

http.interceptors.request.use((config) => {
  if (accessToken) {
    config.headers.set('Authorization', `Bearer ${accessToken}`)
  }
  // The share token goes only to the endpoints a link opens, and in a header
  // rather than the query, which proxies write to their access logs.
  if (config.url?.startsWith('/api/v1/shared')) {
    const token = readShareToken()
    if (token) {
      config.headers.set('X-Share-Token', token)
    }
  }
  return config
})

http.interceptors.response.use(
  (response) => response,
  async (error: unknown) => {
    if (!(error instanceof AxiosError)) {
      throw error
    }
    const config = error.config as (InternalAxiosRequestConfig & { retried?: boolean }) | undefined
    const status = error.response?.status

    // An expired access token is renewed once, transparently, and the request
    // repeated. Anything else is reported to the caller.
    if (status === 401 && config && !config.retried && readRefreshToken()) {
      config.retried = true
      const session = await refreshSession()
      if (session) {
        config.headers.set('Authorization', `Bearer ${session.access_token}`)
        return http.request(config)
      }
      onSessionEnded()
    }
    throw toApiError(error)
  },
)

/** toApiError maps an axios failure onto an ApiError. */
export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error
  }
  if (error instanceof AxiosError) {
    const body = error.response?.data as Partial<ErrorBody> | undefined
    if (error.response && body && typeof body.code === 'string') {
      return new ApiError(error.response.status, body.code, body.message ?? '', body.details ?? {})
    }
    if (!error.response) {
      return new ApiError(0, 'network_error', error.message)
    }
    return new ApiError(error.response.status, 'unexpected_response', error.message)
  }
  return new ApiError(0, 'unknown_error', String(error))
}
