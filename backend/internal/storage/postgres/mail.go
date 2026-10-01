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

// MailRepository keeps the instance mail switch and the persistent outbox.
type MailRepository struct {
	pool *pgxpool.Pool
}

// NewMailRepository - creates the mail repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to the pool.
func NewMailRepository(pool *pgxpool.Pool) *MailRepository {
	return &MailRepository{pool: pool}
}

// MailSettings - reads whether administrators enabled mail delivery.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the stored policy.
//   - an error if it cannot be read.
func (r *MailRepository) MailSettings(ctx context.Context) (domain.MailSettings, error) {
	var settings domain.MailSettings
	if err := r.pool.QueryRow(ctx,
		`SELECT mail_enabled, self_registration FROM instance_settings WHERE id = 1`).
		Scan(&settings.Enabled, &settings.SelfRegistration); err != nil {
		return settings, fmt.Errorf("read mail settings: %w", err)
	}
	return settings, nil
}

// SetMailEnabled - changes the administrator-controlled delivery switch.
//
// Arguments:
//   - ctx: context bounding the query.
//   - enabled: whether queued messages may be delivered.
//
// Returns:
//   - the stored policy.
//   - an error if it cannot be changed.
func (r *MailRepository) SetMailEnabled(ctx context.Context, enabled bool) (domain.MailSettings, error) {
	var settings domain.MailSettings
	err := r.pool.QueryRow(ctx,
		`UPDATE instance_settings SET mail_enabled = $1, updated_at = now() WHERE id = 1
		 RETURNING mail_enabled, self_registration`,
		enabled).Scan(&settings.Enabled, &settings.SelfRegistration)
	if err != nil {
		return settings, fmt.Errorf("set mail enabled: %w", err)
	}
	return settings, nil
}

// SetSelfRegistration - changes the administrator's registration switch.
//
// Arguments:
//   - ctx: context bounding the query.
//   - enabled: whether people may register themselves while mail is delivered.
//
// Returns:
//   - the stored policy.
//   - an error if it cannot be changed.
func (r *MailRepository) SetSelfRegistration(ctx context.Context, enabled bool) (domain.MailSettings, error) {
	var settings domain.MailSettings
	err := r.pool.QueryRow(ctx,
		`UPDATE instance_settings SET self_registration = $1, updated_at = now() WHERE id = 1
		 RETURNING mail_enabled, self_registration`,
		enabled).Scan(&settings.Enabled, &settings.SelfRegistration)
	if err != nil {
		return settings, fmt.Errorf("set self-registration: %w", err)
	}
	return settings, nil
}

// EnqueueMail - appends one rendered message to the durable delivery queue.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - message: the message to keep.
//
// Returns:
//   - an error if the message cannot be queued.
func (r *MailRepository) EnqueueMail(ctx context.Context, message domain.MailMessage) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO mail_outbox (id, recipient, subject, text_body, html_body, discard_after)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		message.ID, message.Recipient, message.Subject, message.TextBody, message.HTMLBody, message.DiscardAfter)
	if err != nil {
		return fmt.Errorf("queue mail: %w", err)
	}
	return nil
}

