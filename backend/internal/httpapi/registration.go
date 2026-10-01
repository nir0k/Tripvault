package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/mailer"
)

const (
	// verificationLifetime is how long the link and the code of one
	// confirmation message work.
	verificationLifetime = time.Hour
	// verificationResendCooldown is the least time between two confirmation
	// messages to one account.
	verificationResendCooldown = 5 * time.Minute
)

// registrationOpen reports whether people may register themselves now: the
// administrator allowed it and mail, which confirms their address, is delivered.
func (s *Server) registrationOpen(r *http.Request) bool {
	if !s.mailConfigured || s.mail == nil || s.registrations == nil {
		return false
	}
	settings, err := s.mail.MailSettings(r.Context())
	return err == nil && settings.Enabled && settings.SelfRegistration
}

// resendWaitSeconds rounds a remaining wait up to whole seconds for a client's timer.
func resendWaitSeconds(wait time.Duration) int {
	return max(int(math.Ceil(wait.Seconds())), 0)
}

// verificationSentResponse tells a client when it may ask for another message.
type verificationSentResponse struct {
	ResendAvailableIn int `json:"resend_available_in"`
}

// newVerification renders a fresh confirmation for an address.
//
// Arguments:
//   - email: the normalised address being confirmed.
//   - name: the display name the message greets.
//   - language: the language of the message.
//   - now: the sending time.
//
// Returns:
//   - the confirmation to store, without its account.
//   - the rendered message, discarded once the confirmation expires.
//   - an error if the random source fails.
func (s *Server) newVerification(email, name, language string, now time.Time) (domain.EmailVerification, domain.MailMessage, error) {
	token, tokenHash, err := auth.NewActionToken()
	if err != nil {
		return domain.EmailVerification{}, domain.MailMessage{}, err
	}
	code, codeHash, err := auth.NewVerificationCode(email)
	if err != nil {
		return domain.EmailVerification{}, domain.MailMessage{}, err
	}
	expiresAt := now.Add(verificationLifetime)
	link := s.mailPublicURL + "/verify-email#token=" + token
	message := mailer.Message(email, mailer.EmailVerification(language, name, link, code))
	message.DiscardAfter = &expiresAt
	verification := domain.EmailVerification{TokenHash: tokenHash, CodeHash: codeHash, ExpiresAt: expiresAt, SentAt: now}
	return verification, message, nil
}

// registerRequest is the body of POST /api/v1/auth/register.
type registerRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	Locale      string `json:"locale"`
}

// handleRegister creates an inactive account and sends the message that confirms its address.
//
// Every acceptable request is answered alike, whether the address was free,
// waiting for confirmation or already in use, so registering cannot tell
// anybody which addresses have accounts.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.registrationOpen(r) {
		s.writeError(w, r, http.StatusForbidden, "registration_closed", "Registration is closed on this instance")
		return
	}
	var body registerRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	name, err := domain.NormalizeDisplayName(body.DisplayName)
	if err != nil {
		s.writeDomainError(w, r, "validate registration", err)
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		s.writeDomainError(w, r, "validate registration", err)
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate registration", err)
		return
	}
	language := strings.TrimSpace(body.Locale)
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate registration", err)
		return
	}
	if wait, ok := s.registrationsPerClient.take(clientAddress(r)); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}
	if wait, ok := s.registrationsPerAccount.take(email); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.writeDomainError(w, r, "hash registered password", err)
		return
	}
	now := s.now()
	verification, message, err := s.newVerification(email, name, language, now)
	if err != nil {
		s.internalError(w, r, "create email confirmation", err)
		return
	}
	user := domain.User{ID: uuid.Must(uuid.NewV7()), Email: email, DisplayName: name, PasswordHash: hash,
		EmailNotifications: true, Locale: language, Theme: domain.ThemeAuto, Units: domain.UnitsKilometres,
		DefaultCurrency: domain.DefaultCurrency}
	if err := s.registrations.RegisterAccount(r.Context(), user, verification, message, now,
		verificationResendCooldown); err != nil {
		s.internalError(w, r, "register account", err)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	writeJSON(w, s.logger, http.StatusAccepted,
		verificationSentResponse{ResendAvailableIn: resendWaitSeconds(verificationResendCooldown)})
}

// verifyEmailRequest confirms an address either with the token of the link or
// with the address and the code of the message.
type verifyEmailRequest struct {
	Token string `json:"token"`
	Email string `json:"email"`
	Code  string `json:"code"`
}

// verifyEmailResponse names the confirmed address, so the page can sign in with it.
type verifyEmailResponse struct {
	Email string `json:"email"`
}

