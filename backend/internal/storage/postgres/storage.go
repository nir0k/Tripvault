package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// StorageRepository keeps instance storage policy and measures files held in
// PostgreSQL. Its fallback is used only until the singleton settings row is
// first created, preserving an upgraded installation's environment setting.
type StorageRepository struct {
	pool             *pgxpool.Pool
	defaultTripQuota int64
}

// NewStorageRepository - creates the instance storage repository.
//
// Arguments:
//   - pool: an established connection pool.
//   - defaultTripQuota: the trip quota used to seed an instance without settings.
//
// Returns:
//   - a repository bound to the pool.
func NewStorageRepository(pool *pgxpool.Pool, defaultTripQuota int64) *StorageRepository {
	return &StorageRepository{pool: pool, defaultTripQuota: defaultTripQuota}
}

// Settings - reads the instance storage policy, creating its singleton row on
// first use so upgrades retain the deployment's previous trip quota.
//
// Arguments:
//   - ctx: context bounding the queries.
//
// Returns:
//   - the current storage settings.
//   - an error if they cannot be created or read.
func (r *StorageRepository) Settings(ctx context.Context) (domain.StorageSettings, error) {
	var settings domain.StorageSettings
	err := r.pool.QueryRow(ctx,
		`SELECT media_trip_quota_bytes FROM instance_settings WHERE id = 1`).Scan(&settings.TripQuotaBytes)
	if err == nil {
		return settings, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return settings, fmt.Errorf("read instance storage settings: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO instance_settings (id, media_trip_quota_bytes) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`,
		r.defaultTripQuota); err != nil {
		return domain.StorageSettings{}, fmt.Errorf("create instance storage settings: %w", err)
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT media_trip_quota_bytes FROM instance_settings WHERE id = 1`).Scan(&settings.TripQuotaBytes); err != nil {
		return settings, fmt.Errorf("read created instance storage settings: %w", err)
	}
	return settings, nil
}

// SetTripQuota - changes how much file data one trip may keep.
//
// Arguments:
//   - ctx: context bounding the query.
//   - quota: the allowance in bytes; zero leaves trips unlimited.
//
// Returns:
//   - the stored settings.
//   - an error if they cannot be changed.
func (r *StorageRepository) SetTripQuota(ctx context.Context, quota int64) (domain.StorageSettings, error) {
	var settings domain.StorageSettings
	err := r.pool.QueryRow(ctx,
		`INSERT INTO instance_settings (id, media_trip_quota_bytes) VALUES (1, $1)
		 ON CONFLICT (id) DO UPDATE
		 SET media_trip_quota_bytes = excluded.media_trip_quota_bytes, updated_at = now()
		 RETURNING media_trip_quota_bytes`, quota).Scan(&settings.TripQuotaBytes)
	if err != nil {
		return settings, fmt.Errorf("set the trip storage quota: %w", err)
	}
	return settings, nil
}

// DatabaseBytes - sums uploaded files kept in PostgreSQL: attachments and
// tracks by the accepted source sizes charged to the quota.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the bytes charged to the instance allowance.
//   - an error if they cannot be measured.
func (r *StorageRepository) DatabaseBytes(ctx context.Context) (int64, error) {
	var size int64
	err := r.pool.QueryRow(ctx,
		`SELECT (SELECT coalesce(sum(size), 0) FROM item_attachments)
		      + (SELECT coalesce(sum(file_size), 0) FROM tracks)`).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("measure database files: %w", err)
	}
	return size, nil
}

// TrackBytes - reports the source file bytes currently held by an activity.
//
// Arguments:
//   - ctx: context bounding the query.
//   - itemID: the activity whose track may be replaced.
//
// Returns:
//   - zero when the activity has no track, otherwise its stored file size.
//   - an error if the size cannot be read.
func (r *StorageRepository) TrackBytes(ctx context.Context, itemID uuid.UUID) (int64, error) {
	var size int64
	err := r.pool.QueryRow(ctx,
		`SELECT coalesce((SELECT file_size FROM tracks WHERE item_id = $1), 0)`, itemID).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("measure track file: %w", err)
	}
	return size, nil
}
