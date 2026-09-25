import { http, PDF_TIMEOUT_MS, UPLOAD_TIMEOUT_MS } from './client'
import { filenameFrom, saveBlob } from '@/utils/download'
import type {
  ActivityType, CostCategory, ItemStatus, PlaceCategory, StayKind, Track, TransferKind, TranslationEntry, TravelMode,
  TripDocument,
} from './types'

// Every change answers with the whole document, recomputed, so each function
// here returns the document as it now stands.

export interface DayChanges {
  title?: string
  notes_md?: string
  start_time?: string
  default_mode?: TravelMode | null
  timezone?: string | null
  morning_anchor?: boolean
  evening_anchor?: boolean
  no_overnight?: boolean
  /** The picture the day is shown by; null takes the cover away. */
  cover_media_id?: string | null
}

export interface PlaceFields {
  kind?: 'place' | 'activity'
  activity_type?: ActivityType
  name?: string
  category?: PlaceCategory
  lat?: number | null
  lng?: number | null
  address?: string
  osm_ref?: string
  description_md?: string
  url?: string
  desired_time?: string | null
  visit_minutes?: number
  is_optional?: boolean
  booking_ref?: string
  planned_cost_amount?: string | null
  cost_per_person?: boolean
  cost_category?: CostCategory
  /** An activity's difficulty, 1 to 5; a place drops it. */
  difficulty?: number | null
  /** The fields below are refused on a plan. */
  status?: ItemStatus
  story_md?: string
  actual_time?: string | null
  actual_end_time?: string | null
  rating?: number | null
  actual_cost_amount?: string | null
  /** The picture the place is shown by; null takes the cover away. */
  cover_media_id?: string | null
}

export interface StayFields {
  name?: string
  kind?: StayKind
  address?: string
  lat?: number | null
  lng?: number | null
  check_in_date?: string
  check_in_time?: string | null
  check_out_date?: string
  check_out_time?: string | null
  booking_ref?: string
  url?: string
  contacts?: string
  notes_md?: string
  planned_cost_amount?: string | null
  /** Refused on a plan. */
  actual_cost_amount?: string | null
}

/** TransferFields holds the fields of a transfer to write. */
export interface TransferFields {
  kind?: TransferKind
  name?: string
  from_name?: string
  from_address?: string
  from_lat?: number | null
  from_lng?: number | null
  to_name?: string
  to_address?: string
  to_lat?: number | null
  to_lng?: number | null
  departure_date?: string
  departure_time?: string | null
  /** Null arrives on the day of departure. */
  arrival_date?: string | null
  arrival_time?: string | null
  booking_ref?: string
  url?: string
  notes_md?: string
  planned_cost_amount?: string | null
  cost_per_person?: boolean
  /** Refused on a plan. */
  actual_cost_amount?: string | null
}

/** ExpenseFields holds the fields to write; a null day_id ties the expense to the whole trip. */
export interface ExpenseFields {
  day_id?: string | null
  category?: CostCategory
  planned_amount?: string | null
  actual_amount?: string | null
  spent_on?: string | null
  note?: string
}

export interface LegChanges {
  mode?: TravelMode
  /** Typed distance in metres; null returns to the calculated one. */
  distance_m?: number | null
  /** Typed time in seconds; null returns to the calculated one. */
  duration_s?: number | null
  planned_cost_amount?: string | null
  /** Refused on a plan. */
  actual_cost_amount?: string | null
  note?: string
  reset_manual?: boolean
}

/** path encodes one identifier into an API path. */
function path(strings: TemplateStringsArray, ...ids: string[]): string {
  return strings.reduce((out, part, index) => out + part + (index < ids.length ? encodeURIComponent(ids[index] ?? '') : ''), '/api/v1')
}

/**
 * downloadReportPDF saves the report as a PDF through the browser.
 *
 * It is fetched rather than linked to, because the endpoint is authenticated and
 * a plain link carries no access token. The filename the server chose is kept.
 */
export async function downloadReportPDF(tripId: string, photos: boolean, contentLang?: string): Promise<void> {
  const params: Record<string, string> = photos ? {} : { photos: 'false' }
  if (contentLang) {
    params.content_lang = contentLang
  }
  const response = await http.get<Blob>(path`/trips/${tripId}/report/pdf`, {
    params,
    timeout: PDF_TIMEOUT_MS,
    responseType: 'blob',
  })
  saveBlob(response.data, filenameFrom(response.headers['content-disposition'], 'report.pdf'))
}

/** updateDocument changes the words before and after a document's days. */
export async function updateDocument(
  documentId: string,
  changes: { intro_md?: string; summary_md?: string },
): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/documents/${documentId}`, changes)).data
}

/**
 * saveTranslations writes fields of a report in one of the languages it is
 * translated into; an empty value removes a translation.
 */
export async function saveTranslations(
  documentId: string,
  lang: string,
  translations: TranslationEntry[],
): Promise<TripDocument> {
  return (await http.put<TripDocument>(path`/documents/${documentId}/translations/${lang}`, { translations })).data
}

/** getDocument reads a whole document. */
export async function getDocument(documentId: string): Promise<TripDocument> {
  return (await http.get<TripDocument>(path`/documents/${documentId}`)).data
}

/** addDay inserts a day; position omitted appends. */
export async function addDay(documentId: string, position?: number): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/days`, { position })).data
}