// handleVerifyEmail confirms a self-registered address and activates its account.
//
// It opens no session: a link may be opened on any device, and signing in
// stays the business of the password.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body verifyEmailRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" && (strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.Code) == "") {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Either a token or an email and a code are required")
		return
	}
	if s.registrations == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "mail_unavailable", "Mail settings are unavailable")
		return
	}
	if wait, ok := s.verificationsPerClient.take(clientAddress(r)); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}

	var user domain.User
	var err error
	if token != "" {
		user, err = s.registrations.ConfirmEmailByToken(r.Context(), auth.HashActionToken(token), s.now())
		if errors.Is(err, domain.ErrTokenInvalid) {
			s.writeError(w, r, http.StatusGone, "verification_closed",
				"This confirmation link is invalid, expired or replaced by a newer one")
			return
		}
	} else {
		// A malformed address cannot have a code; it is answered like a wrong one.
		email, normalizeErr := domain.NormalizeEmail(body.Email)
		err = domain.ErrTokenInvalid
		if normalizeErr == nil {
			user, err = s.registrations.ConfirmEmailByCode(r.Context(), email, auth.HashVerificationCode(email, body.Code), s.now())
		}
		if errors.Is(err, domain.ErrTokenInvalid) {
			s.writeError(w, r, http.StatusBadRequest, "invalid_code",
				"The code is wrong or no longer valid; request a new message if it keeps failing")
			return
		}
	}
	if err != nil {
		s.internalError(w, r, "confirm email", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, verifyEmailResponse{Email: user.Email})
}

// resendVerificationRequest proves the account with its password, so asking
// for another message cannot tell anybody which addresses wait for one.
type resendVerificationRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleResendVerification sends a new confirmation message at most once per cooldown.
func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	var body resendVerificationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Email == "" || body.Password == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Both email and password are required")
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		email = body.Email
	}
	client := clientAddress(r)
	if wait, ok := s.signIns.admit(client, email); !ok {
		s.writeTooManyAttempts(w, r, wait)
		return
	}
	user, err := s.auth.Credentials(r.Context(), email, body.Password)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		s.signIns.failed(client)
		s.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "The email or password is incorrect")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "check credentials for confirmation", err)
		return
	}
	if user.EmailUnverifiedSince == nil {
		s.writeError(w, r, http.StatusConflict, "already_verified", "The address is already confirmed")
		return
	}
	if !s.mailEnabled(r) || s.registrations == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "mail_disabled", "Mail delivery is disabled on this instance")
		return
	}

	now := s.now()
	verification, message, err := s.newVerification(user.Email, user.DisplayName, user.Locale, now)
	if err != nil {
		s.internalError(w, r, "create email confirmation", err)
		return
	}
	verification.UserID = user.ID
	wait, err := s.registrations.ResendVerification(r.Context(), verification, message, now, verificationResendCooldown)
	if errors.Is(err, domain.ErrNotFound) {
		s.writeError(w, r, http.StatusConflict, "already_verified", "The address is already confirmed")
		return
	}
	if err != nil {
		s.internalError(w, r, "resend email confirmation", err)
		return
	}
	if wait > 0 {
		seconds := max(resendWaitSeconds(wait), 1)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		s.writeErrorDetails(w, r, http.StatusTooManyRequests, "resend_too_soon",
			"A confirmation message was sent recently. Try again later.", map[string]any{"retry_after": seconds})
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	writeJSON(w, s.logger, http.StatusAccepted,
		verificationSentResponse{ResendAvailableIn: resendWaitSeconds(verificationResendCooldown)})
}

// updateRegistrationRequest is the administrator's registration switch.
type updateRegistrationRequest struct {
	Enabled bool `json:"enabled"`
}

// handleUpdateRegistration opens or closes self-registration. It can be
// opened only while mail is configured and delivered, since every account it
// makes is confirmed by email.
func (s *Server) handleUpdateRegistration(w http.ResponseWriter, r *http.Request) {
	var body updateRegistrationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if s.mail == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "mail_unavailable", "Mail settings are unavailable")
		return
	}
	if body.Enabled {
		if !s.mailConfigured {
			s.writeError(w, r, http.StatusConflict, "mail_not_configured", "SMTP is not configured by the operator")
			return
		}
		if !s.mailEnabled(r) {
			s.writeError(w, r, http.StatusConflict, "mail_disabled", "Mail delivery must be enabled first")
			return
		}
	}
	if _, err := s.mail.SetSelfRegistration(r.Context(), body.Enabled); err != nil {
		s.internalError(w, r, "update self-registration", err)
		return
	}
	status, err := s.mailStatus(r)
	if err != nil {
		s.internalError(w, r, "read mail status", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, status)
}
