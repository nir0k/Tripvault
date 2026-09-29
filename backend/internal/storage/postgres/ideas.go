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

// IdeaRepository reads and writes a person's ideas. Every query names the
// owner, so an idea of somebody else reads exactly like one that does not
// exist.
type IdeaRepository struct {
	pool *pgxpool.Pool
}

// NewIdeaRepository - creates the idea repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewIdeaRepository(pool *pgxpool.Pool) *IdeaRepository {
	return &IdeaRepository{pool: pool}
}

// ideaColumns is the select list of an idea, kept in step with scanIdea.
// Amounts are read in hundredths so they never pass through a float; the
// places, the ways of getting there, the photos and the tags come aggregated
// in their order.
const ideaColumns = `i.id, i.owner_id, i.title, i.countries, i.months::int[], i.days_min, i.days_max,
	i.days_ideal, i.description_md, i.currency, (i.cost_stay * 100)::bigint, (i.cost_food * 100)::bigint,
	(i.cost_other * 100)::bigint, i.visa, i.created_at, i.updated_at,
	(SELECT jsonb_agg(jsonb_build_object('name', p.name, 'lat', p.lat, 'lng', p.lng) ORDER BY p.position)
	   FROM idea_places p WHERE p.idea_id = i.id) AS places,
	(SELECT jsonb_agg(jsonb_build_object('modes', tr.modes, 'cost', (tr.cost * 100)::bigint, 'minutes', tr.minutes)
	                  ORDER BY tr.position)
	   FROM idea_transports tr WHERE tr.idea_id = i.id) AS transports,
	(SELECT jsonb_agg(jsonb_build_object('id', tg.id, 'name', tg.name, 'color', tg.color) ORDER BY lower(tg.name), tg.id)
	   FROM idea_tags it JOIN tags tg ON tg.id = it.tag_id WHERE it.idea_id = i.id) AS tags,
	(SELECT jsonb_agg(jsonb_build_object('id', ph.id, 'key', ph.storage_key, 'thumb_key', ph.thumb_key,
	                  'width', ph.width, 'height', ph.height, 'created_at', ph.created_at) ORDER BY ph.position)
	   FROM idea_photos ph WHERE ph.idea_id = i.id) AS photos`

// ideaPhotoRow is a photo as the idea query aggregates it into JSON.
type ideaPhotoRow struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	ThumbKey  string    `json:"thumb_key"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	CreatedAt time.Time `json:"created_at"`
}

// ideaPlaceRow and ideaTransportRow are a place and a way of getting there as
// the idea query aggregates them into JSON.
type ideaPlaceRow struct {
	Name string   `json:"name"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
}
type ideaTransportRow struct {
	Modes   []string `json:"modes"`
	Cost    *int64   `json:"cost"`
	Minutes *int     `json:"minutes"`
}

// hundredths turns an amount read in hundredths back into money.
func hundredths(value *int64) *domain.Money {
	if value == nil {
		return nil
	}
	amount := domain.Money(*value)
	return &amount
}

// scanIdea reads one row in the order of ideaColumns.
func scanIdea(row pgx.Row) (domain.Idea, error) {
	var (
		idea       domain.Idea
		costs      [3]*int64
		places     []ideaPlaceRow
		transports []ideaTransportRow
		tags       []tripTagRow
		photos     []ideaPhotoRow
	)
	err := row.Scan(&idea.ID, &idea.OwnerID, &idea.Title, &idea.Countries, &idea.Months,
		&idea.DaysMin, &idea.DaysMax, &idea.DaysIdeal, &idea.DescriptionMD, &idea.Currency,
		&costs[0], &costs[1], &costs[2], &idea.Visa, &idea.CreatedAt, &idea.UpdatedAt, &places, &transports, &tags,
		&photos)
	if err != nil {
		return idea, err
	}
	for _, photo := range photos {
		idea.Photos = append(idea.Photos, domain.IdeaPhoto{
			ID: photo.ID, IdeaID: idea.ID, Key: photo.Key, ThumbKey: photo.ThumbKey, Width: photo.Width,
			Height: photo.Height, CreatedAt: photo.CreatedAt,
		})
	}
	idea.Costs = domain.IdeaCosts{Stay: hundredths(costs[0]), Food: hundredths(costs[1]), Other: hundredths(costs[2])}
	for _, place := range places {
		idea.Places = append(idea.Places, domain.IdeaPlace{Name: place.Name, Lat: place.Lat, Lng: place.Lng})
	}
	for _, transport := range transports {
		modes := make([]domain.TravelMode, 0, len(transport.Modes))
		for _, mode := range transport.Modes {
			modes = append(modes, domain.TravelMode(mode))
		}
		idea.Transports = append(idea.Transports, domain.IdeaTransport{
			Modes: modes, Cost: hundredths(transport.Cost), Minutes: transport.Minutes,
		})
	}
	for _, tag := range tags {
		idea.Tags = append(idea.Tags, domain.TripTag{ID: tag.ID, Name: tag.Name, Color: domain.TagColor(tag.Color)})
	}
	return idea, nil
}

