import { http } from './client'
import type {
  Budget, CoverCrop, CreatedShareLink, DocumentKind, ListResponse, MemberRole, PageResponse, ShareLink, Trip, TripMember,
  TripUser,
} from './types'

export type TripScope = 'all' | 'owned' | 'shared'

/** TripSort orders a list: what matters now first, or what changed last first. */
export type TripSort = 'relevance' | 'updated'

export interface TripListParams {
  scope?: TripScope
  /** Plans or reports; omitted lists both, which is what the search asks for. */
  kind?: DocumentKind
  year?: number
  q?: string
  /** Relevance when omitted; a cursor continues only the order it came with. */
  sort?: TripSort
  /** The page size; the server's default when omitted. */
  limit?: number
  cursor?: string
}

export interface NewTrip {
  title: string
  start_date: string
  end_date: string
  timezone?: string
  currency?: string
  travelers?: number
  budget_amount?: string | null
  /** A plan, or a report written from scratch. */
  kind: DocumentKind
  /** A report's languages, the original first; omitted, the author's own. */
  languages?: string[]
}

/** TripChanges holds the fields to change; null clears the budget. */
export interface TripChanges {
  title?: string
  summary?: string
  start_date?: string
  end_date?: string
  timezone?: string
  currency?: string
  travelers?: number
  budget_amount?: string | null
  /** The picture the trip is shown by; null takes the cover away. */
  cover_media_id?: string | null
  /** The part of the cover shown; null means its middle. A new cover sent without one is shown by its middle. */
  cover_crop?: CoverCrop | null
  /** A report's languages, the original first; a language left out loses its translations. */
  languages?: string[]
  /** Accept that the new period removes days holding content. */
  confirm?: boolean
}

/** tripPath builds the API path of a trip or something inside it. */
function tripPath(tripId: string, suffix = ''): string {
  return `/api/v1/trips/${encodeURIComponent(tripId)}${suffix}`
}

/** listTrips returns one page of the reader's own and shared trips. */
export async function listTrips(params: TripListParams): Promise<PageResponse<Trip>> {
  return (await http.get<PageResponse<Trip>>('/api/v1/trips', { params })).data
}

/** listTripYears returns the years the reader's plans or reports touch, newest first. */
export async function listTripYears(kind: DocumentKind): Promise<number[]> {
  return (await http.get<ListResponse<number>>('/api/v1/trips/years', { params: { kind } })).data.items
}

/** createTrip creates a plan, or a report written from scratch, owned by the reader. */
export async function createTrip(trip: NewTrip): Promise<Trip> {
  return (await http.post<Trip>('/api/v1/trips', trip)).data
}

/**
 * createReportFromPlan writes a new report from a plan: a trip of its own,
 * owned by the reader, with the plan's settings, budget and content copied in
 * and nothing of its people, links or pictures.
 */
export async function createReportFromPlan(planTripId: string): Promise<Trip> {
  return (await http.post<Trip>(tripPath(planTripId, '/reports'))).data
}

/** getTrip reads one trip. */
export async function getTrip(tripId: string): Promise<Trip> {
  return (await http.get<Trip>(tripPath(tripId))).data
}

/** updateTrip changes only the given fields of a trip. */
export async function updateTrip(tripId: string, changes: TripChanges): Promise<Trip> {
  return (await http.patch<Trip>(tripPath(tripId), changes)).data
}

/** deleteTrip deletes a trip for everybody. */
export async function deleteTrip(tripId: string): Promise<void> {
  await http.delete(tripPath(tripId))
}

/** listMembers returns the owner and the members of a trip. */
export async function listMembers(tripId: string): Promise<TripMember[]> {
  return (await http.get<ListResponse<TripMember>>(tripPath(tripId, '/members'))).data.items
}

/** addMember gives a person access to a trip. */
export async function addMember(tripId: string, userId: string, role: MemberRole): Promise<TripMember> {
  return (await http.post<TripMember>(tripPath(tripId, '/members'), { user_id: userId, role })).data
}

/** updateMember changes a member's role. */
export async function updateMember(tripId: string, userId: string, role: MemberRole): Promise<TripMember> {
  return (await http.patch<TripMember>(tripPath(tripId, `/members/${encodeURIComponent(userId)}`), { role })).data
}

/** removeMember takes a member's access away. */
export async function removeMember(tripId: string, userId: string): Promise<void> {
  await http.delete(tripPath(tripId, `/members/${encodeURIComponent(userId)}`))
}

/** NewShareLink describes a link to create; every field has a default. */
export interface NewShareLink {
  label?: string
  include_private_media?: boolean
  /** An RFC 3339 timestamp in the future; null never expires. */
  expires_at?: string | null
}

/** listShareLinks returns a trip's live links, without their tokens. */
export async function listShareLinks(tripId: string): Promise<ShareLink[]> {
  return (await http.get<ListResponse<ShareLink>>(tripPath(tripId, '/share-links'))).data.items
}

/**
 * createShareLink mints a link. The token in the answer is the only copy: it
 * cannot be read again, and a lost link is replaced rather than recovered.
 */
export async function createShareLink(tripId: string, link: NewShareLink): Promise<CreatedShareLink> {
  return (await http.post<CreatedShareLink>(tripPath(tripId, '/share-links'), link)).data
}

/** revokeShareLink stops a link working, at once and for everybody who has it. */
export async function revokeShareLink(linkId: string): Promise<void> {
  await http.delete(`/api/v1/share-links/${encodeURIComponent(linkId)}`)
}

/** getBudget reads a trip's budget: its totals, its breakdown and every cost. */
export async function getBudget(tripId: string): Promise<Budget> {
  return (await http.get<Budget>(tripPath(tripId, '/budget'))).data
}

/** searchUsers finds active accounts by name or email. */
export async function searchUsers(q: string): Promise<TripUser[]> {
  return (await http.get<ListResponse<TripUser>>('/api/v1/users/search', { params: { q } })).data.items
}
