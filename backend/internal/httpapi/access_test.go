package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakeAuth authenticates one fixed token as one fixed account.
type fakeAuth struct {
	user domain.User
}

// Login is not used by these tests.
func (f *fakeAuth) Login(context.Context, string, string, string) (auth.Session, error) {
	return auth.Session{}, domain.ErrInvalidCredentials
}

// Refresh is not used by these tests.
func (f *fakeAuth) Refresh(context.Context, string) (auth.Session, error) {
	return auth.Session{}, domain.ErrTokenInvalid
}

// Logout is not used by these tests.
func (f *fakeAuth) Logout(context.Context, string) error { return nil }

// Authenticate accepts only the token "good".
func (f *fakeAuth) Authenticate(_ context.Context, token string) (domain.User, uuid.UUID, error) {
	if token != "good" {
		return domain.User{}, uuid.Nil, domain.ErrTokenInvalid
	}
	return f.user, uuid.New(), nil
}

// ChangePassword always succeeds.
func (f *fakeAuth) ChangePassword(context.Context, domain.User, uuid.UUID, string, string) error {
	return nil
}

// fakeUsers answers the account queries the gated routes reach.
// fakeUsers stands in for the account store. Its zero value answers what the
// tests that do not care about accounts need; the maps are given only by the
// tests that watch an avatar or a deletion, and are shared by value because a
// map is a reference.
type fakeUsers struct {
	// avatars is the picture each account wears.
	avatars map[uuid.UUID]string
	// deleted records which accounts Delete was asked for.
	deleted map[uuid.UUID]struct{}
	// lastAdmin makes Delete refuse as if the account were the last one.
	lastAdmin bool
}

// avatarChanged is the moment every fake avatar was last changed.
var avatarChanged = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

// Create is not used by these tests.
func (fakeUsers) Create(context.Context, domain.User) (domain.User, error) { return domain.User{}, nil }

// GetByID returns the account with whatever picture the test gave it.
func (f fakeUsers) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	user := domain.User{ID: id, IsActive: true, AvatarKey: f.avatars[id]}
	if user.AvatarKey != "" {
		user.AvatarUpdatedAt = &avatarChanged
	}
	return user, nil
}

// SetAvatar records the picture an account wears and reports the one before it.
func (f fakeUsers) SetAvatar(_ context.Context, id uuid.UUID, key string,
	_ time.Time) (domain.User, string, error) {
	previous := f.avatars[id]
	if f.avatars != nil {
		if key == "" {
			delete(f.avatars, id)
		} else {
			f.avatars[id] = key
		}
	}
	if previous == key {
		previous = ""
	}
	user := domain.User{ID: id, IsActive: true, AvatarKey: key}
	if key != "" {
		user.AvatarUpdatedAt = &avatarChanged
	}
	return user, previous, nil
}

// Delete records the account, or refuses as if it were the last administrator.
func (f fakeUsers) Delete(_ context.Context, id uuid.UUID) ([]string, error) {
	if f.lastAdmin {
		return nil, domain.ErrLastAdmin
	}
	if f.deleted != nil {
		f.deleted[id] = struct{}{}
	}
	return []string{avatarKey(id)}, nil
}

// List returns no accounts.
func (fakeUsers) List(context.Context) ([]domain.User, error) { return nil, nil }

// Update refuses as if the change would remove the last administrator.
func (fakeUsers) Update(context.Context, uuid.UUID, domain.UserChanges) (domain.User, error) {
	return domain.User{}, domain.ErrLastAdmin
}

// UpdateProfile echoes the profile back.
func (fakeUsers) UpdateProfile(_ context.Context, id uuid.UUID, p domain.Profile) (domain.User, error) {
	return domain.User{ID: id, DisplayName: p.DisplayName, Locale: p.Locale, Theme: p.Theme,
		DefaultCurrency: p.DefaultCurrency}, nil
}

// ResetPassword is not used by these tests.
func (fakeUsers) ResetPassword(context.Context, uuid.UUID, string) error { return nil }

// Stats is not used by these tests.
func (fakeUsers) Stats(context.Context) (domain.UserStats, error) { return domain.UserStats{}, nil }