// ideaParams gives the fields an idea keeps in its own row, in the order of
// the insert and the update: from the title to the visa.
func ideaParams(idea domain.Idea) []any {
	countries := idea.Countries
	if countries == nil {
		countries = []string{}
	}
	months := idea.Months
	if months == nil {
		months = []int{}
	}
	return []any{idea.Title, countries, months, idea.DaysMin, idea.DaysMax, idea.DaysIdeal,
		idea.DescriptionMD, idea.Currency, moneyParam(idea.Costs.Stay), moneyParam(idea.Costs.Food),
		moneyParam(idea.Costs.Other), string(idea.Visa)}
}

// saveIdeaParts replaces an idea's places and ways of getting there with the
// ones it holds, in their order.
func saveIdeaParts(ctx context.Context, tx pgx.Tx, idea domain.Idea) error {
	if _, err := tx.Exec(ctx, `DELETE FROM idea_places WHERE idea_id = $1`, idea.ID); err != nil {
		return fmt.Errorf("clear idea places: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM idea_transports WHERE idea_id = $1`, idea.ID); err != nil {
		return fmt.Errorf("clear idea transports: %w", err)
	}
	for position, place := range idea.Places {
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_places (idea_id, position, name, lat, lng) VALUES ($1, $2, $3, $4, $5)`,
			idea.ID, position, place.Name, place.Lat, place.Lng); err != nil {
			return fmt.Errorf("save idea place: %w", err)
		}
	}
	for position, transport := range idea.Transports {
		modes := make([]string, 0, len(transport.Modes))
		for _, mode := range transport.Modes {
			modes = append(modes, string(mode))
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_transports (idea_id, position, modes, cost, minutes) VALUES ($1, $2, $3, $4::numeric, $5)`,
			idea.ID, position, modes, moneyParam(transport.Cost), transport.Minutes); err != nil {
			return fmt.Errorf("save idea transport: %w", err)
		}
	}
	return nil
}

// List - returns a person's ideas, the last changed first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - ownerID: the person.
//
// Returns:
//   - every idea of the person, with their tags.
//   - an error if the query fails.
func (r *IdeaRepository) List(ctx context.Context, ownerID uuid.UUID) ([]domain.Idea, error) {
	ideas, err := collect(ctx, r.pool, scanIdea,
		`SELECT `+ideaColumns+` FROM ideas i WHERE i.owner_id = $1 ORDER BY i.updated_at DESC, i.id`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list ideas: %w", err)
	}
	return ideas, nil
}

// Get - reads one of a person's ideas.
//
// Arguments:
//   - ctx: context bounding the query.
//   - ownerID: the person.
//   - id: the idea.
//
// Returns:
//   - the idea with its tags.
//   - domain.ErrNotFound when the person has no such idea.
func (r *IdeaRepository) Get(ctx context.Context, ownerID, id uuid.UUID) (domain.Idea, error) {
	return oneRow(scanIdea, r.pool.QueryRow(ctx,
		`SELECT `+ideaColumns+` FROM ideas i WHERE i.id = $1 AND i.owner_id = $2`, id, ownerID), "get idea")
}

// Create - stores a new idea with its places and ways of getting there.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - idea: the validated idea with its identifier and owner set.
//
// Returns:
//   - an error if a statement fails.
func (r *IdeaRepository) Create(ctx context.Context, idea domain.Idea) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		args := append([]any{idea.ID, idea.OwnerID}, ideaParams(idea)...)
		if _, err := tx.Exec(ctx,
			`INSERT INTO ideas (id, owner_id, title, countries, months, days_min, days_max, days_ideal,
			                    description_md, currency, cost_stay, cost_food, cost_other, visa)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13::numeric, $14)`,
			args...); err != nil {
			return fmt.Errorf("create idea: %w", err)
		}
		return saveIdeaParts(ctx, tx, idea)
	})
}

// Update - stores every field of an idea, its places and ways of getting
// there included.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - idea: the validated idea.
//
// Returns:
//   - domain.ErrNotFound when the owner has no such idea.
func (r *IdeaRepository) Update(ctx context.Context, idea domain.Idea) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		args := append([]any{idea.ID, idea.OwnerID}, ideaParams(idea)...)
		tag, err := tx.Exec(ctx,
			`UPDATE ideas
			 SET title = $3, countries = $4, months = $5, days_min = $6, days_max = $7,
			     days_ideal = $8, description_md = $9, currency = $10, cost_stay = $11::numeric,
			     cost_food = $12::numeric, cost_other = $13::numeric, visa = $14, updated_at = now()
			 WHERE id = $1 AND owner_id = $2`, args...)
		if err != nil {
			return fmt.Errorf("update idea: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return saveIdeaParts(ctx, tx, idea)
	})
}

