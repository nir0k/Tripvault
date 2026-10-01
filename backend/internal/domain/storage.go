package domain

import "errors"

// ErrStorageQuota reports an instance whose allowance for user files is full.
var ErrStorageQuota = errors.New("the service has no space left for more files")

// StorageSettings are the storage policies an administrator may change.
type StorageSettings struct {
	// TripQuotaBytes bounds the photographs and attachments of one trip; zero
	// leaves a trip unlimited by policy, though the instance limit still holds.
	TripQuotaBytes int64
}

// StorageUsage describes the user files charged to the instance allowance.
type StorageUsage struct {
	// MediaBytes are original trip pictures, avatars and idea pictures in the
	// media store. Generated trip previews are deliberately excluded.
	MediaBytes int64
	// DatabaseBytes are attachments and imported track files held in PostgreSQL.
	DatabaseBytes int64
	// LimitBytes is the operator's ceiling; zero means no instance limit.
	LimitBytes int64
	// TripQuotaBytes is the administrator's allowance for one trip.
	TripQuotaBytes int64
}

// UsedBytes - returns all user file bytes charged to the instance allowance.
//
// Returns:
//   - the sum of media-store and database file bytes.
func (u StorageUsage) UsedBytes() int64 {
	return u.MediaBytes + u.DatabaseBytes
}
