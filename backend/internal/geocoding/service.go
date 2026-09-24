package geocoding

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Cache stores geocoding answers shared by every user.
type Cache interface {
	// Get returns a live cached answer and counts the hit.
	Get(ctx context.Context, key []byte, now time.Time) ([]byte, bool, error)
	// Put stores an answer until expires.
	Put(ctx context.Context, key []byte, provider string, payload []byte, expires time.Time) error
}

// Usage counts provider requests, which the daily limit is checked against.
type Usage interface {
	Requests24h(ctx context.Context, now time.Time) (int, error)
	Record(ctx context.Context, now time.Time, success bool) error
}

// Options configures the geocoding service.
type Options struct {
	PerMinute int
	Daily     int
	CacheTTL  time.Duration
}

// Service answers searches from the cache or the provider, within limits. It
// is safe for concurrent use.
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

// NewService - creates the geocoding service.
//
// Arguments:
//   - provider: the geocoder, or nil when none is configured.
//   - cache: the shared answer cache.
//   - usage: the request counter.
//   - opts: limits and cache lifetime.
//   - logger: destination for provider failures.
//
// Returns:
//   - the service.
func NewService(provider Provider, cache Cache, usage Usage, opts Options, logger *slog.Logger) *Service {
	return &Service{provider: provider, cache: cache, usage: usage, opts: opts, logger: logger, now: time.Now}
}

// Enabled - reports whether a geocoder is configured.
//
// Returns:
//   - false when searches and reverse lookups are unavailable.
func (s *Service) Enabled() bool {
	return s.provider != nil
}

// Search - finds places matching a text, near the focus first.
//
// Searches repeat - several people typing the same town - so answers are
// cached by text, language and a focus rounded to about ten kilometres.
//
// Arguments:
//   - ctx: context bounding the lookups and the provider request.
//   - query: the text, language and optional focus.
//
// Returns:
//   - the places, best first.
//   - ErrDisabled, ErrRateLimited or ErrUnavailable.
func (s *Service) Search(ctx context.Context, query Query) ([]Place, error) {
	focus := "-"
	if query.Focus != nil {
		focus = fmt.Sprintf("%.1f,%.1f", query.Focus.Lat, query.Focus.Lng)
	}
	text := strings.ToLower(strings.Join(strings.Fields(query.Text), " "))
	return s.lookup(ctx, "search|"+text+"|"+query.Lang+"|"+focus, func(provider Provider) ([]Place, error) {
		return provider.Search(ctx, query)
	})
}

// Reverse - names the places at a point, nearest first.
//
// Arguments:
//   - ctx: context bounding the lookups and the provider request.
//   - point: the position, cached rounded to about ten metres.
//   - lang: the language results are named in.
//
// Returns:
//   - the places.
//   - ErrDisabled, ErrRateLimited or ErrUnavailable.
func (s *Service) Reverse(ctx context.Context, point domain.Point, lang string) ([]Place, error) {
	key := fmt.Sprintf("reverse|%.4f,%.4f|%s", point.Lat, point.Lng, lang)
	return s.lookup(ctx, key, func(provider Provider) ([]Place, error) {
		return provider.Reverse(ctx, point, lang)
	})
}

// lookup serves an answer from the cache, else asks the provider within the
// limits and caches what it says, empty answers included.
func (s *Service) lookup(ctx context.Context, identity string, ask func(Provider) ([]Place, error)) ([]Place, error) {
	if s.provider == nil {
		return nil, ErrDisabled
	}
	now := s.now()
	sum := sha256.Sum256([]byte(s.provider.Name() + "|" + identity))
	key := sum[:]

	if payload, ok, err := s.cache.Get(ctx, key, now); err != nil {
		s.logger.Warn("geocode cache read failed", slog.Any("error", err))
	} else if ok {
		var places []Place
		if err := json.Unmarshal(payload, &places); err == nil {
			return places, nil
		}
	}

	if used, err := s.usage.Requests24h(ctx, now); err != nil {
		s.logger.Warn("geocode usage read failed", slog.Any("error", err))
	} else if s.opts.Daily > 0 && used >= s.opts.Daily {
		return nil, ErrRateLimited
	}
	if !s.takeMinuteSlot(now) {
		return nil, ErrRateLimited
	}

	places, err := ask(s.provider)
	if recordErr := s.usage.Record(ctx, now, err == nil); recordErr != nil {
		s.logger.Warn("geocode usage write failed", slog.Any("error", recordErr))
	}
	if err != nil {
		s.logger.Warn("geocode request failed", slog.Any("error", err))
		return nil, err
	}
	if places == nil {
		places = []Place{}
	}
	if payload, err := json.Marshal(places); err == nil {
		if err := s.cache.Put(ctx, key, s.provider.Name(), payload, now.Add(s.opts.CacheTTL)); err != nil {
			s.logger.Warn("geocode cache write failed", slog.Any("error", err))
		}
	}
	return places, nil
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
