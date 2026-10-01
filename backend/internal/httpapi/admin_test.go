package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/mailer"
)

// fakeStorage keeps storage policy and usage in memory for administration tests.
type fakeStorage struct {
	usage      domain.StorageUsage
	reserveErr error
}

// fakeMail keeps the administrator switch and queue statistics in memory.
type fakeMail struct {
	settings domain.MailSettings
	stats    domain.MailStats
	queued   []domain.MailMessage
}

// MailSettings returns the current in-memory delivery policy.
func (f *fakeMail) MailSettings(context.Context) (domain.MailSettings, error) {
	return f.settings, nil
}

// SetMailEnabled changes the in-memory delivery policy.
func (f *fakeMail) SetMailEnabled(_ context.Context, enabled bool) (domain.MailSettings, error) {
	f.settings.Enabled = enabled
	return f.settings, nil
}

// SetSelfRegistration changes the in-memory registration switch.
func (f *fakeMail) SetSelfRegistration(_ context.Context, enabled bool) (domain.MailSettings, error) {
	f.settings.SelfRegistration = enabled
	return f.settings, nil
}

// EnqueueMail records a message accepted by the in-memory queue.
func (f *fakeMail) EnqueueMail(_ context.Context, message domain.MailMessage) error {
	f.queued = append(f.queued, message)
	return nil
}

// MailStats returns the configured in-memory counters.
func (f *fakeMail) MailStats(context.Context) (domain.MailStats, error) { return f.stats, nil }

// fakeMailSender records direct SMTP tests made by an administrator.
type fakeMailSender struct {
	sent []domain.MailMessage
}

// Send records a message as accepted by the fake transport.
func (f *fakeMailSender) Send(_ context.Context, message domain.MailMessage) error {
	f.sent = append(f.sent, message)
	return nil
}

// Usage returns the current in-memory storage state.
func (f *fakeStorage) Usage(context.Context) (domain.StorageUsage, error) { return f.usage, nil }

// Settings returns the current in-memory trip allowance.
func (f *fakeStorage) Settings(context.Context) (domain.StorageSettings, error) {
	return domain.StorageSettings{TripQuotaBytes: f.usage.TripQuotaBytes}, nil
}

// SetTripQuota changes the in-memory trip allowance.
func (f *fakeStorage) SetTripQuota(_ context.Context, quota int64) (domain.StorageSettings, error) {
	f.usage.TripQuotaBytes = quota
	return domain.StorageSettings{TripQuotaBytes: quota}, nil
}

// TrackBytes reports no existing track in administration tests.
func (f *fakeStorage) TrackBytes(context.Context, uuid.UUID) (int64, error) { return 0, nil }

// ObjectBytes reports no existing media object in administration tests.
func (f *fakeStorage) ObjectBytes(context.Context, string) (int64, error) { return 0, nil }

// Reserve accepts a storage operation and returns a no-op release.
func (f *fakeStorage) Reserve(context.Context, int64) (func(), error) {
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	return func() {}, nil
}

// TestStatusNamesTheConfiguredProviders checks the status screen says which
// services this instance talks to, including one that is switched off: a hosted
// service with no key builds no client to ask, and the screen must still name it.
func TestStatusNamesTheConfiguredProviders(t *testing.T) {
	admin := domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:              &fakeAuth{user: admin},
		Users:             fakeUsers{},
		Database:          fakeProbe{},
		RoutingProvider:   "osrm",
		GeocodingProvider: "nominatim",
		RoutingDailyLimit: 0,
	})

	recorder := send(s, http.MethodGet, "/api/v1/admin/status", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}

	var status statusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("response %q is not a status: %v", recorder.Body.String(), err)
	}
	if status.Routing.Provider != "osrm" || status.Geocoding.Provider != "nominatim" {
		t.Errorf("the providers are reported as %s and %s",
			status.Routing.Provider, status.Geocoding.Provider)
	}
	// Neither service was built here, so neither counts as configured.
	if status.Routing.Configured || status.Geocoding.Configured {
		t.Error("a service that was never built reports itself as configured")
	}
	if status.Routing.DailyLimit != 0 {
		t.Errorf("the daily limit is %d, want none", status.Routing.DailyLimit)
	}
}

