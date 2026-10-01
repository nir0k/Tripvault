// Package storagequota measures user files and serialises admissions against
// the operator's instance-wide allowance.
package storagequota

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// Database supplies the settings and uploaded bytes held in PostgreSQL.
type Database interface {
	Settings(ctx context.Context) (domain.StorageSettings, error)
	SetTripQuota(ctx context.Context, quota int64) (domain.StorageSettings, error)
	DatabaseBytes(ctx context.Context) (int64, error)
	TrackBytes(ctx context.Context, itemID uuid.UUID) (int64, error)
}

// Service applies one allowance to user files in PostgreSQL and the media store.
type Service struct {
	database Database
	files    media.Store
	walker   media.Walker
	limit    int64
	mu       sync.Mutex
}

// New - creates an instance storage quota service.
//
// Arguments:
//   - database: source of settings and database file sizes.
//   - files: media store used to measure replacement objects.
//   - walker: the same store with listing support.
//   - limit: the instance allowance in bytes; zero means unlimited.
//
// Returns:
//   - a quota service ready to measure and reserve storage.
func New(database Database, files media.Store, walker media.Walker, limit int64) *Service {
	return &Service{database: database, files: files, walker: walker, limit: limit}
}

// Usage - measures the user files charged to the allowance and reads the
// administrator's current trip quota.
//
// Arguments:
//   - ctx: context bounding the database query and media walk.
//
// Returns:
//   - the usage and configured limits.
//   - an error if either store cannot be measured.
func (s *Service) Usage(ctx context.Context) (domain.StorageUsage, error) {
	settings, err := s.database.Settings(ctx)
	if err != nil {
		return domain.StorageUsage{}, err
	}
	databaseBytes, err := s.database.DatabaseBytes(ctx)
	if err != nil {
		return domain.StorageUsage{}, err
	}
	mediaBytes, err := s.mediaBytes(ctx)
	if err != nil {
		return domain.StorageUsage{}, err
	}
	return domain.StorageUsage{
		MediaBytes: mediaBytes, DatabaseBytes: databaseBytes,
		LimitBytes: s.limit, TripQuotaBytes: settings.TripQuotaBytes,
	}, nil
}

// Settings - returns the administrator-controlled part of the storage policy.
//
// Arguments:
//   - ctx: context bounding the database query.
//
// Returns:
//   - the current settings.
//   - an error if they cannot be read.
func (s *Service) Settings(ctx context.Context) (domain.StorageSettings, error) {
	return s.database.Settings(ctx)
}

// SetTripQuota - stores the allowance shared by all trips.
//
// Arguments:
//   - ctx: context bounding the database query.
//   - quota: the allowance in bytes; zero leaves trips unlimited.
//
// Returns:
//   - the stored settings.
//   - an error if they cannot be changed.
func (s *Service) SetTripQuota(ctx context.Context, quota int64) (domain.StorageSettings, error) {
	return s.database.SetTripQuota(ctx, quota)
}

// TrackBytes - reports what a replaced activity track currently contributes.
//
// Arguments:
//   - ctx: context bounding the database query.
//   - itemID: the activity whose track is being replaced.
//
// Returns:
//   - its charged source bytes, or zero without a track.
//   - an error if they cannot be measured.
func (s *Service) TrackBytes(ctx context.Context, itemID uuid.UUID) (int64, error) {
	return s.database.TrackBytes(ctx, itemID)
}

// ObjectBytes - reports the bytes of one media object, or zero when it is absent.
//
// Arguments:
//   - ctx: context bounding the read.
//   - key: the object's storage key.
//
// Returns:
//   - the object's bytes.
//   - an error when the object cannot be read.
func (s *Service) ObjectBytes(ctx context.Context, key string) (int64, error) {
	if key == "" {
		return 0, nil
	}
	file, err := s.files.Open(ctx, key)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	defer func() { _ = file.Close() }()
	return io.Copy(io.Discard, file)
}

// Reserve - serialises a file-producing operation and refuses it when its net
// growth would cross the configured instance allowance. The caller must invoke
// the returned release function after the operation, whether it succeeds or not.
//
// Arguments:
//   - ctx: context bounding the usage measurement.
//   - growth: expected net growth in charged bytes; it may be negative.
//
// Returns:
//   - a function that releases the reservation lock.
//   - domain.ErrStorageQuota when the operation does not fit.
//   - another error when current usage cannot be measured.
func (s *Service) Reserve(ctx context.Context, growth int64) (func(), error) {
	if s.limit == 0 {
		return func() {}, nil
	}
	s.mu.Lock()
	usage, err := s.Usage(ctx)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if growth < -usage.UsedBytes() {
		growth = -usage.UsedBytes()
	}
	used := usage.UsedBytes()
	exceeds := growth > 0 && (growth > usage.LimitBytes || used > usage.LimitBytes-growth)
	if growth <= 0 {
		exceeds = used+growth > usage.LimitBytes
	}
	if exceeds {
		s.mu.Unlock()
		return nil, domain.ErrStorageQuota
	}
	return s.mu.Unlock, nil
}

// mediaBytes sums live media objects except generated trip previews, which are
// reproducible cache entries rather than user data.
func (s *Service) mediaBytes(ctx context.Context) (int64, error) {
	var total int64
	err := s.walker.Walk(ctx, func(object media.StoredObject) error {
		if !strings.HasPrefix(object.Key, ".previews/") {
			total += object.Size
		}
		return nil
	})
	return total, err
}
