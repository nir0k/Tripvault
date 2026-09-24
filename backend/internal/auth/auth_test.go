package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestHashAndVerifyPassword checks a hash accepts its password and nothing else,
// and that two hashes of one password differ by their salt.
func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(hash, "correct horse battery"); err != nil || !ok {
		t.Fatalf("the right password was refused: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword(hash, "wrong horse battery"); ok {
		t.Fatal("a wrong password was accepted")
	}
	again, _ := HashPassword("correct horse battery")
	if again == hash {
		t.Fatal("two hashes of one password are identical")
	}
	if _, err := VerifyPassword("not-a-hash", "x"); err == nil {
		t.Fatal("a malformed hash was not reported")
	}
}

// TestCheckPasswordStrength checks the length bounds.
func TestCheckPasswordStrength(t *testing.T) {
	var validation *domain.ValidationError
	if err := CheckPasswordStrength("password", "short"); !errors.As(err, &validation) || validation.Code != "too_short" {
		t.Errorf("a short password: %v", err)
	}
	if err := CheckPasswordStrength("password", "пароль12"); err != nil {
		t.Errorf("eight Cyrillic characters were refused: %v", err)
	}
	if err := CheckPasswordStrength("password", strings.Repeat("a", 257)); !errors.As(err, &validation) || validation.Code != "too_long" {
		t.Errorf("a long password: %v", err)
	}
}

// TestAccessToken checks a token round-trips and is refused once expired or
// when signed by another key.
func TestAccessToken(t *testing.T) {
	issuer, err := NewTokenIssuer("test-secret", time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	userID, sessionID := uuid.New(), uuid.New()
	token, err := issuer.IssueAccessToken(userID, sessionID)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := issuer.ParseAccessToken(token)
	if err != nil || claims.UserID != userID || claims.SessionID != sessionID {
		t.Fatalf("round trip: %+v %v", claims, err)
	}

	other, _ := NewTokenIssuer("another-secret", time.Minute, time.Hour)
	if _, err := other.ParseAccessToken(token); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("a token from another key was accepted: %v", err)
	}

	issuer.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := issuer.ParseAccessToken(token); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("an expired token was accepted: %v", err)
	}

	if _, err := NewTokenIssuer("", time.Minute, time.Hour); err == nil {
		t.Error("an empty secret was accepted")
	}
}

// fakeStore keeps accounts and sessions in memory for the service tests.
type fakeStore struct {
	mu       sync.Mutex
	users    map[uuid.UUID]domain.User
	sessions map[uuid.UUID]domain.Session
}

// newFakeStore creates an empty in-memory store.
func newFakeStore() *fakeStore {
	return &fakeStore{users: map[uuid.UUID]domain.User{}, sessions: map[uuid.UUID]domain.Session{}}
}

// addUser stores an account with the given password.
func (f *fakeStore) addUser(t *testing.T, email, password string, active bool) domain.User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	user := domain.User{ID: uuid.New(), Email: email, PasswordHash: hash, IsActive: active}
	f.users[user.ID] = user
	return user
}

