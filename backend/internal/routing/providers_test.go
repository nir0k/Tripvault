package routing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The three clients are tested against a server of their own rather than a real
// one: what matters is that each reads the shape its provider answers with, and
// turns a refusal into the right one of this package's errors.

// TestProfilesAreTranslatedForEachProvider checks a mode reaches every provider
// as the name that provider uses, and that transit follows the car.
func TestProfilesAreTranslatedForEachProvider(t *testing.T) {
	car, _ := Profile(domain.ModeCar)
	transit, _ := Profile(domain.ModeTransit)
	walk, _ := Profile(domain.ModeWalk)
	bike, _ := Profile(domain.ModeBike)

	if car != transit {
		t.Errorf("transit is routed as %q, want the car's %q", transit, car)
	}
	for name, profiles := range map[string]map[string]string{
		"ors":      orsProfiles,
		"osrm":     osrmProfiles,
		"valhalla": valhallaCosting,
	} {
		for _, profile := range []string{car, walk, bike} {
			if profiles[profile] == "" {
				t.Errorf("%s has no name for the %q profile", name, profile)
			}
		}
		if len(profiles) != 3 {
			t.Errorf("%s names %d profiles, want 3", name, len(profiles))
		}
	}
}

// TestOSRMRoute checks the request OSRM is asked and the answer it is read from.
func TestOSRMRoute(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		_, _ = io.WriteString(w, `{"code":"Ok","routes":[{"distance":186020.4,"duration":8112.6,
			"geometry":"_p~iF~ps|U_ulLnnqC"}]}`)
	}))
	defer server.Close()

	profile, _ := Profile(domain.ModeCar)
	routes, err := NewOSRM(server.URL).Route(context.Background(), Request{Profile: profile, Points: []domain.Point{reykjavik, vik}})
	if err != nil || len(routes) != 1 {
		t.Fatalf("Route() returned %d routes and %v", len(routes), err)
	}
	route := routes[0]
	if err != nil {
		t.Fatalf("Route() returned an unexpected error: %v", err)
	}
	if route.DistanceM != 186020 || route.DurationS != 8113 {
		t.Errorf("the route came back as %+v", route)
	}
	// The line is already in the five-decimal format this service stores.
	if route.Geometry != "_p~iF~ps|U_ulLnnqC" {
		t.Errorf("the line came back as %q", route.Geometry)
	}

	// Longitude first, the profile in the path, and a shape worth drawing.
	for _, want := range []string{"/route/v1/driving/", "-21.942600,64.146600;-19.006000,63.418600",
		"overview=full", "geometries=polyline", "alternatives=false"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the request %q does not contain %q", asked, want)
		}
	}
}

// TestOSRMRefusals checks each way a request can fail becomes the error that
// decides what the leg says.
func TestOSRMRefusals(t *testing.T) {
	cases := map[string]refusal{
		"no route":     {http.StatusOK, `{"code":"NoRoute"}`, ErrNoRoute},
		"no segment":   {http.StatusOK, `{"code":"NoSegment"}`, ErrNoRoute},
		"rate limited": {http.StatusTooManyRequests, ``, ErrRateLimited},
		"broken":       {http.StatusBadGateway, ``, ErrUnavailable},
		"empty answer": {http.StatusOK, `{"code":"Ok","routes":[]}`, ErrUnavailable},
		"not json":     {http.StatusOK, `<html>`, ErrUnavailable},
	}
	checkRefusals(t, cases, func(address string) Provider { return NewOSRM(address) })
}

