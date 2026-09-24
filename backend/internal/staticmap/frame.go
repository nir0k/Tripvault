// Package staticmap draws the background of a map as one picture, from the
// same raster tiles the browser shows, for documents that cannot load them on
// their own - the report's PDF.
//
// A map here is a frame: a zoom level and the rectangle of the Web Mercator
// world it covers, in pixels. The frame is chosen to hold every point a map
// has to show; the background is then glued together from the tiles under it,
// and anything drawn over it is placed by projecting through the same frame.
package staticmap

import (
	"math"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TileSize is the width and height of one tile, in pixels.
const TileSize = 256

// Zoom bounds. A map of one place is not zoomed in to its doorstep: a street
// or two around it says more than the pavement.
const (
	minZoom = 1
	maxZoom = 15
)

// framePadding is the room kept free around the points, in pixels, so a pin
// on the edge of the frame is not cut in half.
const framePadding = 28

// Frame is the rectangle of the world one map shows.
type Frame struct {
	Zoom int
	// X and Y are the world pixel at the top left corner, at Zoom.
	X, Y float64
	// Width and Height are the size of the map, in pixels.
	Width, Height int
}

// worldPixel projects a point onto the Web Mercator world at a zoom level, in
// pixels from the top left corner of the world.
func worldPixel(point domain.Point, zoom int) (float64, float64) {
	size := float64(TileSize) * math.Exp2(float64(zoom))
	lat := math.Max(math.Min(point.Lat, 85.05112878), -85.05112878) * math.Pi / 180
	x := (point.Lng + 180) / 360 * size
	y := (1 - math.Log(math.Tan(lat)+1/math.Cos(lat))/math.Pi) / 2 * size
	return x, y
}

// Fit - chooses the frame that shows every point, as close as the size allows.
//
// Arguments:
//   - points: what the map has to show; at least one.
//   - width, height: the size of the map, in pixels.
//
// Returns:
//   - the frame, centred on the points at the highest zoom that holds them all.
func Fit(points []domain.Point, width, height int) Frame {
	zoom := minZoom
	for candidate := maxZoom; candidate >= minZoom; candidate-- {
		left, top, right, bottom := bounds(points, candidate)
		if right-left <= float64(width-2*framePadding) && bottom-top <= float64(height-2*framePadding) {
			zoom = candidate
			break
		}
	}
	left, top, right, bottom := bounds(points, zoom)
	return Frame{
		Zoom:   zoom,
		X:      (left+right)/2 - float64(width)/2,
		Y:      (top+bottom)/2 - float64(height)/2,
		Width:  width,
		Height: height,
	}
}

// bounds is the box around the points at a zoom level, in world pixels.
func bounds(points []domain.Point, zoom int) (left, top, right, bottom float64) {
	left, top = math.Inf(1), math.Inf(1)
	right, bottom = math.Inf(-1), math.Inf(-1)
	for _, point := range points {
		x, y := worldPixel(point, zoom)
		left, right = math.Min(left, x), math.Max(right, x)
		top, bottom = math.Min(top, y), math.Max(bottom, y)
	}
	return left, top, right, bottom
}

// Project - places a point on the map.
//
// Arguments:
//   - point: the position.
//
// Returns:
//   - the pixel it falls on, from the top left corner of the map; it may lie
//     outside the map for a point the frame was not fitted to.
func (f Frame) Project(point domain.Point) (float64, float64) {
	x, y := worldPixel(point, f.Zoom)
	return x - f.X, y - f.Y
}
