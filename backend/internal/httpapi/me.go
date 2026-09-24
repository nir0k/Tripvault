package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// handleGetMe returns the signed-in account.
func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, newUserResponse(principalFrom(r.Context()).user))
}

// updateMeRequest is the body of PATCH /api/v1/me. Every field is optional, so
// the theme switch and the language picker can each change their own setting
// without sending - and overwriting - the other.
type updateMeRequest struct {
	DisplayName     *string `json:"display_name"`
	Locale          *string `json:"locale"`
	Theme           *string `json:"theme"`
	Units           *string `json:"units"`
	DateFormat      *string `json:"date_format"`
	TimeFormat      *string `json:"time_format"`
	DefaultCurrency *string `json:"default_currency"`
}

// handleUpdateMe changes the signed-in account's own settings. They live on the
// server so a preference set on one device follows the user to the next.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user

	var body updateMeRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}

	profile := domain.Profile{
		DisplayName:     user.DisplayName,
		Locale:          user.Locale,
		Theme:           user.Theme,
		Units:           user.Units.OrDefault(),
		DateFormat:      user.DateFormat.OrDefault(),
		TimeFormat:      user.TimeFormat.OrDefault(),
		DefaultCurrency: user.DefaultCurrency,
	}
	if body.DisplayName != nil {
		profile.DisplayName = strings.TrimSpace(*body.DisplayName)
	}
	if body.Locale != nil {
		profile.Locale = strings.TrimSpace(*body.Locale)
	}
	if body.Theme != nil {
		profile.Theme = domain.Theme(strings.TrimSpace(*body.Theme))
	}
	if body.Units != nil {
		profile.Units = domain.Units(strings.TrimSpace(*body.Units))
	}
	if body.DateFormat != nil {
		profile.DateFormat = domain.DateFormat(strings.TrimSpace(*body.DateFormat))
	}
	if body.TimeFormat != nil {
		profile.TimeFormat = domain.TimeFormat(strings.TrimSpace(*body.TimeFormat))
	}
	if body.DefaultCurrency != nil {
		profile.DefaultCurrency = strings.ToUpper(strings.TrimSpace(*body.DefaultCurrency))
	}
	if err := profile.Validate(); err != nil {
		s.writeDomainError(w, r, "validate profile", err)
		return
	}

	updated, err := s.users.UpdateProfile(r.Context(), user.ID, profile)
	if err != nil {
		s.writeDomainError(w, r, "update profile", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newUserResponse(updated))
}

// changePasswordRequest is the body of POST /api/v1/me/password.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword replaces the signed-in account's password. It is the one
// action open to an account holding a temporary password.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())

	var body changePasswordRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}

	err := s.auth.ChangePassword(r.Context(), p.user, p.sessionID, body.CurrentPassword, body.NewPassword)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		s.writeErrorDetails(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"current_password: is incorrect",
			map[string]any{"field": "current_password", "reason": "incorrect"})
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "change password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sessionItemResponse is one signed-in device in the session list.
type sessionItemResponse struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Current marks the session making the request, so the interface can say
	// "this device" rather than let somebody sign themselves out by accident.
	Current bool `json:"current"`
}

// handleListSessions returns the signed-in account's live sessions.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())

	sessions, err := s.sessions.ListActive(r.Context(), p.user.ID, s.now())
	if err != nil {
		s.internalError(w, r, "list sessions", err)
		return
	}

	items := make([]sessionItemResponse, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, sessionItemResponse{
			ID:         session.ID.String(),
			UserAgent:  session.UserAgent,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
			ExpiresAt:  session.ExpiresAt,
			Current:    session.ID == p.sessionID,
		})
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[sessionItemResponse]{Items: items})
}

// handleRevokeSession ends one of the signed-in account's sessions, including
// the current one. Its access token stops working on the next request.
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := s.pathUUID(w, r, "sessionID")
	if !ok {
		return
	}
	if err := s.sessions.Revoke(r.Context(), principalFrom(r.Context()).user.ID, sessionID, s.now()); err != nil {
		s.writeDomainError(w, r, "revoke session", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
