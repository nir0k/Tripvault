package pdf

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestStopLine checks a stop is told by how far along the line it lies, what
// it is, when it comes and what it costs: in a plan the way to it at the
// line's speed, in a report the time it was reached.
func TestStopLine(t *testing.T) {
	trip := domain.Trip{Currency: "EUR", Travelers: 2, TrackSpeedKmh: 4.7}
	activity := domain.Item{ID: uuid.New(), Name: "Reykjadalur"}
	cost := domain.Money(1500)
	reached := domain.ClockTime(12*60 + 40)
	stop := domain.Stop{Kind: domain.StopFood, Name: "Café", DistanceM: 4700, PlannedCost: &cost,
		CostPerPerson: true, ActualTime: &reached}

	plan := stopLine(english, trip, activity, domain.Track{}, stop, false)
	for _, want := range []string{"4.7 km", "Café (café)", "about 1 h 00 min from the start", "30.00 EUR"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan line %q lacks %q", plan, want)
		}
	}
	report := stopLine(english, trip, activity, domain.Track{}, stop, true)
	if !strings.Contains(report, english.clock(int(reached))) || strings.Contains(report, "from the start") {
		t.Errorf("report line %q", report)
	}
	if line := stopLine(russian, trip, activity, domain.Track{}, domain.Stop{Kind: domain.StopRest}, true); !strings.Contains(line, "Отдых") {
		t.Errorf("a stop without a name: %q", line)
	}
	if line := stopLine(english, trip, activity, domain.Track{}, domain.Stop{Kind: domain.StopFood}, false); strings.Contains(line, "from the start") {
		t.Errorf("a stop at the start is timed: %q", line)
	}
}

// TestStopsAreLabelledOnTheDayMap checks the stops along a line are drawn on
// the day's map under their activity's number and a letter, and unlabelled on
// the map of the whole trip.
func TestStopsAreLabelledOnTheDayMap(t *testing.T) {
	day := domain.Day{ID: uuid.New()}
	lat, lng := 64.0, -21.0
	first := domain.Item{ID: uuid.New(), DayID: &day.ID, Kind: domain.ItemPlace, Lat: &lat, Lng: &lng}
	hike := domain.Item{ID: uuid.New(), DayID: &day.ID, Position: 1, Kind: domain.ItemActivity, Lat: &lat, Lng: &lng}
	line := domain.Track{ItemID: hike.ID, Geometry: domain.EncodePolyline([]domain.Point{{Lat: 64, Lng: -21},
		{Lat: 64.1, Lng: -21}}), Stops: []domain.Stop{{Point: domain.Point{Lat: 64.02, Lng: -21}},
		{Point: domain.Point{Lat: 64.05, Lng: -21}}}}
	content := domain.DocumentContent{Days: []domain.Day{day}, Items: []domain.Item{first, hike},
		Tracks: []domain.Track{line}}

	layer := dayLayer(content, day, 0, true)
	if len(layer.stops) != 2 || layer.stops[0].label != "2a" || layer.stops[1].label != "2b" {
		t.Errorf("day map stops %+v", layer.stops)
	}
	if trip := tripLayer(content); len(trip.stops) != 2 || trip.stops[0].label != "" {
		t.Errorf("trip map stops %+v", trip.stops)
	}
}
