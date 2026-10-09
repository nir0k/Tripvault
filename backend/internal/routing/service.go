package routing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/requestwork"
)

// Cache stores provider routes shared by every trip.
type Cache interface {
	// Get returns a live cached route and counts the hit.
	Get(ctx context.Context, key []byte, now time.Time) (Route, bool, error)
	// Put stores a route until expires.
	Put(ctx context.Context, key []byte, provider, profile string, route Route, expires time.Time) error
	// Delete forgets a route, for an explicit recalculation.
	Delete(ctx context.Context, key []byte) error
}

// Usage counts provider requests, which the daily limit is checked against.
type Usage interface {
	// Requests24h counts requests made in the last 24 hours.
	Requests24h(ctx context.Context, now time.Time) (int, error)
	// Record counts one request and whether it succeeded.
	Record(ctx context.Context, now time.Time, success bool) error
}

// Options configures the routing service.
type Options struct {
	// PerMinute and Daily are the request limits the service keeps under,
	// set with a margin below the provider's own.
	PerMinute int
	Daily     int
	CacheTTL  time.Duration
	// CacheNamespace distinguishes installations of the same provider.
	CacheNamespace string
}

// Service calculates legs, using the provider when it can and falling back to
// estimates when it cannot. It is safe for concurrent use.
type Service struct {
	provider Provider
	cache    Cache
	usage    Usage
	opts     Options
	logger   *slog.Logger
	now      func() time.Time
	work     requestwork.Group[Result]

	mu        sync.Mutex
	recent    []time.Time
	inflight  int
	admission sync.Mutex
	requests  requestwork.Group[[]Route]
	memoMu    sync.Mutex
	memo      map[string]routeMemo
}

// NewService - creates the routing service.
//
// Arguments:
//   - provider: the road routing provider, or nil when none is configured.
//   - cache: the shared route cache.
//   - usage: the request counter.
//   - opts: limits and cache lifetime.
//   - logger: destination for provider failures.
//
// Returns:
//   - the service.
func NewService(provider Provider, cache Cache, usage Usage, opts Options, logger *slog.Logger) *Service {
	return &Service{provider: provider, cache: cache, usage: usage, opts: opts, logger: logger, now: time.Now}
}

// Enabled - reports whether road routes come from a provider.
//
// Returns:
//   - false when no provider is configured and every road leg is an estimate.
func (s *Service) Enabled() bool {
	return s.provider != nil
}

// ProviderName - names the configured provider.
//
// Returns:
//   - the name, or "" when none is configured.
func (s *Service) ProviderName() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

// cacheKey hashes what identifies a route: provider, profile, preference, and
// every point it passes through rounded to five decimals (about a metre).
func cacheKey(provider, profile string, route domain.LegRoute, from, to domain.Point) []byte {
	var key strings.Builder
	fmt.Fprintf(&key, "%s|%s|%s|%.5f,%.5f", provider, profile, preferenceOf(route), from.Lat, from.Lng)
	for _, point := range route.Via {
		fmt.Fprintf(&key, "|%.5f,%.5f", point.Lat, point.Lng)
	}
	fmt.Fprintf(&key, "|%.5f,%.5f", to.Lat, to.Lng)
	sum := sha256.Sum256([]byte(key.String()))
	return sum[:]
}

// preferenceOf reads a route's preference, the fastest when none is given.
func preferenceOf(route domain.LegRoute) domain.RoutePreference {
	if route.Preference == "" {
		return domain.RouteFastest
	}
	return route.Preference
}

// routePoints lists where a route starts, passes and ends, in order.
func routePoints(route domain.LegRoute, from, to domain.Point) []domain.Point {
	points := make([]domain.Point, 0, len(route.Via)+2)
	points = append(points, from)
	points = append(points, route.Via...)
	return append(points, to)
}

// takeMinuteSlot reserves a request within the per-minute limit.
func (s *Service) takeMinuteSlot(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-time.Minute)
	kept := s.recent[:0]
	for _, at := range s.recent {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	s.recent = kept
	if s.opts.PerMinute > 0 && len(s.recent) >= s.opts.PerMinute {
		return false
	}
	s.recent = append(s.recent, now)
	return true
}

// Capabilities - says what the configured provider can be asked for beyond
// the fastest route.
//
// Returns:
//   - the provider's capabilities, none when no provider is configured.
func (s *Service) Capabilities() Capabilities {
	if s.provider == nil {
		return Capabilities{}
	}
	return s.provider.Capabilities()
}

