package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// UserStore is the slice of account persistence the auth service needs. It is
// declared next to its consumer so the service can be tested without a database.
type UserStore interface {
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	SetOwnPassword(ctx context.Context, id uuid.UUID, hash string, keepSession uuid.UUID) error
}

// SessionStore is the session persistence the auth service needs.
type SessionStore interface {
	Create(ctx context.Context, session domain.Session) error
	GetByHash(ctx context.Context, hash []byte) (domain.Session, error)
	// Rotate replaces the token of a live session, but only while it still
	// holds oldHash, so two refreshes racing with one token cannot both win.
	Rotate(ctx context.Context, id uuid.UUID, oldHash, newHash []byte, usedAt, expiresAt time.Time) error
	Revoke(ctx context.Context, userID, id uuid.UUID, at time.Time) error
	// ActiveUser returns the account behind a live session of an active user.
	ActiveUser(ctx context.Context, userID, sessionID uuid.UUID, now time.Time) (domain.User, error)
}

// Session is what a successful sign-in or refresh hands back to the client.
type Session struct {
	ID           uuid.UUID
	AccessToken  string
	RefreshToken string
	// ExpiresIn is the access token's lifetime in seconds, so a client can
	// schedule its refresh without parsing the token.
	ExpiresIn int64
	User      domain.User
}

// Service performs sign-in, session refresh, sign-out and password changes.
type Service struct {
	users    UserStore
	sessions SessionStore
	issuer   *TokenIssuer
	now      func() time.Time
}

// NewService - creates the authentication service.
//
// Arguments:
//   - users: account persistence.
//   - sessions: session persistence.
//   - issuer: mints and validates tokens.
//
// Returns:
//   - a ready service.
func NewService(users UserStore, sessions SessionStore, issuer *TokenIssuer) *Service {
	return &Service{users: users, sessions: sessions, issuer: issuer, now: time.Now}
}

// Login - exchanges an email and password for a new session.
//
// An unknown address, a deactivated account and a wrong password produce the
// same error after the same Argon2id work, so neither the response nor its
// timing reveals which addresses are registered or active.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - email: the address to sign in with.
//   - password: the plaintext password.
//   - userAgent: the client's User-Agent, shown in the session list.
//
// Returns:
//   - the new session.
//   - domain.ErrInvalidCredentials when the pair does not open an active account.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Session, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		_, _ = VerifyPassword(dummyPasswordHash(), password)
		return Session{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}

	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok || !user.IsActive {
		return Session{}, domain.ErrInvalidCredentials
	}

	now := s.now()
	if err := s.users.RecordLogin(ctx, user.ID, now); err != nil {
		return Session{}, err
	}
	user.LastLoginAt = &now

	refreshToken, hash, err := NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	record := domain.Session{
		ID:         uuid.Must(uuid.NewV7()),
		UserID:     user.ID,
		TokenHash:  hash,
		UserAgent:  truncate(userAgent, domain.MaxUserAgentLength),
		CreatedAt:  now,
		LastUsedAt: now,
		ExpiresAt:  now.Add(s.issuer.RefreshTokenTTL()),
	}
	if err := s.sessions.Create(ctx, record); err != nil {
		return Session{}, err
	}
	return s.session(record.ID, refreshToken, user)
}

// Refresh - exchanges a refresh token for a new token pair on the same session.
//
// The presented token is replaced, so it can be used only once: a copy that
// leaked stops working as soon as the real client refreshes.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - refreshToken: the token previously handed to the client.
//
// Returns:
//   - the refreshed session.
//   - domain.ErrTokenInvalid when the token is unknown, expired, revoked, or
//     its account has been deactivated.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	oldHash := HashRefreshToken(refreshToken)
	stored, err := s.sessions.GetByHash(ctx, oldHash)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, domain.ErrTokenInvalid
	}
	if err != nil {
		return Session{}, err
	}

	now := s.now()
	if !stored.IsUsable(now) {
		return Session{}, domain.ErrTokenInvalid
	}
	user, err := s.sessions.ActiveUser(ctx, stored.UserID, stored.ID, now)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, domain.ErrTokenInvalid
	}
	if err != nil {
		return Session{}, err
	}

	newToken, newHash, err := NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	err = s.sessions.Rotate(ctx, stored.ID, oldHash, newHash, now, now.Add(s.issuer.RefreshTokenTTL()))
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, domain.ErrTokenInvalid
	}
	if err != nil {
		return Session{}, err
	}
	return s.session(stored.ID, newToken, user)
}

// Logout - revokes the session a refresh token belongs to.
//
// An unknown or already revoked token is not an error: signing out twice must
// still leave the client signed out.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - refreshToken: the token of the session to end.
//
// Returns:
//   - an error only if the store itself fails.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	stored, err := s.sessions.GetByHash(ctx, HashRefreshToken(refreshToken))
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.sessions.Revoke(ctx, stored.UserID, stored.ID, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	return err
}

// Authenticate - resolves an access token into the account and session it names.
//
// The session and the account are read from the database on every request, so
// a revoked session or a deactivated account loses access at once rather than
// when the access token happens to expire.
//
// Arguments:
//   - ctx: context bounding the lookup.
//   - token: the compact JWT from the Authorization header.
//
// Returns:
//   - the account and the session identifier.
//   - domain.ErrTokenInvalid when the token, its session or its account is no
//     longer valid.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, uuid.UUID, error) {
	claims, err := s.issuer.ParseAccessToken(token)
	if err != nil {
		return domain.User{}, uuid.Nil, err
	}
	user, err := s.sessions.ActiveUser(ctx, claims.UserID, claims.SessionID, s.now())
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, uuid.Nil, domain.ErrTokenInvalid
	}
	if err != nil {
		return domain.User{}, uuid.Nil, err
	}
	return user, claims.SessionID, nil
}

// ChangePassword - replaces a signed-in user's password with one they chose.
//
// The current password is required even though the caller is signed in, so a
// session left open on a shared device cannot be used to take the account over.
// Every other session is ended; the one making the change stays signed in.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - user: the signed-in account.
//   - sessionID: the session making the change, which is kept.
//   - current: the password the user signs in with now.
//   - next: the new password.
//
// Returns:
//   - domain.ErrInvalidCredentials when the current password is wrong.
//   - a *domain.ValidationError when the new password is unacceptable.
func (s *Service) ChangePassword(ctx context.Context, user domain.User, sessionID uuid.UUID,
	current, next string) error {
	ok, err := VerifyPassword(user.PasswordHash, current)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return domain.ErrInvalidCredentials
	}
	if err := CheckPasswordStrength("new_password", next); err != nil {
		return err
	}
	if current == next {
		return domain.NewValidationError("new_password", "unchanged", "must differ from the current password")
	}

	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.users.SetOwnPassword(ctx, user.ID, hash, sessionID)
}

// session mints an access token for a session and assembles the response.
func (s *Service) session(id uuid.UUID, refreshToken string, user domain.User) (Session, error) {
	accessToken, err := s.issuer.IssueAccessToken(user.ID, id)
	if err != nil {
		return Session{}, err
	}
	return Session{
		ID:           id,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.issuer.AccessTokenTTL().Seconds()),
		User:         user,
	}, nil
}

// truncate shortens a string to at most limit bytes without splitting a
// multi-byte character.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && (value[cut]&0xC0) == 0x80 {
		cut--
	}
	return value[:cut]
}
