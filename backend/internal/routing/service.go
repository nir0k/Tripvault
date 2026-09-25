package routing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
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

	mu     sync.Mutex
	recent []time.Time
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

// cacheKey hashes what identifies a route: provider, profile and both points
// rounded to five decimals (about a metre).
func cacheKey(provider, profile string, from, to domain.Point) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%.5f,%.5f|%.5f,%.5f",
		provider, profile, from.Lat, from.Lng, to.Lat, to.Lng)))
	return sum[:]
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

// Forget - drops the cached route of a leg so the next calculation asks the
// provider again. Legs that are not routed on roads have nothing cached.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - mode: the leg's mode.
//   - from, to: the leg's points.
//
// Returns:
//   - an error if the cache cannot be changed.
func (s *Service) Forget(ctx context.Context, mode domain.TravelMode, from, to domain.Point) error {
	profile, routed := Profile(mode)
	if !routed || s.provider == nil {
		return nil
	}
	return s.cache.Delete(ctx, cacheKey(s.provider.Name(), profile, from, to))
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
//
// Returns:
//   - the result to store on the leg.
func (s *Service) Calculate(ctx context.Context, mode domain.TravelMode, from, to *domain.Point) Result {
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
	key := cacheKey(s.provider.Name(), profile, *from, *to)
	if route, ok, err := s.cache.Get(ctx, key, now); err != nil {
		s.logger.Warn("route cache read failed", slog.Any("error", err))
	} else if ok {
		return providerResult(route)
	}

	if used, err := s.usage.Requests24h(ctx, now); err != nil {
		s.logger.Warn("routing usage read failed", slog.Any("error", err))
	} else if s.opts.Daily > 0 && used >= s.opts.Daily {
		return Estimate(mode, *from, *to, domain.LegErrorDailyLimit)
	}
	if !s.takeMinuteSlot(now) {
		return Estimate(mode, *from, *to, domain.LegErrorRateLimited)
	}

	route, err := s.provider.Route(ctx, profile, *from, *to)
	if recordErr := s.usage.Record(ctx, now, err == nil || errors.Is(err, ErrNoRoute)); recordErr != nil {
		s.logger.Warn("routing usage write failed", slog.Any("error", recordErr))
	}
	if err != nil {
		reason := domain.LegErrorProvider
		switch {
		case errors.Is(err, ErrNoRoute):
			reason = domain.LegErrorNoRoute
		case errors.Is(err, ErrRateLimited):
			reason = domain.LegErrorRateLimited
		default:
			s.logger.Warn("routing request failed", slog.String("profile", profile), slog.Any("error", err))
		}
		return Estimate(mode, *from, *to, reason)
	}

	if err := s.cache.Put(ctx, key, s.provider.Name(), profile, route, now.Add(s.opts.CacheTTL)); err != nil {
		s.logger.Warn("route cache write failed", slog.Any("error", err))
	}
	return providerResult(route)
}

// providerResult turns a provider route into a stored result.
func providerResult(route Route) Result {
	distance, duration := route.DistanceM, route.DurationS
	return Result{DistanceM: &distance, DurationS: &duration, Geometry: route.Geometry, Source: domain.LegProvider}
}
