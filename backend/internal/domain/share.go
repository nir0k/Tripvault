package domain

import (
	"time"

	"github.com/google/uuid"
)

// A share link lets somebody without an account read a trip - its plan or its
// report, whichever the trip holds. The token is the whole credential, so it
// never touches a log and never leaves the fragment of the URL; only its SHA-256
// is stored, which makes a lost link unrecoverable and a stolen database useless
// for reading trips.

// ShareTokenBytes is the entropy of a share token, matching a refresh token's.
const ShareTokenBytes = 32

// maxShareLabelLength bounds the owner's own note on a link.
const maxShareLabelLength = 200

// ShareLink is a read-only link to a trip, as stored. The token itself is not
// part of it: it exists only in the answer that created the link.
type ShareLink struct {
	ID     uuid.UUID
	TripID uuid.UUID
	// Label is the owner's own note, such as "For my parents".
	Label string
	// IncludePrivateMedia lets the link show media marked private; off by default.
	IncludePrivateMedia bool
	// ExpiresAt is when the link stops working; nil never expires.
	ExpiresAt *time.Time
	RevokedAt *time.Time
	CreatedBy uuid.UUID
	// LastUsedAt and UseCount record how the link is being used.
	LastUsedAt *time.Time
	UseCount   int64
	CreatedAt  time.Time
}

// Normalize - trims a link's label and checks its fields.
//
// Arguments:
//   - now: the current time, against which an expiry date is checked.
//
// Returns:
//   - the normalised link.
//   - the first *ValidationError found.
func (l ShareLink) Normalize(now time.Time) (ShareLink, error) {
	var err error
	if l.Label, err = trimmedText("label", l.Label, maxShareLabelLength); err != nil {
		return l, err
	}
	if l.ExpiresAt != nil && !l.ExpiresAt.After(now) {
		return l, NewValidationError("expires_at", "already_past", "must be in the future")
	}
	return l, nil
}

// Active - reports whether the link still opens the trip.
//
// Arguments:
//   - now: the current time.
//
// Returns:
//   - true when the link is neither revoked nor expired.
func (l ShareLink) Active(now time.Time) bool {
	return l.RevokedAt == nil && (l.ExpiresAt == nil || l.ExpiresAt.After(now))
}

// ShareAccess is what a share token opens: the link and the trip it belongs to.
type ShareAccess struct {
	Link ShareLink
	Trip TripSummary
}

// Opens - reports whether the link opens a kind of document.
//
// Arguments:
//   - kind: the plan or the report.
//
// Returns:
//   - true when the trip the link belongs to holds that document.
func (a ShareAccess) Opens(kind DocumentKind) bool {
	return a.Trip.Kind == kind && a.Trip.DocumentID() != nil
}
