package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// MaxVerificationAttempts is how many wrong codes a confirmation message
// survives; after that only its link or a new message confirms the address.
const MaxVerificationAttempts = 5

// RegisterAccount - creates or refreshes a self-registered account waiting for confirmation.
//
// An address nobody holds gets a new inactive account, its confirmation and
// its message. An address whose account is still waiting takes the new name,
// password and language - the person registering again is the one who can
// read the mail - and a new message unless one was sent within cooldown; it
// keeps the time of its first registration, so registering again never
// postpones its deletion. An address held by any other account is left alone,
// so the caller can answer every address alike.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - user: the account to create, with its ID, address, name, password hash
//     and preferences set.
//   - verification: the new confirmation; its UserID is set here.
//   - message: the rendered confirmation message.
//   - now: the reference time, recorded as the registration and sending time.
//   - cooldown: the least time between two confirmation messages.
//
// Returns:
//   - an error if the account cannot be stored.
func (r *InvitationRepository) RegisterAccount(ctx context.Context, user domain.User, verification domain.EmailVerification,
	message domain.MailMessage, now time.Time, cooldown time.Duration) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var existingID uuid.UUID
		var unverifiedSince *time.Time
		err := tx.QueryRow(ctx,
			`SELECT id, email_unverified_since FROM users WHERE lower(email) = lower($1) FOR UPDATE`, user.Email).
			Scan(&existingID, &unverifiedSince)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx,
				`INSERT INTO users
				 (id, email, display_name, password_hash, is_admin, is_active, must_change_password,
				  email_notifications, email_unverified_since, locale, theme, units, default_currency)
				 VALUES ($1, $2, $3, $4, false, false, false, true, $5, nullif($6, ''), $7, $8, $9)`,
				user.ID, user.Email, user.DisplayName, user.PasswordHash, now, user.Locale,
				user.Theme, user.Units, user.DefaultCurrency); err != nil {
				return fmt.Errorf("create self-registered user: %w", err)
			}
			verification.UserID = user.ID
		case err != nil:
			return fmt.Errorf("find registering address: %w", err)
		case unverifiedSince == nil:
			return nil
		default:
			if _, err := tx.Exec(ctx,
				`UPDATE users SET display_name = $2, password_hash = $3, locale = nullif($4, ''), updated_at = now()
				 WHERE id = $1`, existingID, user.DisplayName, user.PasswordHash, user.Locale); err != nil {
				return fmt.Errorf("refresh self-registered user: %w", err)
			}
			wait, err := verificationWait(ctx, tx, existingID, now, cooldown)
			if err != nil || wait > 0 {
				return err
			}
			verification.UserID = existingID
		}
		return storeVerificationTx(ctx, tx, verification, message)
	})
	// Two registrations of one address at once: the other one made the account.
	if isUniqueViolation(err) {
		return nil
	}
	return err
}

// verificationWait tells how long until another confirmation message may be sent.
func verificationWait(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time, cooldown time.Duration) (time.Duration, error) {
	var sentAt time.Time
	err := tx.QueryRow(ctx, `SELECT sent_at FROM email_verifications WHERE user_id = $1`, userID).Scan(&sentAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read confirmation time: %w", err)
	}
	return max(sentAt.Add(cooldown).Sub(now), 0), nil
}

// storeVerificationTx replaces an account's confirmation and queues its message.
func storeVerificationTx(ctx context.Context, tx pgx.Tx, verification domain.EmailVerification, message domain.MailMessage) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO email_verifications (user_id, token_hash, code_hash, attempts, expires_at, sent_at)
		 VALUES ($1, $2, $3, 0, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE
		 SET token_hash = excluded.token_hash, code_hash = excluded.code_hash, attempts = 0,
		     expires_at = excluded.expires_at, sent_at = excluded.sent_at`,
		verification.UserID, verification.TokenHash, verification.CodeHash,
		verification.ExpiresAt, verification.SentAt); err != nil {
		return fmt.Errorf("store email confirmation: %w", err)
	}
	return enqueueMailTx(ctx, tx, message)
}

// VerificationWait - tells how long an account waits before another confirmation message.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the self-registered account.
//   - now: the reference time.
//   - cooldown: the least time between two confirmation messages.
//
// Returns:
//   - the time left, zero when a message may be sent now.
//   - an error if it cannot be read.
func (r *InvitationRepository) VerificationWait(ctx context.Context, userID uuid.UUID, now time.Time,
	cooldown time.Duration) (time.Duration, error) {
	var wait time.Duration
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		wait, err = verificationWait(ctx, tx, userID, now, cooldown)
		return err
	})
	return wait, err
}

// ResendVerification - replaces an account's confirmation unless the last one is too recent.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - verification: the new confirmation, with its UserID set.
//   - message: the rendered confirmation message.
//   - now: the reference time.
//   - cooldown: the least time between two confirmation messages.
//
// Returns:
//   - the time left before a message may be sent; nothing was sent when it is above zero.
//   - domain.ErrNotFound when the account is not waiting for confirmation.
//   - another error if the confirmation cannot be stored.
func (r *InvitationRepository) ResendVerification(ctx context.Context, verification domain.EmailVerification,
	message domain.MailMessage, now time.Time, cooldown time.Duration) (time.Duration, error) {
	var wait time.Duration
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var pending bool
		err := tx.QueryRow(ctx,
			`SELECT email_unverified_since IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, verification.UserID).
			Scan(&pending)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !pending) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock unconfirmed user: %w", err)
		}
		if wait, err = verificationWait(ctx, tx, verification.UserID, now, cooldown); err != nil || wait > 0 {
			return err
		}
		return storeVerificationTx(ctx, tx, verification, message)
	})
	return wait, err
}

