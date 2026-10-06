import { http, UPLOAD_TIMEOUT_MS } from './client'
import type {
  Idea, IdeaChange, IdeaFields, IdeaList, IdeaMember, ListResponse, MemberRole, TripInvitation,
} from './types'

// The ideas of where to go the reader may open: their own list and the lists
// shared with them. The ideas come whole and are filtered in the browser: a
// person keeps tens of ideas, not thousands. Only the owner of a list decides
// who it is shared with, so the members and invitations are always the
// reader's own list's.

/** ideaPath builds the API path of an idea. */
function ideaPath(ideaId: string, rest = ''): string {
  return `/api/v1/ideas/${encodeURIComponent(ideaId)}${rest}`
}

/** listIdeas returns every idea the reader may open, the last changed first. */
export async function listIdeas(): Promise<Idea[]> {
  return (await http.get<ListResponse<Idea>>('/api/v1/ideas')).data.items
}

/** getIdea reads one idea the reader may open. */
export async function getIdea(ideaId: string): Promise<Idea> {
  return (await http.get<Idea>(ideaPath(ideaId))).data
}

/**
 * createIdea adds an idea to the reader's own list, or to the list of ownerId
 * when the reader edits it; without a currency it takes the reader's own.
 */
export async function createIdea(fields: IdeaFields, ownerId?: string): Promise<Idea> {
  return (await http.post<Idea>('/api/v1/ideas', ownerId ? { ...fields, owner_id: ownerId } : fields)).data
}

/** updateIdea saves every field of an idea. */
export async function updateIdea(ideaId: string, fields: IdeaFields): Promise<Idea> {
  return (await http.put<Idea>(ideaPath(ideaId), fields)).data
}

/** deleteIdea removes an idea. */
export async function deleteIdea(ideaId: string): Promise<void> {
  await http.delete(ideaPath(ideaId))
}

/** setIdeaTags replaces the reader's tags on an idea and returns the idea. */
export async function setIdeaTags(ideaId: string, tagIds: string[]): Promise<Idea> {
  return (await http.put<Idea>(ideaPath(ideaId, '/tags'), { tag_ids: tagIds })).data
}

/**
 * ideaPhotoPath is the address a photo of an idea is read from, or its preview;
 * it needs the reader's token, so it is loaded through useMediaUrl.
 */
export function ideaPhotoPath(ideaId: string, photoId: string, preview = false): string {
  return ideaPath(ideaId, `/photos/${encodeURIComponent(photoId)}${preview ? '?size=preview' : ''}`)
}

/** addIdeaPhoto uploads a picture to an idea and returns the idea with it. */
export async function addIdeaPhoto(ideaId: string, file: File): Promise<Idea> {
  const form = new FormData()
  form.append('file', file)
  return (await http.post<Idea>(ideaPath(ideaId, '/photos'), form, { timeout: UPLOAD_TIMEOUT_MS })).data
}

/** deleteIdeaPhoto removes a photo of an idea and returns the idea without it. */
export async function deleteIdeaPhoto(ideaId: string, photoId: string): Promise<Idea> {
  return (await http.delete<Idea>(ideaPath(ideaId, `/photos/${encodeURIComponent(photoId)}`))).data
}

/** listIdeaLists returns the reader's own list of ideas first, then the ones shared with them. */
export async function listIdeaLists(): Promise<IdeaList[]> {
  return (await http.get<ListResponse<IdeaList>>('/api/v1/ideas/lists')).data.items
}

/** leaveIdeaList takes the reader off a list of ideas shared with them. */
export async function leaveIdeaList(ownerId: string): Promise<void> {
  await http.delete(`/api/v1/ideas/lists/${encodeURIComponent(ownerId)}`)
}

/** listIdeaMembers returns the people the reader's own ideas are shared with. */
export async function listIdeaMembers(): Promise<IdeaMember[]> {
  return (await http.get<ListResponse<IdeaMember>>('/api/v1/ideas/members')).data.items
}

/** addIdeaMember shares the reader's ideas with an existing account. */
export async function addIdeaMember(userId: string, role: MemberRole): Promise<IdeaMember> {
  return (await http.post<IdeaMember>('/api/v1/ideas/members', { user_id: userId, role })).data
}

/** updateIdeaMember switches a member of the reader's ideas between editor and viewer. */
export async function updateIdeaMember(userId: string, role: MemberRole): Promise<IdeaMember> {
  return (await http.patch<IdeaMember>(`/api/v1/ideas/members/${encodeURIComponent(userId)}`, { role })).data
}

/** removeIdeaMember stops sharing the reader's ideas with a person. */
export async function removeIdeaMember(userId: string): Promise<void> {
  await http.delete(`/api/v1/ideas/members/${encodeURIComponent(userId)}`)
}

/** listIdeaInvitations returns the invitations to the reader's ideas. */
export async function listIdeaInvitations(): Promise<TripInvitation[]> {
  return (await http.get<ListResponse<TripInvitation>>('/api/v1/ideas/invitations')).data.items
}

/** inviteIdeaMember emails an invitation to the reader's ideas to one address. */
export async function inviteIdeaMember(email: string, role: MemberRole, locale: string): Promise<TripInvitation> {
  return (await http.post<TripInvitation>('/api/v1/ideas/invitations', { email, role, locale })).data
}

/** revokeIdeaInvitation withdraws an unused invitation to the reader's ideas. */
export async function revokeIdeaInvitation(invitationId: string): Promise<void> {
  await http.delete(`/api/v1/ideas/invitations/${encodeURIComponent(invitationId)}`)
}

/** acceptIdeaInvitation joins the signed-in account to the offered list of ideas. */
export async function acceptIdeaInvitation(token: string): Promise<string> {
  return (await http.post<{ owner_id: string }>('/api/v1/invitations/ideas/accept', { token })).data.owner_id
}

/**
 * ideaHistory reads the latest changes, the newest first: of one idea, or of
 * the list of ownerId, the reader's own when neither is given.
 */
export async function ideaHistory(scope: { ideaId?: string; ownerId?: string } = {}): Promise<IdeaChange[]> {
  const params: Record<string, string> = {}
  if (scope.ideaId) {
    params.idea_id = scope.ideaId
  } else if (scope.ownerId) {
    params.owner_id = scope.ownerId
  }
  return (await http.get<ListResponse<IdeaChange>>('/api/v1/ideas/history', { params })).data.items
}
