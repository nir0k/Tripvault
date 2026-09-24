package pdf

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/google/uuid"

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
