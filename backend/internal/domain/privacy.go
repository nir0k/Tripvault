package domain

import (
	"math"
	"math/rand/v2"
	"slices"
)

// A person may name where they live, and the circle around it is then kept out
// of everything their trips show to somebody who is not on them: a read-only
// link and every PDF. A recording begins at the front door and a plan's first
// leg leaves from it, so without the circle a shared report would print the
// address on its first map.
//
// The circle is not centred on the home. Trimming lines at the edge of a circle
// around the door would draw that circle with their ends, and its centre is the
// door; the centre is moved instead to a random point within half the radius,
// so the home lies well inside the circle but nowhere in particular.

// Bounds of the radius of a home zone, in metres.
const (
	MinHomeRadiusM     = 200
	MaxHomeRadiusM     = 2000
	DefaultHomeRadiusM = 500
)

// metresPerDegree is the length of a degree of latitude, near enough for
// moving a point by a few hundred metres.
const metresPerDegree = 111_320

// PrivacyZone is a circle nothing inside of which is shown to a reader from
// outside the trip.
type PrivacyZone struct {
	Center  Point
	RadiusM int
}

// HomeZone is the home a person named and the circle that hides it.
type HomeZone struct {
	// Home is the point the person chose, shown back only to them.
	Home Point
	// Zone is the circle that is hidden, its centre moved off the home.
	Zone PrivacyZone
}

// NewHomeZone - validates a home and draws the circle that hides it, its
// centre at a random point within half the radius of the home.
//
// Arguments:
//   - home: the point the person chose.
//   - radiusM: the radius of the circle, MinHomeRadiusM to MaxHomeRadiusM.
//
// Returns:
//   - the home with its circle.
//   - a *ValidationError when the point or the radius is out of range.
func NewHomeZone(home Point, radiusM int) (HomeZone, error) {
	if err := validatePoint("home", home); err != nil {
		return HomeZone{}, err
	}
	if radiusM < MinHomeRadiusM || radiusM > MaxHomeRadiusM {
		return HomeZone{}, NewValidationError("radius_m", "out_of_range", "must be between 200 and 2000 metres")
	}
	// The square root spreads the centre evenly over the disc rather than
	// crowding it near the home.
	distance := float64(radiusM) / 2 * math.Sqrt(rand.Float64())
	bearing := 2 * math.Pi * rand.Float64()
	return HomeZone{Home: home, Zone: PrivacyZone{Center: offset(home, distance, bearing), RadiusM: radiusM}}, nil
}

// offset moves a point by a distance in metres along a bearing in radians,
// on a flat approximation that holds for the few hundred metres it is used for.
func offset(point Point, distance, bearing float64) Point {
	scale := math.Max(math.Cos(point.Lat*math.Pi/180), 0.01)
	moved := Point{
		Lat: point.Lat + distance*math.Cos(bearing)/metresPerDegree,
		Lng: point.Lng + distance*math.Sin(bearing)/(metresPerDegree*scale),
	}
	moved.Lat = math.Max(-90, math.Min(90, moved.Lat))
	if moved.Lng > 180 {
		moved.Lng -= 360
	} else if moved.Lng < -180 {
		moved.Lng += 360
	}
	return moved
}

// PrivacyZones are the circles hidden from a reader of one trip: the homes of
// everybody on it.
type PrivacyZones []PrivacyZone

// Contains - reports whether a point lies inside any of the circles.
//
// Arguments:
//   - point: the point.
//
// Returns:
//   - true when the point is hidden.
func (z PrivacyZones) Contains(point Point) bool {
	for _, zone := range z {
		if greatCircle(zone.Center, point) <= float64(zone.RadiusM) {
			return true
		}
	}
	return false
}

// Hides - reports whether a pair of optional coordinates lies inside a circle;
// a position that is not set lies nowhere.
//
// Arguments:
//   - lat, lng: the coordinates, both set or neither.
//
// Returns:
//   - true when the position is hidden.
func (z PrivacyZones) Hides(lat, lng *float64) bool {
	return lat != nil && lng != nil && z.Contains(Point{Lat: *lat, Lng: *lng})
}

// earthRadiusM is the mean Earth radius the great-circle distance uses.
const earthRadiusM = 6_371_008.8

