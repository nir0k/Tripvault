package googlelink

import (
	"errors"
	"net/url"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// near reports whether two points are the same to six decimals.
func near(a, b domain.Point) bool {
	const epsilon = 1e-6
	return a.Lat-b.Lat < epsilon && b.Lat-a.Lat < epsilon && a.Lng-b.Lng < epsilon && b.Lng-a.Lng < epsilon
}

// TestParseReadsStopsAndDraggedPoints reads a link the way the site writes it
// for a route from Reykjavík to Vík dragged through two points: each stop's
// message holds its position, then the points the line leaving it passes.
func TestParseReadsStopsAndDraggedPoints(t *testing.T) {
	link := "https://www.google.com/maps/dir/Reykjav%C3%ADk/V%C3%ADk/@63.8,-20.4,8z/data=" +
		"!3m1!4b1!4m24!4m23" +
		"!1m15!1m1!1s0x48d674b9eedcedc3:0xec912ca230d26071!2m2!1d-21.9426354!2d64.146582" +
		"!3m4!1m2!1d-21.0!2d63.95!3s0x0:0x0!3m4!1m2!1d-20.3!2d63.8!3s0x0:0x0" +
		"!1m5!1m1!1s0x48d71c7b2e0bd0cf:0x9d7b3e0d0f0c1e1c!2m2!1d-19.0060!2d63.4186!3e0?entry=ttu"
	route, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []Point{
		{Point: domain.Point{Lat: 64.146582, Lng: -21.9426354}, Stop: true},
		{Point: domain.Point{Lat: 63.95, Lng: -21.0}},
		{Point: domain.Point{Lat: 63.8, Lng: -20.3}},
		{Point: domain.Point{Lat: 63.4186, Lng: -19.006}, Stop: true},
	}
	if !route.Labelled || len(route.Points) != len(want) {
		t.Fatalf("got %+v", route)
	}
	for index, point := range want {
		got := route.Points[index]
		if !near(got.Point, point.Point) || got.Stop != point.Stop {
			t.Errorf("point %d is %+v, want %+v", index, got, point)
		}
	}
}

// TestParseTakesATypedPositionForAStopWithoutOne checks a stop typed in as
// coordinates, whose message carries no position of its own.
func TestParseTakesATypedPositionForAStopWithoutOne(t *testing.T) {
	link := "https://www.google.de/maps/dir/64.1466,-21.9426/V%C3%ADk/data=" +
		"!4m12!4m11!1m3!3m2!1m0!3s0x0" +
		"!1m5!1m1!1s0x48d71c7b2e0bd0cf:0x0!2m2!1d-19.0060!2d63.4186!3e0"
	route, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(route.Points) != 2 || !near(route.Points[0].Point, domain.Point{Lat: 64.1466, Lng: -21.9426}) ||
		!route.Points[0].Stop {
		t.Errorf("got %+v", route.Points)
	}
}

// TestParseFallsBackToThePairs checks a data parameter whose structure is not
// the expected one is still read, pair by pair, with nothing labelled.
func TestParseFallsBackToThePairs(t *testing.T) {
	link := "https://www.google.com/maps/dir/A/B/data=!9m99!2m2!1d-21.5!2d64.0!7x1!1d-19.0!2d63.4"
	route, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if route.Labelled || len(route.Points) != 2 || !near(route.Points[1].Point, domain.Point{Lat: 63.4, Lng: -19.0}) {
		t.Errorf("got %+v", route)
	}
}

// TestParseReadsTheDocumentedFormat checks the api=1 link, whose stops are in
// the query.
func TestParseReadsTheDocumentedFormat(t *testing.T) {
	link := "https://www.google.com/maps/dir/?api=1&origin=64.1466,-21.9426&destination=63.4186,-19.006" +
		"&waypoints=" + url.QueryEscape("63.9,-20.5|Selfoss")
	route, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(route.Points) != 3 || !near(route.Points[1].Point, domain.Point{Lat: 63.9, Lng: -20.5}) {
		t.Errorf("got %+v", route.Points)
	}
}

// TestParseRefuses checks what is not a readable route link.
func TestParseRefuses(t *testing.T) {
	cases := map[string]error{
		"https://example.com/maps/dir/a/b":                      ErrNotALink,
		"https://www.google.com.evil.example/maps/dir/a/b":      ErrNotALink,
		"https://www.google.com/maps/place/Vik/@63.4,-19.0,12z": ErrNotALink,
		"https://maps.app.goo.gl/abcdef":                        ErrNotALink,
		"not a link at all":                                     ErrNotALink,
		"https://www.google.com/maps/dir/Reykjavik/Vik/":        ErrNoPoints,
	}
	for link, want := range cases {
		if _, err := Parse(link); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", link, err, want)
		}
	}
}

// TestIsGoogleHost checks the hosts a link may come from or lead to.
func TestIsGoogleHost(t *testing.T) {
	for host, want := range map[string]bool{
		"www.google.com": true, "google.de": true, "maps.google.co.uk": true, "consent.google.com": true,
		"maps.app.goo.gl": true, "goo.gl": true,
		"google.example.com.evil": false, "notgoogle.com": false, "example.com": false,
	} {
		if got := IsGoogleHost(host); got != want {
			t.Errorf("IsGoogleHost(%q) = %v, want %v", host, got, want)
		}
	}
}
