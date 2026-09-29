package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TagRepository reads and writes a person's tags and the trips they are put on.
type TagRepository struct {
	pool *pgxpool.Pool
}

// NewTagRepository - creates the tag repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewTagRepository(pool *pgxpool.Pool) *TagRepository {
	return &TagRepository{pool: pool}
}

// tagColumns is the select list of a tag with the number of trips and ideas
// wearing it, kept in step with scanTag. A trip the person can no longer open
// is not counted, since they would never see the tag on it.
const tagColumns = `tg.id, tg.user_id, tg.name, tg.color, tg.created_at,
	(SELECT count(*) FROM trip_tags tt JOIN trips t ON t.id = tt.trip_id
	 LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = tg.user_id
	 WHERE tt.tag_id = tg.id AND (t.owner_id = tg.user_id OR m.user_id IS NOT NULL)),
	(SELECT count(*) FROM idea_tags it WHERE it.tag_id = tg.id)`

// scanTag reads one row in the order of tagColumns.
func scanTag(row pgx.Row) (domain.Tag, error) {
	var tag domain.Tag
	err := row.Scan(&tag.ID, &tag.UserID, &tag.Name, &tag.Color, &tag.CreatedAt, &tag.TripCount, &tag.IdeaCount)
	return tag, err
}

// List - returns a person's tags in the order of their names.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the person.
//
// Returns:
//   - the tags, each with the number of trips wearing it.
//   - an error if the query fails.
func (r *TagRepository) List(ctx context.Context, userID uuid.UUID) ([]domain.Tag, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+tagColumns+` FROM tags tg WHERE tg.user_id = $1 ORDER BY lower(tg.name), tg.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	tags, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Tag, error) { return scanTag(row) })
	if err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	return tags, nil
}

// Create - adds a tag to a person's list.
//
// A tag without a colour of its own takes the one the person's tags use
// least, the earlier in the palette on a tie, so a list made without choosing
// comes out in different colours.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tag: the tag, with its identifier, owner, normalised name and a colour of
//     the palette or none.
//
// Returns:
//   - the stored tag, with the colour it got.
//   - domain.ErrAlreadyExists when the person has a tag of that name already,
//     whatever its case.
func (r *TagRepository) Create(ctx context.Context, tag domain.Tag) (domain.Tag, error) {
	palette := make([]string, 0, len(domain.TagColors))
	for _, color := range domain.TagColors {
		palette = append(palette, string(color))
	}
	stored, err := scanTag(r.pool.QueryRow(ctx,
		`WITH tg AS (
		   INSERT INTO tags (id, user_id, name, color)
		   VALUES ($1, $2, $3, coalesce(nullif($4, ''), (
		     SELECT p.color FROM unnest($5::text[]) WITH ORDINALITY AS p (color, n)
		     ORDER BY (SELECT count(*) FROM tags WHERE user_id = $2 AND color = p.color), p.n
		     LIMIT 1)))
		   RETURNING *)
		 SELECT tg.id, tg.user_id, tg.name, tg.color, tg.created_at, 0::bigint, 0::bigint FROM tg`,
		tag.ID, tag.UserID, tag.Name, string(tag.Color), palette))
	switch {
	case isUniqueViolation(err):
		return domain.Tag{}, domain.ErrAlreadyExists
	case err != nil:
		return domain.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return stored, nil
}

// Update - renames one of a person's tags or changes its colour.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the person, who must own the tag.
//   - tagID: the tag.
//   - name: the normalised new name; empty keeps the name.
//   - color: the new colour of the palette; empty keeps the colour.
//
// Returns:
//   - the changed tag.
//   - domain.ErrNotFound when the person has no such tag.
//   - domain.ErrAlreadyExists when another of their tags has that name.
func (r *TagRepository) Update(ctx context.Context, userID, tagID uuid.UUID, name string,
	color domain.TagColor) (domain.Tag, error) {
	_, err := r.pool.Exec(ctx,
		`UPDATE tags SET name = coalesce(nullif($3, ''), name), color = coalesce(nullif($4, ''), color)
		 WHERE id = $1 AND user_id = $2`, tagID, userID, name, string(color))
	if isUniqueViolation(err) {
		return domain.Tag{}, domain.ErrAlreadyExists
	}
	if err != nil {
		return domain.Tag{}, fmt.Errorf("update tag: %w", err)
	}
	tag, err := scanTag(r.pool.QueryRow(ctx,
		`SELECT `+tagColumns+` FROM tags tg WHERE tg.id = $1 AND tg.user_id = $2`, tagID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Tag{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Tag{}, fmt.Errorf("read tag: %w", err)
	}
	return tag, nil
}

// Delete - removes one of a person's tags from their list and from every trip.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the person, who must own the tag.
//   - tagID: the tag.
//
// Returns:
//   - domain.ErrNotFound when the person has no such tag.
func (r *TagRepository) Delete(ctx context.Context, userID, tagID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM tags WHERE id = $1 AND user_id = $2`, tagID, userID)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetTripTags - replaces the tags a person has put on a trip.
//
// Only the person's own tags are touched: what other members put on the same
// trip stays as it is. The caller has checked that the person can open the trip.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - userID: the person.
//   - tripID: the trip.
//   - tagIDs: the tags the trip wears afterwards, without repeats.
//
// Returns:
//   - a *domain.ValidationError on tag_ids when one of them is not the person's.
//   - an error if the transaction fails.
func (r *TagRepository) SetTripTags(ctx context.Context, userID, tripID uuid.UUID, tagIDs []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var owned int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE user_id = $1 AND id = ANY($2)`,
			userID, tagIDs).Scan(&owned); err != nil {
			return fmt.Errorf("check tags: %w", err)
		}
		if owned != len(tagIDs) {
			return domain.NewValidationError("tag_ids", "unknown_tag", "must name the reader's own tags")
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM trip_tags tt USING tags tg
			 WHERE tt.tag_id = tg.id AND tt.trip_id = $1 AND tg.user_id = $2`, tripID, userID); err != nil {
			return fmt.Errorf("clear trip tags: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO trip_tags (tag_id, trip_id) SELECT unnest($2::uuid[]), $1`, tripID, tagIDs); err != nil {
			return fmt.Errorf("tag trip: %w", err)
		}
		return nil
	})
}
