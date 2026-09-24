package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// SessionRepository stores the refresh tokens that back signed-in devices.
type SessionRepository struct {
	pool *pgxpool.Pool
}

// NewSessionRepository - creates the session repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// sessionColumns is the shared select list, kept in step with scanSession.
const sessionColumns = `id, user_id, token_hash, user_agent, created_at, last_used_at, expires_at, revoked_at`

// scanSession reads one row in the order of sessionColumns.
func scanSession(row pgx.Row) (domain.Session, error) {
	var s domain.Session
	err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.UserAgent, &s.CreatedAt, &s.LastUsedAt,
		&s.ExpiresAt, &s.RevokedAt)
	return s, err
}

// Create - stores a new session.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - session: the session, carrying the token hash rather than the token.
//
// Returns:
//   - an error if the row cannot be inserted.
func (r *SessionRepository) Create(ctx context.Context, session domain.Session) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, user_agent, created_at, last_used_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		session.ID, session.UserID, session.TokenHash, session.UserAgent,
		session.CreatedAt, session.LastUsedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetByHash - finds the session whose current token has this hash.
//
// Arguments:
//   - ctx: context bounding the query.
//   - hash: the hash of the presented refresh token.
//
// Returns:
//   - the session, including its revocation state, which the caller checks.
//   - domain.ErrNotFound when no session holds that token.
func (r *SessionRepository) GetByHash(ctx context.Context, hash []byte) (domain.Session, error) {
	session, err := scanSession(r.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM refresh_tokens WHERE token_hash = $1`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

// Rotate - replaces a live session's token.
//
// The update matches on the old hash as well as the id, so of two refreshes
// racing with the same token only one changes the row.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the session.
//   - oldHash: the hash of the token being exchanged.
//   - newHash: the hash of the token replacing it.
//   - usedAt: when the refresh happened.
//   - expiresAt: the session's new expiry.
//
// Returns:
//   - domain.ErrNotFound when the session no longer holds oldHash or is revoked.
func (r *SessionRepository) Rotate(ctx context.Context, id uuid.UUID, oldHash, newHash []byte,
	usedAt, expiresAt time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET token_hash = $3, last_used_at = $4, expires_at = $5
		 WHERE id = $1 AND token_hash = $2 AND revoked_at IS NULL`,
		id, oldHash, newHash, usedAt, expiresAt)
	if err != nil {
		return fmt.Errorf("rotate session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Revoke - ends one live session of an account.
//
// The account is part of the match, so a session identifier alone cannot be
// used to end somebody else's session.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - userID: the account the session must belong to.
//   - id: the session.
//   - at: the revocation time.
//
// Returns:
//   - domain.ErrNotFound when the account has no such live session.
func (r *SessionRepository) Revoke(ctx context.Context, userID, id uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID, at)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ActiveUser - returns the account behind a live session of an active account.
//
// One query answers both questions an authenticated request asks: is the
// session still live, and is its account still allowed in.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the account the token names.
//   - sessionID: the session the token names.
//   - now: the reference time for expiry.
//
// Returns:
//   - the account.
//   - domain.ErrNotFound when the session is gone, expired, revoked, belongs to
//     another account, or the account is deactivated.
func (r *SessionRepository) ActiveUser(ctx context.Context, userID, sessionID uuid.UUID,
	now time.Time) (domain.User, error) {
	return oneUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+`
		 FROM users u
		 JOIN refresh_tokens t ON t.user_id = u.id
		 WHERE u.id = $1 AND t.id = $2 AND u.is_active
		   AND t.revoked_at IS NULL AND t.expires_at > $3`,
		userID, sessionID, now), "load session account")
}

// ListActive - returns an account's live sessions, most recently used first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the account.
//   - now: the reference time for expiry.
//
// Returns:
//   - the live sessions.
//   - an error if the query fails.
func (r *SessionRepository) ListActive(ctx context.Context, userID uuid.UUID, now time.Time) ([]domain.Session, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+sessionColumns+` FROM refresh_tokens
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2
		 ORDER BY last_used_at DESC`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	sessions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Session, error) {
		return scanSession(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	return sessions, nil
}

// DeleteFinished - removes sessions that can never be used again.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - before: sessions that expired or were revoked before this time go.
//
// Returns:
//   - the number of rows removed.
//   - an error if the statement fails.
func (r *SessionRepository) DeleteFinished(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < $1 OR revoked_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("delete finished sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
