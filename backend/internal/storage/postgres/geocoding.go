package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// GeocodingRepository stores the shared geocode cache and counts geocoder
// requests.
type GeocodingRepository struct {
	*UsageCounter
	pool *pgxpool.Pool
}

// NewGeocodingRepository - creates the geocoding repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewGeocodingRepository(pool *pgxpool.Pool) *GeocodingRepository {
	return &GeocodingRepository{UsageCounter: NewUsageCounter(pool, "geocode"), pool: pool}
}

// Get - returns a live cached answer and counts the hit.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the answer's cache key.
//   - now: the reference time for expiry.
//
// Returns:
//   - the JSON payload and true, or false when nothing live is cached.
//   - an error if the statement fails.
func (r *GeocodingRepository) Get(ctx context.Context, key []byte, now time.Time) ([]byte, bool, error) {
	var payload []byte
	err := r.pool.QueryRow(ctx,
		`UPDATE geocode_cache SET hits = hits + 1 WHERE key = $1 AND expires_at > $2 RETURNING payload`, key, now).
		Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read geocode cache: %w", err)
	}
	return payload, true, nil
}

// Put - stores an answer, replacing an older one under the same key.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the answer's cache key.
//   - provider: the geocoder that answered.
//   - payload: the answer as JSON.
//   - expires: when the entry stops being used.
//
// Returns:
//   - an error if the statement fails.
func (r *GeocodingRepository) Put(ctx context.Context, key []byte, provider string, payload []byte, expires time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO geocode_cache (key, provider, payload, expires_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (key) DO UPDATE SET payload = excluded.payload, created_at = now(), expires_at = excluded.expires_at`,
		key, provider, payload, expires)
	if err != nil {
		return fmt.Errorf("write geocode cache: %w", err)
	}
	return nil
}

// Stats - summarises geocoder usage and the cache for the status screen.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - now: the reference time.
//
// Returns:
//   - the figures.
//   - an error if a query fails.
func (r *GeocodingRepository) Stats(ctx context.Context, now time.Time) (domain.ProviderStats, error) {
	stats, err := r.usage(ctx, now)
	if err != nil {
		return stats, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*), coalesce(sum(hits), 0) FROM geocode_cache WHERE expires_at > $1`, now).
		Scan(&stats.CacheEntries, &stats.CacheHits); err != nil {
		return stats, fmt.Errorf("read geocode cache size: %w", err)
	}
	return stats, nil
}

// Purge - deletes expired answers and usage older than two days.
//
// Arguments:
//   - ctx: context bounding the statements.
//   - now: the reference time.
//
// Returns:
//   - the number of answers removed.
//   - an error if a statement fails.
func (r *GeocodingRepository) Purge(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM geocode_cache WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("purge geocode cache: %w", err)
	}
	if err := r.purgeUsage(ctx, now); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
