package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// InvitationRepository keeps one-time password recovery and invitation credentials.
type InvitationRepository struct {
	pool *pgxpool.Pool
}

// NewInvitationRepository - creates the invitation repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to the pool.
func NewInvitationRepository(pool *pgxpool.Pool) *InvitationRepository {
	return &InvitationRepository{pool: pool}
}

// PasswordResetUser - finds an active account eligible for email recovery.
//
// Arguments:
//   - ctx: context bounding the query.
//   - email: the normalised address.
//
// Returns:
//   - the active account.
//   - domain.ErrNotFound for an unknown or inactive address.
func (r *InvitationRepository) PasswordResetUser(ctx context.Context, email string) (domain.User, error) {
	user, err := oneUser(r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users u WHERE lower(u.email) = lower($1) AND u.is_active`, email),
		"read password recovery account")
	return user, err
}

// enqueueMailTx appends a rendered message inside another domain transaction.
func enqueueMailTx(ctx context.Context, tx pgx.Tx, message domain.MailMessage) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO mail_outbox (id, recipient, subject, text_body, html_body, discard_after)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		message.ID, message.Recipient, message.Subject, message.TextBody, message.HTMLBody, message.DiscardAfter)
	if err != nil {
		return fmt.Errorf("queue mail: %w", err)
	}
	return nil
}

// CreatePasswordReset - replaces an account's unused recovery credentials and queues its message.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - userID: the account recovering access.
//   - tokenHash: digest of the credential sent by email.
//   - expiresAt: when the credential stops working.
//   - message: the rendered recovery message.
//
// Returns:
//   - an error if the token and message cannot be committed together.
func (r *InvitationRepository) CreatePasswordReset(ctx context.Context, userID uuid.UUID, tokenHash []byte,
	expiresAt time.Time, message domain.MailMessage) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
			return fmt.Errorf("close old password reset tokens: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
			uuid.Must(uuid.NewV7()), userID, tokenHash, expiresAt); err != nil {
			return fmt.Errorf("create password reset token: %w", err)
		}
		return enqueueMailTx(ctx, tx, message)
	})
}

// PasswordResetValid - checks a recovery credential before expensive password hashing.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tokenHash: digest of the presented credential.
//   - now: the reference time.
//
// Returns:
//   - nil when the credential is currently usable.
//   - domain.ErrTokenInvalid otherwise.
func (r *InvitationRepository) PasswordResetValid(ctx context.Context, tokenHash []byte, now time.Time) error {
	var usable bool
	err := r.pool.QueryRow(ctx,
		`SELECT used_at IS NULL AND expires_at > $2 FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash, now).Scan(&usable)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !usable) {
		return domain.ErrTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("check password reset token: %w", err)
	}
	return nil
}

