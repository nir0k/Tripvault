package routing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

var (
	reykjavik = domain.Point{Lat: 64.1466, Lng: -21.9426}
	vik       = domain.Point{Lat: 63.4186, Lng: -19.0060}
)

// TestGeometry checks the distance formula and the polyline encoding against
// known values.
func TestGeometry(t *testing.T) {
	if got := Haversine(reykjavik, vik) / 1000; math.Abs(got-165.5) > 1 {
		t.Errorf("Reykjavik–Vík: %.1f km", got)
	}
	// The reference example of the polyline format.
	line := []domain.Point{{Lat: 38.5, Lng: -120.2}, {Lat: 40.7, Lng: -120.95}, {Lat: 43.252, Lng: -126.453}}
	if got := domain.EncodePolyline(line); got != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Errorf("polyline: %s", got)
	}
}

// TestStraightLinesAndEstimates checks flights, cable cars, "other" legs and
// estimates.
func TestStraightLinesAndEstimates(t *testing.T) {
	flight := StraightLine(domain.ModeFlight, reykjavik, vik)
	if flight.Source != domain.LegStraightLine || flight.DurationS == nil || *flight.DurationS < 30*60 {
		t.Errorf("flight: %+v", flight)
	}
	// A three-kilometre gondola: twelve minutes along the cable and five at
	// the stations.
	top := domain.Point{Lat: reykjavik.Lat + 3.0/111.195, Lng: reykjavik.Lng}
	gondola := StraightLine(domain.ModeCableCar, reykjavik, top)
	if gondola.Source != domain.LegStraightLine || gondola.DurationS == nil ||
		math.Abs(float64(*gondola.DurationS)-17*60) > 5 {
		t.Errorf("cable car: %+v", gondola)
	}
	if _, routed := Profile(domain.ModeCableCar); routed {
		t.Error("a cable car is routed on roads")
	}
	if other := StraightLine(domain.ModeOther, reykjavik, vik); other.DurationS != nil {
		t.Errorf("an other leg got a time: %+v", other)
	}
	walk := Estimate(domain.ModeWalk, reykjavik, vik, domain.LegErrorNoRoute)
	if walk.Source != domain.LegEstimate || walk.Error != domain.LegErrorNoRoute ||
		math.Abs(float64(*walk.DistanceM)-Haversine(reykjavik, vik)*1.2) > 1 {
		t.Errorf("walk estimate: %+v", walk)
	}
	if _, routed := Profile(domain.ModeTransit); !routed {
		t.Error("transit is not routed on roads")
	}
}

// fakeProvider answers with a fixed route or error and counts calls.
type fakeProvider struct {
	err   error
	calls int
	last  Request
}

// Name identifies the fake.
func (f *fakeProvider) Name() string { return "fake" }

// Capabilities says the fake can do everything.
func (f *fakeProvider) Capabilities() Capabilities {
	return Capabilities{Shortest: true, Alternatives: true}
}

// Route returns the fixed answer, as many routes as were asked for.
func (f *fakeProvider) Route(_ context.Context, request Request) ([]Route, error) {
	f.calls++
	f.last = request
	if f.err != nil {
		return nil, f.err
	}
	routes := []Route{{DistanceM: 186000, DurationS: 8000, Geometry: "abc"}}
	for index := 1; index < request.Alternatives; index++ {
		routes = append(routes, Route{DistanceM: 186000 + index, DurationS: 8000 + index, Geometry: "alt"})
	}
	return routes, nil
}

// memoryStore is an in-memory cache and usage counter.
type memoryStore struct {
	mu       sync.Mutex
	routes   map[string]Route
	requests int
}

// Get returns a cached route.
func (m *memoryStore) Get(_ context.Context, key []byte, _ time.Time) (Route, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	route, ok := m.routes[string(key)]
	return route, ok, nil
}

// Put stores a route.
func (m *memoryStore) Put(_ context.Context, key []byte, _, _ string, route Route, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes[string(key)] = route
	return nil
}

// Delete forgets a route.
func (m *memoryStore) Delete(_ context.Context, key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routes, string(key))
	return nil
}

// Requests24h counts recorded requests.
func (m *memoryStore) Requests24h(context.Context, time.Time) (int, error) { return m.requests, nil }

// Record counts a request.
func (m *memoryStore) Record(context.Context, time.Time, bool) error { m.requests++; return nil }

