package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakeRegistrations keeps self-registrations in memory and answers
// confirmations with a fixed result.
type fakeRegistrations struct {
	registered []domain.User
	messages   []domain.MailMessage
	// wait is what the account has left before another message.
	wait time.Duration
	// confirmed is returned by both confirmations unless confirmErr is set.
	confirmed  domain.User
	confirmErr error
	codes      [][]byte
}

// RegisterAccount records the account and its message.
func (f *fakeRegistrations) RegisterAccount(_ context.Context, user domain.User, _ domain.EmailVerification,
	message domain.MailMessage, _ time.Time, _ time.Duration) error {
	f.registered = append(f.registered, user)
	f.messages = append(f.messages, message)
	return nil
}

// VerificationWait returns the configured wait.
func (f *fakeRegistrations) VerificationWait(context.Context, uuid.UUID, time.Time, time.Duration) (time.Duration, error) {
	return f.wait, nil
}

// ResendVerification records the message unless the configured wait forbids it.
func (f *fakeRegistrations) ResendVerification(_ context.Context, _ domain.EmailVerification,
	message domain.MailMessage, _ time.Time, _ time.Duration) (time.Duration, error) {
	if f.wait > 0 {
		return f.wait, nil
	}
	f.messages = append(f.messages, message)
	return 0, nil
}

// ConfirmEmailByToken returns the configured result.
func (f *fakeRegistrations) ConfirmEmailByToken(context.Context, []byte, time.Time) (domain.User, error) {
	return f.confirmed, f.confirmErr
}

// ConfirmEmailByCode records the digest and returns the configured result.
func (f *fakeRegistrations) ConfirmEmailByCode(_ context.Context, _ string, codeHash []byte, _ time.Time) (domain.User, error) {
	f.codes = append(f.codes, codeHash)
	return f.confirmed, f.confirmErr
}

// newRegistrationServer builds a server whose mail and registrations live in memory.
func newRegistrationServer(user domain.User, mail *fakeMail, registrations *fakeRegistrations, configured bool) *Server {
	return NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: &fakeAuth{user: user}, Users: fakeUsers{}, Database: fakeProbe{}, Mail: mail,
		Registrations: registrations, MailConfigured: configured, MailPublicURL: "https://trips.example.com",
	})
}

// TestRegistrationSwitchNeedsMail checks an administrator can open
// registration only while mail is configured and delivered, and that turning
// mail off closes it without forgetting the switch.
func TestRegistrationSwitchNeedsMail(t *testing.T) {
	admin := domain.User{ID: uuid.New(), Email: "admin@example.com", IsActive: true, IsAdmin: true}

	unconfigured := newRegistrationServer(admin, &fakeMail{}, &fakeRegistrations{}, false)
	recorder := send(unconfigured, http.MethodPatch, "/api/v1/admin/registration", "good", `{"enabled":true}`)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "mail_not_configured" {
		t.Fatalf("unconfigured: %d %s", recorder.Code, recorder.Body.String())
	}

	mail := &fakeMail{}
	s := newRegistrationServer(admin, mail, &fakeRegistrations{}, true)
	recorder = send(s, http.MethodPatch, "/api/v1/admin/registration", "good", `{"enabled":true}`)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "mail_disabled" {
		t.Fatalf("mail disabled: %d %s", recorder.Code, recorder.Body.String())
	}

	mail.settings.Enabled = true
	recorder = send(s, http.MethodPatch, "/api/v1/admin/registration", "good", `{"enabled":true}`)
	var status mailStatusResponse
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &status) != nil || !status.SelfRegistration {
		t.Fatalf("enable: %d %s", recorder.Code, recorder.Body.String())
	}
	if !clientSelfRegistration(t, s) {
		t.Fatal("open registration is not offered to clients")
	}

	mail.settings.Enabled = false
	if clientSelfRegistration(t, s) {
		t.Fatal("registration stayed open with mail disabled")
	}
	recorder = send(s, http.MethodPost, "/api/v1/auth/register", "",
		`{"display_name":"Sam","email":"sam@example.com","password":"a long enough password"}`)
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "registration_closed" {
		t.Fatalf("register while closed: %d %s", recorder.Code, recorder.Body.String())
	}
}

