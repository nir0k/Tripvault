package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/storage/migrations"
)

// Probe answers the health questions the HTTP layer asks about the database.
type Probe struct {
	pool *pgxpool.Pool
}

// NewProbe - creates the database probe.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a probe bound to that pool.
func NewProbe(pool *pgxpool.Pool) *Probe {
	return &Probe{pool: pool}
}

// Ping - checks the database answers.
//
// Arguments:
//   - ctx: context bounding the check.
//
// Returns:
//   - an error when the database is unreachable.
func (p *Probe) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// SchemaVersion - reports the applied migration version.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the version.
//   - an error if it cannot be read.
func (p *Probe) SchemaVersion(ctx context.Context) (int64, error) {
	return migrations.Version(ctx, p.pool)
}
