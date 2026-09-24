// Package postgres owns the PostgreSQL connection pool and the repository
// implementations built on top of it.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig describes how the connection pool should be built.
type PoolConfig struct {
	DSN            string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// NewPool - creates and verifies a PostgreSQL connection pool.
//
// The pool is eagerly probed with a Ping so that an unreachable or
// misconfigured database fails at start-up rather than on the first request.
//
// Arguments:
//   - ctx: context bounding pool creation and the initial connectivity probe.
//   - cfg: DSN and sizing parameters for the pool.
//
// Returns:
//   - a ready-to-use connection pool that the caller must Close.
//   - an error if the DSN is invalid or the database is unreachable.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(probeCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
