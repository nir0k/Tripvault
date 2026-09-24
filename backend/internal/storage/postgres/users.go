package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// UserRepository reads and writes accounts.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository - creates the account repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// userColumns is the shared select list, kept in step with scanUser.
const userColumns = `u.id, u.email, u.display_name, u.password_hash, u.is_admin, u.is_active,
	u.must_change_password, coalesce(u.locale, ''), u.theme, u.units, u.date_format, u.time_format,
	u.default_currency, u.avatar_key, u.avatar_updated_at, u.last_login_at, u.created_at, u.updated_at`

// scanUser reads one row in the order of userColumns.
func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.IsActive,
		&u.MustChangePassword, &u.Locale, &u.Theme, &u.Units, &u.DateFormat, &u.TimeFormat,
		&u.DefaultCurrency, &u.AvatarKey, &u.AvatarUpdatedAt, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// oneUser scans a single-row result, mapping "no rows" to domain.ErrNotFound.
func oneUser(row pgx.Row, action string) (domain.User, error) {
	user, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("%s: %w", action, err)
	}
	return user, nil
}

// GetByEmail - looks up an account by address, case-insensitively.
//
// Arguments:
//   - ctx: context bounding the query.
//   - email: the address to look up.
//
// Returns:
//   - the account.
//   - domain.ErrNotFound when no account has that address.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	return oneUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE lower(u.email) = lower($1)`, email), "get user by email")
}

// GetByID - looks up an account by identifier.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the account identifier.
//
// Returns:
//   - the account.
//   - domain.ErrNotFound when no such account exists.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return oneUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE u.id = $1`, id), "get user by id")
}

// Create - inserts a new account.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - user: the account to create, with its ID and password hash set.
//
// Returns:
//   - the stored account with the timestamps the database assigned.
//   - domain.ErrAlreadyExists when the address is taken.
func (r *UserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users AS u (id, email, display_name, password_hash, is_admin, is_active,
		                         must_change_password, locale, theme, units, default_currency)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), $9, $10, $11)
		 RETURNING `+userColumns,
		user.ID, user.Email, user.DisplayName, user.PasswordHash, user.IsAdmin, user.IsActive,
		user.MustChangePassword, user.Locale, user.Theme, user.Units, user.DefaultCurrency)

	created, err := scanUser(row)
	if isUniqueViolation(err) {
		return domain.User{}, domain.ErrAlreadyExists
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return created, nil
}

// Count - reports how many accounts exist.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the number of accounts.
//   - an error if the query fails.
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// Stats - counts accounts, active accounts and active administrators.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the counts.
//   - an error if the query fails.
func (r *UserRepository) Stats(ctx context.Context) (domain.UserStats, error) {
	var stats domain.UserStats
	err := r.pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE is_active),
		        count(*) FILTER (WHERE is_active AND is_admin)
		 FROM users`).Scan(&stats.Total, &stats.Active, &stats.Admins)
	if err != nil {
		return domain.UserStats{}, fmt.Errorf("count users: %w", err)
	}
	return stats, nil
}

// List - returns every account, ordered by name.
//
// An instance's accounts are counted in people rather than thousands, so the
// list is unpaged.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the accounts.
//   - an error if the query fails.
func (r *UserRepository) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users u ORDER BY lower(u.display_name), u.email`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.User, error) {
		return scanUser(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	return users, nil
}

// RecordLogin - stamps the time of a successful sign-in.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the account that signed in.
//   - at: when it happened.
//
// Returns:
//   - an error if the statement fails.
func (r *UserRepository) RecordLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	if _, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = $2 WHERE id = $1`, id, at); err != nil {
		return fmt.Errorf("record login: %w", err)
	}
	return nil
}