// ResetPassword - consumes a recovery credential, stores the new password and ends every session.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - passwordHash: hash of the new password.
//   - now: the reference time.
//
// Returns:
//   - the account whose password changed.
//   - domain.ErrTokenInvalid when the credential is unusable.
func (r *InvitationRepository) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash string,
	now time.Time) (domain.User, error) {
	var user domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var userID uuid.UUID
		var expiresAt time.Time
		var usedAt *time.Time
		err := tx.QueryRow(ctx,
			`SELECT user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1 FOR UPDATE`, tokenHash).
			Scan(&userID, &expiresAt, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTokenInvalid
		}
		if err != nil {
			return fmt.Errorf("read password reset token: %w", err)
		}
		if usedAt != nil || !now.Before(expiresAt) {
			return domain.ErrTokenInvalid
		}
		if _, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, must_change_password = false, updated_at = now() WHERE id = $1`,
			userID, passwordHash); err != nil {
			return fmt.Errorf("reset recovered password: %w", err)
		}
		if err := revokeAllSessions(ctx, tx, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE password_reset_tokens SET used_at = $2 WHERE token_hash = $1`, tokenHash, now); err != nil {
			return fmt.Errorf("consume password reset token: %w", err)
		}
		user, err = oneUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = $1`, userID), "read recovered account")
		return err
	})
	return user, err
}

// CreateUserInvitation - stores an administrator invitation and its message atomically.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - invitation: the invitation metadata.
//   - tokenHash: digest of the credential sent by email.
//   - message: the rendered invitation message.
//
// Returns:
//   - domain.ErrAlreadyExists when the address already has an account or open invitation.
func (r *InvitationRepository) CreateUserInvitation(ctx context.Context, invitation domain.UserInvitation,
	tokenHash []byte, message domain.MailMessage) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email) = lower($1) AND email_unverified_since IS NULL)`, invitation.Email).Scan(&exists); err != nil {
			return fmt.Errorf("check invited account: %w", err)
		}
		if exists {
			return domain.ErrAlreadyExists
		}
		if _, err := tx.Exec(ctx,
			`UPDATE user_invitations SET revoked_at = now()
			 WHERE lower(email) = lower($1) AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= now()`, invitation.Email); err != nil {
			return fmt.Errorf("close expired user invitations: %w", err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO user_invitations
			 (id, email, display_name, is_admin, token_hash, expires_at, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`, invitation.ID, invitation.Email,
			invitation.DisplayName, invitation.IsAdmin, tokenHash, invitation.ExpiresAt, invitation.CreatedBy)
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create user invitation: %w", err)
		}
		return enqueueMailTx(ctx, tx, message)
	})
}

// UserInvitations - lists administrator invitations, newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - all account invitations without their credential hashes.
//   - an error if they cannot be read.
func (r *InvitationRepository) UserInvitations(ctx context.Context) ([]domain.UserInvitation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, email, display_name, is_admin, expires_at, accepted_at, revoked_at, created_by, created_at
		 FROM user_invitations ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list user invitations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.UserInvitation, error) {
		var item domain.UserInvitation
		err := row.Scan(&item.ID, &item.Email, &item.DisplayName, &item.IsAdmin, &item.ExpiresAt,
			&item.AcceptedAt, &item.RevokedAt, &item.CreatedBy, &item.CreatedAt)
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("read user invitations: %w", err)
	}
	return items, nil
}

// RevokeUserInvitation - withdraws an open administrator invitation.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the invitation to withdraw.
//   - now: the reference time used to reject expired invitations.
//
// Returns:
//   - domain.ErrNotFound when the invitation is no longer open.
//   - another error when the change cannot be stored.
func (r *InvitationRepository) RevokeUserInvitation(ctx context.Context, id uuid.UUID, now time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE user_invitations SET revoked_at = $2
		 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2`, id, now)
	if err != nil {
		return fmt.Errorf("revoke user invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UserInvitationByToken - reads what a user invitation offers.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tokenHash: digest of the presented credential.
//
// Returns:
//   - the invitation, including its lifecycle timestamps.
//   - domain.ErrNotFound for an unknown credential.
func (r *InvitationRepository) UserInvitationByToken(ctx context.Context, tokenHash []byte) (domain.UserInvitation, error) {
	var item domain.UserInvitation
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, display_name, is_admin, expires_at, accepted_at, revoked_at, created_by, created_at
		 FROM user_invitations WHERE token_hash = $1`, tokenHash).
		Scan(&item.ID, &item.Email, &item.DisplayName, &item.IsAdmin, &item.ExpiresAt,
			&item.AcceptedAt, &item.RevokedAt, &item.CreatedBy, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read user invitation: %w", err)
	}
	return item, nil
}

// AcceptUserInvitation - consumes an invitation and creates its account.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - passwordHash: the new account's already hashed password.
//   - locale: the language chosen by the invitee.
//   - now: the reference time used to check expiry and record acceptance.
//
// Returns:
//   - the newly created account.
//   - domain.ErrTokenInvalid when the invitation cannot be redeemed.
//   - domain.ErrAlreadyExists when its email already has an account.
func (r *InvitationRepository) AcceptUserInvitation(ctx context.Context, tokenHash []byte, passwordHash, locale string,
	now time.Time) (domain.User, error) {
	var created domain.User
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var invitation domain.UserInvitation
		err := tx.QueryRow(ctx,
			`SELECT id, email, display_name, is_admin, expires_at, accepted_at, revoked_at, created_by, created_at
			 FROM user_invitations WHERE token_hash = $1 FOR UPDATE`, tokenHash).
			Scan(&invitation.ID, &invitation.Email, &invitation.DisplayName, &invitation.IsAdmin,
				&invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.RevokedAt,
				&invitation.CreatedBy, &invitation.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTokenInvalid
		}
		if err != nil {
			return fmt.Errorf("lock user invitation: %w", err)
		}
		if !invitation.Redeemable(now) {
			return domain.ErrTokenInvalid
		}
		if err := dropUnverifiedTx(ctx, tx, invitation.Email); err != nil {
			return err
		}
		created, err = scanUser(tx.QueryRow(ctx,
			`INSERT INTO users AS u
			 (id, email, display_name, password_hash, is_admin, is_active, must_change_password,
			  email_notifications, locale, theme, units, default_currency)
			 VALUES ($1, $2, $3, $4, $5, true, false, true, nullif($6, ''), $7, $8, $9)
			 RETURNING `+userColumns,
			uuid.Must(uuid.NewV7()), invitation.Email, invitation.DisplayName, passwordHash, invitation.IsAdmin,
			locale, domain.ThemeAuto, domain.UnitsKilometres, domain.DefaultCurrency))
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create invited user: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE user_invitations SET accepted_at = $2 WHERE id = $1`, invitation.ID, now); err != nil {
			return fmt.Errorf("accept user invitation: %w", err)
		}
		return nil
	})
	return created, err
}

// CreateTripInvitation - stores a trip invitation and its message atomically.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - invitation: the offered trip role and lifecycle metadata.
//   - tokenHash: digest of the credential sent by email.
//   - message: the rendered invitation message.
//
// Returns:
//   - domain.ErrAlreadyMember when the address already belongs to the trip.
//   - domain.ErrAlreadyExists when an open invitation already exists.
//   - another error when the invitation and message cannot be committed.
func (r *InvitationRepository) CreateTripInvitation(ctx context.Context, invitation domain.TripInvitation,
	tokenHash []byte, message domain.MailMessage) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var alreadyMember bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (
			   SELECT 1 FROM trips t JOIN users u ON u.id = t.owner_id
			   WHERE t.id = $1 AND lower(u.email) = lower($2)
			   UNION ALL
			   SELECT 1 FROM trip_members m JOIN users u ON u.id = m.user_id
			   WHERE m.trip_id = $1 AND lower(u.email) = lower($2)
			 )`, invitation.TripID, invitation.Email).Scan(&alreadyMember); err != nil {
			return fmt.Errorf("check invited trip member: %w", err)
		}
		if alreadyMember {
			return domain.ErrAlreadyMember
		}
		if _, err := tx.Exec(ctx,
			`UPDATE trip_invitations SET revoked_at = now()
			 WHERE trip_id = $1 AND lower(email) = lower($2) AND accepted_at IS NULL
			   AND revoked_at IS NULL AND expires_at <= now()`, invitation.TripID, invitation.Email); err != nil {
			return fmt.Errorf("close expired trip invitations: %w", err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO trip_invitations
			 (id, trip_id, email, role, token_hash, expires_at, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`, invitation.ID, invitation.TripID,
			invitation.Email, invitation.Role, tokenHash, invitation.ExpiresAt, invitation.CreatedBy)
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create trip invitation: %w", err)
		}
		return enqueueMailTx(ctx, tx, message)
	})
}

