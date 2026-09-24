package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/routing"
	"github.com/nir0k/tripvault/backend/internal/telemetry"
)

// adminUserResponse is one account as the administration screen lists it.
type adminUserResponse struct {
	userResponse
	// IsSelf marks the reader's own row, so the interface can warn before
	// somebody withdraws their own rights.
	IsSelf bool `json:"is_self"`
}

// handleListUsers returns every account.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	self := principalFrom(r.Context()).user.ID

	users, err := s.users.List(r.Context())
	if err != nil {
		s.internalError(w, r, "list users", err)
		return
	}

	items := make([]adminUserResponse, 0, len(users))
	for _, user := range users {
		items = append(items, adminUserResponse{userResponse: newUserResponse(user), IsSelf: user.ID == self})
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[adminUserResponse]{Items: items})
}

// createUserRequest is the body of POST /api/v1/admin/users.
type createUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	IsAdmin     bool   `json:"is_admin"`
}

// handleCreateUser creates an account with a temporary password, which its
// owner must replace at the first sign-in.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body createUserRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}

	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		s.writeDomainError(w, r, "validate user", err)
		return
	}
	name, err := domain.NormalizeDisplayName(body.DisplayName)
	if err != nil {
		s.writeDomainError(w, r, "validate user", err)
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate user", err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.internalError(w, r, "hash password", err)
		return
	}

	created, err := s.users.Create(r.Context(), domain.User{
		ID:                 uuid.Must(uuid.NewV7()),
		Email:              email,
		DisplayName:        name,
		PasswordHash:       hash,
		IsAdmin:            body.IsAdmin,
		IsActive:           true,
		MustChangePassword: true,
		Theme:              domain.ThemeAuto,
		Units:              domain.UnitsKilometres,
		DefaultCurrency:    domain.DefaultCurrency,
	})
	if err != nil {
		s.writeDomainError(w, r, "create user", err)
		return
	}

	s.audit(r.Context(), "created an account", created.ID)
	writeJSON(w, s.logger, http.StatusCreated, adminUserResponse{userResponse: newUserResponse(created)})
}

// updateUserRequest is the body of PATCH /api/v1/admin/users/{userID}.
type updateUserRequest struct {
	DisplayName *string `json:"display_name"`
	IsAdmin     *bool   `json:"is_admin"`
	IsActive    *bool   `json:"is_active"`
}

// handleUpdateUser renames an account, grants or withdraws administration, or
// deactivates and reactivates it. Deactivation ends every session at once.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var body updateUserRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}

	changes := domain.UserChanges{IsAdmin: body.IsAdmin, IsActive: body.IsActive}
	if body.DisplayName != nil {
		name, err := domain.NormalizeDisplayName(*body.DisplayName)
		if err != nil {
			s.writeDomainError(w, r, "validate user", err)
			return
		}
		changes.DisplayName = &name
	}

	updated, err := s.users.Update(r.Context(), userID, changes)
	if err != nil {
		s.writeDomainError(w, r, "update user", err)
		return
	}

	s.audit(r.Context(), "changed an account", updated.ID)
	writeJSON(w, s.logger, http.StatusOK, adminUserResponse{
		userResponse: newUserResponse(updated),
		IsSelf:       updated.ID == principalFrom(r.Context()).user.ID,
	})
}

// resetPasswordRequest is the body of the password reset.
type resetPasswordRequest struct {
	Password string `json:"password"`
}

// handleResetPassword gives an account a new temporary password and ends every
// session it has.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var body resetPasswordRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate password", err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.internalError(w, r, "hash password", err)
		return
	}
	if err := s.users.ResetPassword(r.Context(), userID, hash); err != nil {
		s.writeDomainError(w, r, "reset password", err)
		return
	}

	s.audit(r.Context(), "reset a password", userID)
	w.WriteHeader(http.StatusNoContent)
}

// statusResponse is the service state the administration screen shows.
type statusResponse struct {
	Version       string                `json:"version"`
	SchemaVersion int64                 `json:"schema_version"`
	Users         statusUsersResponse   `json:"users"`
	Routing       statusRoutingResponse `json:"routing"`
	Geocoding     statusRoutingResponse `json:"geocoding"`
}

// statusRoutingResponse describes an external provider and its cache.
type statusRoutingResponse struct {
	Provider      string     `json:"provider"`
	Configured    bool       `json:"configured"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	Requests24h   int        `json:"requests_24h"`
	Errors24h     int        `json:"errors_24h"`
	DailyLimit    int        `json:"daily_limit"`
	CacheEntries  int64      `json:"cache_entries"`
	CacheHits     int64      `json:"cache_hits"`
}

// statusUsersResponse counts the instance's accounts.
type statusUsersResponse struct {
	Total  int64 `json:"total"`
	Active int64 `json:"active"`
	Admins int64 `json:"admins"`
}

// handleStatus reports the build, the schema, the accounts and routing. Media
// joins it when that part of the service is built.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	version, err := s.database.SchemaVersion(r.Context())
	if err != nil {
		s.internalError(w, r, "read schema version", err)
		return
	}
	stats, err := s.users.Stats(r.Context())
	if err != nil {
		s.internalError(w, r, "count users", err)
		return
	}
	var routingStats, geocodeStats routing.UsageStats
	if s.routingStats != nil {
		if routingStats, err = s.routingStats.Stats(r.Context(), s.now()); err != nil {
			s.internalError(w, r, "read routing stats", err)
			return
		}
	}
	if s.geocodeStats != nil {
		if geocodeStats, err = s.geocodeStats.Stats(r.Context(), s.now()); err != nil {
			s.internalError(w, r, "read geocoding stats", err)
			return
		}
	}
	writeJSON(w, s.logger, http.StatusOK, statusResponse{
		Version:       telemetry.Build().Version,
		SchemaVersion: version,
		Users:         statusUsersResponse{Total: stats.Total, Active: stats.Active, Admins: stats.Admins},
		Routing: statusRoutingResponse{
			Provider:      s.routingProvider,
			Configured:    s.routing != nil && s.routing.Enabled(),
			LastSuccessAt: routingStats.LastSuccessAt,
			LastErrorAt:   routingStats.LastErrorAt,
			Requests24h:   routingStats.Requests24h,
			Errors24h:     routingStats.Errors24h,
			DailyLimit:    s.routingDaily,
			CacheEntries:  routingStats.CacheEntries,
			CacheHits:     routingStats.CacheHits,
		},
		Geocoding: statusRoutingResponse{
			Provider:      s.geocodingProvider,
			Configured:    s.geocoder != nil && s.geocoder.Enabled(),
			LastSuccessAt: geocodeStats.LastSuccessAt,
			LastErrorAt:   geocodeStats.LastErrorAt,
			Requests24h:   geocodeStats.Requests24h,
			Errors24h:     geocodeStats.Errors24h,
			DailyLimit:    s.geocodeDaily,
			CacheEntries:  geocodeStats.CacheEntries,
			CacheHits:     geocodeStats.CacheHits,
		},
	})
}

// audit records an administrator's change to an account: who did it and to
// whom, never what the password was.
func (s *Server) audit(ctx context.Context, action string, target uuid.UUID) {
	s.logger.Info(action,
		slog.String("request_id", RequestIDFrom(ctx)),
		slog.String("admin_id", principalFrom(ctx).user.ID.String()),
		slog.String("user_id", target.String()))
}
