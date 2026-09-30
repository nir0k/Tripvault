import { http, PDF_TIMEOUT_MS } from './client'
import { filenameFrom, saveBlob } from '@/utils/download'
import type { PackingIcon, PackingList, TagColor } from './types'

// A plan's list of what to take. Every change answers with the whole list,
// because a move renumbers its neighbours and a tick changes the counts.

/** PackingItemFields are the fields of an item a change may name; an omitted one is kept. */
export interface PackingItemFields {
  name?: string
  quantity?: number
  note?: string
  packed?: boolean
  bringer_id?: string | null
}

/** tripPath builds the path of a trip's list. */
function tripPath(tripId: string, rest = ''): string {
  return `/api/v1/trips/${encodeURIComponent(tripId)}/packing${rest}`
}

/** getPacking reads a plan's list; a read-only link reads the list of the plan it opens. */
export async function getPacking(tripId: string | null): Promise<PackingList> {
  return (await http.get<PackingList>(tripId === null ? '/api/v1/shared/packing' : tripPath(tripId))).data
}

/** PackingCategoryFields are the fields of a category a change may name; an omitted one is kept. */
export interface PackingCategoryFields {
  name?: string
  color?: TagColor
  icon?: PackingIcon
}

/**
 * createCategory adds a category at the end of the list. Without a colour it
 * takes the next one of the palette, without an icon "other".
 */
export async function createCategory(tripId: string, fields: PackingCategoryFields & { name: string }): Promise<PackingList> {
  return (await http.post<PackingList>(tripPath(tripId, '/categories'), fields)).data
}

/** PackingAddCategory is a category added with its things at once, as a template adds it. */
export interface PackingAddCategory {
  name: string
  color?: TagColor
  icon?: PackingIcon
  items: { name: string; quantity?: number; note?: string }[]
}

/**
 * addToPacking adds categories with their things in one go. A category the
 * list has under the same name is filled rather than repeated, and a thing it
 * holds already is left out.
 */
export async function addToPacking(tripId: string, categories: PackingAddCategory[]): Promise<PackingList> {
  return (await http.post<PackingList>(tripPath(tripId, ':add'), { categories })).data
}

/** updateCategory renames a category or changes its colour or icon. */
export async function updateCategory(categoryId: string, fields: PackingCategoryFields): Promise<PackingList> {
  return (await http.patch<PackingList>(`/api/v1/packing-categories/${encodeURIComponent(categoryId)}`, fields)).data
}

/** deleteCategory removes a category with everything in it. */
export async function deleteCategory(categoryId: string): Promise<PackingList> {
  return (await http.delete<PackingList>(`/api/v1/packing-categories/${encodeURIComponent(categoryId)}`)).data
}

/** reorderCategories puts the categories in a new order, naming every one of them. */
export async function reorderCategories(tripId: string, order: string[]): Promise<PackingList> {
  return (await http.post<PackingList>(tripPath(tripId, '/categories:reorder'), { order })).data
}

/** createItem adds an item at the end of its category, or among the ones without one. */
export async function createItem(
  tripId: string,
  categoryId: string | null,
  fields: PackingItemFields & { name: string },
): Promise<PackingList> {
  return (await http.post<PackingList>(tripPath(tripId, '/items'), { ...fields, category_id: categoryId })).data
}

/** updateItem changes an item's name, quantity, note, tick or bringer. */
export async function updateItem(itemId: string, fields: PackingItemFields): Promise<PackingList> {
  return (await http.patch<PackingList>(`/api/v1/packing-items/${encodeURIComponent(itemId)}`, fields)).data
}

/** moveItem puts an item at a position of a category, or among the ones without one. */
export async function moveItem(itemId: string, categoryId: string | null, position: number): Promise<PackingList> {
  return (await http.post<PackingList>(`/api/v1/packing-items/${encodeURIComponent(itemId)}:move`, {
    category_id: categoryId,
    position,
  })).data
}

/** deleteItem removes an item. */
export async function deleteItem(itemId: string): Promise<PackingList> {
  return (await http.delete<PackingList>(`/api/v1/packing-items/${encodeURIComponent(itemId)}`)).data
}

/** resetPacking takes every tick off, to pack again. */
export async function resetPacking(tripId: string): Promise<PackingList> {
  return (await http.post<PackingList>(tripPath(tripId, ':reset'))).data
}

/**
 * downloadPackingPDF saves the list as a checklist to print, every box empty.
 * A read-only link has no account for the server to read the language from, so
 * it travels with the request.
 */
export async function downloadPackingPDF(tripId: string | null, language: string): Promise<void> {
  const url = tripId === null ? '/api/v1/shared/packing/pdf' : tripPath(tripId, '/pdf')
  const response = await http.get<Blob>(url, {
    params: tripId === null ? { lang: language } : {},
    responseType: 'blob',
    timeout: PDF_TIMEOUT_MS,
  })
  saveBlob(response.data, filenameFrom(response.headers['content-disposition'], 'packing.pdf'))
}
