package domain

import (
	"math"
	"strings"
)

// Lines - the legs a provider calculated and the tracks a recording left - are
// stored in the Google encoded polyline format. The codec lives here rather
// than beside the provider code because the domain itself reads lines: a leg
// leaving a recorded place starts where its recording ended.

// EncodePolyline - encodes points in the Google polyline format with five
// decimals, the format the provider returns and the map draws.
//
// Arguments:
//   - points: the line's points in order.
//
// Returns:
//   - the encoded line.
func EncodePolyline(points []Point) string {
	var out strings.Builder
	var prevLat, prevLng int
	encode := func(value int) {
		shifted := value << 1
		if value < 0 {
			shifted = ^shifted
		}
		for shifted >= 0x20 {
			out.WriteByte(byte((0x20 | (shifted & 0x1f)) + 63))
			shifted >>= 5
		}
		out.WriteByte(byte(shifted + 63))
	}
	for _, point := range points {
		lat := int(math.Round(point.Lat * 1e5))
		lng := int(math.Round(point.Lng * 1e5))
		encode(lat - prevLat)
		encode(lng - prevLng)
		prevLat, prevLng = lat, lng
	}
	return out.String()
}

// DecodePolyline - reads a line encoded in the Google polyline format.
//
// The precision is a parameter because the format does not carry it: five
// decimals is what this service stores and what most providers return, while
// Valhalla answers in six and would otherwise come back ten times too small.
//
// Arguments:
//   - encoded: the line.
//   - precision: the number of decimals the values were scaled by.
//
// Returns:
//   - the points in order; an empty slice for an empty or malformed line, which
//     the caller treats as a route without a drawable shape rather than as a
//     failure.
func DecodePolyline(encoded string, precision int) []Point {
	scale := math.Pow10(precision)
	points := make([]Point, 0, len(encoded)/4)

	var lat, lng int
	for index := 0; index < len(encoded); {
		value, read := decodeValue(encoded, index)
		if read == 0 {
			return points
		}
		lat += value
		index += read

		value, read = decodeValue(encoded, index)
		if read == 0 {
			return points
		}
		lng += value
		index += read

		points = append(points, Point{
			Lat: float64(lat) / scale,
			Lng: float64(lng) / scale,
		})
	}
	return points
}

// decodeValue reads one number of a polyline, returning it and how many
// characters it took; zero characters means the line ended mid-number.
func decodeValue(encoded string, index int) (int, int) {
	var result, shift, read int
	for index+read < len(encoded) {
		b := int(encoded[index+read]) - 63
		read++
		result |= (b & 0x1f) << shift
		shift += 5
		if b < 0x20 {
			if result&1 == 1 {
				return ^(result >> 1), read
			}
			return result >> 1, read
		}
	}
	return 0, 0
}
