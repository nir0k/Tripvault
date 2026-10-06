import axios from 'axios'
import { clearSession, readRefreshToken, storeSession, toApiError } from './client'
import type {
  IdeaInvitationPreview, SessionResponse, TripInvitationPreview, User, UserInvitationPreview, VerificationSent,
} from './types'

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

/** register creates an account that stays inactive until its address is confirmed. */
export async function register(displayName: string, email: string, password: string, locale: string): Promise<VerificationSent> {
  try {
    return (await axios.post<VerificationSent>('/api/v1/auth/register', {
      display_name: displayName, email, password, locale,
    })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** verifyEmailByToken confirms an address with the token of the link in its message. */
export async function verifyEmailByToken(token: string): Promise<{ email: string }> {
  try {
    return (await axios.post<{ email: string }>('/api/v1/auth/verify-email', { token })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** verifyEmailByCode confirms an address with the code typed from its message. */
export async function verifyEmailByCode(email: string, code: string): Promise<{ email: string }> {
  try {
    return (await axios.post<{ email: string }>('/api/v1/auth/verify-email', { email, code })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** resendVerification asks for another confirmation message, proving the account with its password. */
export async function resendVerification(email: string, password: string): Promise<VerificationSent> {
  try {
    return (await axios.post<VerificationSent>('/api/v1/auth/verify-email/resend', { email, password })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** requestPasswordReset asks for a recovery message without revealing whether the account exists. */
export async function requestPasswordReset(email: string): Promise<void> {
  try {
    await axios.post('/api/v1/auth/password-reset/request', { email })
  } catch (error) {
    throw toApiError(error)
  }
}

/** completePasswordReset consumes a recovery token and stores a new password. */
export async function completePasswordReset(token: string, password: string): Promise<void> {
  try {
    await axios.post('/api/v1/auth/password-reset/complete', { token, password })
  } catch (error) {
    throw toApiError(error)
  }
}

/** previewUserInvitation describes an account invitation held by the browser. */
export async function previewUserInvitation(token: string): Promise<UserInvitationPreview> {
  try {
    return (await axios.post<UserInvitationPreview>('/api/v1/invitations/user/preview', { token })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** acceptUserInvitation creates the invited account with its owner's password. */
export async function acceptUserInvitation(token: string, password: string, locale: string): Promise<User> {
  try {
    return (await axios.post<User>('/api/v1/invitations/user/accept', { token, password, locale })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** previewTripInvitation describes the trip and role offered by a token. */
export async function previewTripInvitation(token: string): Promise<TripInvitationPreview> {
  try {
    return (await axios.post<TripInvitationPreview>('/api/v1/invitations/trip/preview', { token })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** registerTripInvitation creates an account and accepts its trip invitation. */
export async function registerTripInvitation(token: string, displayName: string, password: string, locale: string): Promise<{ user: User; trip_id: string }> {
  try {
    return (await axios.post<{ user: User; trip_id: string }>('/api/v1/invitations/trip/register', {
      token, display_name: displayName, password, locale,
    })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** previewIdeaInvitation describes the list of ideas and role offered by a token. */
export async function previewIdeaInvitation(token: string): Promise<IdeaInvitationPreview> {
  try {
    return (await axios.post<IdeaInvitationPreview>('/api/v1/invitations/ideas/preview', { token })).data
  } catch (error) {
    throw toApiError(error)
  }
}

/** registerIdeaInvitation creates an account and accepts its invitation to a list of ideas. */
export async function registerIdeaInvitation(token: string, displayName: string, password: string, locale: string): Promise<{ user: User; owner_id: string }> {
  try {
    return (await axios.post<{ user: User; owner_id: string }>('/api/v1/invitations/ideas/register', {
      token, display_name: displayName, password, locale,
    })).data
  } catch (error) {
    throw toApiError(error)
  }
}
