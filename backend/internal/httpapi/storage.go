package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

const bytesPerMegabyte int64 = 1024 * 1024

// storageResponse is the storage state shown to an administrator.
type storageResponse struct {
	MediaBytes     int64 `json:"media_bytes"`
	DatabaseBytes  int64 `json:"database_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	LimitBytes     int64 `json:"limit_bytes"`
	TripQuotaBytes int64 `json:"trip_quota_bytes"`
}

// newStorageResponse maps measured usage and policy onto the wire.
func newStorageResponse(usage domain.StorageUsage) storageResponse {
	return storageResponse{
		MediaBytes: usage.MediaBytes, DatabaseBytes: usage.DatabaseBytes,
		UsedBytes: usage.UsedBytes(), LimitBytes: usage.LimitBytes, TripQuotaBytes: usage.TripQuotaBytes,
	}
}

// storageChangeRequest changes only the administrator-controlled trip quota.
type storageChangeRequest struct {
	TripQuotaMB int64 `json:"trip_quota_mb"`
}

// handleUpdateStorage changes the allowance shared by every trip; the
// instance ceiling remains read-only because it belongs to the deployment.
func (s *Server) handleUpdateStorage(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "unavailable", "Storage settings are not available")
		return
	}
	var body storageChangeRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.TripQuotaMB < 0 || body.TripQuotaMB > math.MaxInt64/bytesPerMegabyte {
		s.writeDomainError(w, r, "validate storage settings",
			domain.NewValidationError("trip_quota_mb", "out_of_range", "must not be negative or overflow bytes"))
		return
	}
	if _, err := s.storage.SetTripQuota(r.Context(), body.TripQuotaMB*bytesPerMegabyte); err != nil {
		s.internalError(w, r, "set trip storage quota", err)
		return
	}
	usage, err := s.storage.Usage(r.Context())
	if err != nil {
		s.internalError(w, r, "measure storage", err)
		return
	}
	s.logger.Info("changed the trip storage quota", "request_id", RequestIDFrom(r.Context()),
		"admin_id", principalFrom(r.Context()).user.ID.String(), "quota_bytes", usage.TripQuotaBytes)
	writeJSON(w, s.logger, http.StatusOK, newStorageResponse(usage))
}

// tripQuota returns the current administrator-controlled trip allowance, with
// the configured seed retained as a fallback for focused handler tests.
func (s *Server) tripQuota(ctx context.Context) (int64, error) {
	if s.storage == nil {
		return s.mediaTripQuota, nil
	}
	settings, err := s.storage.Settings(ctx)
	return settings.TripQuotaBytes, err
}

// reserveStorage holds the instance quota lock for a file-producing operation.
func (s *Server) reserveStorage(ctx context.Context, growth int64) (func(), error) {
	if s.storage == nil {
		return func() {}, nil
	}
	return s.storage.Reserve(ctx, growth)
}

// writeStorageError maps an exhausted instance allowance and delegates every
// other failure to the ordinary domain error writer.
func (s *Server) writeStorageError(w http.ResponseWriter, r *http.Request, action string, err error) {
	if errors.Is(err, domain.ErrStorageQuota) {
		s.writeError(w, r, http.StatusInsufficientStorage, "storage_quota",
			"The service has no space left for more files")
		return
	}
	s.writeDomainError(w, r, action, err)
}