// TestValhallaRoute checks the request Valhalla is sent, and that kilometres and
// its six-decimal line are converted to what this service stores.
func TestValhallaRoute(t *testing.T) {
	// The same three points as the polyline reference, encoded with six decimals.
	line := []domain.Point{{Lat: 38.5, Lng: -120.2}, {Lat: 40.7, Lng: -120.95}, {Lat: 43.252, Lng: -126.453}}
	shape := encodeWithPrecision(line, 6)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"trip":{"status":0,"summary":{"length":186.0204,"time":8112.6},
			"legs":[{"shape":"`+shape+`"}]}}`)
	}))
	defer server.Close()

	profile, _ := Profile(domain.ModeWalk)
	routes, err := NewValhalla(server.URL).Route(context.Background(), Request{Profile: profile, Points: []domain.Point{reykjavik, vik}})
	if err != nil || len(routes) != 1 {
		t.Fatalf("Route() returned %d routes and %v", len(routes), err)
	}
	route := routes[0]
	if err != nil {
		t.Fatalf("Route() returned an unexpected error: %v", err)
	}
	if route.DistanceM != 186020 || route.DurationS != 8113 {
		t.Errorf("the route came back as %+v", route)
	}
	// Written again with five decimals, which is the reference encoding.
	if route.Geometry != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Errorf("the line came back as %q", route.Geometry)
	}

	if body["costing"] != "pedestrian" {
		t.Errorf("the request asked for costing %v", body["costing"])
	}
	if body["units"] != "kilometers" {
		t.Errorf("the request asked for units %v", body["units"])
	}
	locations, _ := body["locations"].([]any)
	if len(locations) != 2 {
		t.Fatalf("the request carried %d locations", len(locations))
	}
	first, _ := locations[0].(map[string]any)
	if math.Abs(first["lat"].(float64)-reykjavik.Lat) > 1e-9 || math.Abs(first["lon"].(float64)-reykjavik.Lng) > 1e-9 {
		t.Errorf("the first location is %v", first)
	}
}

// TestValhallaRefusals checks its refusals are read from the codes it answers
// with rather than from the status alone.
func TestValhallaRefusals(t *testing.T) {
	cases := map[string]refusal{
		"no path":       {http.StatusBadRequest, `{"error_code":442,"error":"No path could be found"}`, ErrNoRoute},
		"cannot snap":   {http.StatusBadRequest, `{"error_code":171,"error":"No suitable edges near location"}`, ErrNoRoute},
		"rate limited":  {http.StatusTooManyRequests, ``, ErrRateLimited},
		"broken":        {http.StatusBadGateway, ``, ErrUnavailable},
		"no legs":       {http.StatusOK, `{"trip":{"status":0,"legs":[]}}`, ErrUnavailable},
		"failed inside": {http.StatusOK, `{"trip":{"status":1,"status_message":"bad"}}`, ErrUnavailable},
	}
	checkRefusals(t, cases, func(address string) Provider { return NewValhalla(address) })
}

// TestOSRMThroughPointsAndAlternatives checks that via points go into the path
// in order, and that alternatives are asked for only between two points.
func TestOSRMThroughPointsAndAlternatives(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.String()
		_, _ = io.WriteString(w, `{"code":"Ok","routes":[{"distance":1,"duration":1,"geometry":"a"},
			{"distance":2,"duration":2,"geometry":"b"}]}`)
	}))
	defer server.Close()
	osrm := NewOSRM(server.URL)
	via := domain.Point{Lat: 63.9, Lng: -20.5}

	if _, err := osrm.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, via, vik}, Alternatives: 3}); err != nil {
		t.Fatalf("route through a point: %v", err)
	}
	if !strings.Contains(asked, "-21.942600,64.146600;-20.500000,63.900000;-19.006000,63.418600") ||
		!strings.Contains(asked, "alternatives=false") {
		t.Errorf("a route through a point asked %q", asked)
	}

	routes, err := osrm.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, vik}, Alternatives: 3})
	if err != nil || len(routes) != 2 || routes[1].DistanceM != 2 {
		t.Errorf("alternatives came back as %+v %v", routes, err)
	}
	if !strings.Contains(asked, "alternatives=2") {
		t.Errorf("alternatives were asked as %q", asked)
	}
}

// TestValhallaPreferenceThroughPointsAndAlternates checks the shortest route,
// via locations and alternates are asked for the way Valhalla takes them, and
// that the lines of several legs become one.
func TestValhallaPreferenceThroughPointsAndAlternates(t *testing.T) {
	first := encodeWithPrecision([]domain.Point{{Lat: 1, Lng: 1}, {Lat: 2, Lng: 2}}, 6)
	second := encodeWithPrecision([]domain.Point{{Lat: 2, Lng: 2}, {Lat: 3, Lng: 3}}, 6)
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = nil
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"trip":{"status":0,"summary":{"length":1,"time":60},
			"legs":[{"shape":"`+first+`"},{"shape":"`+second+`"}]},
			"alternates":[{"trip":{"status":0,"summary":{"length":2,"time":90},"legs":[{"shape":"`+first+`"}]}}]}`)
	}))
	defer server.Close()
	valhalla := NewValhalla(server.URL)
	via := domain.Point{Lat: 63.9, Lng: -20.5}

	routes, err := valhalla.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, via, vik}, Preference: domain.RouteShortest, Alternatives: 3})
	if err != nil || len(routes) != 2 {
		t.Fatalf("route came back as %+v %v", routes, err)
	}
	if line := domain.DecodePolyline(routes[0].Geometry, 5); len(line) != 3 {
		t.Errorf("the legs were joined into %d points, want 3", len(line))
	}
	locations, _ := body["locations"].([]any)
	middle, _ := locations[1].(map[string]any)
	if len(locations) != 3 || middle["type"] != "via" {
		t.Errorf("the locations were %v", locations)
	}
	options, _ := body["costing_options"].(map[string]any)
	auto, _ := options["auto"].(map[string]any)
	if auto["shortest"] != true {
		t.Errorf("the shortest route was asked as %v", body["costing_options"])
	}
	if _, asked := body["alternates"]; asked {
		t.Error("alternates were asked for a route through a point")
	}

	if _, err := valhalla.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, vik}, Alternatives: 3}); err != nil {
		t.Fatalf("route: %v", err)
	}
	if body["alternates"] != float64(2) || body["costing_options"] != nil {
		t.Errorf("a fastest route with alternatives asked %v and %v", body["alternates"], body["costing_options"])
	}
}

