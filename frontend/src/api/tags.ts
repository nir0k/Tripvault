import { http } from './client'
import type { ListResponse, Tag, TagColor, Trip } from './types'

/** TagChanges holds what to change about a tag; an omitted field is kept. */
export interface TagChanges {
  name?: string
  color?: TagColor
}

/** tagPath builds the API path of a tag. */
function tagPath(tagId: string): string {
  return `/api/v1/tags/${encodeURIComponent(tagId)}`
}

/** listTags returns the reader's own tags by name. */
export async function listTags(): Promise<Tag[]> {
  return (await http.get<ListResponse<Tag>>('/api/v1/tags')).data.items
}

/**
 * createTag adds a tag to the reader's list; a name already in use answers 409.
 * Without a colour the server gives the one the reader's tags use least.
 */
export async function createTag(name: string, color?: TagColor): Promise<Tag> {
  return (await http.post<Tag>('/api/v1/tags', { name, color })).data
}

/** updateTag renames one of the reader's tags or recolours it, on every trip that wears it. */
export async function updateTag(tagId: string, changes: TagChanges): Promise<Tag> {
  return (await http.patch<Tag>(tagPath(tagId), changes)).data
}

/** deleteTag removes one of the reader's tags from the list and every trip. */
export async function deleteTag(tagId: string): Promise<void> {
  await http.delete(tagPath(tagId))
}

/** setTripTags replaces the reader's tags on a trip and returns the trip. */
export async function setTripTags(tripId: string, tagIds: string[]): Promise<Trip> {
  return (await http.put<Trip>(`/api/v1/trips/${encodeURIComponent(tripId)}/tags`, { tag_ids: tagIds })).data
}
