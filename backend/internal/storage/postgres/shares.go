package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Share links live with the trips because they are part of who may read one. The
// token itself never reaches this layer in plaintext: callers hand over its hash,
// which is all the table holds, so neither a query log nor a database dump can be
// replayed as a working link.

var shareLinkColumns = `s.id, s.trip_id, s.label, s.include_private_media, s.expires_at, s.revoked_at,
	s.created_by, s.last_used_at, s.use_count, s.created_at`

// scanShareLink reads one row in the order of shareLinkColumns.
func scanShareLink(row pgx.Row) (domain.ShareLink, error) {
	var l domain.ShareLink
	err := row.Scan(&l.ID, &l.TripID, &l.Label, &l.IncludePrivateMedia, &l.ExpiresAt, &l.RevokedAt,
		&l.CreatedBy, &l.LastUsedAt, &l.UseCount, &l.CreatedAt)
	return l, err
}

// CreateShareLink - stores a new share link for a trip.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - link: the validated link with its ID, trip and author set.
//   - tokenHash: the hash of the token handed to the owner; the plaintext is
//     never stored.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist or is deleted.
func (r *TripRepository) CreateShareLink(ctx context.Context, link domain.ShareLink, tokenHash []byte) error {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO share_links (id, trip_id, label, token_hash, include_private_media, expires_at,
		                          created_by, created_at)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8
		 WHERE EXISTS (SELECT 1 FROM trips WHERE id = $2)`,
		link.ID, link.TripID, link.Label, tokenHash, link.IncludePrivateMedia, link.ExpiresAt,
		link.CreatedBy, link.CreatedAt)
	if err != nil {
		return fmt.Errorf("create share link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ShareLinks - lists a trip's links that have not been revoked, newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - the links, without their tokens.
func (r *TripRepository) ShareLinks(ctx context.Context, tripID uuid.UUID) ([]domain.ShareLink, error) {
	return collect(ctx, r.pool, scanShareLink,
		`SELECT `+shareLinkColumns+` FROM share_links s
		 WHERE s.trip_id = $1 AND s.revoked_at IS NULL
		 ORDER BY s.created_at DESC, s.id`, tripID)
}

// ShareLinkTrip - reports which trip a link belongs to.
//
// A link is addressed on its own, without its trip, so the role has to be
// checked against the trip the link names.
//
// Arguments:
//   - ctx: context bounding the query.
//   - linkID: the link.
//
// Returns:
//   - the trip the link belongs to.
//   - domain.ErrNotFound when the link does not exist or is already revoked.
func (r *TripRepository) ShareLinkTrip(ctx context.Context, linkID uuid.UUID) (uuid.UUID, error) {
	var tripID uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT trip_id FROM share_links WHERE id = $1 AND revoked_at IS NULL`, linkID).Scan(&tripID)
	return one(tripID, err, "find share link")
}

// RevokeShareLink - stops a link working, at once and for everybody.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - tripID: the trip the link belongs to, so a link of another trip cannot be
//     revoked through it.
//   - linkID: the link.
//   - at: the revocation time.
//
// Returns:
//   - domain.ErrNotFound when the link does not exist or is already revoked.
func (r *TripRepository) RevokeShareLink(ctx context.Context, tripID, linkID uuid.UUID, at time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE share_links SET revoked_at = $3
		 WHERE id = $2 AND trip_id = $1 AND revoked_at IS NULL`, tripID, linkID, at)
	if err != nil {
		return fmt.Errorf("revoke share link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ShareAccess - finds what a share token opens.
//
// It only reads: every picture and file behind a link asks for it again, and a
// write on each of those would queue a whole gallery on one row. The visit is
// counted by RecordShareVisit, once, when the link is opened. A revoked or
// expired link, and one whose trip is deleted, is reported as missing.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - tokenHash: the hash of the token presented in X-Share-Token.
//   - at: the moment the link must still be live at.
//
// Returns:
//   - the link and the trip it opens, with no role on it.
//   - domain.ErrNotFound when no live link has this token.
func (r *TripRepository) ShareAccess(ctx context.Context, tokenHash []byte, at time.Time) (domain.ShareAccess, error) {
	link, err := oneRow(scanShareLink, r.pool.QueryRow(ctx,
		`SELECT `+shareLinkColumns+` FROM share_links s
		 WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > $2)`,
		tokenHash, at), "find share link")
	if err != nil {
		return domain.ShareAccess{}, err
	}

	// The trip is read without a reader, so the summary carries no role: a link
	// grants reading, never a role on the trip.
	trip, err := scanTripSummary(r.pool.QueryRow(ctx,
		`SELECT `+tripSummaryColumns("")+`, NULL
		 FROM trips t
		 JOIN users o ON o.id = t.owner_id
		 WHERE t.id = $1`, link.TripID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ShareAccess{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ShareAccess{}, fmt.Errorf("get shared trip: %w", err)
	}
	return domain.ShareAccess{Link: link, Trip: trip}, nil
}

// RecordShareVisit - counts one opening of a share link.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - linkID: the link that was opened.
//   - at: the time of the visit, stored as the last use.
//
// Returns:
//   - an error if the counters could not be written.
func (r *TripRepository) RecordShareVisit(ctx context.Context, linkID uuid.UUID, at time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE share_links SET last_used_at = $2, use_count = use_count + 1 WHERE id = $1`,
		linkID, at); err != nil {
		return fmt.Errorf("record share visit: %w", err)
	}
	return nil
}