// Forget - drops the cached route of a leg so the next calculation asks the
// provider again. Legs that are not routed on roads have nothing cached.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - mode: the leg's mode.
//   - from, to: the leg's points.
//   - route: how the leg asks to be routed.
//
// Returns:
//   - an error if the cache cannot be changed.
func (s *Service) Forget(ctx context.Context, mode domain.TravelMode, from, to domain.Point, route domain.LegRoute) error {
	profile, routed := Profile(mode)
	if !routed || s.provider == nil {
		return nil
	}
	key := cacheKey(s.provider.Name()+"|"+s.opts.CacheNamespace, profile, route, from, to)
	s.memoMu.Lock()
	for _, count := range []int{1, alternativeCount} {
		delete(s.memo, string(key)+fmt.Sprint(count))
	}
	s.memoMu.Unlock()
	return s.cache.Delete(ctx, key)
}

// Calculate - works out a leg's distance, time and line.
//
// Flights, cable cars and "other" legs follow the straight line. Road legs come from the
// cache, else from the provider; when there is no provider, a limit is reached,
// no road route exists or the provider fails, the leg becomes an estimate with
// the reason, which the interface shows next to a retry button.
//
// Arguments:
//   - ctx: context bounding the lookups and the provider request.
//   - mode: the travel mode.
//   - from, to: the points; nil when unknown.
//   - route: what the route is optimised for and the points it passes.
//
// Returns:
//   - the result to store on the leg.
func (s *Service) Calculate(ctx context.Context, mode domain.TravelMode, from, to *domain.Point,
	route domain.LegRoute) Result {
	if from == nil || to == nil {
		return Result{Source: domain.LegMissingCoordinates}
	}
	profile, routed := Profile(mode)
	if !routed {
		return StraightLine(mode, *from, *to)
	}
	if s.provider == nil {
		return Estimate(mode, *from, *to, domain.LegErrorProviderDisabled)
	}

	now := s.now()
	key := cacheKey(s.provider.Name()+"|"+s.opts.CacheNamespace, profile, route, *from, *to)
	if cached, ok, err := s.cache.Get(ctx, key, now); err != nil {
		s.logger.Warn("route cache read failed", slog.Any("error", err))
	} else if ok {
		return providerResult(cached)
	}

	result, err := s.work.Do(ctx, string(key), func(ctx context.Context) (Result, error) {
		if cached, ok, err := s.cache.Get(ctx, key, s.now()); err == nil && ok {
			return providerResult(cached), nil
		}

		routes, err := s.requestRoutes(ctx, Request{
			Profile: profile, Points: routePoints(route, *from, *to), Preference: preferenceOf(route), Alternatives: 1,
		})
		if err != nil {
			return Result{}, err
		}
		if err := s.cache.Put(ctx, key, s.provider.Name(), profile, routes[0], now.Add(s.opts.CacheTTL)); err != nil {
			s.logger.Warn("route cache write failed", slog.Any("error", err))
		}
		return providerResult(routes[0]), nil
	})
	if err != nil {
		return Estimate(mode, *from, *to, estimateReason(err))
	}
	return result
}

// alternativeCount is how many routes a leg is offered to choose from.
const alternativeCount = 3

// Alternatives - asks the provider for the routes a leg could take, for
// somebody to choose one. Choices are cached briefly, and their primary route
// fills the ordinary cache; the chosen route is stored on the leg.
//
// Arguments:
//   - ctx: context bounding the provider request.
//   - mode: the travel mode, which must follow roads.
//   - from, to: the points.
//   - preference: what the best of the routes is optimised for.
//
// Returns:
//   - the routes, the best first; one when the provider offers no others.
//   - ErrNotRouted for a mode that does not follow roads, ErrDisabled when no
//     provider is configured, ErrDailyLimit or ErrRateLimited when a limit is
//     reached, ErrNoRoute or ErrUnavailable from the provider.
func (s *Service) Alternatives(ctx context.Context, mode domain.TravelMode, from, to domain.Point,
	preference domain.RoutePreference) ([]Result, error) {
	profile, routed := Profile(mode)
	if !routed {
		return nil, ErrNotRouted
	}
	if s.provider == nil {
		return nil, ErrDisabled
	}
	routes, err := s.requestRoutes(ctx, Request{
		Profile: profile, Points: []domain.Point{from, to},
		Preference: preferenceOf(domain.LegRoute{Preference: preference}), Alternatives: alternativeCount,
	})
	if err != nil {
		return nil, err
	}
	key := cacheKey(s.provider.Name()+"|"+s.opts.CacheNamespace, profile, domain.LegRoute{Preference: preference}, from, to)
	if err := s.cache.Put(ctx, key, s.provider.Name(), profile, routes[0], s.now().Add(s.opts.CacheTTL)); err != nil {
		s.logger.Warn("route cache write failed", slog.Any("error", err))
	}

	results := make([]Result, len(routes))
	for index, route := range routes {
		results[index] = providerResult(route)
	}
	return results, nil
}

