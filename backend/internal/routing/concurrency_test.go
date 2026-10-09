package routing

import (
	"context"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentRoutingCoalescesAndReserves checks identical requests cost one
// call, while distinct misses still cannot overspend the last daily slot.
func TestConcurrentRoutingCoalescesAndReserves(t *testing.T) {
	for _, identical := range []bool{true, false} {
		t.Run(map[bool]string{true: "same route", false: "daily reservation"}[identical], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"routes":[{"summary":{"distance":1000,"duration":900},"geometry":"abc"}]}`))
			}))
			defer server.Close()
			service, _ := newTestService(NewORS(server.URL, ""), Options{Daily: 1, CacheTTL: time.Hour})
			var workers sync.WaitGroup
			for i := range 8 {
				workers.Go(func() {
					from := reykjavik
					if !identical {
						from.Lat += float64(i) / 100
					}
					service.Calculate(t.Context(), domain.ModeCar, &from, &vik, domain.LegRoute{})
				})
			}
			workers.Wait()
			if calls.Load() != 1 {
				t.Fatalf("provider calls=%d", calls.Load())
			}
		})
	}
}

// TestRouteChoicesReuseThePrimaryAndForget checks reopening a choice and
// ordinary routing reuse it, while explicit recalculation fetches it afresh.
func TestRouteChoicesReuseThePrimaryAndForget(t *testing.T) {
	provider := &fakeProvider{}
	service, _ := newTestService(provider, Options{CacheTTL: time.Hour})
	ctx := context.Background()
	for range 2 {
		if _, err := service.Alternatives(ctx, domain.ModeCar, reykjavik, vik, domain.RouteFastest); err != nil {
			t.Fatal(err)
		}
	}
	service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	if provider.calls != 1 {
		t.Fatalf("repeated choices/primary cost %d calls", provider.calls)
	}
	if err := service.Forget(ctx, domain.ModeCar, reykjavik, vik, domain.LegRoute{}); err != nil {
		t.Fatal(err)
	}
	service.Calculate(ctx, domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	if provider.calls != 2 {
		t.Fatalf("explicit recalculation cost %d calls", provider.calls)
	}
}

// TestRoutingCacheNamespaces separates installations of the same provider.
func TestRoutingCacheNamespaces(t *testing.T) {
	provider := &fakeProvider{}
	first, store := newTestService(provider, Options{CacheNamespace: "first", CacheTTL: time.Hour})
	second := NewService(provider, store, store, Options{CacheNamespace: "second", CacheTTL: time.Hour}, first.logger)
	first.Calculate(t.Context(), domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	second.Calculate(t.Context(), domain.ModeCar, &reykjavik, &vik, domain.LegRoute{})
	if provider.calls != 2 {
		t.Fatalf("different installations shared %d calls", provider.calls)
	}
}