// UpdateProfile - stores the settings an account's owner may change.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the account to change.
//   - profile: the validated settings.
//
// Returns:
//   - the stored account.
//   - domain.ErrNotFound when no such account exists.
func (r *UserRepository) UpdateProfile(ctx context.Context, id uuid.UUID, profile domain.Profile) (domain.User, error) {
	return oneUser(r.pool.QueryRow(ctx,
		`UPDATE users AS u
		 SET display_name = $2, locale = nullif($3, ''), theme = $4, units = $5,
		     date_format = $6, time_format = $7, default_currency = $8, updated_at = now()
		 WHERE u.id = $1
		 RETURNING `+userColumns,
		id, profile.DisplayName, profile.Locale, profile.Theme, profile.Units,
		profile.DateFormat, profile.TimeFormat, profile.DefaultCurrency), "update profile")
}

// SetAvatar - stores where an account's picture lives and when it was put there.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the account.
//   - key: the object in the media store, or "" to take the picture away.
//   - at: when it changed, which is what makes a browser fetch the new one.
//
// Returns:
//   - the stored account.
//   - the object the account wore before, so the caller can delete its bytes.
//   - domain.ErrNotFound when no such account exists.
func (r *UserRepository) SetAvatar(ctx context.Context, id uuid.UUID, key string,
	at time.Time) (domain.User, string, error) {
	var previous string
	var user domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT avatar_key FROM users WHERE id = $1`, id).Scan(&previous); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			return fmt.Errorf("read the avatar: %w", err)
		}
		updated, err := oneUser(tx.QueryRow(ctx,
			`UPDATE users AS u
			 SET avatar_key = $2, avatar_updated_at = nullif($3, to_timestamp(0)), updated_at = now()
			 WHERE u.id = $1
			 RETURNING `+userColumns, id, key, avatarTime(key, at)), "set avatar")
		if err != nil {
			return err
		}
		user = updated
		return nil
	})
	if err != nil {
		return domain.User{}, "", err
	}
	if previous == key {
		// The same object was overwritten in place; its bytes are the new ones.
		previous = ""
	}
	return user, previous, nil
}

// avatarTime gives the moment an avatar changed, and the zero moment when the
// account is left without one: nothing changed that a browser could fetch.
func avatarTime(key string, at time.Time) time.Time {
	if key == "" {
		return time.Unix(0, 0).UTC()
	}
	return at
}

// Delete - removes an account with everything only it holds: the trips it owns,
// their documents, costs and files, its memberships of other people's trips and
// its sessions. The photographs it uploaded into somebody else's trip stay
// there; only the record of who added them is forgotten.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - id: the account.
//
// Returns:
//   - the media store objects the deletion orphaned, to be deleted from disk.
//   - domain.ErrNotFound when no such account exists.
//   - domain.ErrLastAdmin when it is the last active administrator.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var isAdmin, isActive bool
		var avatarKey string
		err := tx.QueryRow(ctx,
			`SELECT is_admin, is_active, avatar_key FROM users WHERE id = $1`, id).
			Scan(&isAdmin, &isActive, &avatarKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read the account: %w", err)
		}
		if isAdmin && isActive {
			var others int64
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM users WHERE is_admin AND is_active AND id <> $1`, id).Scan(&others); err != nil {
				return fmt.Errorf("count administrators: %w", err)
			}
			if others == 0 {
				return domain.ErrLastAdmin
			}
		}

		rows, err := tx.Query(ctx,
			`SELECT storage_key FROM media WHERE trip_id IN (SELECT id FROM trips WHERE owner_id = $1)`, id)
		if err != nil {
			return fmt.Errorf("list the media of the deleted trips: %w", err)
		}
		keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("read media keys: %w", err)
		}
		if avatarKey != "" {
			keys = append(keys, avatarKey)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM trips WHERE owner_id = $1`, id); err != nil {
			return fmt.Errorf("delete the trips of the account: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete the account: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// SetOwnPassword - stores a password the owner chose and ends their other sessions.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - id: the account.
//   - hash: the new password hash.
//   - keepSession: the session that made the change, which stays signed in.
//
// Returns:
//   - domain.ErrNotFound when no such account exists.
func (r *UserRepository) SetOwnPassword(ctx context.Context, id uuid.UUID, hash string, keepSession uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, must_change_password = false, updated_at = now() WHERE id = $1`,
			id, hash)
		if err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		_, err = tx.Exec(ctx,
			`UPDATE refresh_tokens SET revoked_at = now()
			 WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, id, keepSession)
		if err != nil {
			return fmt.Errorf("end other sessions: %w", err)
		}
		return nil
	})
}

// ResetPassword - stores a password an administrator chose and ends every session.
//
// The owner has to replace it at their next sign-in: a password somebody else
// knows is a temporary one.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - id: the account.
//   - hash: the temporary password's hash.
//
// Returns:
//   - domain.ErrNotFound when no such account exists.
func (r *UserRepository) ResetPassword(ctx context.Context, id uuid.UUID, hash string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, must_change_password = true, updated_at = now() WHERE id = $1`,
			id, hash)
		if err != nil {
			return fmt.Errorf("reset password: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return revokeAllSessions(ctx, tx, id)
	})
}

