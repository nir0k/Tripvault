/** LatLng is a point as [latitude, longitude], the order Leaflet takes. */
export type LatLng = [number, number]

/**
 * decodePolyline reads a line in the encoded polyline format with five
 * decimals, the format the backend stores leg geometry in.
 */
export function decodePolyline(encoded: string): LatLng[] {
  const points: LatLng[] = []
  let index = 0
  let lat = 0
  let lng = 0

  // next reads one zigzag-encoded, 5-bit chunked signed value.
  const next = (): number => {
    let result = 0
    let shift = 0
    let byte: number
    do {
      byte = encoded.charCodeAt(index++) - 63
      result |= (byte & 0x1f) << shift
      shift += 5
    } while (byte >= 0x20 && index < encoded.length + 1)
    return result & 1 ? ~(result >> 1) : result >> 1
  }

  while (index < encoded.length) {
    lat += next()
    lng += next()
    points.push([lat / 1e5, lng / 1e5])
  }
  return points
}
