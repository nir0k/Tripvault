package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/mailer"
)

const passwordResetLifetime = 30 * time.Minute

// mailStatusResponse is the operator configuration, administrator switch and queue state.
type mailStatusResponse struct {
	Configured bool `json:"configured"`
	Enabled    bool `json:"enabled"`
	// SelfRegistration is the administrator's switch; registration is open
	// only while it is on and mail is configured and enabled.
	SelfRegistration bool       `json:"self_registration"`
	Queued           int64      `json:"queued"`
	Failed           int64      `json:"failed"`
	LastSuccessAt    *time.Time `json:"last_success_at"`
	LastErrorAt      *time.Time `json:"last_error_at"`
	LastError        string     `json:"last_error"`
}

// mailStatus reads the complete state shown to an administrator.
func (s *Server) mailStatus(r *http.Request) (mailStatusResponse, error) {
	response := mailStatusResponse{Configured: s.mailConfigured}
	if s.mail == nil {
		return response, nil
	}
	settings, err := s.mail.MailSettings(r.Context())
	if err != nil {
		return response, err
	}
	stats, err := s.mail.MailStats(r.Context())
	if err != nil {
		return response, err
	}
	response.Enabled = settings.Enabled
	response.SelfRegistration = settings.SelfRegistration
	response.Queued = stats.Queued
	response.Failed = stats.Failed
	response.LastSuccessAt = stats.LastSuccessAt
	response.LastErrorAt = stats.LastErrorAt
	response.LastError = stats.LastError
	return response, nil
}

// mailEnabled reports whether both the operator and administrator made delivery available.
func (s *Server) mailEnabled(r *http.Request) bool {
	if !s.mailConfigured || s.mail == nil {
		return false
	}
	settings, err := s.mail.MailSettings(r.Context())
	return err == nil && settings.Enabled
}

// updateMailRequest is the administrator-controlled mail switch.
type updateMailRequest struct {
	Enabled bool `json:"enabled"`
}

// handleUpdateMail enables or pauses delivery without changing SMTP credentials.
func (s *Server) handleUpdateMail(w http.ResponseWriter, r *http.Request) {
	var body updateMailRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Enabled && !s.mailConfigured {
		s.writeError(w, r, http.StatusConflict, "mail_not_configured", "SMTP is not configured by the operator")
		return
	}
	if s.mail == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "mail_unavailable", "Mail settings are unavailable")
		return
	}
	if _, err := s.mail.SetMailEnabled(r.Context(), body.Enabled); err != nil {
		s.internalError(w, r, "update mail settings", err)
		return
	}
	if body.Enabled && s.wakeMail != nil {
		s.wakeMail()
	}
	status, err := s.mailStatus(r)
	if err != nil {
		s.internalError(w, r, "read mail status", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, status)
}

// testMailRequest names where the administrator's test message is delivered.
type testMailRequest struct {
	Email string `json:"email"`
}

// handleTestMail verifies the configured SMTP connection even while delivery is paused.
func (s *Server) handleTestMail(w http.ResponseWriter, r *http.Request) {
	if !s.mailConfigured || s.mailSender == nil {
		s.writeError(w, r, http.StatusConflict, "mail_not_configured", "SMTP is not configured by the operator")
		return
	}
	var body testMailRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		s.writeDomainError(w, r, "validate test email", err)
		return
	}
	content := mailer.SMTPTest(principalFrom(r.Context()).user.Locale)
	if err := s.mailSender.Send(r.Context(), mailer.Message(email, content)); err != nil {
		s.logger.Warn("test mail delivery failed", slog.Any("error", err))
		s.writeError(w, r, http.StatusBadGateway, "mail_delivery_failed", "The SMTP server did not accept the test message")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// passwordResetRequest names the account that may need recovery.
type passwordResetRequest struct {
	Email string `json:"email"`
}

// handleRequestPasswordReset answers every address alike and queues mail only for an eligible account.
func (s *Server) handleRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body passwordResetRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	email, normalizeErr := domain.NormalizeEmail(body.Email)
	accountKey := email
	if normalizeErr != nil {
		accountKey = "invalid"
	}
	if wait, ok := s.passwordResetsPerClient.take(clientAddress(r)); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}
	if wait, ok := s.passwordResetsPerAccount.take(accountKey); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}
	// The public answer remains the same when mail is disabled, the address is
	// malformed, or no account has it.
	if normalizeErr != nil || !s.mailEnabled(r) || s.invitations == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	user, err := s.invitations.PasswordResetUser(r.Context(), email)
	if errors.Is(err, domain.ErrNotFound) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if err != nil {
		s.internalError(w, r, "find password recovery account", err)
		return
	}
	token, hash, err := auth.NewActionToken()
	if err != nil {
		s.internalError(w, r, "generate password reset token", err)
		return
	}
	now := s.now()
	expiresAt := now.Add(passwordResetLifetime)
	link := s.mailPublicURL + "/reset-password#token=" + token
	message := mailer.Message(user.Email, mailer.PasswordReset(user.Locale, user.DisplayName, link))
	message.DiscardAfter = &expiresAt
	if err := s.invitations.CreatePasswordReset(r.Context(), user.ID, hash, expiresAt, message); err != nil {
		s.internalError(w, r, "create password reset", err)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	w.WriteHeader(http.StatusAccepted)
}

// completePasswordResetRequest carries a one-time credential and the replacement password.
type completePasswordResetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleCompletePasswordReset consumes a recovery credential and ends every existing session.
func (s *Server) handleCompletePasswordReset(w http.ResponseWriter, r *http.Request) {
	var body completePasswordResetRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Token) == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "A reset token is required")
		return
	}
	tokenHash := auth.HashActionToken(body.Token)
	if err := s.invitations.PasswordResetValid(r.Context(), tokenHash, s.now()); errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "reset_closed", "This password reset link is invalid, expired or already used")
		return
	} else if err != nil {
		s.internalError(w, r, "check password reset token", err)
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate recovered password", err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.internalError(w, r, "hash recovered password", err)
		return
	}
	user, err := s.invitations.ResetPassword(r.Context(), tokenHash, hash, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "reset_closed", "This password reset link is invalid, expired or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "complete password reset", err)
		return
	}
	// The successful reset is itself a security event. It is queued after the
	// reset; failure to send it never rolls the password back.
	s.queueSecurityMail(r, user, mailer.PasswordChangedByRecovery(user.Locale))
	w.WriteHeader(http.StatusNoContent)
}
