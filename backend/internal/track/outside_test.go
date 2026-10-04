package track

import (
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestOutsideDropsHiddenPointsAndBreaksTheLine checks the hidden points are
// gone, the line is broken where they were, and the rest reads back.
func TestOutsideDropsHiddenPointsAndBreaksTheLine(t *testing.T) {
	data := gpxOf([][2]float64{{64.0, -21.0}, {64.1, -21.0}, {64.2, -21.0}, {64.3, -21.0}, {64.4, -21.0}})
	hidden := func(point domain.Point) bool { return point.Lat < 64.05 || (point.Lat > 64.25 && point.Lat < 64.35) }

	out, changed, err := Outside(data, hidden)
	if err != nil || !changed {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	if strings.Count(string(out), "<trkseg>") != 2 {
		t.Errorf("want two segments around the hidden point:\n%s", out)
	}
	if !strings.Contains(string(out), "<ele>10</ele>") {
		t.Error("the heights were lost")
	}
	read, err := readPoints(out)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(read.points) != 3 {
		t.Fatalf("%d points, want 3", len(read.points))
	}
	for _, point := range read.points {
		if hidden(point) {
			t.Errorf("hidden point %v survived", point)
		}
	}
}

// TestOutsideLeavesAnUntouchedFile checks a file with nothing hidden is
// reported as one to send as it is, whichever format it was.
func TestOutsideLeavesAnUntouchedFile(t *testing.T) {
	never := func(domain.Point) bool { return false }
	for name, data := range map[string][]byte{
		"gpx": gpxOf([][2]float64{{64.0, -21.0}, {64.1, -21.0}}),
		"kml": kmlOf([][2]float64{{64.0, -21.0}, {64.1, -21.0}}),
	} {
		out, changed, err := Outside(data, never)
		if err != nil || changed || out != nil {
			t.Errorf("%s: changed %v, err %v", name, changed, err)
		}
	}
}
