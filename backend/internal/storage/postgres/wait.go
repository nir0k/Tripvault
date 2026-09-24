package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WaitForPool - repeatedly attempts to build a connection pool until it
// succeeds or the timeout expires.
//
// Container orchestration starts the API and the database concurrently, so a
// short retry loop avoids a crash-restart cycle while PostgreSQL is still
// initialising. Health checks in the compose file cover the common case; this
// loop covers the rest.
//
// Arguments:
//   - ctx: context that cancels the wait early.
//   - cfg: pool configuration passed to each attempt.
//   - timeout: total time to keep retrying before giving up.
//   - interval: delay between attempts.
//
// Returns:
//   - a ready connection pool that the caller must Close.
//   - an error if the database is still unreachable when the timeout expires.
func WaitForPool(ctx context.Context, cfg PoolConfig, timeout, interval time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)

	var lastErr error
	for {
		pool, err := NewPool(ctx, cfg)
		if err == nil {
			return pool, nil
		}
		lastErr = err

		if time.Now().Add(interval).After(deadline) {
			return nil, fmt.Errorf("database not reachable within %s: %w", timeout, lastErr)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}
