package domain

import (
	"time"

	"github.com/google/uuid"
)

// MailSettings is the administrator-controlled mail policy of the instance.
type MailSettings struct {
	Enabled bool
	// SelfRegistration is the administrator's switch for registration. It
	// opens registration only while mail is configured and enabled.
	SelfRegistration bool
}

// UnverifiedAccountLifetime is how long a self-registered account may wait
// for its address to be confirmed before it is deleted, counted from its first
// registration.
const UnverifiedAccountLifetime = 5 * 24 * time.Hour

// EmailVerification is the pending confirmation of a self-registered account.
// Only digests of its link and code are kept.
type EmailVerification struct {
	UserID    uuid.UUID
	TokenHash []byte
	CodeHash  []byte
	Attempts  int
	ExpiresAt time.Time
	SentAt    time.Time
}

// MailStats describes the persistent delivery queue for the status screen.
type MailStats struct {
	Queued        int64
	Failed        int64
	LastSuccessAt *time.Time
	LastErrorAt   *time.Time
	LastError     string
}

// MailMessage is one fully rendered message waiting for delivery.
type MailMessage struct {
	ID        uuid.UUID
	Recipient string
	Subject   string
	TextBody  string
	HTMLBody  string
	Attempts  int
	// DiscardAfter keeps an action link from being delivered after its token
	// expires. Ordinary notifications leave it nil.
	DiscardAfter *time.Time
	CreatedAt    time.Time
}

// UserInvitation offers an account on a closed instance.
type UserInvitation struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	IsAdmin     bool
	ExpiresAt   time.Time
	AcceptedAt  *time.Time
	RevokedAt   *time.Time
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
}

// TripInvitation offers a role on one trip to one email address.
type TripInvitation struct {
	ID         uuid.UUID
	TripID     uuid.UUID
	TripTitle  string
	Email      string
	Role       TripRole
	ExpiresAt  time.Time
	AcceptedBy *uuid.UUID
	AcceptedAt *time.Time
	RevokedAt  *time.Time
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
}

// IdeaInvitation offers a role on one person's list of ideas to one email
// address. Only the owner invites, so the owner is the one who sent it.
type IdeaInvitation struct {
	ID         uuid.UUID
	OwnerID    uuid.UUID
	OwnerName  string
	Email      string
	Role       TripRole
	ExpiresAt  time.Time
	AcceptedBy *uuid.UUID
	AcceptedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// Redeemable - reports whether a user invitation can still be used.
//
// Arguments:
//   - now: the reference time.
//
// Returns:
//   - true when the invitation is open and has not expired.
func (i UserInvitation) Redeemable(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}

// Redeemable - reports whether a trip invitation can still be used.
//
// Arguments:
//   - now: the reference time.
//
// Returns:
//   - true when the invitation is open and has not expired.
func (i TripInvitation) Redeemable(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}

// Redeemable - reports whether an invitation to a list of ideas can still be used.
//
// Arguments:
//   - now: the reference time.
//
// Returns:
//   - true when the invitation is open and has not expired.
func (i IdeaInvitation) Redeemable(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}