// Delete - removes one of a person's ideas with its tags and photos.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the person.
//   - id: the idea.
//
// Returns:
//   - the media store objects of the idea's photos, to be deleted from disk.
//   - domain.ErrNotFound when the person has no such idea.
func (r *IdeaRepository) Delete(ctx context.Context, ownerID, id uuid.UUID) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT unnest(ARRAY[p.storage_key, p.thumb_key]) FROM idea_photos p
			 JOIN ideas i ON i.id = p.idea_id WHERE i.id = $1 AND i.owner_id = $2`, id, ownerID)
		if err != nil {
			return fmt.Errorf("list idea photos: %w", err)
		}
		if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("read idea photo keys: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ideas WHERE id = $1 AND owner_id = $2`, id, ownerID)
		if err != nil {
			return fmt.Errorf("delete idea: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	return keys, err
}

// AddPhoto - puts a photo at the end of one of a person's ideas.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the person.
//   - photo: the photo with its identifier, idea and keys set.
//
// Returns:
//   - domain.ErrNotFound when the person has no such idea.
//   - a *domain.ValidationError on photos when the idea holds MaxIdeaPhotos already.
func (r *IdeaRepository) AddPhoto(ctx context.Context, ownerID uuid.UUID, photo domain.IdeaPhoto) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// The idea's row is locked, so two uploads at once count one after the other.
		tag, err := tx.Exec(ctx, `SELECT 1 FROM ideas WHERE id = $1 AND owner_id = $2 FOR UPDATE`, photo.IdeaID, ownerID)
		if err != nil {
			return fmt.Errorf("lock idea: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM idea_photos WHERE idea_id = $1`, photo.IdeaID).Scan(&count); err != nil {
			return fmt.Errorf("count idea photos: %w", err)
		}
		if count >= domain.MaxIdeaPhotos {
			return domain.NewValidationError("photos", "too_many", "an idea keeps at most 10 photos")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_photos (id, idea_id, position, storage_key, thumb_key, width, height)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			photo.ID, photo.IdeaID, count, photo.Key, photo.ThumbKey, photo.Width, photo.Height); err != nil {
			return fmt.Errorf("add idea photo: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE ideas SET updated_at = now() WHERE id = $1`, photo.IdeaID)
		return err
	})
}

// DeletePhoto - removes a photo of one of a person's ideas and closes the gap
// it leaves.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the person.
//   - ideaID: the idea.
//   - photoID: the photo.
//
// Returns:
//   - the photo as it was, whose files are to be deleted from disk.
//   - domain.ErrNotFound when the person has no such photo.
func (r *IdeaRepository) DeletePhoto(ctx context.Context, ownerID, ideaID, photoID uuid.UUID) (domain.IdeaPhoto, error) {
	var photo domain.IdeaPhoto
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`DELETE FROM idea_photos p USING ideas i
			 WHERE p.id = $1 AND p.idea_id = $2 AND i.id = p.idea_id AND i.owner_id = $3
			 RETURNING p.id, p.idea_id, p.storage_key, p.thumb_key, p.width, p.height, p.created_at`,
			photoID, ideaID, ownerID).Scan(&photo.ID, &photo.IdeaID, &photo.Key, &photo.ThumbKey, &photo.Width,
			&photo.Height, &photo.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("delete idea photo: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE idea_photos p SET position = r.rn - 1
			 FROM (SELECT id, row_number() OVER (ORDER BY position) AS rn FROM idea_photos WHERE idea_id = $1) r
			 WHERE p.id = r.id AND p.position <> r.rn - 1`, ideaID); err != nil {
			return fmt.Errorf("number idea photos: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE ideas SET updated_at = now() WHERE id = $1`, ideaID)
		return err
	})
	return photo, err
}

// SetTags - replaces the tags on one of a person's ideas.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the person.
//   - id: the idea.
//   - tagIDs: the tags the idea wears afterwards, without repeats.
//
// Returns:
//   - domain.ErrNotFound when the person has no such idea.
//   - a *domain.ValidationError on tag_ids when one of them is not the person's.
func (r *IdeaRepository) SetTags(ctx context.Context, ownerID, id uuid.UUID, tagIDs []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE ideas SET updated_at = now() WHERE id = $1 AND owner_id = $2`, id, ownerID)
		if err != nil {
			return fmt.Errorf("touch idea: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		var owned int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE user_id = $1 AND id = ANY($2)`,
			ownerID, tagIDs).Scan(&owned); err != nil {
			return fmt.Errorf("check tags: %w", err)
		}
		if owned != len(tagIDs) {
			return domain.NewValidationError("tag_ids", "unknown_tag", "must name the reader's own tags")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM idea_tags WHERE idea_id = $1`, id); err != nil {
			return fmt.Errorf("clear idea tags: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_tags (tag_id, idea_id) SELECT unnest($2::uuid[]), $1`, id, tagIDs); err != nil {
			return fmt.Errorf("tag idea: %w", err)
		}
		return nil
	})
}