// clientSelfRegistration reads whether the client configuration offers registration.
func clientSelfRegistration(t *testing.T, s *Server) bool {
	t.Helper()
	var config clientConfigResponse
	recorder := send(s, http.MethodGet, "/api/v1/config", "", "")
	if err := json.Unmarshal(recorder.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	return config.SelfRegistration
}

// TestRegisterSendsLinkAndCode checks a registration creates an inactive
// account and a message carrying both ways to confirm it, discarded once they expire.
func TestRegisterSendsLinkAndCode(t *testing.T) {
	registrations := &fakeRegistrations{}
	mail := &fakeMail{settings: domain.MailSettings{Enabled: true, SelfRegistration: true}}
	s := newRegistrationServer(domain.User{}, mail, registrations, true)

	recorder := send(s, http.MethodPost, "/api/v1/auth/register", "",
		`{"display_name":" Sam ","email":"Sam@Example.com","password":"a long enough password","locale":"en"}`)
	if recorder.Code != http.StatusAccepted || len(registrations.registered) != 1 {
		t.Fatalf("register: %d %s", recorder.Code, recorder.Body.String())
	}
	var sent verificationSentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &sent); err != nil || sent.ResendAvailableIn != 300 {
		t.Fatalf("unexpected resend timer: %s", recorder.Body.String())
	}
	user := registrations.registered[0]
	if user.Email != "sam@example.com" || user.DisplayName != "Sam" || user.IsActive || user.PasswordHash == "" {
		t.Fatalf("unexpected account: %+v", user)
	}
	message := registrations.messages[0]
	if message.Recipient != "sam@example.com" || message.DiscardAfter == nil ||
		!strings.Contains(message.TextBody, "https://trips.example.com/verify-email#token=") {
		t.Fatalf("unexpected message: %+v", message)
	}
}

// TestUnconfirmedSignInAndResend checks a right password of an unconfirmed
// account is told to confirm it, and that another message is refused until
// the cooldown has passed and only to its owner.
func TestUnconfirmedSignInAndResend(t *testing.T) {
	since := time.Now()
	user := domain.User{ID: uuid.New(), Email: "sam@example.com", DisplayName: "Sam", EmailUnverifiedSince: &since}
	registrations := &fakeRegistrations{wait: 90 * time.Second}
	mail := &fakeMail{settings: domain.MailSettings{Enabled: true}}
	s := newRegistrationServer(user, mail, registrations, true)
	credentials := `{"email":"sam@example.com","password":"` + fakePassword + `"}`

	recorder := send(s, http.MethodPost, "/api/v1/auth/login", "", credentials)
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "email_not_verified" {
		t.Fatalf("login: %d %s", recorder.Code, recorder.Body.String())
	}
	var refused ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &refused); err != nil || refused.Details["resend_available_in"] != float64(90) {
		t.Fatalf("login does not tell the wait: %s", recorder.Body.String())
	}
	if len(registrations.messages) != 0 {
		t.Fatal("signing in sent a message by itself")
	}

	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email/resend", "", `{"email":"sam@example.com","password":"wrong"}`)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("resend with a wrong password: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email/resend", "", credentials)
	if recorder.Code != http.StatusTooManyRequests || errorCode(t, recorder) != "resend_too_soon" ||
		recorder.Header().Get("Retry-After") != "90" {
		t.Fatalf("resend within cooldown: %d %s", recorder.Code, recorder.Body.String())
	}

	registrations.wait = 0
	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email/resend", "", credentials)
	if recorder.Code != http.StatusAccepted || len(registrations.messages) != 1 {
		t.Fatalf("resend: %d %s", recorder.Code, recorder.Body.String())
	}

	confirmed := newRegistrationServer(domain.User{Email: "sam@example.com"}, mail, registrations, true)
	recorder = send(confirmed, http.MethodPost, "/api/v1/auth/verify-email/resend", "", credentials)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "already_verified" {
		t.Fatalf("resend for a confirmed account: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestVerifyEmailByLinkOrCode checks both ways of confirming and how each
// reports a credential that no longer works.
func TestVerifyEmailByLinkOrCode(t *testing.T) {
	registrations := &fakeRegistrations{confirmed: domain.User{Email: "sam@example.com"}}
	s := newRegistrationServer(domain.User{}, &fakeMail{}, registrations, true)

	recorder := send(s, http.MethodPost, "/api/v1/auth/verify-email", "", `{"token":"abc"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "sam@example.com") {
		t.Fatalf("link: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email", "", `{"email":"Sam@example.com","code":"123 456"}`)
	if recorder.Code != http.StatusOK || len(registrations.codes) != 1 {
		t.Fatalf("code: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email", "", `{"email":"sam@example.com"}`)
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_request" {
		t.Fatalf("missing code: %d %s", recorder.Code, recorder.Body.String())
	}

	registrations.confirmErr = domain.ErrTokenInvalid
	recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email", "", `{"token":"abc"}`)
	if recorder.Code != http.StatusGone || errorCode(t, recorder) != "verification_closed" {
		t.Fatalf("closed link: %d %s", recorder.Code, recorder.Body.String())
	}
	for _, body := range []string{`{"email":"sam@example.com","code":"000000"}`, `{"email":"not an address","code":"1"}`} {
		recorder = send(s, http.MethodPost, "/api/v1/auth/verify-email", "", body)
		if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_code" {
			t.Fatalf("wrong code %s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
}
