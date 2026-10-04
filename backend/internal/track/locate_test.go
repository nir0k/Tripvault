package track

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// climbingGPX builds a line due north climbing 10 % all the way: a point every
// 0.001° of latitude, about 111 m, each 11.1 m higher than the last.
func climbingGPX(count int) []byte {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><gpx version="1.1"><trk><trkseg>`)
	for index := range count {
		fmt.Fprintf(&body, `<trkpt lat="%f" lon="-19.5"><ele>%f</ele></trkpt>`,
			63.5+float64(index)*0.001, float64(index)*11.1)
	}
	body.WriteString(`</trkseg></trk></gpx>`)
	return []byte(body.String())
}

// TestLocatePutsAPointOnTheLine checks a point beside the line is moved onto
// it, measured along it from its start and given the slopes of the way there,
// and that a point past the end lies at the end.
func TestLocatePutsAPointOnTheLine(t *testing.T) {
	data := climbingGPX(21)
	whole, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Halfway between the 11th and the 12th point, some way off to the east.
	position, err := Locate(data, domain.Point{Lat: 63.5105, Lng: -19.499})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if math.Abs(position.Point.Lng+19.5) > 1e-9 || math.Abs(position.Point.Lat-63.5105) > 1e-6 {
		t.Errorf("point %+v is not on the line", position.Point)
	}
	if want := whole.DistanceM * 105 / 200; position.DistanceM < want-2 || position.DistanceM > want+2 {
		t.Errorf("distance %d, want about %d", position.DistanceM, want)
	}
	if len(position.Grades) != 2*MaxGrade+1 {
		t.Fatalf("grades: %v", position.Grades)
	}
	sum := 0
	for _, metres := range position.Grades {
		sum += metres
	}
	if math.Abs(float64(sum-position.DistanceM)) > 2 || position.Grades[MaxGrade+10] < sum*9/10 {
		t.Errorf("grades %v do not climb 10 %% over %d m", position.Grades, position.DistanceM)
	}

	end, err := Locate(data, domain.Point{Lat: 64, Lng: -19.5})
	if err != nil || end.DistanceM != whole.DistanceM {
		t.Errorf("past the end: %+v, %v; want %d m", end, err, whole.DistanceM)
	}

	// There and back the line passes a point twice; it is taken on the way out.
	back := gpxOf([][2]float64{{63.50, -19.5}, {63.51, -19.5}, {63.50, -19.5}})
	out, err := Locate(back, domain.Point{Lat: 63.502, Lng: -19.5})
	if err != nil || out.DistanceM > 300 {
		t.Errorf("there and back: %+v, %v; want the way out", out, err)
	}

	if _, err := Locate(gpxOf([][2]float64{{63.5, -19.5}}), domain.Point{}); err != ErrEmpty {
		t.Errorf("a single point gave %v, want ErrEmpty", err)
	}
}
