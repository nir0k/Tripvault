package geocoding

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentGeocodingCoalescesAndReserves checks equal cache misses cost one
// request, and distinct ones reserve the remaining daily allowance atomically.
func TestConcurrentGeocodingCoalescesAndReserves(t *testing.T) {
	for _, identical := range []bool{true, false} {
		t.Run(map[bool]string{true: "same query", false: "daily reservation"}[identical], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"features":[{"geometry":{"coordinates":[20,40]},"properties":{"name":"Cafe"}}]}`))
			}))
			defer server.Close()
			store := &memory{entries: map[string][]byte{}}
			service := NewService(NewPhoton(server.URL), store, store, Options{Daily: 1, CacheTTL: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			var workers sync.WaitGroup
			for i := range 8 {
				workers.Go(func() {
					text := "Cafe"
					if !identical {
						text = fmt.Sprint("Cafe ", i)
					}
					_, _ = service.Search(t.Context(), Query{Text: text, Lang: "en"})
				})
			}
			workers.Wait()
			if calls.Load() != 1 {
				t.Fatalf("provider calls=%d", calls.Load())
			}
		})
	}
}

// TestGeocodingCacheNamespaces separates responses from different installations.
func TestGeocodingCacheNamespaces(t *testing.T) {
	store := &memory{entries: map[string][]byte{}}
	provider := &fakeGeocoder{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, namespace := range []string{"first", "second"} {
		service := NewService(provider, store, store, Options{CacheNamespace: namespace, CacheTTL: time.Hour}, logger)
		if _, err := service.Search(t.Context(), Query{Text: "Cafe"}); err != nil {
			t.Fatal(err)
		}
	}
	if provider.calls != 2 {
		t.Fatalf("different installations shared %d calls", provider.calls)
	}
}
