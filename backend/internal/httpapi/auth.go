package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// userResponse is the public view of an account. The password hash is never
// part of it.
type userResponse struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	DisplayName        string `json:"display_name"`
	IsAdmin            bool   `json:"is_admin"`
	IsActive           bool   `json:"is_active"`
	MustChangePassword bool   `json:"must_change_password"`
	Locale             string `json:"locale"`
	Theme              string `json:"theme"`
	Units              string `json:"units"`
	DateFormat         string `json:"date_format"`
	TimeFormat         string `json:"time_format"`
	DefaultCurrency    string `json:"default_currency"`
	// HasAvatar says whether the account wears a picture, and AvatarUpdatedAt
	// when it last changed: the bytes are fetched separately, and this is what
	// tells a client to fetch them again.
	HasAvatar       bool       `json:"has_avatar"`
	AvatarUpdatedAt *time.Time `json:"avatar_updated_at"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// newUserResponse maps an account onto its public representation.
func newUserResponse(user domain.User) userResponse {
	return userResponse{
		ID:                 user.ID.String(),
		Email:              user.Email,
		DisplayName:        user.DisplayName,
		IsAdmin:            user.IsAdmin,
		IsActive:           user.IsActive,
		MustChangePassword: user.MustChangePassword,
		Locale:             user.Locale,
		Theme:              string(user.Theme),
		Units:              string(user.Units.OrDefault()),
		DateFormat:         string(user.DateFormat.OrDefault()),
		TimeFormat:         string(user.TimeFormat.OrDefault()),
		DefaultCurrency:    user.DefaultCurrency,
		HasAvatar:          user.AvatarKey != "",
		AvatarUpdatedAt:    user.AvatarUpdatedAt,
		LastLoginAt:        user.LastLoginAt,
		CreatedAt:          user.CreatedAt,
	}
}

// sessionResponse is what a client receives after signing in or refreshing.
type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int64        `json:"expires_in"`
	SessionID    string       `json:"session_id"`
	User         userResponse `json:"user"`
}

// newSessionResponse maps a session onto its wire representation.
func newSessionResponse(session auth.Session) sessionResponse {
	return sessionResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    session.ExpiresIn,
		SessionID:    session.ID.String(),
		User:         newUserResponse(session.User),
	}
}

// loginRequest is the body of POST /api/v1/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the body of the refresh and logout endpoints.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// handleLogin exchanges credentials for a session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Email == "" || body.Password == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Both email and password are required")
		return
	}

	// An address that is not one cannot have an account; it is answered like a
	// wrong password rather than as a validation error, and still counted.
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		email = body.Email
	}

	client := clientAddress(r)
	if wait, ok := s.signIns.admit(client, email); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}

	session, err := s.auth.Login(r.Context(), email, body.Password, r.UserAgent())
	if errors.Is(err, domain.ErrInvalidCredentials) {
		s.signIns.failed(client)
		s.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "The email or password is incorrect")
		return
	}
	if err != nil {
		s.internalError(w, r, "login", err)
		return
	}

	s.signIns.succeeded(email)
	writeJSON(w, s.logger, http.StatusOK, newSessionResponse(session))
}

// handleRefresh exchanges a refresh token for a new token pair.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.RefreshToken == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "A refresh token is required")
		return
	}

	session, err := s.auth.Refresh(r.Context(), body.RefreshToken)
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusUnauthorized, "invalid_token",
			"The refresh token is invalid, expired or has already been used")
		return
	}
	if err != nil {
		s.internalError(w, r, "refresh", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newSessionResponse(session))
}

// handleLogout ends the session a refresh token belongs to. It answers 204 even
// for a token the server does not know, so a client can always sign out.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.RefreshToken != "" {
		if err := s.auth.Logout(r.Context(), body.RefreshToken); err != nil {
			s.internalError(w, r, "logout", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