// refusal is one answer a provider can give and the error it must become.
type refusal struct {
	status int
	body   string
	want   error
}

// checkRefusals serves each answer to a provider built against a test server
// and checks the error the route request comes back with.
func checkRefusals(t *testing.T, cases map[string]refusal, provider func(address string) Provider) {
	t.Helper()
	profile, _ := Profile(domain.ModeCar)
	for name, test := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(test.status)
			_, _ = io.WriteString(w, test.body)
		}))
		_, err := provider(server.URL).Route(context.Background(), Request{Profile: profile, Points: []domain.Point{reykjavik, vik}})
		server.Close()

		if !errors.Is(err, test.want) {
			t.Errorf("%s: error = %v, want %v", name, err, test.want)
		}
	}
}

// TestPolylineRoundTrip checks the decoder reads back what the encoder wrote, at
// both precisions, and that a truncated line is read as far as it goes rather
// than failing.
func TestPolylineRoundTrip(t *testing.T) {
	line := []domain.Point{{Lat: 38.5, Lng: -120.2}, {Lat: 40.7, Lng: -120.95}, {Lat: 43.252, Lng: -126.453}}

	for _, precision := range []int{5, 6} {
		decoded := domain.DecodePolyline(encodeWithPrecision(line, precision), precision)
		if len(decoded) != len(line) {
			t.Fatalf("precision %d: %d points, want %d", precision, len(decoded), len(line))
		}
		for i, point := range decoded {
			if math.Abs(point.Lat-line[i].Lat) > 1e-5 || math.Abs(point.Lng-line[i].Lng) > 1e-5 {
				t.Errorf("precision %d: point %d came back as %+v, want %+v", precision, i, point, line[i])
			}
		}
	}

	if points := domain.DecodePolyline("", 5); len(points) != 0 {
		t.Errorf("an empty line decoded to %d points", len(points))
	}
	// Half a number at the end: what is readable is kept, the rest is dropped.
	if points := domain.DecodePolyline("_p~iF~ps|U_ulL", 5); len(points) != 1 {
		t.Errorf("a truncated line decoded to %d points, want 1", len(points))
	}
}

// encodeWithPrecision encodes a line the way a provider of a given precision
// would, which is how the six-decimal case is produced for these tests.
func encodeWithPrecision(points []domain.Point, precision int) string {
	if precision == 5 {
		return domain.EncodePolyline(points)
	}
	scaled := make([]domain.Point, 0, len(points))
	for _, point := range points {
		scaled = append(scaled, domain.Point{Lat: point.Lat * 10, Lng: point.Lng * 10})
	}
	return domain.EncodePolyline(scaled)
}
