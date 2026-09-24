// Package routing calculates legs: road routes from a provider, great-circle
// lines for flights, and estimates when the provider cannot answer. Only the
// backend talks to the provider; its key never reaches a browser.
package routing

import (
	"math"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// earthRadiusM is the mean Earth radius the great-circle distance uses.
const earthRadiusM = 6_371_008.8

// Haversine - measures the great-circle distance between two points.
//
// Arguments:
//   - a, b: the points.
//
// Returns:
//   - the distance in metres.
func Haversine(a, b domain.Point) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat := toRad(b.Lat - a.Lat)
	dLng := toRad(b.Lng - a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(a.Lat))*math.Cos(toRad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// The profiles a road route is asked for. They are this service's own words
// rather than any provider's: each provider translates them into the names it
// uses, so adding a provider does not rename anything the cache is keyed by.
const (
	profileWalk = "walk"
	profileBike = "bike"
	profileCar  = "car"
)

// Profile - names the profile a road mode is routed with.
//
// Public transport has no profile of its own: it follows the car route as an
// approximation, and the interface says so.
//
// Arguments:
//   - mode: the travel mode.
//
// Returns:
//   - the profile, and false for modes that are not routed on roads.
func Profile(mode domain.TravelMode) (string, bool) {
	switch mode {
	case domain.ModeWalk:
		return profileWalk, true
	case domain.ModeBike:
		return profileBike, true
	case domain.ModeCar, domain.ModeTransit:
		return profileCar, true
	default:
		return "", false
	}
}

// estimates holds, per road mode, how much longer than a straight line a road
// usually is, and an average speed for the time of an estimated leg.
var estimates = map[domain.TravelMode]struct {
	factor   float64
	speedKmH float64
}{
	domain.ModeWalk:    {1.2, 4.5},
	domain.ModeBike:    {1.25, 15},
	domain.ModeCar:     {1.3, 60},
	domain.ModeTransit: {1.3, 45},
}

// Flight estimate: cruising speed plus the time spent getting up and down.
const (
	flightSpeedKmH       = 800
	flightOverheadSecond = 30 * 60
)

// Result is a calculated leg, ready to be stored.
type Result = domain.LegCalculation

// straightResult builds a result on the straight line between two points.
func straightResult(from, to domain.Point, distance float64, duration *int, source domain.LegSource, reason string) Result {
	metres := int(math.Round(distance))
	return Result{
		DistanceM: &metres,
		DurationS: duration,
		Geometry:  domain.EncodePolyline([]domain.Point{from, to}),
		Source:    source,
		Error:     reason,
	}
}

// StraightLine - calculates a flight or an "other" leg on the great circle.
//
// A flight gets an estimated time until somebody types the scheduled one; an
// "other" leg - a ferry, a taxi - gets none.
//
// Arguments:
//   - mode: flight or other.
//   - from, to: the points.
//
// Returns:
//   - the result.
func StraightLine(mode domain.TravelMode, from, to domain.Point) Result {
	distance := Haversine(from, to)
	var duration *int
	if mode == domain.ModeFlight {
		seconds := int(math.Round(distance/1000/flightSpeedKmH*3600)) + flightOverheadSecond
		duration = &seconds
	}
	return straightResult(from, to, distance, duration, domain.LegStraightLine, "")
}

// Estimate - approximates a road leg the provider could not calculate: the
// straight line scaled by the mode's detour factor, at the mode's average speed.
//
// Arguments:
//   - mode: a road mode.
//   - from, to: the points.
//   - reason: why the provider was not used, one of the domain.LegError values.
//
// Returns:
//   - the result, marked as an estimate with its reason.
func Estimate(mode domain.TravelMode, from, to domain.Point, reason string) Result {
	params, ok := estimates[mode]
	if !ok {
		params = estimates[domain.ModeCar]
	}
	distance := Haversine(from, to) * params.factor
	seconds := int(math.Round(distance / 1000 / params.speedKmH * 3600))
	return straightResult(from, to, distance, &seconds, domain.LegEstimate, reason)
}
