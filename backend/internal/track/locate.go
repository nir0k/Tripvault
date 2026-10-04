package track

import (
	"math"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/routing"
)

// passTolerance is how much further, in degrees of latitude - about fifteen
// metres - a later stretch of a line may lie from a point than the nearest one
// and still lose to an earlier one. A walk there and back passes a point twice,
// and the stop is where it is reached first.
const passTolerance = 15.0 / 111_320

// Position is where a point lies on a line: the nearest point of the line to
// it, how far along the line that is, and the slopes of the way there.
type Position struct {
	// Point is the point of the line nearest to the one asked about.
	Point domain.Point
	// DistanceM is how far along the line from its start Point lies, measured
	// over every point of the file as the line's own length is.
	DistanceM int
	// Grades is how many metres of the line up to Point run at each slope,
	// measured as the whole line's grades are; nil when the file records no
	// heights.
	Grades []int
}

// Locate - finds where on the line of a file a point lies, for a stop along it.
//
// The point is moved onto the nearest stretch of the line, so a stop clicked
// beside a trail lies on it. A line that passes the point twice, as a walk
// there and back does, takes it on the first pass. The way up to it is
// measured from the file at its full resolution, so the distance and the
// slopes agree with the whole line's.
//
// Arguments:
//   - data: the file the line was imported from.
//   - near: the point to place on the line.
//
// Returns:
//   - the position on the line.
//   - ErrUnsupported, ErrEmpty or ErrTooManyPoints, or a parsing error.
func Locate(data []byte, near domain.Point) (Position, error) {
	read, err := readPoints(data)
	if err != nil {
		return Position{}, err
	}
	points := read.points
	if len(points) < 2 {
		return Position{}, ErrEmpty
	}

	closest := math.Inf(1)
	for index := 0; index+1 < len(points); index++ {
		_, away := project(near, points[index], points[index+1])
		closest = math.Min(closest, away)
	}
	segment, share := 0, 0.0
	for index := 0; index+1 < len(points); index++ {
		along, away := project(near, points[index], points[index+1])
		if away <= closest+passTolerance {
			segment, share = index, along
			break
		}
	}
	from, to := points[segment], points[segment+1]
	on := domain.Point{Lat: from.Lat + share*(to.Lat-from.Lat), Lng: from.Lng + share*(to.Lng-from.Lng)}

	var distance float64
	for index := 1; index <= segment; index++ {
		distance += routing.Haversine(points[index-1], points[index])
	}
	distance += routing.Haversine(from, on)
	position := Position{Point: on, DistanceM: int(math.Round(distance))}

	if _, _, ok := climb(read.heights, read.moments); ok {
		// The way up to the point ends at it, its height and time those of
		// the stretch it lies on at that share.
		prefixPoints := append(append([]domain.Point{}, points[:segment+1]...), on)
		prefixHeights := append(append([]float64{}, read.heights[:segment+1]...),
			interpolate(read.heights[segment], read.heights[segment+1], share))
		prefixMoments := append(append([]time.Time{}, read.moments[:segment+1]...),
			between(read.moments[segment], read.moments[segment+1], share))
		position.Grades = grades(prefixPoints, prefixHeights, prefixMoments)
	}
	return position, nil
}

// project finds where a point falls on the stretch between two others: the
// share of the way from the first to the second, clamped to the stretch, and
// how far the point lies from it in degrees, with longitudes narrowed by the
// latitude as perpendicular does.
func project(point, from, to domain.Point) (float64, float64) {
	scale := math.Cos(point.Lat * math.Pi / 180)
	px, py := (point.Lng-from.Lng)*scale, point.Lat-from.Lat
	lx, ly := (to.Lng-from.Lng)*scale, to.Lat-from.Lat
	length := lx*lx + ly*ly
	if length == 0 {
		return 0, math.Hypot(px, py)
	}
	share := math.Max(0, math.Min(1, (px*lx+py*ly)/length))
	return share, math.Hypot(px-share*lx, py-share*ly)
}

// interpolate is the height at a share of the way between two, NaN unless
// both are known.
func interpolate(from, to, share float64) float64 {
	if math.IsNaN(from) || math.IsNaN(to) {
		return math.NaN()
	}
	return from + share*(to-from)
}

// between is the moment at a share of the way between two, zero unless both
// are known.
func between(from, to time.Time, share float64) time.Time {
	if from.IsZero() || to.IsZero() {
		return time.Time{}
	}
	return from.Add(time.Duration(share * float64(to.Sub(from))))
}