// newTestService builds a service over a fake provider and memory stores.
func newTestService(provider Provider, opts Options) (*Service, *memoryStore) {
	store := &memoryStore{routes: map[string]Route{}}
	return NewService(provider, store, store, opts, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

// TestServiceCalculate checks caching, limits, fallbacks and forgetting.
func TestServiceCalculate(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{}
	service, store := newTestService(provider, Options{PerMinute: 2, Daily: 100, CacheTTL: time.Hour})

	first := service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	second := service.Calculate(ctx, domain.ModeTransit, &reykjavik, &vik, domain.LegRoute{})
	if first.Source != domain.LegProvider || second.Source != domain.LegProvider || provider.calls != 1 {
		t.Errorf("transit should reuse the car route from the cache: %+v %+v calls=%d", first, second, provider.calls)
	}

	service.Calculate(ctx, domain.ModeWalk, &reykjavik, &vik, domain.LegRoute{})
	limited := service.Calculate(ctx, domain.ModeBike, &reykjavik, &vik, domain.LegRoute{})
	if limited.Source != domain.LegEstimate || limited.Error != domain.LegErrorRateLimited {
		t.Errorf("per-minute limit: %+v", limited)
	}

	if err := service.Forget(ctx, domain.ModeCar, reykjavik, vik, domain.LegRoute{}); err != nil || len(store.routes) != 1 {
		t.Errorf("forget: %v, %d cached", err, len(store.routes))
	}

	if got := service.Calculate(ctx, domain.ModeCar, nil, &vik, domain.LegRoute{}); got.Source != domain.LegMissingCoordinates {
		t.Errorf("missing point: %+v", got)
	}

	daily, dailyStore := newTestService(&fakeProvider{}, Options{Daily: 1})
	dailyStore.requests = 1
	if got := daily.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{}); got.Error != domain.LegErrorDailyLimit {
		t.Errorf("daily limit: %+v", got)
	}

	failing, _ := newTestService(&fakeProvider{err: ErrNoRoute}, Options{})
	if got := failing.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{}); got.Error != domain.LegErrorNoRoute {
		t.Errorf("no route: %+v", got)
	}

	disabled, _ := newTestService(nil, Options{})
	if got := disabled.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{}); got.Error != domain.LegErrorProviderDisabled || disabled.Enabled() {
		t.Errorf("no provider: %+v", got)
	}
}

// TestServiceRoutesAsTheLegAsks checks that a leg's preference and via points
// reach the provider and keep routes apart in the cache, and that forgetting
// forgets the route the leg asked for.
func TestServiceRoutesAsTheLegAsks(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{}
	service, store := newTestService(provider, Options{CacheTTL: time.Hour})
	via := domain.Point{Lat: 63.9, Lng: -20.5}
	shortest := domain.LegRoute{Preference: domain.RouteShortest}
	through := domain.LegRoute{Via: []domain.Point{via}}

	service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, shortest)
	if provider.last.Preference != domain.RouteShortest || provider.calls != 2 {
		t.Errorf("the shortest route was asked as %+v after %d calls", provider.last, provider.calls)
	}
	service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, through)
	if len(provider.last.Points) != 3 || provider.last.Points[1] != via || provider.last.Preference != domain.RouteFastest {
		t.Errorf("a route through a point was asked as %+v", provider.last)
	}
	if len(store.routes) != 3 {
		t.Errorf("three ways of routing one leg share %d cache entries", len(store.routes))
	}
	if err := service.Forget(ctx, domain.ModeCar, reykjavik, vik, through); err != nil || len(store.routes) != 2 {
		t.Errorf("forget: %v, %d cached", err, len(store.routes))
	}
}

