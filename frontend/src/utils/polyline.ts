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

/** PASS_TOLERANCE is about fifteen metres in degrees of latitude, as the server's. */
const PASS_TOLERANCE = 15 / 111_320

/**
 * nearestOnLine finds the point of a line nearest to another: the point is
 * dropped onto each stretch of the line, longitudes narrowed by the latitude so
 * the shape is not distorted, and the closest of those is taken - the first
 * of them within about fifteen metres, so a walk there and back takes the
 * point on the way out. The server places a stop on its line the same way.
 *
 * Arguments:
 *   - line: the line, at least one point.
 *   - point: the point to place on it.
 *
 * Returns:
 *   - the nearest point of the line.
 */
export function nearestOnLine(line: readonly LatLng[], point: LatLng): LatLng {
  if (line.length === 1) {
    return line[0]!
  }
  const scale = Math.cos((point[0] * Math.PI) / 180)
  const stretches = line.slice(0, -1).map((from, index) => {
    const to = line[index + 1]!
    const px = (point[1] - from[1]) * scale
    const py = point[0] - from[0]
    const lx = (to[1] - from[1]) * scale
    const ly = to[0] - from[0]
    const length = lx * lx + ly * ly
    const share = length === 0 ? 0 : Math.max(0, Math.min(1, (px * lx + py * ly) / length))
    const away = Math.hypot(px - share * lx, py - share * ly)
    return { away, at: [from[0] + share * (to[0] - from[0]), from[1] + share * (to[1] - from[1])] as LatLng }
  })
  const closest = Math.min(...stretches.map((stretch) => stretch.away))
  return stretches.find((stretch) => stretch.away <= closest + PASS_TOLERANCE)!.at
}