// TestStorageStatusAndUpdate checks the operator's limit is reported but only
// the per-trip policy can be changed through administration.
func TestStorageStatusAndUpdate(t *testing.T) {
	admin := domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}
	storage := &fakeStorage{usage: domain.StorageUsage{
		MediaBytes: 10, DatabaseBytes: 5, LimitBytes: 100, TripQuotaBytes: 20,
	}}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: &fakeAuth{user: admin}, Users: fakeUsers{}, Database: fakeProbe{}, Storage: storage,
	})

	recorder := send(s, http.MethodGet, "/api/v1/admin/status", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body.String())
	}
	var status statusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Storage.UsedBytes != 15 || status.Storage.LimitBytes != 100 || status.Storage.TripQuotaBytes != 20 {
		t.Fatalf("unexpected storage status: %+v", status.Storage)
	}

	recorder = send(s, http.MethodPatch, "/api/v1/admin/storage", "good", `{"trip_quota_mb":3}`)
	if recorder.Code != http.StatusOK || storage.usage.TripQuotaBytes != 3*1024*1024 {
		t.Fatalf("update: %d %s, quota %d", recorder.Code, recorder.Body.String(), storage.usage.TripQuotaBytes)
	}
	recorder = send(s, http.MethodPatch, "/api/v1/admin/storage", "good", `{"trip_quota_mb":-1}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("negative quota: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestMailStatusSwitchAndTest checks SMTP credentials stay operator-owned while
// an administrator can pause delivery and test the transport independently.
func TestMailStatusSwitchAndTest(t *testing.T) {
	admin := domain.User{ID: uuid.New(), Email: "admin@example.com", Locale: "en", IsActive: true, IsAdmin: true}
	mail := &fakeMail{stats: domain.MailStats{Queued: 3, Failed: 1}}
	sender := &fakeMailSender{}
	wakes := 0
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: &fakeAuth{user: admin}, Users: fakeUsers{}, Database: fakeProbe{}, Mail: mail,
		MailConfigured: true, MailSender: sender, WakeMail: func() { wakes++ },
	})

	recorder := send(s, http.MethodPatch, "/api/v1/admin/mail", "good", `{"enabled":true}`)
	if recorder.Code != http.StatusOK || !mail.settings.Enabled || wakes != 1 {
		t.Fatalf("enable: %d %s settings=%+v wakes=%d", recorder.Code, recorder.Body.String(), mail.settings, wakes)
	}
	var status mailStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.Enabled || status.Queued != 3 || status.Failed != 1 {
		t.Fatalf("unexpected mail status: %+v", status)
	}

	recorder = send(s, http.MethodPatch, "/api/v1/admin/mail", "good", `{"enabled":false}`)
	if recorder.Code != http.StatusOK || mail.settings.Enabled {
		t.Fatalf("disable: %d %s settings=%+v", recorder.Code, recorder.Body.String(), mail.settings)
	}
	recorder = send(s, http.MethodPost, "/api/v1/admin/mail/test", "good", `{"email":"owner@example.com"}`)
	if recorder.Code != http.StatusNoContent || len(sender.sent) != 1 || sender.sent[0].Recipient != "owner@example.com" {
		t.Fatalf("test while disabled: %d %s sent=%+v", recorder.Code, recorder.Body.String(), sender.sent)
	}

	unconfigured := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: &fakeAuth{user: admin}, Users: fakeUsers{}, Database: fakeProbe{}, Mail: &fakeMail{},
	})
	recorder = send(unconfigured, http.MethodPatch, "/api/v1/admin/mail", "good", `{"enabled":true}`)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "mail_not_configured" {
		t.Fatalf("unconfigured mail was enabled: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestPasswordResetRequestDoesNotRevealDisabledMail checks the public response
// remains generic for valid and malformed addresses when delivery is paused.
func TestPasswordResetRequestDoesNotRevealDisabledMail(t *testing.T) {
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Mail: &fakeMail{}, MailConfigured: true,
	})
	for _, body := range []string{`{"email":"person@example.com"}`, `{"email":"not an address"}`} {
		recorder := send(s, http.MethodPost, "/api/v1/auth/password-reset/request", "", body)
		if recorder.Code != http.StatusAccepted || recorder.Body.Len() != 0 {
			t.Fatalf("request %s disclosed state: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
}

// TestPasswordResetRequestLimitsAccountsAndClients checks neither distributed
// requests to one mailbox nor one client targeting many mailboxes can flood
// the recovery endpoint.
func TestPasswordResetRequestLimitsAccountsAndClients(t *testing.T) {
	newServer := func() *Server {
		return NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
			Mail: &fakeMail{}, MailConfigured: true,
		})
	}
	request := func(s *Server, address, email string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset/request",
			strings.NewReader(`{"email":"`+email+`"}`))
		r.Header.Set("X-Real-IP", address)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		return w
	}

	s := newServer()
	for i := range passwordResetsPerAccount {
		if got := request(s, "192.0.2."+strconv.Itoa(i+1), "person@example.com").Code; got != http.StatusAccepted {
			t.Fatalf("account request %d = %d, want %d", i+1, got, http.StatusAccepted)
		}
	}
	if got := request(s, "192.0.2.200", "person@example.com").Code; got != http.StatusTooManyRequests {
		t.Fatalf("distributed request past account limit = %d, want %d", got, http.StatusTooManyRequests)
	}

	s = newServer()
	for i := range passwordResetsPerClient {
		email := "person" + strconv.Itoa(i) + "@example.com"
		if got := request(s, "192.0.2.1", email).Code; got != http.StatusAccepted {
			t.Fatalf("client request %d = %d, want %d", i+1, got, http.StatusAccepted)
		}
	}
	if got := request(s, "192.0.2.1", "another@example.com").Code; got != http.StatusTooManyRequests {
		t.Fatalf("request past client limit = %d, want %d", got, http.StatusTooManyRequests)
	}
}

// TestAccountAccessMailIgnoresOrdinaryPreference checks administrator and
// activation changes remain security notifications a recipient cannot silence.
func TestAccountAccessMailIgnoresOrdinaryPreference(t *testing.T) {
	mail := &fakeMail{settings: domain.MailSettings{Enabled: true}}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Mail: mail, MailConfigured: true, WakeMail: func() {},
	})
	user := domain.User{ID: uuid.New(), Email: "person@example.com", Locale: "ru", IsActive: true,
		EmailNotifications: false}
	s.notifyAccountAdministration(httptest.NewRequest(http.MethodPatch, "/", nil), user,
		domain.User{ID: user.ID, Email: user.Email, Locale: user.Locale, IsActive: false,
			IsAdmin: true, EmailNotifications: false})
	wantSubject := mailer.AccountAccessChanged("ru", true, false, false, true).Subject
	if len(mail.queued) != 1 || mail.queued[0].Recipient != user.Email || mail.queued[0].Subject != wantSubject {
		t.Fatalf("security change was not queued in the recipient's language: %+v", mail.queued)
	}
}

// fakeProbe answers the readiness questions the status screen asks.
type fakeProbe struct{}

// Ping reports a database that answers.
func (fakeProbe) Ping(context.Context) error { return nil }

// SchemaVersion reports the migration the fake database is at.
func (fakeProbe) SchemaVersion(context.Context) (int64, error) { return 1, nil }
