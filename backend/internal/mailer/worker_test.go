package mailer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// workerStore is a one-message outbox used to observe delivery decisions.
type workerStore struct {
	enabled    bool
	message    domain.MailMessage
	expired    int
	claimed    int
	sent       int
	retried    int
	retryAt    time.Time
	retryFinal bool
}

// ExpireMail - records one expiry sweep.
func (s *workerStore) ExpireMail(context.Context, time.Time) error {
	s.expired++
	return nil
}

// MailSettings - returns the test's administrator policy.
func (s *workerStore) MailSettings(context.Context) (domain.MailSettings, error) {
	return domain.MailSettings{Enabled: s.enabled}, nil
}

// ClaimMail - yields the message once and then reports an empty queue.
func (s *workerStore) ClaimMail(context.Context, time.Time) (domain.MailMessage, error) {
	if s.claimed > 0 || s.message.ID == uuid.Nil {
		return domain.MailMessage{}, domain.ErrNotFound
	}
	s.claimed++
	s.message.Attempts++
	return s.message, nil
}

// MarkMailSent - records successful delivery.
func (s *workerStore) MarkMailSent(context.Context, uuid.UUID, time.Time) error {
	s.sent++
	return nil
}

// RetryMail - records the worker's retry decision.
func (s *workerStore) RetryMail(_ context.Context, _ uuid.UUID, next time.Time, _ string, final bool) error {
	s.retried++
	s.retryAt = next
	s.retryFinal = final
	return nil
}

// workerSender records sends and may return a configured transport failure.
type workerSender struct {
	sent int
	err  error
}

// Send - records one attempted transport delivery.
func (s *workerSender) Send(context.Context, domain.MailMessage) error {
	s.sent++
	return s.err
}

// TestWorkerHonoursTheAdministratorSwitch checks a paused queue is not claimed
// and resumes normally after it is enabled.
func TestWorkerHonoursTheAdministratorSwitch(t *testing.T) {
	store := &workerStore{message: domain.MailMessage{ID: uuid.New()}}
	sender := &workerSender{}
	worker := NewWorker(store, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.deliver(context.Background())
	if store.expired != 1 || store.claimed != 0 || sender.sent != 0 {
		t.Fatalf("disabled delivery did not only expire mail: expired=%d claimed=%d sent=%d", store.expired, store.claimed, sender.sent)
	}
	store.enabled = true
	worker.deliver(context.Background())
	if store.claimed != 1 || sender.sent != 1 || store.sent != 1 {
		t.Fatalf("enabled delivery did not finish: store=%+v sender=%+v", store, sender)
	}
}

// TestWorkerRetriesTransportFailures checks failures stay in the queue with a
// future attempt time and become final on the sixth attempt.
func TestWorkerRetriesTransportFailures(t *testing.T) {
	now := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	store := &workerStore{enabled: true, message: domain.MailMessage{ID: uuid.New(), Attempts: 5}}
	sender := &workerSender{err: errors.New("SMTP unavailable")}
	worker := NewWorker(store, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.now = func() time.Time { return now }
	worker.deliver(context.Background())
	if store.retried != 1 || !store.retryFinal || !store.retryAt.After(now) {
		t.Fatalf("unexpected retry decision: %+v", store)
	}
}