// Search finds nobody.
func (fakeUsers) Search(context.Context, string, uuid.UUID) ([]domain.TripUser, error) {
	return nil, nil
}

// newTestServer builds a server whose token "good" signs in as user.
func newTestServer(user domain.User) *Server {
	return NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:  &fakeAuth{user: user},
		Users: fakeUsers{},
	})
}

// send performs one request against the server's routes.
func send(s *Server, method, path, token, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// errorCode reads the code from an error envelope.
func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response %q is not an error envelope: %v", recorder.Body.String(), err)
	}
	return body.Code
}

// TestProtectedRoutesNeedAToken checks the 401 envelope.
func TestProtectedRoutesNeedAToken(t *testing.T) {
	s := newTestServer(domain.User{ID: uuid.New(), IsActive: true})
	for _, token := range []string{"", "bad"} {
		recorder := send(s, http.MethodGet, "/api/v1/me", token, "")
		if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != "unauthorized" {
			t.Errorf("token %q: %d %s", token, recorder.Code, recorder.Body.String())
		}
	}
}

// TestTemporaryPasswordAllowsOnlyThePasswordChange checks an account holding
// an administrator's password can read itself and change the password, and
// nothing else.
func TestTemporaryPasswordAllowsOnlyThePasswordChange(t *testing.T) {
	s := newTestServer(domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true, MustChangePassword: true,
		Theme: domain.ThemeAuto, DefaultCurrency: "EUR"})

	if recorder := send(s, http.MethodGet, "/api/v1/me", "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("GET /me: %d", recorder.Code)
	}
	body := `{"current_password":"temporary","new_password":"brand new password"}`
	if recorder := send(s, http.MethodPost, "/api/v1/me/password", "good", body); recorder.Code != http.StatusNoContent {
		t.Errorf("POST /me/password: %d %s", recorder.Code, recorder.Body.String())
	}

	for _, route := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/me", `{"theme":"dark"}`},
		{http.MethodGet, "/api/v1/me/sessions", ""},
		{http.MethodGet, "/api/v1/admin/users", ""},
	} {
		recorder := send(s, route.method, route.path, "good", route.body)
		if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "password_change_required" {
			t.Errorf("%s %s: %d %s", route.method, route.path, recorder.Code, recorder.Body.String())
		}
	}
}

// TestAdministrationNeedsAnAdministrator checks the admin routes refuse an
// ordinary account.
func TestAdministrationNeedsAnAdministrator(t *testing.T) {
	s := newTestServer(domain.User{ID: uuid.New(), IsActive: true})
	recorder := send(s, http.MethodGet, "/api/v1/admin/users", "good", "")
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "forbidden" {
		t.Errorf("GET /admin/users: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestErrorMapping checks validation details, unknown fields and the last
// administrator conflict come back in the documented shape.
func TestErrorMapping(t *testing.T) {
	s := newTestServer(domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true,
		Theme: domain.ThemeAuto, DefaultCurrency: "EUR", DisplayName: "Ann"})

	recorder := send(s, http.MethodPatch, "/api/v1/me", "good", `{"theme":"purple"}`)
	var body ErrorResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusUnprocessableEntity || body.Code != "validation_failed" ||
		body.Details["field"] != "theme" || body.Details["reason"] != "unsupported" {
		t.Errorf("bad theme: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, "/api/v1/me", "good", `{"colour":"dark"}`)
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_request" {
		t.Errorf("unknown field: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, "/api/v1/admin/users/"+uuid.NewString(), "good", `{"is_admin":false}`)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "last_admin" {
		t.Errorf("last admin: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodGet, "/api/v1/nothing-here", "", "")
	if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "not_found" {
		t.Errorf("unknown route: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("an API response carries no security headers")
	}

	recorder = send(s, http.MethodPatch, "/api/v1/me", "good", `{"default_currency":"isk","locale":"ru"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"default_currency":"ISK"`) {
		t.Errorf("profile update: %d %s", recorder.Code, recorder.Body.String())
	}
}