// ConfirmEmailByToken - confirms an address with the link of its message and activates the account.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the token in the link.
//   - now: the reference time.
//
// Returns:
//   - the activated account.
//   - domain.ErrTokenInvalid when the link is unknown, replaced or expired.
func (r *InvitationRepository) ConfirmEmailByToken(ctx context.Context, tokenHash []byte, now time.Time) (domain.User, error) {
	var user domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var userID uuid.UUID
		var expiresAt time.Time
		err := tx.QueryRow(ctx,
			`SELECT user_id, expires_at FROM email_verifications WHERE token_hash = $1 FOR UPDATE`, tokenHash).
			Scan(&userID, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTokenInvalid
		}
		if err != nil {
			return fmt.Errorf("read email confirmation: %w", err)
		}
		if !now.Before(expiresAt) {
			return domain.ErrTokenInvalid
		}
		user, err = activateTx(ctx, tx, userID)
		return err
	})
	return user, err
}

// ConfirmEmailByCode - confirms an address with the code of its message and activates the account.
//
// A wrong code is counted and the count is kept; after
// MaxVerificationAttempts of them the code stops working, while the link of
// the same message still does.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - email: the normalised address being confirmed.
//   - codeHash: digest of the typed code.
//   - now: the reference time.
//
// Returns:
//   - the activated account.
//   - domain.ErrTokenInvalid for an unknown address, a wrong, used up or expired code.
func (r *InvitationRepository) ConfirmEmailByCode(ctx context.Context, email string, codeHash []byte,
	now time.Time) (domain.User, error) {
	var user domain.User
	wrong := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var userID uuid.UUID
		var storedHash []byte
		var attempts int
		var expiresAt time.Time
		err := tx.QueryRow(ctx,
			`SELECT v.user_id, v.code_hash, v.attempts, v.expires_at
			 FROM email_verifications v JOIN users u ON u.id = v.user_id
			 WHERE lower(u.email) = lower($1) FOR UPDATE OF v`, email).
			Scan(&userID, &storedHash, &attempts, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTokenInvalid
		}
		if err != nil {
			return fmt.Errorf("read email confirmation: %w", err)
		}
		if attempts >= MaxVerificationAttempts || !now.Before(expiresAt) {
			return domain.ErrTokenInvalid
		}
		if subtle.ConstantTimeCompare(storedHash, codeHash) != 1 {
			// The count is committed: the transaction succeeds and the caller
			// is told the code was wrong.
			wrong = true
			_, err := tx.Exec(ctx, `UPDATE email_verifications SET attempts = attempts + 1 WHERE user_id = $1`, userID)
			if err != nil {
				return fmt.Errorf("count wrong confirmation code: %w", err)
			}
			return nil
		}
		user, err = activateTx(ctx, tx, userID)
		return err
	})
	if err == nil && wrong {
		return domain.User{}, domain.ErrTokenInvalid
	}
	return user, err
}

// activateTx makes a confirmed account active and drops its confirmation.
func activateTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (domain.User, error) {
	user, err := oneUser(tx.QueryRow(ctx,
		`UPDATE users AS u SET is_active = true, email_unverified_since = NULL, updated_at = now()
		 WHERE u.id = $1 AND u.email_unverified_since IS NOT NULL
		 RETURNING `+userColumns, userID), "activate confirmed user")
	if errors.Is(err, domain.ErrNotFound) {
		return user, domain.ErrTokenInvalid
	}
	if err != nil {
		return user, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID); err != nil {
		return user, fmt.Errorf("drop email confirmation: %w", err)
	}
	return user, nil
}

// PurgeUnverified - deletes self-registered accounts that never confirmed their address.
//
// Such an account was never active, so it owns no trips, files or sessions;
// its confirmation goes with it.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - registeredBefore: accounts first registered before this are deleted.
//
// Returns:
//   - how many accounts were deleted.
//   - an error if they cannot be deleted.
func (r *InvitationRepository) PurgeUnverified(ctx context.Context, registeredBefore time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM users WHERE email_unverified_since IS NOT NULL AND email_unverified_since < $1 AND NOT is_active`,
		registeredBefore)
	if err != nil {
		return 0, fmt.Errorf("purge unconfirmed users: %w", err)
	}
	return tag.RowsAffected(), nil
}
