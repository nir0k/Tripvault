import { http } from './client'
import type { GeoPlace, ListResponse } from './types'

/** searchPlaces finds places by name or address, near focus first. */
export async function searchPlaces(q: string, lang: string, focus: { lat: number; lng: number } | null): Promise<GeoPlace[]> {
  const params = { q, lang, ...(focus ? { lat: focus.lat, lng: focus.lng } : {}) }
  return (await http.get<ListResponse<GeoPlace>>('/api/v1/geo/search', { params })).data.items
}

/** reversePlace names the places at a position, nearest first. */
export async function reversePlace(lat: number, lng: number, lang: string): Promise<GeoPlace[]> {
  return (await http.get<ListResponse<GeoPlace>>('/api/v1/geo/reverse', { params: { lat, lng, lang } })).data.items
}

/** parseLink reads a position from pasted coordinates or a map link. */
export async function parseLink(url: string): Promise<{ lat: number; lng: number; name: string }> {
  return (await http.get<{ lat: number; lng: number; name: string }>('/api/v1/geo/parse-link', { params: { url } })).data
}
