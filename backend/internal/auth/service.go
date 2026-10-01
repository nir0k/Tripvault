package auth

import (
	"context"
	"errors"
	"fmt"
	"runtime"
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
	// Superseded finds the session whose previous token has this hash.
	Superseded(ctx context.Context, hash []byte) (domain.Session, error)
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

// reuseGrace is how soon after a refresh the token it replaced may be presented
// again without ending the session. Two tabs of one browser share the token
// and may refresh at the same moment; the one that loses is not a thief.
const reuseGrace = 30 * time.Second

// hashingWait is how long a request waits for room to hash a password before
// it is told the server is busy.
const hashingWait = 2 * time.Second

// Service performs sign-in, session refresh, sign-out and password changes.
type Service struct {
	users    UserStore
	sessions SessionStore
	issuer   *TokenIssuer
	now      func() time.Time
	// hashing bounds the Argon2id work done at once. Each hash holds about
	// 19 MiB, and the sign-in limits count attempts only once they finish, so
	// without it a burst of sign-ins to many addresses could take all memory.
	hashing     chan struct{}
	hashingWait time.Duration
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
	return &Service{
		users: users, sessions: sessions, issuer: issuer, now: time.Now,
		hashing:     make(chan struct{}, max(2, runtime.GOMAXPROCS(0)/2)),
		hashingWait: hashingWait,
	}
}

// acquireHashing waits for room to hash a password and returns what gives it
// back, or domain.ErrBusy when none frees up in time.
func (s *Service) acquireHashing(ctx context.Context) (func(), error) {
	timer := time.NewTimer(s.hashingWait)
	defer timer.Stop()
	select {
	case s.hashing <- struct{}{}:
		return func() { <-s.hashing }, nil
	case <-timer.C:
		return nil, domain.ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// verifyPassword checks a password against a stored hash within the bound on
// concurrent hashing. The errors of waiting are returned as they are, so
// domain.ErrBusy reaches the caller.
func (s *Service) verifyPassword(ctx context.Context, encodedHash, password string) (bool, error) {
	release, err := s.acquireHashing(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	ok, err := VerifyPassword(encodedHash, password)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return ok, nil
}

// CheckPassword - confirms a signed-in person knows their own password, before
// something that cannot be undone, such as deleting the account.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - user: the signed-in account.
//   - password: the password they typed.
//
// Returns:
//   - domain.ErrInvalidCredentials when it is not their password.
//   - domain.ErrBusy when the server has no room to check it now.
func (s *Service) CheckPassword(ctx context.Context, user domain.User, password string) error {
	ok, err := s.verifyPassword(ctx, user.PasswordHash, password)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrInvalidCredentials
	}
	return nil
}

// Credentials - checks an email and password without opening a session.
//
// An unknown address is checked against a dummy hash, so it costs the same
// Argon2id work as a wrong password and is answered with the same error.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - email: the address to check.
//   - password: the plaintext password.
//
// Returns:
//   - the account the pair opens, whether or not it is active.
//   - domain.ErrInvalidCredentials when the address is unknown or the password wrong.
//   - domain.ErrBusy when the server has no room to check the password now.
func (s *Service) Credentials(ctx context.Context, email, password string) (domain.User, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		if _, err := s.verifyPassword(ctx, dummyPasswordHash(), password); errors.Is(err, domain.ErrBusy) {
			return domain.User{}, err
		}
		return domain.User{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return domain.User{}, err
	}
	ok, err := s.verifyPassword(ctx, user.PasswordHash, password)
	if err != nil {
		return domain.User{}, err
	}
	if !ok {
		return domain.User{}, domain.ErrInvalidCredentials
	}
	return user, nil
}

// Login - exchanges an email and password for a new session.
//
// An unknown address, a deactivated account and a wrong password produce the
// same error after the same Argon2id work, so neither the response nor its
// timing reveals which addresses are registered or active. A self-registered
// account whose address is not confirmed is told so, but only once its
// password matched.
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
//   - domain.ErrEmailNotVerified when it opens a self-registered account that is
//     waiting for its address to be confirmed; the session then carries only
//     that account.
//   - domain.ErrBusy when the server has no room to check the password now.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Session, error) {
	user, err := s.Credentials(ctx, email, password)
	if err != nil {
		return Session{}, err
	}
	if !user.IsActive && user.EmailUnverifiedSince != nil {
		// The account comes back without a session, so the caller can tell
		// its owner when another confirmation message may be sent.
		return Session{User: user}, domain.ErrEmailNotVerified
	}
	if !user.IsActive {
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
// leaked stops working as soon as the real client refreshes. A token presented
// again after its session exchanged it means two parties hold the session, and
// the session is ended for both (see replayed).
//
// Arguments:
//   - ctx: context bounding the operation.
//   - refreshToken: the token previously handed to the client.
//
// Returns:
//   - the refreshed session.
//   - domain.ErrTokenInvalid when the token is unknown, expired, revoked, or
//     its account has been deactivated.
//   - domain.ErrTokenReused, which is also an ErrTokenInvalid, when the token
//     had been exchanged already and its session was ended for it.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	oldHash := HashRefreshToken(refreshToken)
	stored, err := s.sessions.GetByHash(ctx, oldHash)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, s.replayed(ctx, oldHash)
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

// replayed answers a refresh token that opens no session.
//
// A token its session has already exchanged is a copy that should not exist:
// either a thief refreshed first and the owner is presenting the old one, or
// the other way round. Nobody can tell which, so the session is ended for both
// and the owner signs in again. A token exchanged moments ago is only refused,
// because two tabs sharing it may simply have refreshed at once.
//
// Arguments:
//   - ctx: context bounding the lookup and the revocation.
//   - hash: the hash of the presented token.
//
// Returns:
//   - domain.ErrTokenReused when the session was ended for it.
//   - domain.ErrTokenInvalid in every other case.
//   - an error if the store fails.
func (s *Service) replayed(ctx context.Context, hash []byte) error {
	stored, err := s.sessions.Superseded(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrTokenInvalid
	}
	if err != nil {
		return err
	}
	now := s.now()
	if stored.RevokedAt != nil || stored.RotatedAt == nil || now.Sub(*stored.RotatedAt) < reuseGrace {
		return domain.ErrTokenInvalid
	}
	if err := s.sessions.Revoke(ctx, stored.UserID, stored.ID, now); err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return domain.ErrTokenReused
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
//   - domain.ErrBusy when the server has no room to hash a password now.
func (s *Service) ChangePassword(ctx context.Context, user domain.User, sessionID uuid.UUID,
	current, next string) error {
	if err := s.CheckPassword(ctx, user, current); err != nil {
		return err
	}
	if err := CheckPasswordStrength("new_password", next); err != nil {
		return err
	}
	if current == next {
		return domain.NewValidationError("new_password", "unchanged", "must differ from the current password")
	}

	release, err := s.acquireHashing(ctx)
	if err != nil {
		return err
	}
	hash, err := HashPassword(next)
	release()
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
