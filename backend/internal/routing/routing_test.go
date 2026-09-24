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

// TestStraightLinesAndEstimates checks flights, "other" legs and estimates.
func TestStraightLinesAndEstimates(t *testing.T) {
	flight := StraightLine(domain.ModeFlight, reykjavik, vik)
	if flight.Source != domain.LegStraightLine || flight.DurationS == nil || *flight.DurationS < 30*60 {
		t.Errorf("flight: %+v", flight)
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
}

// Name identifies the fake.
func (f *fakeProvider) Name() string { return "fake" }

// Route returns the fixed answer.
func (f *fakeProvider) Route(context.Context, string, domain.Point, domain.Point) (Route, error) {
	f.calls++
	if f.err != nil {
		return Route{}, f.err
	}
	return Route{DistanceM: 186000, DurationS: 8000, Geometry: "abc"}, nil
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

	first := service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik)
	second := service.Calculate(ctx, domain.ModeTransit, &reykjavik, &vik)
	if first.Source != domain.LegProvider || second.Source != domain.LegProvider || provider.calls != 1 {
		t.Errorf("transit should reuse the car route from the cache: %+v %+v calls=%d", first, second, provider.calls)
	}

	service.Calculate(ctx, domain.ModeWalk, &reykjavik, &vik)
	limited := service.Calculate(ctx, domain.ModeBike, &reykjavik, &vik)
	if limited.Source != domain.LegEstimate || limited.Error != domain.LegErrorRateLimited {
		t.Errorf("per-minute limit: %+v", limited)
	}

	if err := service.Forget(ctx, domain.ModeCar, reykjavik, vik); err != nil || len(store.routes) != 1 {
		t.Errorf("forget: %v, %d cached", err, len(store.routes))
	}

	if got := service.Calculate(ctx, domain.ModeCar, nil, &vik); got.Source != domain.LegMissingCoordinates {
		t.Errorf("missing point: %+v", got)
	}

	daily, dailyStore := newTestService(&fakeProvider{}, Options{Daily: 1})
	dailyStore.requests = 1
	if got := daily.Calculate(ctx, domain.ModeCar, &reykjavik, &vik); got.Error != domain.LegErrorDailyLimit {
		t.Errorf("daily limit: %+v", got)
	}

	failing, _ := newTestService(&fakeProvider{err: ErrNoRoute}, Options{})
	if got := failing.Calculate(ctx, domain.ModeCar, &reykjavik, &vik); got.Error != domain.LegErrorNoRoute {
		t.Errorf("no route: %+v", got)
	}

	disabled, _ := newTestService(nil, Options{})
	if got := disabled.Calculate(ctx, domain.ModeCar, &reykjavik, &vik); got.Error != domain.LegErrorProviderDisabled || disabled.Enabled() {
		t.Errorf("no provider: %+v", got)
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
	route, err := client.Route(context.Background(), "driving-car", reykjavik, vik)
	if err != nil || route.DistanceM != 186020 || route.DurationS != 8041 || route.Geometry != "_p~iF" {
		t.Errorf("route: %+v %v", route, err)
	}
	if gotAuth != "secret-key" || gotPath != "/openrouteservice/v2/directions/driving-car" ||
		!strings.Contains(gotBody, `"coordinates":[[-21.9426,64.1466],[-19.006,63.4186]]`) {
		t.Errorf("request: auth=%q path=%q body=%s", gotAuth, gotPath, gotBody)
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
		if _, err := client.Route(context.Background(), "driving-car", reykjavik, vik); !errors.Is(err, tc.want) {
			t.Errorf("status %d: %v, want %v", tc.status, err, tc.want)
		}
	}
}