// TripInvitations - lists invitations of one trip, newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip whose invitations are requested.
//
// Returns:
//   - all invitations without their credential hashes.
//   - an error if they cannot be read.
func (r *InvitationRepository) TripInvitations(ctx context.Context, tripID uuid.UUID) ([]domain.TripInvitation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT i.id, i.trip_id, t.title, i.email, i.role, i.expires_at, i.accepted_by,
		        i.accepted_at, i.revoked_at, i.created_by, i.created_at
		 FROM trip_invitations i JOIN trips t ON t.id = i.trip_id
		 WHERE i.trip_id = $1 ORDER BY i.created_at DESC`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list trip invitations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TripInvitation, error) {
		return scanTripInvitation(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read trip invitations: %w", err)
	}
	return items, nil
}

// scanTripInvitation reads one trip invitation row.
func scanTripInvitation(row interface{ Scan(...any) error }) (domain.TripInvitation, error) {
	var item domain.TripInvitation
	err := row.Scan(&item.ID, &item.TripID, &item.TripTitle, &item.Email, &item.Role, &item.ExpiresAt,
		&item.AcceptedBy, &item.AcceptedAt, &item.RevokedAt, &item.CreatedBy, &item.CreatedAt)
	return item, err
}

// TripInvitationByToken - reads what a trip invitation offers.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tokenHash: digest of the presented credential.
//
// Returns:
//   - the invitation and trip title.
//   - domain.ErrNotFound for an unknown credential.
func (r *InvitationRepository) TripInvitationByToken(ctx context.Context, tokenHash []byte) (domain.TripInvitation, error) {
	item, err := scanTripInvitation(r.pool.QueryRow(ctx,
		`SELECT i.id, i.trip_id, t.title, i.email, i.role, i.expires_at, i.accepted_by,
		        i.accepted_at, i.revoked_at, i.created_by, i.created_at
		 FROM trip_invitations i JOIN trips t ON t.id = i.trip_id WHERE i.token_hash = $1`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read trip invitation: %w", err)
	}
	return item, nil
}

// RevokeTripInvitation - withdraws an open invitation belonging to a trip.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - tripID: the trip that must own the invitation.
//   - id: the invitation to withdraw.
//   - now: the reference time used to reject expired invitations.
//
// Returns:
//   - domain.ErrNotFound when the invitation is absent or no longer open.
//   - another error when the change cannot be stored.
func (r *InvitationRepository) RevokeTripInvitation(ctx context.Context, tripID, id uuid.UUID, now time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE trip_invitations SET revoked_at = $3
		 WHERE id = $1 AND trip_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3`,
		id, tripID, now)
	if err != nil {
		return fmt.Errorf("revoke trip invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AcceptTripInvitation - adds an existing account to a trip and consumes the invitation.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - user: the signed-in account claiming the invitation.
//   - now: the reference time used to check expiry and record acceptance.
//
// Returns:
//   - the trip joined by the account.
//   - domain.ErrTokenInvalid when the invitation cannot be redeemed.
//   - domain.ErrForbidden when the account email does not match.
func (r *InvitationRepository) AcceptTripInvitation(ctx context.Context, tokenHash []byte, user domain.User,
	now time.Time) (uuid.UUID, error) {
	var tripID uuid.UUID
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		invitation, err := lockTripInvitation(ctx, tx, tokenHash, now)
		if err != nil {
			return err
		}
		if !user.IsActive || !equalEmail(user.Email, invitation.Email) {
			return domain.ErrForbidden
		}
		ownerID, err := lockTrip(ctx, tx, invitation.TripID)
		if err != nil {
			return err
		}
		if ownerID == user.ID {
			return domain.ErrAlreadyMember
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)`,
			invitation.TripID, user.ID, invitation.Role); err != nil {
			if isUniqueViolation(err) {
				return domain.ErrAlreadyMember
			}
			return fmt.Errorf("add invited trip member: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE trip_invitations SET accepted_by = $2, accepted_at = $3 WHERE id = $1`,
			invitation.ID, user.ID, now); err != nil {
			return fmt.Errorf("accept trip invitation: %w", err)
		}
		tripID = invitation.TripID
		return nil
	})
	return tripID, err
}

