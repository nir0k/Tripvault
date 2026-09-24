package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// UsageCounter counts the requests one external service receives, per hour.
// The daily limits and the status screen read the last 24 hours.
type UsageCounter struct {
	pool    *pgxpool.Pool
	service string
}

// NewUsageCounter - creates the counter of one service.
//
// Arguments:
//   - pool: an established connection pool.
//   - service: "directions" or "geocode".
//
// Returns:
//   - the counter.
func NewUsageCounter(pool *pgxpool.Pool, service string) *UsageCounter {
	return &UsageCounter{pool: pool, service: service}
}

// Requests24h - counts the service's requests in the last 24 hours.
//
// Arguments:
//   - ctx: context bounding the query.
//   - now: the reference time.
//
// Returns:
//   - the count, by whole hours.
//   - an error if the query fails.
func (u *UsageCounter) Requests24h(ctx context.Context, now time.Time) (int, error) {
	var count int
	if err := u.pool.QueryRow(ctx,
		`SELECT coalesce(sum(requests), 0) FROM provider_usage
		 WHERE service = $1 AND hour > date_trunc('hour', $2::timestamptz) - interval '24 hours'`,
		u.service, now).Scan(&count); err != nil {
		return 0, fmt.Errorf("count %s requests: %w", u.service, err)
	}
	return count, nil
}

// Record - counts one request in its hour.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - now: when the request was made.
//   - success: whether the provider answered.
//
// Returns:
//   - an error if the statement fails.
func (u *UsageCounter) Record(ctx context.Context, now time.Time, success bool) error {
	_, err := u.pool.Exec(ctx,
		`INSERT INTO provider_usage AS p (service, hour, requests, errors, last_success_at, last_error_at)
		 VALUES ($1, date_trunc('hour', $2::timestamptz), 1, CASE WHEN $3 THEN 0 ELSE 1 END,
		         CASE WHEN $3 THEN $2::timestamptz END, CASE WHEN $3 THEN NULL ELSE $2::timestamptz END)
		 ON CONFLICT (service, hour) DO UPDATE SET
		     requests = p.requests + 1,
		     errors = p.errors + excluded.errors,
		     last_success_at = coalesce(excluded.last_success_at, p.last_success_at),
		     last_error_at = coalesce(excluded.last_error_at, p.last_error_at)`, u.service, now, success)
	if err != nil {
		return fmt.Errorf("record %s request: %w", u.service, err)
	}
	return nil
}

// usage reads the service's request figures for the status screen.
func (u *UsageCounter) usage(ctx context.Context, now time.Time) (domain.ProviderStats, error) {
	var stats domain.ProviderStats
	err := u.pool.QueryRow(ctx,
		`SELECT coalesce(sum(requests) FILTER (WHERE hour > date_trunc('hour', $2::timestamptz) - interval '24 hours'), 0),
		        coalesce(sum(errors) FILTER (WHERE hour > date_trunc('hour', $2::timestamptz) - interval '24 hours'), 0),
		        max(last_success_at), max(last_error_at)
		 FROM provider_usage WHERE service = $1`, u.service, now).
		Scan(&stats.Requests24h, &stats.Errors24h, &stats.LastSuccessAt, &stats.LastErrorAt)
	if err != nil {
		return stats, fmt.Errorf("read %s usage: %w", u.service, err)
	}
	return stats, nil
}

// purgeUsage deletes the service's hours older than two days.
func (u *UsageCounter) purgeUsage(ctx context.Context, now time.Time) error {
	if _, err := u.pool.Exec(ctx,
		`DELETE FROM provider_usage WHERE service = $1 AND hour < $2::timestamptz - interval '48 hours'`,
		u.service, now); err != nil {
		return fmt.Errorf("purge %s usage: %w", u.service, err)
	}
	return nil
}
