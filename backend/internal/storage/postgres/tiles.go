package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TileRepository keeps the map tiles a report's PDF was drawn with, so a second
// export of the same trip does not ask the tile server again.
type TileRepository struct {
	pool *pgxpool.Pool
}

// NewTileRepository - creates the tile cache repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewTileRepository(pool *pgxpool.Pool) *TileRepository {
	return &TileRepository{pool: pool}
}

// Tile - returns a live cached tile.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the tile's cache key.
//   - now: the reference time for expiry.
//
// Returns:
//   - the tile's bytes and true, or false when nothing live is cached.
//   - an error if the statement fails.
func (r *TileRepository) Tile(ctx context.Context, key []byte, now time.Time) ([]byte, bool, error) {
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM tile_cache WHERE key = $1 AND expires_at > $2`, key, now).
		Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cached tile: %w", err)
	}
	return body, true, nil
}

// SaveTile - stores a tile, replacing an expired copy.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - key: the tile's cache key.
//   - body: the tile as the server sent it.
//   - expiresAt: when it must be fetched again.
//
// Returns:
//   - an error if the statement fails.
func (r *TileRepository) SaveTile(ctx context.Context, key, body []byte, expiresAt time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO tile_cache (key, body, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE SET body = EXCLUDED.body, created_at = now(), expires_at = EXCLUDED.expires_at`,
		key, body, expiresAt); err != nil {
		return fmt.Errorf("store tile: %w", err)
	}
	return nil
}

// Purge - deletes expired tiles.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - now: the reference time.
//
// Returns:
//   - the number of tiles removed.
//   - an error if the statement fails.
func (r *TileRepository) Purge(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tile_cache WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("purge tile cache: %w", err)
	}
	return tag.RowsAffected(), nil
}
