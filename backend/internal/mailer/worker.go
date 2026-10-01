package mailer

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Store is the persistent queue and administrator policy used by Worker.
type Store interface {
	MailSettings(ctx context.Context) (domain.MailSettings, error)
	ExpireMail(ctx context.Context, now time.Time) error
	ClaimMail(ctx context.Context, now time.Time) (domain.MailMessage, error)
	MarkMailSent(ctx context.Context, id uuid.UUID, at time.Time) error
	RetryMail(ctx context.Context, id uuid.UUID, next time.Time, message string, final bool) error
}

// Worker sends due messages one at a time and retries temporary failures.
type Worker struct {
	store  Store
	sender Sender
	logger *slog.Logger
	wake   chan struct{}
	now    func() time.Time
}

// NewWorker - creates a persistent mail delivery worker.
//
// Arguments:
//   - store: the queue and its enabled setting.
//   - sender: the configured SMTP transport, or nil when only expiry cleanup runs.
//   - logger: destination for delivery failures.
//
// Returns:
//   - a worker ready to run.
func NewWorker(store Store, sender Sender, logger *slog.Logger) *Worker {
	return &Worker{store: store, sender: sender, logger: logger, wake: make(chan struct{}, 1), now: time.Now}
}

// Wake - asks the worker to inspect the queue without waiting for its next poll.
//
// A pending wake is coalesced with further requests so an HTTP handler never
// waits for the worker.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run - delivers mail until the process context ends.
//
// Arguments:
//   - ctx: process lifetime.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	w.deliver(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.deliver(ctx)
		case <-w.wake:
			w.deliver(ctx)
		}
	}
}

// deliver drains due messages while delivery is enabled.
func (w *Worker) deliver(ctx context.Context) {
	if err := w.store.ExpireMail(ctx, w.now()); err != nil {
		w.logger.Warn("expire queued mail failed", slog.Any("error", err))
		return
	}
	if w.sender == nil {
		return
	}
	for {
		settings, err := w.store.MailSettings(ctx)
		if err != nil {
			w.logger.Warn("read mail settings failed", slog.Any("error", err))
			return
		}
		if !settings.Enabled {
			return
		}
		message, err := w.store.ClaimMail(ctx, w.now())
		if err == domain.ErrNotFound {
			return
		}
		if err != nil {
			w.logger.Warn("claim queued mail failed", slog.Any("error", err))
			return
		}
		if err := w.sender.Send(ctx, message); err != nil {
			final := message.Attempts >= 6
			delay := time.Minute << min(message.Attempts-1, 6)
			if storeErr := w.store.RetryMail(ctx, message.ID, w.now().Add(delay), err.Error(), final); storeErr != nil {
				w.logger.Warn("record mail delivery failure failed", slog.Any("error", storeErr))
			}
			w.logger.Warn("mail delivery failed", slog.String("message_id", message.ID.String()), slog.Int("attempt", message.Attempts), slog.Any("error", err))
			continue
		}
		if err := w.store.MarkMailSent(ctx, message.ID, w.now()); err != nil {
			w.logger.Warn("record delivered mail failed", slog.String("message_id", message.ID.String()), slog.Any("error", err))
		}
	}
}