// ExpireMail - closes and redacts action messages whose credentials have expired.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - now: the reference time.
//
// Returns:
//   - an error if expired messages cannot be closed.
func (r *MailRepository) ExpireMail(ctx context.Context, now time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mail_outbox SET failed_at = $1, claimed_at = NULL,
		        recipient = '', subject = '', text_body = '', html_body = '',
		        last_error = 'message expired before delivery'
		 WHERE sent_at IS NULL AND failed_at IS NULL AND discard_after <= $1
		   AND (claimed_at IS NULL OR claimed_at < $1 - interval '10 minutes')`, now)
	if err != nil {
		return fmt.Errorf("expire queued mail: %w", err)
	}
	return nil
}

// ClaimMail - reserves the oldest due message for one delivery attempt.
//
// A claim abandoned by a stopped process becomes available again after ten
// minutes. SKIP LOCKED lets a future multi-process deployment share the queue.
//
// Arguments:
//   - ctx: context bounding the query.
//   - now: the reference time.
//
// Returns:
//   - the claimed message with its incremented attempt count.
//   - domain.ErrNotFound when no message is due.
func (r *MailRepository) ClaimMail(ctx context.Context, now time.Time) (domain.MailMessage, error) {
	var message domain.MailMessage
	err := r.pool.QueryRow(ctx,
		`WITH candidate AS (
		   SELECT id FROM mail_outbox
		   WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= $1
		     AND (discard_after IS NULL OR discard_after > $1)
		     AND (claimed_at IS NULL OR claimed_at < $1 - interval '10 minutes')
		   ORDER BY next_attempt_at, created_at
		   FOR UPDATE SKIP LOCKED LIMIT 1
		 )
		 UPDATE mail_outbox m SET claimed_at = $1, attempts = attempts + 1
		 FROM candidate c WHERE m.id = c.id
		 RETURNING m.id, m.recipient, m.subject, m.text_body, m.html_body, m.attempts,
		           m.discard_after, m.created_at`, now).
		Scan(&message.ID, &message.Recipient, &message.Subject, &message.TextBody, &message.HTMLBody,
			&message.Attempts, &message.DiscardAfter, &message.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MailMessage{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.MailMessage{}, fmt.Errorf("claim mail: %w", err)
	}
	return message, nil
}

// MarkMailSent - records successful acceptance by the SMTP server.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the message.
//   - at: when delivery completed.
//
// Returns:
//   - an error if the result cannot be stored.
func (r *MailRepository) MarkMailSent(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE mail_outbox SET sent_at = $2, claimed_at = NULL, last_error = '',
		 recipient = '', subject = '', text_body = '', html_body = '' WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("mark mail sent: %w", err)
	}
	return nil
}

// RetryMail - records a failed attempt and either schedules another or closes it.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the message.
//   - next: when another attempt may begin.
//   - message: the transport error, truncated before storage.
//   - final: whether no more attempts should be made.
//
// Returns:
//   - an error if the result cannot be stored.
func (r *MailRepository) RetryMail(ctx context.Context, id uuid.UUID, next time.Time, message string, final bool) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE mail_outbox
		 SET claimed_at = NULL, next_attempt_at = $2, last_error = $3,
		     failed_at = CASE WHEN $4 THEN now() ELSE NULL END,
		     recipient = CASE WHEN $4 THEN '' ELSE recipient END,
		     subject = CASE WHEN $4 THEN '' ELSE subject END,
		     text_body = CASE WHEN $4 THEN '' ELSE text_body END,
		     html_body = CASE WHEN $4 THEN '' ELSE html_body END
		 WHERE id = $1`, id, next, message, final)
	if err != nil {
		return fmt.Errorf("record mail failure: %w", err)
	}
	return nil
}

// MailStats - reports the delivery queue and its latest outcomes.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the queue statistics.
//   - an error if they cannot be read.
func (r *MailRepository) MailStats(ctx context.Context) (domain.MailStats, error) {
	var stats domain.MailStats
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE sent_at IS NULL AND failed_at IS NULL),
		        count(*) FILTER (WHERE failed_at IS NOT NULL),
		        max(sent_at), max(failed_at)
		 FROM mail_outbox`).Scan(&stats.Queued, &stats.Failed, &stats.LastSuccessAt, &stats.LastErrorAt)
	if err != nil {
		return stats, fmt.Errorf("read mail statistics: %w", err)
	}
	if stats.LastErrorAt != nil {
		if err := r.pool.QueryRow(ctx,
			`SELECT last_error FROM mail_outbox WHERE failed_at = $1 ORDER BY created_at DESC LIMIT 1`,
			*stats.LastErrorAt).Scan(&stats.LastError); err != nil {
			return stats, fmt.Errorf("read last mail error: %w", err)
		}
	}
	return stats, nil
}
