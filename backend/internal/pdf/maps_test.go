package pdf

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
)

// placedReport is the sample report with its two places on the map: the
// harbour and the museum of the first day, a short drive apart.
func placedReport(t *testing.T) Report {
	t.Helper()
	report := sampleReport(t)
	positions := map[string][2]float64{
		"The old harbour":       {64.1520, -21.9510},
		"The settlement museum": {64.1470, -21.9400},
	}
	for index, item := range report.Content.Items {
		if position, ok := positions[item.Name]; ok {
			report.Content.Items[index].Lat = &position[0]
			report.Content.Items[index].Lng = &position[1]
		}
	}
	return report
}

// framed turns what MapRequests asks for into maps, with a plain background
// when one is given.
func framed(t *testing.T, requests []MapRequest, background bool) map[uuid.UUID]Map {
	t.Helper()
	maps := make(map[uuid.UUID]Map, len(requests))
	for _, request := range requests {
		m := Map{Frame: staticmap.Fit(request.Points, request.Width, request.Height)}
		if background {
			var out bytes.Buffer
			if err := jpeg.Encode(&out, image.NewGray(image.Rect(0, 0, request.Width, request.Height)), nil); err != nil {
				t.Fatal(err)
			}
			m.Background = out.Bytes()
		}
		maps[request.Key] = m
	}
	return maps
}

// TestMapRequestsAskForWhatHasAPlace checks the whole trip and a day with
// places on it get a map, and a day with nothing to show does not.
func TestMapRequestsAskForWhatHasAPlace(t *testing.T) {
	if requests := MapRequests(sampleReport(t).Content); len(requests) != 0 {
		t.Errorf("a report without positions asked for %d maps", len(requests))
	}

	report := placedReport(t)
	requests := MapRequests(report.Content)
	keys := map[uuid.UUID]bool{}
	for _, request := range requests {
		keys[request.Key] = true
	}
	if !keys[uuid.Nil] || !keys[report.Content.Days[0].ID] || keys[report.Content.Days[1].ID] {
		t.Errorf("the maps asked for are %v; want the trip and the first day only", keys)
	}
}

// TestDayLayerNumbersThePlaces checks the pins of a day carry the numbers of
// their places in the day, the journey between them is dashed while it was
// only estimated, and the trip's map carries no numbers.
func TestDayLayerNumbersThePlaces(t *testing.T) {
	report := placedReport(t)
	layer := dayLayer(report.Content, report.Content.Days[0], 0, true)
	var labels []string
	for _, marker := range layer.markers {
		labels = append(labels, marker.label)
	}
	if len(labels) != 2 || labels[0] != "1" || labels[1] != "2" {
		t.Errorf("the pins are labelled %v, want 1 and 2", labels)
	}
	if len(layer.lines) != 1 || !layer.lines[0].dashed {
		t.Errorf("the lines are %+v, want the one estimated journey, dashed", layer.lines)
	}
	for _, marker := range tripLayer(report.Content).markers {
		if marker.label != "" {
			t.Errorf("the trip's map numbers a place %q", marker.label)
		}
	}
}

// TestDayLayerDrawsTrackAsTrail checks a recorded track is drawn as a trail
// apart from the journeys, with its start and finish marked, and that the
// trip's map keeps them.
func TestDayLayerDrawsTrackAsTrail(t *testing.T) {
	report := placedReport(t)
	recorded := []domain.Point{{Lat: 64.1520, Lng: -21.9510}, {Lat: 64.1600, Lng: -21.9300}, {Lat: 64.1650, Lng: -21.9100}}
	report.Content.Tracks[0].Geometry = domain.EncodePolyline(recorded)

	layer := dayLayer(report.Content, report.Content.Days[0], 0, true)
	var tracks []mapLine
	for _, line := range layer.lines {
		if line.track {
			tracks = append(tracks, line)
		}
	}
	if len(tracks) != 1 || tracks[0].dashed || len(tracks[0].points) != len(recorded) {
		t.Fatalf("the tracks are %+v, want the one recording, not dashed", tracks)
	}
	if len(linePasses(tracks[0])) != 3 {
		t.Errorf("a track is drawn in %d strokes, want casing, colour and core", len(linePasses(tracks[0])))
	}
	if len(layer.ends) != 2 || layer.ends[0].finish || !layer.ends[1].finish ||
		layer.ends[1].point != tracks[0].points[len(tracks[0].points)-1] {
		t.Errorf("the track ends are %+v, want its start and then its finish", layer.ends)
	}
	if ends := tripLayer(report.Content).ends; len(ends) != 2 {
		t.Errorf("the trip's map marks %d track ends, want 2", len(ends))
	}
}

// TestArrowsAlongPointToTheFinish checks the arrows along a track are spaced a
// step apart from half a step in, point the way the path goes, and that a path
// too short for one gets none.
func TestArrowsAlongPointToTheFinish(t *testing.T) {
	// Right along the top for 60 mm, then down for 20.
	path := []paperPoint{{0, 0}, {60, 0}, {60, 20}}
	arrows := arrowsAlong(path, 25)
	if len(arrows) != 3 {
		t.Fatalf("got %d arrows, want 3: %+v", len(arrows), arrows)
	}
	want := []trackArrow{{x: 12.5, y: 0, angle: 0}, {x: 37.5, y: 0, angle: 0}, {x: 60, y: 2.5, angle: math.Pi / 2}}
	for index, arrow := range arrows {
		if math.Abs(arrow.x-want[index].x) > 1e-9 || math.Abs(arrow.y-want[index].y) > 1e-9 ||
			math.Abs(arrow.angle-want[index].angle) > 1e-9 {
			t.Errorf("arrow %d is %+v, want %+v", index, arrow, want[index])
		}
	}
	if short := arrowsAlong([]paperPoint{{0, 0}, {10, 0}}, 25); len(short) != 0 {
		t.Errorf("a 10 mm path got %d arrows, want none", len(short))
	}
}

// TestRenderWithMaps checks a report renders with its maps, with a background
// and without one, and that a background is a picture of the document.
func TestRenderWithMaps(t *testing.T) {
	report := placedReport(t)
	report.Photos, report.Cover = nil, nil
	report.MapAttribution = "© OpenStreetMap contributors"

	report.Maps = framed(t, MapRequests(report.Content), false)
	var plain bytes.Buffer
	if err := Render(&plain, report); err != nil {
		t.Fatalf("Render() without backgrounds returned an unexpected error: %v", err)
	}
	if bytes.Contains(plain.Bytes(), []byte("/Subtype /Image")) {
		t.Error("a map without a background put a picture in the document")
	}

	report.Maps = framed(t, MapRequests(report.Content), true)
	var pictured bytes.Buffer
	if err := Render(&pictured, report); err != nil {
		t.Fatalf("Render() with backgrounds returned an unexpected error: %v", err)
	}
	if images := bytes.Count(pictured.Bytes(), []byte("/Subtype /Image")); images != 2 {
		t.Errorf("the document holds %d pictures, want the two maps", images)
	}
}
