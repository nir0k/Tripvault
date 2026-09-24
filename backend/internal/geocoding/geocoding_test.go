package geocoding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestParseLocation checks the coordinate and link forms people paste.
func TestParseLocation(t *testing.T) {
	cases := map[string]Location{
		"64.1466, -21.9426":         {Lat: 64.1466, Lng: -21.9426},
		"64.1466 -21.9426":          {Lat: 64.1466, Lng: -21.9426},
		"geo:63.6156,-19.9885;u=35": {Lat: 63.6156, Lng: -19.9885},
		"https://www.google.com/maps/place/Seljalandsfoss/@63.6156,-19.9985,17z/data=!3m1!4b1!4m6!3m5!1s0x0:0x0!8m2!3d63.6156!4d-19.9885": {
			Lat: 63.6156, Lng: -19.9885, Name: "Seljalandsfoss"},
		"https://www.google.com/maps/@64.1466,-21.9426,14z":                 {Lat: 64.1466, Lng: -21.9426},
		"https://maps.google.com/?q=64.1466,-21.9426":                       {Lat: 64.1466, Lng: -21.9426},
		"https://www.google.com/maps/search/?api=1&query=63.4186%2C-19.006": {Lat: 63.4186, Lng: -19.006},
		"https://www.google.com/maps/search/63.4186,+-19.006?entry=ttu":     {Lat: 63.4186, Lng: -19.006},
		"https://www.google.com/maps/place/Hallgr%C3%ADmskirkja/@64.1417,-21.9266,17z": {
			Lat: 64.1417, Lng: -21.9266, Name: "Hallgrímskirkja"},
		"https://www.openstreetmap.org/?mlat=64.1466&mlon=-21.9426#map=15/64.1/-21.9": {Lat: 64.1466, Lng: -21.9426},
		"https://www.openstreetmap.org/#map=15/64.14660/-21.94260":                    {Lat: 64.1466, Lng: -21.9426},
	}
	for text, want := range cases {
		got, err := ParseLocation(text)
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", text, got, err, want)
		}
	}
	for _, text := range []string{"Reykjavik", "91, 10", "https://example.com/?q=64.1,-21.9", "https://www.google.com/maps/place/Reykjavik", ""} {
		if _, err := ParseLocation(text); !errors.Is(err, ErrUnrecognized) {
			t.Errorf("%q accepted", text)
		}
	}
	if !IsShortLink("https://maps.app.goo.gl/abc123") || IsShortLink("http://maps.app.goo.gl/abc") || IsShortLink("https://evil.example/abc") {
		t.Error("short link detection")
	}
}

// TestExpandRefusesOtherHosts checks the expander never requests a host
// outside its list.
func TestExpandRefusesOtherHosts(t *testing.T) {
	requested := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requested = true }))
	defer server.Close()
	if _, err := NewExpander().Expand(context.Background(), server.URL+"/abc"); !errors.Is(err, ErrUnrecognized) || requested {
		t.Errorf("expanded an unknown host: %v requested=%v", err, requested)
	}
}

// TestPelias checks the requests the client sends and how answers map.
func TestPelias(t *testing.T) {
	var gotAuth, gotPath, gotQuery string
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"features":[{"geometry":{"coordinates":[-19.9885,63.6156]},
			"properties":{"name":"Seljalandsfoss","label":"Seljalandsfoss, Iceland","layer":"venue","country":"Iceland","gid":"openstreetmap:venue:node/1"}}]}`))
	}))
	defer server.Close()

	client := NewPelias(server.URL+"/pelias/v1/", "secret-key")
	places, err := client.Search(context.Background(), Query{Text: "Seljalandsfoss", Lang: "ru", Focus: &domain.Point{Lat: 64, Lng: -21}})
	if err != nil || len(places) != 1 || places[0].Lat != 63.6156 || places[0].Ref != "openstreetmap:venue:node/1" {
		t.Errorf("search: %+v %v", places, err)
	}
	if gotAuth != "secret-key" || gotPath != "/pelias/v1/search" || !strings.Contains(gotQuery, "focus.point.lat=64.000000") ||
		!strings.Contains(gotQuery, "lang=ru") {
		t.Errorf("request: %s %s %s", gotAuth, gotPath, gotQuery)
	}
	if _, err := client.Reverse(context.Background(), domain.Point{Lat: 63.6, Lng: -19.9}, "en"); err != nil || gotPath != "/pelias/v1/reverse" {
		t.Errorf("reverse: %v %s", err, gotPath)
	}
	status = http.StatusTooManyRequests
	if _, err := client.Search(context.Background(), Query{Text: "x"}); !errors.Is(err, ErrRateLimited) {
		t.Errorf("rate limit: %v", err)
	}
}

// fakeGeocoder counts calls.
type fakeGeocoder struct{ calls int }

// Name identifies the fake.
func (f *fakeGeocoder) Name() string { return "fake" }

// Search returns one place.
func (f *fakeGeocoder) Search(context.Context, Query) ([]Place, error) {
	f.calls++
	return []Place{{Name: "Vík"}}, nil
}

// Reverse returns nothing.
func (f *fakeGeocoder) Reverse(context.Context, domain.Point, string) ([]Place, error) {
	f.calls++
	return nil, nil
}

// memory is an in-memory cache and usage counter.
type memory struct {
	entries  map[string][]byte
	requests int
}

// Get returns a cached answer.
func (m *memory) Get(_ context.Context, key []byte, _ time.Time) ([]byte, bool, error) {
	value, ok := m.entries[string(key)]
	return value, ok, nil
}

// Put stores an answer.
func (m *memory) Put(_ context.Context, key []byte, _ string, payload []byte, _ time.Time) error {
	m.entries[string(key)] = payload
	return nil
}

// Requests24h counts requests.
func (m *memory) Requests24h(context.Context, time.Time) (int, error) { return m.requests, nil }

// Record counts a request.
func (m *memory) Record(context.Context, time.Time, bool) error { m.requests++; return nil }

// TestService checks caching, the limits and a missing provider.
func TestService(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := &fakeGeocoder{}
	store := &memory{entries: map[string][]byte{}}
	service := NewService(provider, store, store, Options{PerMinute: 2, Daily: 10, CacheTTL: time.Hour}, logger)

	focus := &domain.Point{Lat: 63.41, Lng: -19.01}
	first, _ := service.Search(ctx, Query{Text: "Vík ", Lang: "en", Focus: focus})
	second, _ := service.Search(ctx, Query{Text: "vík", Lang: "en", Focus: &domain.Point{Lat: 63.44, Lng: -19.04}})
	if len(first) != 1 || len(second) != 1 || provider.calls != 1 {
		t.Errorf("cache: %v %v calls=%d", first, second, provider.calls)
	}
	if places, err := service.Reverse(ctx, domain.Point{Lat: 63.4, Lng: -19}, "en"); err != nil || places == nil {
		t.Errorf("empty reverse: %v %v", places, err)
	}
	if _, err := service.Search(ctx, Query{Text: "Reykjavik"}); !errors.Is(err, ErrRateLimited) {
		t.Errorf("per-minute limit: %v", err)
	}
	if _, err := NewService(nil, store, store, Options{}, logger).Search(ctx, Query{Text: "x"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("no provider: %v", err)
	}
}
