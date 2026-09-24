package track

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// gpxOf builds a GPX file out of the given points.
func gpxOf(points [][2]float64) []byte {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><gpx version="1.1"><trk><trkseg>`)
	for _, point := range points {
		fmt.Fprintf(&body, `<trkpt lat="%f" lon="%f"><ele>10</ele></trkpt>`, point[0], point[1])
	}
	body.WriteString(`</trkseg></trk></gpx>`)
	return []byte(body.String())
}

// kmlOf builds a KML file out of the given points, in that format's own order.
func kmlOf(points [][2]float64) []byte {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><kml><Document><Placemark><LineString><coordinates>`)
	for _, point := range points {
		fmt.Fprintf(&body, "%f,%f,0 ", point[1], point[0])
	}
	body.WriteString(`</coordinates></LineString></Placemark></Document></kml>`)
	return []byte(body.String())
}

// TestParseReadsBothFormats checks a line comes back the same whichever of the
// two formats carried it, including the reversed coordinate order of KML.
func TestParseReadsBothFormats(t *testing.T) {
	// Roughly Skógafoss to Sólheimasandur: about eight and a half kilometres.
	points := [][2]float64{{63.532, -19.511}, {63.500, -19.400}, {63.491, -19.364}}
	for name, data := range map[string][]byte{"gpx": gpxOf(points), "kml": kmlOf(points)} {
		parsed, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if parsed.PointCount != 3 {
			t.Errorf("%s: read %d points, want 3", name, parsed.PointCount)
		}
		if parsed.DistanceM < 8000 || parsed.DistanceM > 9500 {
			t.Errorf("%s: measured %d m, want about 8.6 km", name, parsed.DistanceM)
		}
		if parsed.Geometry == "" {
			t.Errorf("%s: the line came back empty", name)
		}
		if math.Abs(parsed.Start.Lat-63.532) > 1e-6 || math.Abs(parsed.Start.Lng+19.511) > 1e-6 {
			t.Errorf("%s: started at %+v, want the first point", name, parsed.Start)
		}
	}
}

// TestParseThinsLongTracksWithoutLosingLength checks a recording of tens of
// thousands of points is stored as a line a map can draw, while its length is
// still measured over every point.
func TestParseThinsLongTracksWithoutLosingLength(t *testing.T) {
	points := make([][2]float64, 0, 40_000)
	for index := range 40_000 {
		// A gentle curve, so thinning has something to keep.
		lat := 63.5 + float64(index)*0.00002
		lng := -19.5 + math.Sin(float64(index)/500)*0.01
		points = append(points, [2]float64{lat, lng})
	}
	parsed, err := Parse(gpxOf(points))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.PointCount != 40_000 {
		t.Errorf("read %d points, want 40000", parsed.PointCount)
	}
	drawn := strings.Count(parsed.Geometry, "") // a rough size check on the stored line
	if drawn > 30_000 {
		t.Errorf("the stored line is %d bytes, which is more than a map should be given", drawn)
	}
	if parsed.DistanceM < 80_000 {
		t.Errorf("measured %d m over 40000 points, which is too little", parsed.DistanceM)
	}
}

// TestParseRefusesWhatIsNotATrack covers the three refusals.
func TestParseRefusesWhatIsNotATrack(t *testing.T) {
	if _, err := Parse([]byte(`<html><body>hello</body></html>`)); err != ErrUnsupported {
		t.Errorf("an HTML page gave %v, want ErrUnsupported", err)
	}
	if _, err := Parse(gpxOf([][2]float64{{63.5, -19.5}})); err != ErrEmpty {
		t.Errorf("a single point gave %v, want ErrEmpty", err)
	}
	if _, err := Parse([]byte(`<gpx><trk><trkseg><trkpt lat="200" lon="0"/></trkseg></trk></gpx>`)); err != ErrEmpty {
		t.Errorf("an impossible coordinate gave %v, want ErrEmpty", err)
	}
}