// RegisterTripInvitation - creates an account and accepts its trip invitation atomically.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - user: the validated profile and hashed password for the new account.
//   - now: the reference time used to check expiry and record acceptance.
//
// Returns:
//   - the newly created account.
//   - the trip joined by that account.
//   - domain.ErrTokenInvalid when the invitation cannot be redeemed.
//   - domain.ErrAlreadyExists when its email already has an account.
func (r *InvitationRepository) RegisterTripInvitation(ctx context.Context, tokenHash []byte, user domain.User,
	now time.Time) (domain.User, uuid.UUID, error) {
	var created domain.User
	var tripID uuid.UUID
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		invitation, err := lockTripInvitation(ctx, tx, tokenHash, now)
		if err != nil {
			return err
		}
		if err := dropUnverifiedTx(ctx, tx, invitation.Email); err != nil {
			return err
		}
		created, err = scanUser(tx.QueryRow(ctx,
			`INSERT INTO users AS u
			 (id, email, display_name, password_hash, is_admin, is_active, must_change_password,
			  email_notifications, locale, theme, units, default_currency)
			 VALUES ($1, $2, $3, $4, false, true, false, true, nullif($5, ''), $6, $7, $8)
			 RETURNING `+userColumns,
			user.ID, invitation.Email, user.DisplayName, user.PasswordHash, user.Locale,
			user.Theme, user.Units, user.DefaultCurrency))
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create trip invitee: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)`,
			invitation.TripID, created.ID, invitation.Role); err != nil {
			return fmt.Errorf("add registered invitee: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE trip_invitations SET accepted_by = $2, accepted_at = $3 WHERE id = $1`,
			invitation.ID, created.ID, now); err != nil {
			return fmt.Errorf("accept registered invitation: %w", err)
		}
		tripID = invitation.TripID
		return nil
	})
	return created, tripID, err
}

