package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/routing"
)

// RoutingRepository stores the shared route cache and counts directions
// requests.
type RoutingRepository struct {
	*UsageCounter
	pool *pgxpool.Pool
}

// NewRoutingRepository - creates the routing repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewRoutingRepository(pool *pgxpool.Pool) *RoutingRepository {
	return &RoutingRepository{UsageCounter: NewUsageCounter(pool, "directions"), pool: pool}
}

// Get - returns a live cached route and counts the hit.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the route's cache key.
//   - now: the reference time for expiry.
//
// Returns:
//   - the route and true, or false when nothing live is cached.
//   - an error if the statement fails.
func (r *RoutingRepository) Get(ctx context.Context, key []byte, now time.Time) (routing.Route, bool, error) {
	var route routing.Route
	err := r.pool.QueryRow(ctx,
		`UPDATE route_cache SET hits = hits + 1 WHERE key = $1 AND expires_at > $2
		 RETURNING distance_m, duration_s, geometry`, key, now).
		Scan(&route.DistanceM, &route.DurationS, &route.Geometry)
	if errors.Is(err, pgx.ErrNoRows) {
		return routing.Route{}, false, nil
	}
	if err != nil {
		return routing.Route{}, false, fmt.Errorf("read route cache: %w", err)
	}
	return route, true, nil
}

// Put - stores a route, replacing an older entry under the same key.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the route's cache key.
//   - provider, profile: what calculated it.
//   - route: the route.
//   - expires: when the entry stops being used.
//
// Returns:
//   - an error if the statement fails.
func (r *RoutingRepository) Put(ctx context.Context, key []byte, provider, profile string, route routing.Route, expires time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO route_cache (key, provider, profile, distance_m, duration_s, geometry, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (key) DO UPDATE SET distance_m = excluded.distance_m, duration_s = excluded.duration_s,
		     geometry = excluded.geometry, created_at = now(), expires_at = excluded.expires_at`,
		key, provider, profile, route.DistanceM, route.DurationS, route.Geometry, expires)
	if err != nil {
		return fmt.Errorf("write route cache: %w", err)
	}
	return nil
}

// Delete - forgets a cached route.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the route's cache key.
//
// Returns:
//   - an error if the statement fails.
func (r *RoutingRepository) Delete(ctx context.Context, key []byte) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM route_cache WHERE key = $1`, key); err != nil {
		return fmt.Errorf("delete route cache entry: %w", err)
	}
	return nil
}

// Stats - summarises provider usage and the cache for the status screen.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - now: the reference time.
//
// Returns:
//   - the figures.
//   - an error if a query fails.
func (r *RoutingRepository) Stats(ctx context.Context, now time.Time) (routing.UsageStats, error) {
	stats, err := r.usage(ctx, now)
	if err != nil {
		return stats, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*), coalesce(sum(hits), 0) FROM route_cache WHERE expires_at > $1`, now).
		Scan(&stats.CacheEntries, &stats.CacheHits); err != nil {
		return stats, fmt.Errorf("read route cache size: %w", err)
	}
	return stats, nil
}

// Purge - deletes expired routes and usage older than two days, which neither
// the limits nor the status screen read.
//
// Arguments:
//   - ctx: context bounding the statements.
//   - now: the reference time.
//
// Returns:
//   - the number of cache entries removed.
//   - an error if a statement fails.
func (r *RoutingRepository) Purge(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM route_cache WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("purge route cache: %w", err)
	}
	if err := r.purgeUsage(ctx, now); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