// TestParseMeasuresClimb checks the height gained and lost is summed over a real
// slope while the wandering of a GPS height is left out, and that a file with
// no heights reports none rather than a flat zero.
func TestParseMeasuresClimb(t *testing.T) {
	var body strings.Builder
	body.WriteString(`<gpx><trk><trkseg>`)
	// Up 100 m in steps of 10 with two metres of noise on every point, then
	// down 60 m: the noise alone never reaches the threshold.
	heights := []float64{100, 102, 110, 108, 120, 122, 130, 128, 140, 142, 150, 148, 160, 162, 170, 168,
		180, 182, 190, 188, 200, 190, 180, 170, 160, 150, 140}
	for index, height := range heights {
		fmt.Fprintf(&body, `<trkpt lat="%f" lon="-19.5"><ele>%.1f</ele></trkpt>`, 63.5+float64(index)*0.001, height)
	}
	body.WriteString(`</trkseg></trk></gpx>`)
	parsed, err := Parse([]byte(body.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Format != FormatGPX {
		t.Errorf("format %q, want gpx", parsed.Format)
	}
	if parsed.AscentM == nil || parsed.DescentM == nil {
		t.Fatal("the heights of the file were not read")
	}
	if *parsed.AscentM < 95 || *parsed.AscentM > 105 {
		t.Errorf("climbed %d m, want about 100", *parsed.AscentM)
	}
	if *parsed.DescentM < 55 || *parsed.DescentM > 65 {
		t.Errorf("descended %d m, want about 60", *parsed.DescentM)
	}

	bare := []byte(`<gpx><trk><trkseg><trkpt lat="63.5" lon="-19.5"/><trkpt lat="63.6" lon="-19.5"/></trkseg></trk></gpx>`)
	if parsed, err := Parse(bare); err != nil || parsed.AscentM != nil || parsed.DescentM != nil {
		t.Errorf("a file without heights: %+v %v", parsed, err)
	}
	// A KML line drawn on the ground carries zeros, which are not heights.
	if parsed, err := Parse(kmlOf([][2]float64{{63.5, -19.5}, {63.6, -19.5}})); err != nil ||
		parsed.AscentM != nil || parsed.Format != FormatKML {
		t.Errorf("a KML line with zero heights: %+v %v", parsed, err)
	}
	kml := []byte(`<kml><Placemark><LineString><coordinates>-19.5,63.5,10 -19.5,63.6,40 -19.5,63.7,25</coordinates></LineString></Placemark></kml>`)
	if parsed, err := Parse(kml); err != nil || parsed.AscentM == nil || *parsed.AscentM != 30 || *parsed.DescentM != 15 {
		t.Errorf("a KML line with heights: %+v %v", parsed, err)
	}
}

// TestParseReadsThePeriod checks the first and last moment of a recording are
// read from its points - never from the time the file itself was written - and
// that a file without timestamps reports no period at all.
func TestParseReadsThePeriod(t *testing.T) {
	gpx := []byte(`<gpx><metadata><time>2026-01-01T00:00:00Z</time></metadata><trk><trkseg>
		<trkpt lat="63.5" lon="-19.5"><time>2025-07-02T09:15:00Z</time></trkpt>
		<trkpt lat="63.6" lon="-19.5"><time>2025-07-02T10:00:00+01:00</time></trkpt>
		<trkpt lat="63.7" lon="-19.5"><time>2025-07-02T13:40:30Z</time></trkpt>
	</trkseg></trk></gpx>`)
	parsed, err := Parse(gpx)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.StartedAt == nil || parsed.EndedAt == nil {
		t.Fatal("the period of the recording was not read")
	}
	if got := parsed.StartedAt.Format(time.RFC3339); got != "2025-07-02T09:00:00Z" {
		t.Errorf("started at %s, want the earliest point in UTC", got)
	}
	if got := parsed.EndedAt.Format(time.RFC3339); got != "2025-07-02T13:40:30Z" {
		t.Errorf("ended at %s", got)
	}

	kml := []byte(`<kml><Placemark><TimeSpan><begin>2025-07-03T08:00:00Z</begin><end>2025-07-03T11:30:00Z</end></TimeSpan>
		<LineString><coordinates>-19.5,63.5 -19.5,63.6</coordinates></LineString></Placemark></kml>`)
	if parsed, err := Parse(kml); err != nil || parsed.StartedAt == nil ||
		parsed.StartedAt.Hour() != 8 || parsed.EndedAt.Hour() != 11 {
		t.Errorf("a KML time span: %+v %v", parsed, err)
	}

	if parsed, err := Parse(gpxOf([][2]float64{{63.5, -19.5}, {63.6, -19.5}})); err != nil || parsed.StartedAt != nil {
		t.Errorf("a file without timestamps: %+v %v", parsed, err)
	}
}