// greatCircle measures the distance between two points in metres.
func greatCircle(a, b Point) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat := toRad(b.Lat - a.Lat)
	dLng := toRad(b.Lng - a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(a.Lat))*math.Cos(toRad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Line - removes the points of a line that lie inside a circle. A line that
// leaves home starts at the edge of the circle; one that crosses it is joined
// straight across.
//
// Arguments:
//   - encoded: the line in the encoded polyline format.
//
// Returns:
//   - the line without those points, or "" when fewer than two are left.
func (z PrivacyZones) Line(encoded string) string {
	if len(z) == 0 || encoded == "" {
		return encoded
	}
	points := DecodePolyline(encoded, 5)
	kept := z.points(points)
	if len(kept) == len(points) {
		return encoded
	}
	if len(kept) < 2 {
		return ""
	}
	return EncodePolyline(kept)
}

// points returns the points outside every circle, in order.
func (z PrivacyZones) points(points []Point) []Point {
	kept := make([]Point, 0, len(points))
	for _, point := range points {
		if !z.Contains(point) {
			kept = append(kept, point)
		}
	}
	return kept
}

// Media - returns a picture without the place it was taken at, when that place
// is hidden.
//
// Arguments:
//   - item: the picture.
//
// Returns:
//   - the picture, its coordinates cleared when they lie inside a circle.
func (z PrivacyZones) Media(item Media) Media {
	if z.Hides(item.Lat, item.Lng) {
		item.Lat, item.Lng = nil, nil
	}
	return item
}

// Outside - returns the content as a reader from outside the trip sees it:
// nothing inside the circles. A place, a stay, an end of a transfer or a
// change of a journey there keeps its name but loses its position, address
// and map reference; lines lose the points there; a stop there moves to the
// nearest point of its line still shown, and goes when none is. Distances,
// times and costs are left as they were: they say how far, not where. The
// content it is called on is left as it was.
//
// Arguments:
//   - zones: the circles to hide.
//
// Returns:
//   - a copy with what lies inside them removed.
func (c DocumentContent) Outside(zones PrivacyZones) DocumentContent {
	if len(zones) == 0 {
		return c
	}
	c.Items = slices.Clone(c.Items)
	for index := range c.Items {
		item := &c.Items[index]
		if zones.Hides(item.Lat, item.Lng) {
			item.Lat, item.Lng, item.Address, item.OSMRef = nil, nil, "", ""
		}
	}
	c.Stays = slices.Clone(c.Stays)
	for index := range c.Stays {
		stay := &c.Stays[index]
		if zones.Hides(stay.Lat, stay.Lng) {
			stay.Lat, stay.Lng, stay.Address = nil, nil, ""
		}
	}
	c.Transfers = slices.Clone(c.Transfers)
	for index := range c.Transfers {
		transfer := &c.Transfers[index]
		if zones.Hides(transfer.FromLat, transfer.FromLng) {
			transfer.FromLat, transfer.FromLng, transfer.FromAddress = nil, nil, ""
		}
		if zones.Hides(transfer.ToLat, transfer.ToLng) {
			transfer.ToLat, transfer.ToLng, transfer.ToAddress = nil, nil, ""
		}
	}
	c.Legs = slices.Clone(c.Legs)
	for index := range c.Legs {
		leg := &c.Legs[index]
		leg.Geometry = zones.Line(leg.Geometry)
		if leg.Via != nil {
			leg.Via = zones.points(leg.Via)
		}
		leg.Segments = slices.Clone(leg.Segments)
		for position := range leg.Segments {
			segment := &leg.Segments[position]
			segment.Geometry = zones.Line(segment.Geometry)
			if zones.Hides(segment.StopLat, segment.StopLng) {
				segment.StopLat, segment.StopLng = nil, nil
			}
		}
	}
	c.Tracks = slices.Clone(c.Tracks)
	for index := range c.Tracks {
		recorded := &c.Tracks[index]
		recorded.Geometry = zones.Line(recorded.Geometry)
		recorded.Stops = zones.stops(recorded.Stops, DecodePolyline(recorded.Geometry, 5))
	}
	return c
}

// stops moves the stops inside a circle to the nearest point of the line still
// shown, and leaves them out when nothing of the line is.
func (z PrivacyZones) stops(stops []Stop, line []Point) []Stop {
	kept := make([]Stop, 0, len(stops))
	for _, stop := range stops {
		if z.Contains(stop.Point) {
			if len(line) == 0 {
				continue
			}
			nearest := line[0]
			for _, point := range line[1:] {
				if greatCircle(point, stop.Point) < greatCircle(nearest, stop.Point) {
					nearest = point
				}
			}
			stop.Point = nearest
		}
		kept = append(kept, stop)
	}
	return kept
}