// GetByEmail finds an account by address.
func (f *fakeStore) GetByEmail(_ context.Context, email string) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, user := range f.users {
		if user.Email == email {
			return user, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

// RecordLogin stamps the last sign-in time.
func (f *fakeStore) RecordLogin(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	user := f.users[id]
	user.LastLoginAt = &at
	f.users[id] = user
	return nil
}

// SetOwnPassword replaces the hash and ends every other session.
func (f *fakeStore) SetOwnPassword(_ context.Context, id uuid.UUID, hash string, keep uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	user := f.users[id]
	user.PasswordHash = hash
	user.MustChangePassword = false
	f.users[id] = user
	now := time.Now()
	for sid, session := range f.sessions {
		if session.UserID == id && sid != keep {
			session.RevokedAt = &now
			f.sessions[sid] = session
		}
	}
	return nil
}

// Create stores a session.
func (f *fakeStore) Create(_ context.Context, session domain.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[session.ID] = session
	return nil
}

// GetByHash finds a session by its current token hash.
func (f *fakeStore) GetByHash(_ context.Context, hash []byte) (domain.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, session := range f.sessions {
		if bytes.Equal(session.TokenHash, hash) {
			return session, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}

// Rotate replaces the hash while the old one is still current.
func (f *fakeStore) Rotate(_ context.Context, id uuid.UUID, oldHash, newHash []byte, usedAt, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[id]
	if !ok || !bytes.Equal(session.TokenHash, oldHash) || session.RevokedAt != nil {
		return domain.ErrNotFound
	}
	session.TokenHash, session.LastUsedAt, session.ExpiresAt = newHash, usedAt, expiresAt
	f.sessions[id] = session
	return nil
}

// Revoke ends a session.
func (f *fakeStore) Revoke(_ context.Context, userID, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[id]
	if !ok || session.UserID != userID {
		return domain.ErrNotFound
	}
	session.RevokedAt = &at
	f.sessions[id] = session
	return nil
}

// ActiveUser returns the account behind a live session of an active user.
func (f *fakeStore) ActiveUser(_ context.Context, userID, sessionID uuid.UUID, now time.Time) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	session, ok := f.sessions[sessionID]
	user, found := f.users[userID]
	if !ok || !found || session.UserID != userID || !session.IsUsable(now) || !user.IsActive {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

// newTestService builds a service over an in-memory store.
func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	issuer, err := NewTokenIssuer("test-secret", time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, store, issuer), store
}

// TestLoginRefreshLogout walks a whole session through its life.
func TestLoginRefreshLogout(t *testing.T) {
	ctx := context.Background()
	service, store := newTestService(t)
	user := store.addUser(t, "ann@example.com", "long enough", true)

	session, err := service.Login(ctx, "ann@example.com", "long enough", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if session.User.ID != user.ID || session.User.LastLoginAt == nil {
		t.Fatalf("login returned %+v", session.User)
	}

	if _, sid, err := service.Authenticate(ctx, session.AccessToken); err != nil || sid != session.ID {
		t.Fatalf("authenticate: %v %v", sid, err)
	}

	refreshed, err := service.Refresh(ctx, session.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ID != session.ID || refreshed.RefreshToken == session.RefreshToken {
		t.Fatal("refresh did not rotate the token on the same session")
	}
	if _, err := service.Refresh(ctx, session.RefreshToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("a used refresh token worked again: %v", err)
	}

	if err := service.Logout(ctx, refreshed.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Authenticate(ctx, refreshed.AccessToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("an access token outlived its session: %v", err)
	}
	if err := service.Logout(ctx, refreshed.RefreshToken); err != nil {
		t.Fatalf("signing out twice failed: %v", err)
	}
}

// TestLoginRefusals checks an unknown address, a wrong password and a
// deactivated account all fail the same way.
func TestLoginRefusals(t *testing.T) {
	ctx := context.Background()
	service, store := newTestService(t)
	store.addUser(t, "ann@example.com", "long enough", true)
	store.addUser(t, "bob@example.com", "long enough", false)

	for _, attempt := range []struct{ email, password string }{
		{"nobody@example.com", "long enough"},
		{"ann@example.com", "wrong password"},
		{"bob@example.com", "long enough"},
	} {
		if _, err := service.Login(ctx, attempt.email, attempt.password, ""); !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Errorf("login as %s: %v", attempt.email, err)
		}
	}
}

// TestDeactivationEndsAccess checks a deactivated account loses its access
// token and cannot refresh.
func TestDeactivationEndsAccess(t *testing.T) {
	ctx := context.Background()
	service, store := newTestService(t)
	user := store.addUser(t, "ann@example.com", "long enough", true)
	session, err := service.Login(ctx, "ann@example.com", "long enough", "")
	if err != nil {
		t.Fatal(err)
	}

	user.IsActive = false
	store.users[user.ID] = user

	if _, _, err := service.Authenticate(ctx, session.AccessToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("a deactivated account authenticated: %v", err)
	}
	if _, err := service.Refresh(ctx, session.RefreshToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("a deactivated account refreshed: %v", err)
	}
}

// TestChangePassword checks the current password is required, the new one is
// validated, and other sessions end while the caller's stays.
func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	service, store := newTestService(t)
	store.addUser(t, "ann@example.com", "temporary pw", true)

	first, _ := service.Login(ctx, "ann@example.com", "temporary pw", "phone")
	second, _ := service.Login(ctx, "ann@example.com", "temporary pw", "laptop")
	user, _, _ := service.Authenticate(ctx, first.AccessToken)

	if err := service.ChangePassword(ctx, user, first.ID, "wrong", "brand new password"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("a wrong current password: %v", err)
	}
	var validation *domain.ValidationError
	if err := service.ChangePassword(ctx, user, first.ID, "temporary pw", "short"); !errors.As(err, &validation) {
		t.Fatalf("a short new password: %v", err)
	}
	if err := service.ChangePassword(ctx, user, first.ID, "temporary pw", "temporary pw"); !errors.As(err, &validation) || validation.Code != "unchanged" {
		t.Fatalf("an unchanged password: %v", err)
	}
	if err := service.ChangePassword(ctx, user, first.ID, "temporary pw", "brand new password"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.Authenticate(ctx, first.AccessToken); err != nil {
		t.Errorf("the session that changed the password was ended: %v", err)
	}
	if _, _, err := service.Authenticate(ctx, second.AccessToken); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Errorf("another session survived a password change: %v", err)
	}
	if _, err := service.Login(ctx, "ann@example.com", "brand new password", ""); err != nil {
		t.Errorf("the new password does not sign in: %v", err)
	}
}

// TestTruncate checks a multi-byte character is never split.
func TestTruncate(t *testing.T) {
	if got := truncate("абв", 3); got != "а" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate = %q", got)
	}
}