// Update - applies an administrator's changes to an account.
//
// Deactivating an account ends every session it has in the same transaction.
// The rule that the service keeps an active administrator is checked inside the
// transaction too, after the change and with the administrators' rows locked,
// so two administrators demoting each other at once cannot both succeed.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - id: the account to change.
//   - changes: the fields to change; nil fields are left alone.
//
// Returns:
//   - the stored account.
//   - domain.ErrNotFound when no such account exists.
//   - domain.ErrLastAdmin when the change would leave no active administrator.
func (r *UserRepository) Update(ctx context.Context, id uuid.UUID, changes domain.UserChanges) (domain.User, error) {
	var updated domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Serialises every change that could affect the administrator count.
		if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE is_admin AND is_active FOR UPDATE`); err != nil {
			return fmt.Errorf("lock administrators: %w", err)
		}

		var err error
		updated, err = oneUser(tx.QueryRow(ctx,
			`UPDATE users AS u
			 SET display_name = coalesce($2, u.display_name),
			     is_admin     = coalesce($3, u.is_admin),
			     is_active    = coalesce($4, u.is_active),
			     updated_at   = now()
			 WHERE u.id = $1
			 RETURNING `+userColumns,
			id, changes.DisplayName, changes.IsAdmin, changes.IsActive), "update user")
		if err != nil {
			return err
		}

		var admins int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_admin AND is_active`).Scan(&admins); err != nil {
			return fmt.Errorf("count administrators: %w", err)
		}
		if admins == 0 {
			return domain.ErrLastAdmin
		}

		if !updated.IsActive {
			return revokeAllSessions(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}
	return updated, nil
}

// revokeAllSessions ends every live session of an account inside a transaction.
func revokeAllSessions(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return nil
}

// uniqueViolationCode is PostgreSQL's SQLSTATE for a unique constraint failure.
const uniqueViolationCode = "23505"

// isUniqueViolation reports whether an error came from a unique constraint, so
// a duplicate can be reported as a conflict rather than an internal failure.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}

// maxUserSearchResults bounds the people search: it picks one person, it does
// not browse the directory.
const maxUserSearchResults = 10

// Search - finds active accounts by name or email, for adding trip members.
//
// Arguments:
//   - ctx: context bounding the query.
//   - query: the text to match, already trimmed and non-empty.
//   - exclude: an account left out of the results, normally the reader.
//
// Returns:
//   - up to ten matches ordered by name.
//   - an error if the query fails.
func (r *UserRepository) Search(ctx context.Context, query string, exclude uuid.UUID) ([]domain.TripUser, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.display_name, u.email FROM users u
		 WHERE u.is_active AND u.id <> $2
		   AND (u.display_name ILIKE $1 ESCAPE '\' OR u.email ILIKE $1 ESCAPE '\')
		 ORDER BY lower(u.display_name), u.email
		 LIMIT $3`, likePattern(query), exclude, maxUserSearchResults)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TripUser, error) {
		var user domain.TripUser
		err := row.Scan(&user.ID, &user.DisplayName, &user.Email)
		return user, err
	})
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	return users, nil
}