// dropUnverifiedTx removes a self-registered account still waiting for its
// address to be confirmed. An invitation that is being accepted proves the
// address, so the account it creates takes the place of that waiting one.
func dropUnverifiedTx(ctx context.Context, tx pgx.Tx, email string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM users WHERE lower(email) = lower($1) AND email_unverified_since IS NOT NULL`, email); err != nil {
		return fmt.Errorf("drop unconfirmed account: %w", err)
	}
	return nil
}

// lockTripInvitation reads a usable invitation for an accepting transaction.
func lockTripInvitation(ctx context.Context, tx pgx.Tx, tokenHash []byte, now time.Time) (domain.TripInvitation, error) {
	var item domain.TripInvitation
	err := tx.QueryRow(ctx,
		`SELECT i.id, i.trip_id, t.title, i.email, i.role, i.expires_at, i.accepted_by,
		        i.accepted_at, i.revoked_at, i.created_by, i.created_at
		 FROM trip_invitations i JOIN trips t ON t.id = i.trip_id
		 WHERE i.token_hash = $1 FOR UPDATE OF i`, tokenHash).
		Scan(&item.ID, &item.TripID, &item.TripTitle, &item.Email, &item.Role, &item.ExpiresAt,
			&item.AcceptedBy, &item.AcceptedAt, &item.RevokedAt, &item.CreatedBy, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrTokenInvalid
	}
	if err != nil {
		return item, fmt.Errorf("lock trip invitation: %w", err)
	}
	if !item.Redeemable(now) {
		return item, domain.ErrTokenInvalid
	}
	return item, nil
}

// equalEmail compares normalised addresses defensively for old rows.
func equalEmail(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// ideaInvitationColumns is the select list of an invitation to a list of
// ideas, kept in step with scanIdeaInvitation; i is the invitation and o its owner.
const ideaInvitationColumns = `i.id, i.owner_id, o.display_name, i.email, i.role, i.expires_at, i.accepted_by,
	i.accepted_at, i.revoked_at, i.created_at`

// scanIdeaInvitation reads one invitation to a list of ideas.
func scanIdeaInvitation(row interface{ Scan(...any) error }) (domain.IdeaInvitation, error) {
	var item domain.IdeaInvitation
	err := row.Scan(&item.ID, &item.OwnerID, &item.OwnerName, &item.Email, &item.Role, &item.ExpiresAt,
		&item.AcceptedBy, &item.AcceptedAt, &item.RevokedAt, &item.CreatedAt)
	return item, err
}

// CreateIdeaInvitation - stores an invitation to a list of ideas and its
// message atomically.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - invitation: the offered role and lifecycle metadata.
//   - tokenHash: digest of the credential sent by email.
//   - message: the rendered invitation message.
//
// Returns:
//   - domain.ErrAlreadyMember when the address is the owner's or a member's.
//   - domain.ErrAlreadyExists when an open invitation already exists.
//   - another error when the invitation and message cannot be committed.
func (r *InvitationRepository) CreateIdeaInvitation(ctx context.Context, invitation domain.IdeaInvitation,
	tokenHash []byte, message domain.MailMessage) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var alreadyMember bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (
			   SELECT 1 FROM users u WHERE u.id = $1 AND lower(u.email) = lower($2)
			   UNION ALL
			   SELECT 1 FROM idea_members m JOIN users u ON u.id = m.user_id
			   WHERE m.owner_id = $1 AND lower(u.email) = lower($2)
			 )`, invitation.OwnerID, invitation.Email).Scan(&alreadyMember); err != nil {
			return fmt.Errorf("check invited idea member: %w", err)
		}
		if alreadyMember {
			return domain.ErrAlreadyMember
		}
		if _, err := tx.Exec(ctx,
			`UPDATE idea_invitations SET revoked_at = now()
			 WHERE owner_id = $1 AND lower(email) = lower($2) AND accepted_at IS NULL
			   AND revoked_at IS NULL AND expires_at <= now()`, invitation.OwnerID, invitation.Email); err != nil {
			return fmt.Errorf("close expired idea invitations: %w", err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO idea_invitations (id, owner_id, email, role, token_hash, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`, invitation.ID, invitation.OwnerID,
			invitation.Email, invitation.Role, tokenHash, invitation.ExpiresAt)
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create idea invitation: %w", err)
		}
		return enqueueMailTx(ctx, tx, message)
	})
}

// IdeaInvitations - lists the invitations to a person's list of ideas, newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - ownerID: the owner of the list.
//
// Returns:
//   - all invitations without their credential hashes.
//   - an error if they cannot be read.
func (r *InvitationRepository) IdeaInvitations(ctx context.Context, ownerID uuid.UUID) ([]domain.IdeaInvitation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+ideaInvitationColumns+`
		 FROM idea_invitations i JOIN users o ON o.id = i.owner_id
		 WHERE i.owner_id = $1 ORDER BY i.created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list idea invitations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.IdeaInvitation, error) {
		return scanIdeaInvitation(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read idea invitations: %w", err)
	}
	return items, nil
}

// IdeaInvitationByToken - reads what an invitation to a list of ideas offers.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tokenHash: digest of the presented credential.
//
// Returns:
//   - the invitation with its owner's name.
//   - domain.ErrNotFound for an unknown credential.
func (r *InvitationRepository) IdeaInvitationByToken(ctx context.Context, tokenHash []byte) (domain.IdeaInvitation, error) {
	item, err := scanIdeaInvitation(r.pool.QueryRow(ctx,
		`SELECT `+ideaInvitationColumns+`
		 FROM idea_invitations i JOIN users o ON o.id = i.owner_id WHERE i.token_hash = $1`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read idea invitation: %w", err)
	}
	return item, nil
}

// RevokeIdeaInvitation - withdraws an open invitation to a person's list of ideas.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - ownerID: the owner of the list, who must own the invitation.
//   - id: the invitation to withdraw.
//   - now: the reference time used to reject expired invitations.
//
// Returns:
//   - domain.ErrNotFound when the invitation is absent or no longer open.
//   - another error when the change cannot be stored.
func (r *InvitationRepository) RevokeIdeaInvitation(ctx context.Context, ownerID, id uuid.UUID, now time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE idea_invitations SET revoked_at = $3
		 WHERE id = $1 AND owner_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3`,
		id, ownerID, now)
	if err != nil {
		return fmt.Errorf("revoke idea invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AcceptIdeaInvitation - adds an existing account to a list of ideas and
// consumes the invitation.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - user: the signed-in account claiming the invitation.
//   - now: the reference time used to check expiry and record acceptance.
//
// Returns:
//   - the invitation as it was accepted.
//   - domain.ErrTokenInvalid when the invitation cannot be redeemed.
//   - domain.ErrForbidden when the account email does not match.
//   - domain.ErrAlreadyMember when the account has the list already.
func (r *InvitationRepository) AcceptIdeaInvitation(ctx context.Context, tokenHash []byte, user domain.User,
	now time.Time) (domain.IdeaInvitation, error) {
	var invitation domain.IdeaInvitation
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if invitation, err = lockIdeaInvitation(ctx, tx, tokenHash, now); err != nil {
			return err
		}
		if !user.IsActive || !equalEmail(user.Email, invitation.Email) {
			return domain.ErrForbidden
		}
		if invitation.OwnerID == user.ID {
			return domain.ErrAlreadyMember
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_members (owner_id, user_id, role) VALUES ($1, $2, $3)`,
			invitation.OwnerID, user.ID, invitation.Role); err != nil {
			if isUniqueViolation(err) {
				return domain.ErrAlreadyMember
			}
			return fmt.Errorf("add invited idea member: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE idea_invitations SET accepted_by = $2, accepted_at = $3 WHERE id = $1`,
			invitation.ID, user.ID, now); err != nil {
			return fmt.Errorf("accept idea invitation: %w", err)
		}
		return nil
	})
	return invitation, err
}

// RegisterIdeaInvitation - creates an account and accepts its invitation to a
// list of ideas atomically.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tokenHash: digest of the presented credential.
//   - user: the validated profile and hashed password for the new account.
//   - now: the reference time used to check expiry and record acceptance.
//
// Returns:
//   - the newly created account.
//   - the invitation as it was accepted.
//   - domain.ErrTokenInvalid when the invitation cannot be redeemed.
//   - domain.ErrAlreadyExists when its email already has an account.
func (r *InvitationRepository) RegisterIdeaInvitation(ctx context.Context, tokenHash []byte, user domain.User,
	now time.Time) (domain.User, domain.IdeaInvitation, error) {
	var created domain.User
	var invitation domain.IdeaInvitation
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if invitation, err = lockIdeaInvitation(ctx, tx, tokenHash, now); err != nil {
			return err
		}
		if err := dropUnverifiedTx(ctx, tx, invitation.Email); err != nil {
			return err
		}
		created, err = scanUser(tx.QueryRow(ctx,
			`INSERT INTO users AS u
			 (id, email, display_name, password_hash, is_admin, is_active, must_change_password,
			  email_notifications, locale, theme, units, default_currency)
			 VALUES ($1, $2, $3, $4, false, true, false, true, nullif($5, ''), $6, $7, $8)
			 RETURNING `+userColumns,
			user.ID, invitation.Email, user.DisplayName, user.PasswordHash, user.Locale,
			user.Theme, user.Units, user.DefaultCurrency))
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("create idea invitee: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_members (owner_id, user_id, role) VALUES ($1, $2, $3)`,
			invitation.OwnerID, created.ID, invitation.Role); err != nil {
			return fmt.Errorf("add registered idea invitee: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE idea_invitations SET accepted_by = $2, accepted_at = $3 WHERE id = $1`,
			invitation.ID, created.ID, now); err != nil {
			return fmt.Errorf("accept registered idea invitation: %w", err)
		}
		return nil
	})
	return created, invitation, err
}

// lockIdeaInvitation reads a usable invitation to a list of ideas for an
// accepting transaction.
func lockIdeaInvitation(ctx context.Context, tx pgx.Tx, tokenHash []byte, now time.Time) (domain.IdeaInvitation, error) {
	item, err := scanIdeaInvitation(tx.QueryRow(ctx,
		`SELECT `+ideaInvitationColumns+`
		 FROM idea_invitations i JOIN users o ON o.id = i.owner_id
		 WHERE i.token_hash = $1 FOR UPDATE OF i`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrTokenInvalid
	}
	if err != nil {
		return item, fmt.Errorf("lock idea invitation: %w", err)
	}
	if !item.Redeemable(now) {
		return item, domain.ErrTokenInvalid
	}
	return item, nil
}