/** updateDay changes only the given fields of a day. */
export async function updateDay(dayId: string, changes: DayChanges): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/days/${dayId}`, changes)).data
}

/** deleteDay removes a day; confirm accepts removing content elsewhere. */
export async function deleteDay(dayId: string, confirm = false): Promise<TripDocument> {
  return (await http.delete<TripDocument>(path`/days/${dayId}`, { params: confirm ? { confirm: true } : {} })).data
}

/** duplicateDay copies a day with its places right after it. */
export async function duplicateDay(dayId: string): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/days/${dayId}:duplicate`)).data
}

/** reorderDays puts the days in the given order. */
export async function reorderDays(documentId: string, dayIds: string[]): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/days:reorder`, { day_ids: dayIds })).data
}

/** createPlace adds a place to a day, or to the unassigned list when dayId is null. */
export async function createPlace(documentId: string, dayId: string | null, fields: PlaceFields): Promise<TripDocument> {
  const url = dayId ? path`/days/${dayId}/items` : path`/documents/${documentId}/items`
  return (await http.post<TripDocument>(url, fields)).data
}

/** updatePlace changes only the given fields of a place. */
export async function updatePlace(itemId: string, fields: PlaceFields): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/items/${itemId}`, fields)).data
}

/** deletePlace removes a place. */
export async function deletePlace(itemId: string): Promise<TripDocument> {
  return (await http.delete<TripDocument>(path`/items/${itemId}`)).data
}

/** movePlace moves a place to a position in a day, or the unassigned list when dayId is null. */
export async function movePlace(documentId: string, itemId: string, dayId: string | null, position?: number): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/items:move`, {
    item_id: itemId, to_day_id: dayId, position,
  })).data
}

/** copyPlace copies a place to the end of a day, or the unassigned list when dayId is null. */
export async function copyPlace(itemId: string, dayId: string | null): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/items/${itemId}:copy`, { to_day_id: dayId })).data
}

/** createStay adds a stay. */
export async function createStay(documentId: string, fields: StayFields): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/stays`, fields)).data
}

/** updateStay changes only the given fields of a stay. */
export async function updateStay(stayId: string, fields: StayFields): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/stays/${stayId}`, fields)).data
}

/** deleteStay removes a stay. */
export async function deleteStay(stayId: string): Promise<TripDocument> {
  return (await http.delete<TripDocument>(path`/stays/${stayId}`)).data
}

/** createTransfer adds a booked journey. */
export async function createTransfer(documentId: string, fields: TransferFields): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/transfers`, fields)).data
}

/** updateTransfer changes only the given fields of a transfer. */
export async function updateTransfer(transferId: string, fields: TransferFields): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/transfers/${transferId}`, fields)).data
}

/** deleteTransfer removes a transfer. */
export async function deleteTransfer(transferId: string): Promise<TripDocument> {
  return (await http.delete<TripDocument>(path`/transfers/${transferId}`)).data
}

/** createExpense adds a cost that belongs to no place, stay or leg. */
export async function createExpense(documentId: string, fields: ExpenseFields): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/expenses`, fields)).data
}

/** updateExpense changes only the given fields of an expense. */
export async function updateExpense(expenseId: string, fields: ExpenseFields): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/expenses/${expenseId}`, fields)).data
}

/** deleteExpense removes an expense. */
export async function deleteExpense(expenseId: string): Promise<TripDocument> {
  return (await http.delete<TripDocument>(path`/expenses/${expenseId}`)).data
}

/** calculateLegs calculates the legs waiting after changes. */
export async function calculateLegs(documentId: string): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/legs:calculate`)).data
}

/** updateLeg changes only the given fields of a leg. */
export async function updateLeg(legId: string, changes: LegChanges): Promise<TripDocument> {
  return (await http.patch<TripDocument>(path`/legs/${legId}`, changes)).data
}

/**
 * retryEstimatedLegs asks the provider again about the legs that came back as
 * estimates: the ones whose reason can pass, such as a provider that was not
 * configured yet or a daily limit that has since reset.
 */
export async function retryEstimatedLegs(documentId: string): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/documents/${documentId}/legs:retry`)).data
}

/** recalculateLeg calculates a leg again, bypassing the route cache. */
export async function recalculateLeg(legId: string): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/legs/${legId}:recalculate`)).data
}

/** recalculateDay calculates every leg of a day again, bypassing the route cache. */
export async function recalculateDay(dayId: string): Promise<TripDocument> {
  return (await http.post<TripDocument>(path`/days/${dayId}:recalculate`)).data
}

/**
 * importItemTrack stores the recorded line of a place or an activity, such as
 * the hike that started there.
 */
export async function importItemTrack(itemId: string, file: File): Promise<TripDocument> {
  const form = new FormData()
  form.append('file', file, file.name)
  return (await http.post<TripDocument>(`/api/v1/items/${encodeURIComponent(itemId)}/track`, form, {
    timeout: UPLOAD_TIMEOUT_MS,
  })).data
}

/** deleteItemTrack removes the recorded line of a place or an activity. */
export async function deleteItemTrack(itemId: string): Promise<TripDocument> {
  return (await http.delete<TripDocument>(`/api/v1/items/${encodeURIComponent(itemId)}/track`)).data
}

/**
 * downloadTrack saves the file a track was imported from, under the name it was
 * uploaded with. The file needs the reader's token, which travels in a header,
 * so it is fetched and handed to the browser rather than linked to.
 *
 * Arguments:
 *   - track: the track whose file is saved.
 *   - shared: whether the page reads through a read-only link.
 */
export async function downloadTrack(track: Track, shared: boolean): Promise<void> {
  const base = shared ? '/api/v1/shared/tracks' : '/api/v1/tracks'
  const response = await http.get<Blob>(`${base}/${encodeURIComponent(track.id)}/file`, { responseType: 'blob' })
  saveBlob(response.data, track.original_name || `track.${track.format}`)
}
