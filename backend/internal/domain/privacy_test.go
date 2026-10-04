package domain

import (
	"testing"
)

// home is the point the tests hide, in Reykjavík.
var home = Point{Lat: 64.1466, Lng: -21.9426}

// north moves a point north by a distance in metres.
func north(point Point, metres float64) Point {
	return Point{Lat: point.Lat + metres/metresPerDegree, Lng: point.Lng}
}

// ptr returns the address of a copy of a value.
func ptr[T any](value T) *T { return &value }

// TestNewHomeZoneMovesTheCentreOffTheHome checks the circle always hides the
// home with half its radius to spare, and is not simply centred on it.
func TestNewHomeZoneMovesTheCentreOffTheHome(t *testing.T) {
	moved := false
	for range 200 {
		zone, err := NewHomeZone(home, 500)
		if err != nil {
			t.Fatalf("new zone: %v", err)
		}
		if zone.Home != home || zone.Zone.RadiusM != 500 {
			t.Fatalf("zone %+v", zone)
		}
		if distance := greatCircle(zone.Zone.Center, home); distance > 250.5 {
			t.Fatalf("the centre is %.0f m from the home, more than half the radius", distance)
		} else if distance > 1 {
			moved = true
		}
	}
	if !moved {
		t.Error("the centre never moved off the home")
	}
}

// TestNewHomeZoneRefusesWhatIsOutOfRange checks the radius and the point are
// validated.
func TestNewHomeZoneRefusesWhatIsOutOfRange(t *testing.T) {
	for name, input := range map[string]struct {
		point  Point
		radius int
		code   string
	}{
		"too small": {home, MinHomeRadiusM - 1, "radius_m:out_of_range"},
		"too large": {home, MaxHomeRadiusM + 1, "radius_m:out_of_range"},
		"off earth": {Point{Lat: 91, Lng: 0}, 500, "home:invalid_point"},
	} {
		if _, err := NewHomeZone(input.point, input.radius); validationCode(t, err) != input.code {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestOutsideHidesWhatLiesInsideTheCircle checks a place, a stay, a transfer's
// end, a change, lines and stops inside the circle are hidden while what lies
// outside it stays, and the content it was made from is left as it was.
func TestOutsideHidesWhatLiesInsideTheCircle(t *testing.T) {
	zones := PrivacyZones{{Center: home, RadiusM: 500}}
	near, far := north(home, 100), north(home, 5000)
	farther := north(home, 6000)
	line := EncodePolyline([]Point{home, near, far, farther})

	content := DocumentContent{
		Items: []Item{
			{Name: "Home", Lat: &near.Lat, Lng: &near.Lng, Address: "Laugavegur 1", OSMRef: "node/1"},
			{Name: "Harpa", Lat: &far.Lat, Lng: &far.Lng, Address: "Austurbakki 2"},
		},
		Stays:     []Stay{{Name: "Flat", Lat: &near.Lat, Lng: &near.Lng, Address: "Laugavegur 1"}},
		Transfers: []Transfer{{FromLat: &near.Lat, FromLng: &near.Lng, FromAddress: "Laugavegur 1", ToLat: &far.Lat, ToLng: &far.Lng}},
		Legs: []Leg{{Geometry: line, DistanceM: ptr(6000), Via: []Point{near, far},
			Segments: []LegSegment{{Geometry: line, StopLat: &near.Lat, StopLng: &near.Lng}}}},
		Tracks: []Track{
			{Geometry: line, DistanceM: 6000, Stops: []Stop{{Name: "Café", Point: near}, {Name: "Spring", Point: farther}}},
			{Geometry: EncodePolyline([]Point{home, near}), Stops: []Stop{{Name: "Bench", Point: near}}},
		},
	}

	hidden := content.Outside(zones)
	if item := hidden.Items[0]; item.Lat != nil || item.Address != "" || item.OSMRef != "" || item.Name != "Home" {
		t.Errorf("the place at home: %+v", item)
	}
	if item := hidden.Items[1]; item.Lat == nil || item.Address == "" {
		t.Errorf("the place far away lost its position: %+v", item)
	}
	if stay := hidden.Stays[0]; stay.Lat != nil || stay.Address != "" {
		t.Errorf("the stay at home: %+v", stay)
	}
	if transfer := hidden.Transfers[0]; transfer.FromLat != nil || transfer.FromAddress != "" || transfer.ToLat == nil {
		t.Errorf("the transfer from home: %+v", transfer)
	}
	leg := hidden.Legs[0]
	for _, point := range DecodePolyline(leg.Geometry, 5) {
		if zones.Contains(point) {
			t.Errorf("the leg still passes %v", point)
		}
	}
	if len(DecodePolyline(leg.Geometry, 5)) != 2 || *leg.DistanceM != 6000 || len(leg.Via) != 1 {
		t.Errorf("the leg: %+v", leg)
	}
	if segment := leg.Segments[0]; segment.StopLat != nil || segment.Geometry != leg.Geometry {
		t.Errorf("the change at home: %+v", segment)
	}
	walk := hidden.Tracks[0]
	if walk.DistanceM != 6000 || len(walk.Stops) != 2 {
		t.Fatalf("the walk: %+v", walk)
	}
	if walk.Stops[0].Point != DecodePolyline(walk.Geometry, 5)[0] || walk.Stops[1].Point != farther {
		t.Errorf("the stops: %+v", walk.Stops)
	}
	if around := hidden.Tracks[1]; around.Geometry != "" || len(around.Stops) != 0 {
		t.Errorf("a walk wholly at home should keep nothing: %+v", around)
	}

	if content.Items[0].Lat == nil || content.Legs[0].Geometry != line || content.Tracks[0].Stops[0].Point != near {
		t.Error("the content the copy was made from was changed")
	}
}

// TestOutsideWithoutZonesChangesNothing checks nobody's home means the content
// comes back as it was, and a picture keeps where it was taken.
func TestOutsideWithoutZonesChangesNothing(t *testing.T) {
	near := north(home, 100)
	content := DocumentContent{Items: []Item{{Lat: &near.Lat, Lng: &near.Lng}}}
	if content.Outside(nil).Items[0].Lat == nil {
		t.Error("a position was hidden with no circle")
	}
	picture := Media{Lat: &near.Lat, Lng: &near.Lng}
	if PrivacyZones(nil).Media(picture).Lat == nil {
		t.Error("a picture lost its position with no circle")
	}
	if (PrivacyZones{{Center: home, RadiusM: 500}}).Media(picture).Lat != nil {
		t.Error("a picture taken at home kept its position")
	}
}
