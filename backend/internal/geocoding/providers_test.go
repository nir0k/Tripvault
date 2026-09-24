package geocoding

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The two clients people run themselves are tested against a server of their
// own: what matters is that each reads the shape its provider answers with, and
// presents it as the same place the rest of the service expects.

// reykjavikPoint is the focus and the reverse lookup these tests use.
var reykjavikPoint = domain.Point{Lat: 64.1466, Lng: -21.9426}

// TestPhotonSearch checks a Photon answer becomes a place with a line a person
// can read, which Photon itself does not provide.
func TestPhotonSearch(t *testing.T) {
	var asked url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query()
		_, _ = io.WriteString(w, `{"features":[
			{"geometry":{"coordinates":[-21.9333,64.1475]},
			 "properties":{"name":"Hallgrímskirkja","street":"Hallgrímstorg","housenumber":"1",
			  "postcode":"101","city":"Reykjavík","state":"Capital Region","country":"Iceland",
			  "osm_key":"amenity","osm_value":"place_of_worship","osm_type":"W","osm_id":24819820}},
			{"geometry":{"coordinates":[-21.94,64.14]},
			 "properties":{"street":"Laugavegur","housenumber":"12","city":"Reykjavík",
			  "country":"Iceland","osm_key":"building","osm_value":"yes","osm_type":"N","osm_id":7}},
			{"geometry":{"coordinates":[]},"properties":{"name":"Nowhere"}}]}`)
	}))
	defer server.Close()

	places, err := NewPhoton(server.URL).Search(context.Background(), Query{
		Text: "hallgrim", Lang: "en", Focus: &reykjavikPoint,
	})
	if err != nil {
		t.Fatalf("Search() returned an unexpected error: %v", err)
	}
	// The third result has no coordinates, so it is not a place.
	if len(places) != 2 {
		t.Fatalf("got %d places, want 2: %+v", len(places), places)
	}

	first := places[0]
	if first.Name != "Hallgrímskirkja" || first.Lat != 64.1475 || first.Lng != -21.9333 {
		t.Errorf("the first place is %+v", first)
	}
	if first.Label != "Hallgrímskirkja, Hallgrímstorg, 1, 101, Reykjavík, Capital Region, Iceland" {
		t.Errorf("the line a person reads is %q", first.Label)
	}
	if first.Layer != "venue" || first.Country != "Iceland" || first.Ref != "openstreetmap:way/24819820" {
		t.Errorf("the first place describes itself as %+v", first)
	}

	// A result with no name of its own is known by its address.
	if places[1].Name != "Laugavegur 12" || places[1].Layer != "address" {
		t.Errorf("the second place is %+v", places[1])
	}

	if asked.Get("q") != "hallgrim" || asked.Get("lang") != "en" {
		t.Errorf("the request asked %v", asked)
	}
	if asked.Get("lat") == "" || asked.Get("lon") == "" {
		t.Errorf("the focus did not reach the provider: %v", asked)
	}
}

// TestPhotonReverseAndRefusals checks the reverse lookup and the two failures
// that mean different things to the service above.
func TestPhotonReverseAndRefusals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/reverse") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"features":[{"geometry":{"coordinates":[-21.9426,64.1466]},
			"properties":{"name":"Reykjavík","osm_key":"place","osm_value":"city","country":"Iceland"}}]}`)
	}))
	defer server.Close()

	places, err := NewPhoton(server.URL).Reverse(context.Background(), reykjavikPoint, "ru")
	if err != nil {
		t.Fatalf("Reverse() returned an unexpected error: %v", err)
	}
	if len(places) != 1 || places[0].Layer != "locality" {
		t.Fatalf("the reverse lookup returned %+v", places)
	}

	for name, test := range map[string]struct {
		status int
		want   error
	}{
		"rate limited": {http.StatusTooManyRequests, ErrRateLimited},
		"broken":       {http.StatusInternalServerError, ErrUnavailable},
	} {
		refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(test.status)
		}))
		_, err := NewPhoton(refusing.URL).Search(context.Background(), Query{Text: "x"})
		refusing.Close()

		if !errors.Is(err, test.want) {
			t.Errorf("%s: error = %v, want %v", name, err, test.want)
		}
	}
}

// TestNominatimSearch checks its answer is read, its usage policy is met, and a
// focus becomes the viewbox that is the nearest thing it offers.
func TestNominatimSearch(t *testing.T) {
	var asked url.Values
	var agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked, agent = r.URL.Query(), r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `[
			{"lat":"64.1475","lon":"-21.9333","name":"Hallgrímskirkja",
			 "display_name":"Hallgrímskirkja, Hallgrímstorg, Reykjavík, Iceland",
			 "category":"amenity","type":"place_of_worship","addresstype":"amenity",
			 "osm_type":"way","osm_id":24819820,"address":{"country":"Iceland"}},
			{"lat":"bad","lon":"-21.9","display_name":"Unreadable"}]`)
	}))
	defer server.Close()

	places, err := NewNominatim(server.URL, "tripvault/test").Search(context.Background(), Query{
		Text: "hallgrim", Lang: "en", Focus: &reykjavikPoint,
	})
	if err != nil {
		t.Fatalf("Search() returned an unexpected error: %v", err)
	}
	// The second result has coordinates that cannot be read, so it is dropped.
	if len(places) != 1 {
		t.Fatalf("got %d places, want 1: %+v", len(places), places)
	}
	place := places[0]
	if place.Name != "Hallgrímskirkja" || place.Lat != 64.1475 || place.Lng != -21.9333 {
		t.Errorf("the place is %+v", place)
	}
	if place.Label != "Hallgrímskirkja, Hallgrímstorg, Reykjavík, Iceland" || place.Country != "Iceland" {
		t.Errorf("the place describes itself as %+v", place)
	}
	if place.Ref != "openstreetmap:way/24819820" {
		t.Errorf("the reference is %q", place.Ref)
	}

	if agent != "tripvault/test" {
		t.Errorf("the request identified itself as %q, which the usage policy forbids", agent)
	}
	if asked.Get("format") != "jsonv2" || asked.Get("accept-language") != "en" {
		t.Errorf("the request asked %v", asked)
	}
	if asked.Get("viewbox") == "" {
		t.Error("the focus did not become a viewbox")
	}
}

// TestNominatimReverse checks the one place it answers a reverse lookup with,
// and the sea, which it answers with an error object rather than a place.
func TestNominatimReverse(t *testing.T) {
	answers := `{"lat":"64.1466","lon":"-21.9426","name":"","display_name":"Reykjavík, Iceland",
		"addresstype":"city","osm_type":"relation","osm_id":2580605,"address":{"country":"Iceland"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, answers)
	}))
	defer server.Close()

	client := NewNominatim(server.URL, "tripvault/test")
	places, err := client.Reverse(context.Background(), reykjavikPoint, "en")
	if err != nil {
		t.Fatalf("Reverse() returned an unexpected error: %v", err)
	}
	if len(places) != 1 || places[0].Layer != "locality" {
		t.Fatalf("the reverse lookup returned %+v", places)
	}
	// With no name of its own, the place is known by the head of its line.
	if places[0].Name != "Reykjavík" {
		t.Errorf("the place is called %q", places[0].Name)
	}

	answers = `{"error":"Unable to geocode"}`
	places, err = client.Reverse(context.Background(), domain.Point{Lat: 0, Lng: 0}, "en")
	if err != nil || len(places) != 0 {
		t.Errorf("a point in the sea returned %+v, %v", places, err)
	}
}