// ask sends one request to the provider within the limits, counting it.
//
// Returns:
//   - the routes, never empty without an error.
//   - ErrDailyLimit or ErrRateLimited when a limit is reached first, or the
//     provider's own error.
func (s *Service) ask(ctx context.Context, request Request) ([]Route, error) {
	release, err := s.admit(ctx)
	if err != nil {
		return nil, err
	}

	routes, err := s.provider.Route(ctx, request)
	if err == nil && len(routes) == 0 {
		err = fmt.Errorf("%w: the answer held no route", ErrUnavailable)
	}
	release(err == nil || errors.Is(err, ErrNoRoute))
	if err != nil && !errors.Is(err, ErrNoRoute) && !errors.Is(err, ErrRateLimited) {
		s.logger.Warn("routing request failed", slog.String("profile", request.Profile), slog.Any("error", err))
	}
	return routes, err
}

// estimateReason names why a leg became an estimate, from the error that made it one.
func estimateReason(err error) string {
	switch {
	case errors.Is(err, ErrDailyLimit):
		return domain.LegErrorDailyLimit
	case errors.Is(err, ErrNoRoute):
		return domain.LegErrorNoRoute
	case errors.Is(err, ErrRateLimited):
		return domain.LegErrorRateLimited
	default:
		return domain.LegErrorProvider
	}
}

// providerResult turns a provider route into a stored result.
func providerResult(route Route) Result {
	distance, duration := route.DistanceM, route.DurationS
	return Result{DistanceM: &distance, DurationS: &duration, Geometry: route.Geometry, Source: domain.LegProvider}
}

// admit reserves a daily and minute allowance before the provider is called.
// Its release records the attempt before freeing the in-flight daily slot.
func (s *Service) admit(ctx context.Context) (func(bool), error) {
	s.admission.Lock()
	defer s.admission.Unlock()
	now := s.now()
	used, err := s.usage.Requests24h(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("read provider usage: %w", err)
	}
	if s.opts.Daily > 0 && used+s.inflight >= s.opts.Daily {
		return nil, ErrDailyLimit
	}
	if !s.takeMinuteSlot(now) {
		return nil, ErrRateLimited
	}
	s.inflight++
	return func(success bool) {
		s.admission.Lock()
		defer s.admission.Unlock()
		// The provider has finished even when its readers cancelled. Keep its
		// attempted call in the quota, using a bounded cleanup context.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.usage.Record(recordCtx, now, success); err != nil {
			s.logger.Warn("record provider usage failed", slog.Any("error", err))
			// Keep the reservation when persistence fails: an uncounted attempt
			// must not grant another slot until this process restarts.
			return
		}
		s.inflight--
	}, nil
}

// routeMemo keeps a brief route choice or a negative answer; transient provider
// failures are never retained.
type routeMemo struct {
	routes []Route
	err    error
	until  time.Time
}

// requestRoutes coalesces identical provider requests and briefly reuses route
// choices and no-route answers. The bounded memory cache complements the
// persistent primary-route cache without storing failed provider responses.
func (s *Service) requestRoutes(ctx context.Context, request Request) ([]Route, error) {
	last := len(request.Points) - 1
	key := cacheKey(s.provider.Name()+"|"+s.opts.CacheNamespace, request.Profile,
		domain.LegRoute{Preference: request.Preference, Via: request.Points[1:last]}, request.Points[0], request.Points[last])
	identity := string(key) + fmt.Sprint(request.Alternatives)
	return s.requests.Do(ctx, identity, func(ctx context.Context) ([]Route, error) {
		s.memoMu.Lock()
		cached, found := s.memo[identity]
		s.memoMu.Unlock()
		if found && s.now().Before(cached.until) {
			return cached.routes, cached.err
		}
		routes, err := s.ask(ctx, request)
		lifetime := time.Duration(0)
		if errors.Is(err, ErrNoRoute) {
			lifetime = time.Minute
		}
		if err == nil && request.Alternatives > 1 {
			lifetime = 5 * time.Minute
		}
		if lifetime > 0 {
			s.memoMu.Lock()
			if s.memo == nil {
				s.memo = make(map[string]routeMemo)
			}
			for key, entry := range s.memo {
				if !s.now().Before(entry.until) {
					delete(s.memo, key)
				}
			}
			if len(s.memo) >= 256 {
				for key := range s.memo {
					delete(s.memo, key)
					break
				}
			}
			s.memo[identity] = routeMemo{routes: routes, err: err, until: s.now().Add(lifetime)}
			s.memoMu.Unlock()
		}
		return routes, err
	})
}