// TestServiceAlternatives checks the routes a leg is offered and the refusals.
func TestServiceAlternatives(t *testing.T) {
	ctx := context.Background()
	provider := &fakeProvider{}
	service, store := newTestService(provider, Options{CacheTTL: time.Hour})

	results, err := service.Alternatives(ctx, domain.ModeCar, reykjavik, vik, domain.RouteShortest)
	if err != nil || len(results) != alternativeCount || results[1].Source != domain.LegProvider {
		t.Fatalf("alternatives: %+v %v", results, err)
	}
	if provider.last.Alternatives != alternativeCount || provider.last.Preference != domain.RouteShortest ||
		len(store.routes) != 0 || store.requests != 1 {
		t.Errorf("asked %+v, cached %d, counted %d", provider.last, len(store.routes), store.requests)
	}
	if _, err := service.Alternatives(ctx, domain.ModeFlight, reykjavik, vik, ""); !errors.Is(err, ErrNotRouted) {
		t.Errorf("a flight: %v", err)
	}
	disabled, _ := newTestService(nil, Options{})
	if _, err := disabled.Alternatives(ctx, domain.ModeCar, reykjavik, vik, ""); !errors.Is(err, ErrDisabled) {
		t.Errorf("no provider: %v", err)
	}
	limited, limitedStore := newTestService(&fakeProvider{}, Options{Daily: 1})
	limitedStore.requests = 1
	if _, err := limited.Alternatives(ctx, domain.ModeCar, reykjavik, vik, ""); !errors.Is(err, ErrDailyLimit) {
		t.Errorf("daily limit: %v", err)
	}
}

// TestORSClient checks the request the client sends and how answers map.
func TestORSClient(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	status, answer := http.StatusOK, `{"routes":[{"summary":{"distance":186020.4,"duration":8040.6},"geometry":"_p~iF"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	defer server.Close()

	client := NewORS(server.URL+"/openrouteservice/", "secret-key")
	routes, err := client.Route(context.Background(), Request{Profile: profileCar, Points: []domain.Point{reykjavik, vik}})
	if err != nil || len(routes) != 1 || routes[0].DistanceM != 186020 || routes[0].DurationS != 8041 ||
		routes[0].Geometry != "_p~iF" {
		t.Errorf("route: %+v %v", routes, err)
	}
	// The fastest route is asked for by name: openrouteservice's own default is
	// a judgement of its own that often leaves the main road.
	if gotAuth != "secret-key" || gotPath != "/openrouteservice/v2/directions/driving-car" ||
		!strings.Contains(gotBody, `"coordinates":[[-21.9426,64.1466],[-19.006,63.4186]]`) ||
		!strings.Contains(gotBody, `"radiuses":[5000,5000]`) || !strings.Contains(gotBody, `"preference":"fastest"`) ||
		strings.Contains(gotBody, "alternative_routes") {
		t.Errorf("request: auth=%q path=%q body=%s", gotAuth, gotPath, gotBody)
	}

	via := domain.Point{Lat: 63.9, Lng: -20.5}
	if _, err := client.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, via, vik}, Preference: domain.RouteShortest, Alternatives: 3}); err != nil {
		t.Fatalf("route through a point: %v", err)
	}
	if !strings.Contains(gotBody, `"coordinates":[[-21.9426,64.1466],[-20.5,63.9],[-19.006,63.4186]]`) ||
		!strings.Contains(gotBody, `"radiuses":[5000,5000,5000]`) || !strings.Contains(gotBody, `"preference":"shortest"`) ||
		strings.Contains(gotBody, "alternative_routes") {
		t.Errorf("a shortest route through a point asked %s", gotBody)
	}

	if _, err := client.Route(context.Background(), Request{Profile: profileCar,
		Points: []domain.Point{reykjavik, vik}, Alternatives: 3}); err != nil {
		t.Fatalf("alternatives: %v", err)
	}
	if !strings.Contains(gotBody, `"alternative_routes":{"share_factor":0.6,"target_count":3,"weight_factor":1.4}`) {
		t.Errorf("alternatives were asked as %s", gotBody)
	}

	cases := []struct {
		status int
		answer string
		want   error
	}{
		{http.StatusNotFound, `{"error":{"code":2010,"message":"Could not find routable point"}}`, ErrNoRoute},
		{http.StatusTooManyRequests, `{"error":"Rate limit exceeded"}`, ErrRateLimited},
		{http.StatusForbidden, `{"error":"Quota exceeded"}`, ErrRateLimited},
		{http.StatusInternalServerError, `oops`, ErrUnavailable},
	}
	for _, tc := range cases {
		status, answer = tc.status, tc.answer
		if _, err := client.Route(context.Background(), Request{Profile: profileCar, Points: []domain.Point{reykjavik, vik}}); !errors.Is(err, tc.want) {
			t.Errorf("status %d: %v, want %v", tc.status, err, tc.want)
		}
	}
}
