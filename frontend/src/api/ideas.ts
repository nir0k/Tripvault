import { http, UPLOAD_TIMEOUT_MS } from './client'
import type { Idea, IdeaFields, ListResponse } from './types'

// The reader's own ideas of where to go. The list comes whole and is filtered
// in the browser: a person keeps tens of ideas, not thousands.

/** ideaPath builds the API path of an idea. */
function ideaPath(ideaId: string, rest = ''): string {
  return `/api/v1/ideas/${encodeURIComponent(ideaId)}${rest}`
}

/** listIdeas returns every idea of the reader, the last changed first. */
export async function listIdeas(): Promise<Idea[]> {
  return (await http.get<ListResponse<Idea>>('/api/v1/ideas')).data.items
}

/** getIdea reads one of the reader's ideas. */
export async function getIdea(ideaId: string): Promise<Idea> {
  return (await http.get<Idea>(ideaPath(ideaId))).data
}

/** createIdea adds an idea; without a currency it takes the reader's own. */
export async function createIdea(fields: IdeaFields): Promise<Idea> {
  return (await http.post<Idea>('/api/v1/ideas', fields)).data
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
