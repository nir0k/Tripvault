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

// IdeaRepository reads and writes people's lists of ideas. Every query names
// the reader, and an idea of a list the reader is not a member of reads exactly
// like one that does not exist.
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

// ideaSelect is the query of the ideas a reader may open, kept in step with
// scanIdea; reader is the placeholder of the reader's identifier. The reader's
// role comes from the list's membership, and the tags are only the reader's
// own. Amounts are read in hundredths so they never pass through a float; the
// places, the ways of getting there, the photos and the tags come aggregated
// in their order.
func ideaSelect(reader string) string {
	return `SELECT i.id, i.owner_id, ou.display_name, ou.email,
	CASE WHEN i.owner_id = ` + reader + ` THEN 'owner' ELSE am.role END,
	cu.id, cu.display_name, cu.email, uu.id, uu.display_name, uu.email,
	i.title, i.countries, i.months::int[], i.days_min, i.days_max,
	i.days_ideal, i.description_md, i.currency, (i.cost_stay * 100)::bigint, (i.cost_food * 100)::bigint,
	(i.cost_other * 100)::bigint, i.visa, i.created_at, i.updated_at,
	(SELECT jsonb_agg(jsonb_build_object('name', p.name, 'lat', p.lat, 'lng', p.lng) ORDER BY p.position)
	   FROM idea_places p WHERE p.idea_id = i.id) AS places,
	(SELECT jsonb_agg(jsonb_build_object('modes', tr.modes, 'cost', (tr.cost * 100)::bigint, 'minutes', tr.minutes)
	                  ORDER BY tr.position)
	   FROM idea_transports tr WHERE tr.idea_id = i.id) AS transports,
	(SELECT jsonb_agg(jsonb_build_object('id', tg.id, 'name', tg.name, 'color', tg.color) ORDER BY lower(tg.name), tg.id)
	   FROM idea_tags it JOIN tags tg ON tg.id = it.tag_id
	   WHERE it.idea_id = i.id AND tg.user_id = ` + reader + `) AS tags,
	(SELECT jsonb_agg(jsonb_build_object('id', ph.id, 'key', ph.storage_key, 'thumb_key', ph.thumb_key,
	                  'width', ph.width, 'height', ph.height, 'created_at', ph.created_at) ORDER BY ph.position)
	   FROM idea_photos ph WHERE ph.idea_id = i.id) AS photos
	FROM ideas i
	JOIN users ou ON ou.id = i.owner_id
	LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users uu ON uu.id = i.updated_by
	LEFT JOIN idea_members am ON am.owner_id = i.owner_id AND am.user_id = ` + reader + `
	WHERE (i.owner_id = ` + reader + ` OR am.user_id IS NOT NULL)`
}

// ideaEditable is the condition an idea i meets when the person named by
// actor may change it: it is theirs, or they edit its list.
func ideaEditable(actor string) string {
	return `(i.owner_id = ` + actor + ` OR EXISTS (SELECT 1 FROM idea_members m
	   WHERE m.owner_id = i.owner_id AND m.user_id = ` + actor + ` AND m.role = 'editor'))`
}

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

// userRefRow is a person a row may name, every column null once their
// account is deleted.
type userRefRow struct {
	ID          *uuid.UUID
	DisplayName *string
	Email       *string
}

// user reads the person, or nil when the row names nobody.
func (r userRefRow) user() *domain.TripUser {
	if r.ID == nil {
		return nil
	}
	user := domain.TripUser{ID: *r.ID}
	if r.DisplayName != nil {
		user.DisplayName = *r.DisplayName
	}
	if r.Email != nil {
		user.Email = *r.Email
	}
	return &user
}

// scanIdea reads one row in the order of ideaSelect.
func scanIdea(row pgx.Row) (domain.Idea, error) {
	var (
		idea       domain.Idea
		authors    [2]userRefRow
		costs      [3]*int64
		places     []ideaPlaceRow
		transports []ideaTransportRow
		tags       []tripTagRow
		photos     []ideaPhotoRow
	)
	err := row.Scan(&idea.ID, &idea.OwnerID, &idea.Owner.DisplayName, &idea.Owner.Email, &idea.Role,
		&authors[0].ID, &authors[0].DisplayName, &authors[0].Email,
		&authors[1].ID, &authors[1].DisplayName, &authors[1].Email, &idea.Title, &idea.Countries, &idea.Months,
		&idea.DaysMin, &idea.DaysMax, &idea.DaysIdeal, &idea.DescriptionMD, &idea.Currency,
		&costs[0], &costs[1], &costs[2], &idea.Visa, &idea.CreatedAt, &idea.UpdatedAt, &places, &transports, &tags,
		&photos)
	if err != nil {
		return idea, err
	}
	idea.Owner.ID = idea.OwnerID
	idea.CreatedBy, idea.UpdatedBy = authors[0].user(), authors[1].user()
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

// lockEditableIdea locks an idea the actor may change for the rest of the
// transaction.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - tx: the open transaction.
//   - actorID: the person changing the idea.
//   - ideaID: the idea.
//
// Returns:
//   - the owner of the idea's list and the idea's title.
//   - domain.ErrNotFound when there is no such idea the actor may change.
func lockEditableIdea(ctx context.Context, tx pgx.Tx, actorID, ideaID uuid.UUID) (uuid.UUID, string, error) {
	var ownerID uuid.UUID
	var title string
	err := tx.QueryRow(ctx,
		`SELECT i.owner_id, i.title FROM ideas i WHERE i.id = $1 AND `+ideaEditable("$2")+` FOR UPDATE OF i`,
		ideaID, actorID).Scan(&ownerID, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("lock idea: %w", err)
	}
	return ownerID, title, nil
}

// logIdeaChange writes one entry of a list's history.
func logIdeaChange(ctx context.Context, tx pgx.Tx, ownerID, ideaID uuid.UUID, title string, actorID uuid.UUID,
	action domain.IdeaAction) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO idea_changes (id, owner_id, idea_id, idea_title, user_id, action) VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.Must(uuid.NewV7()), ownerID, ideaID, title, actorID, string(action)); err != nil {
		return fmt.Errorf("log idea change: %w", err)
	}
	return nil
}

// touchIdea records who changed an idea last, and when.
func touchIdea(ctx context.Context, tx pgx.Tx, ideaID, actorID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE ideas SET updated_at = now(), updated_by = $2 WHERE id = $1`,
		ideaID, actorID); err != nil {
		return fmt.Errorf("touch idea: %w", err)
	}
	return nil
}

// List - returns every idea a person may open - their own and those of the
// lists shared with them - the last changed first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - readerID: the person.
//
// Returns:
//   - the ideas, each with the reader's role and the reader's tags.
//   - an error if the query fails.
func (r *IdeaRepository) List(ctx context.Context, readerID uuid.UUID) ([]domain.Idea, error) {
	ideas, err := collect(ctx, r.pool, scanIdea, ideaSelect("$1")+` ORDER BY i.updated_at DESC, i.id`, readerID)
	if err != nil {
		return nil, fmt.Errorf("list ideas: %w", err)
	}
	return ideas, nil
}

// Get - reads one idea a person may open.
//
// Arguments:
//   - ctx: context bounding the query.
//   - readerID: the person.
//   - id: the idea.
//
// Returns:
//   - the idea with the reader's role and tags.
//   - domain.ErrNotFound when the person may open no such idea.
func (r *IdeaRepository) Get(ctx context.Context, readerID, id uuid.UUID) (domain.Idea, error) {
	return oneRow(scanIdea, r.pool.QueryRow(ctx, ideaSelect("$1")+` AND i.id = $2`, readerID, id), "get idea")
}

// Lists - returns the lists of ideas a person may open: their own first, then
// the ones shared with them by their owners' names.
//
// Arguments:
//   - ctx: context bounding the query.
//   - readerID: the person.
//
// Returns:
//   - the lists with the reader's role on each.
//   - an error if the query fails.
func (r *IdeaRepository) Lists(ctx context.Context, readerID uuid.UUID) ([]domain.IdeaList, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.display_name, u.email, 'owner', 0 AS rank FROM users u WHERE u.id = $1
		 UNION ALL
		 SELECT u.id, u.display_name, u.email, m.role, 1
		 FROM idea_members m JOIN users u ON u.id = m.owner_id
		 WHERE m.user_id = $1
		 ORDER BY rank, 2, 1`, readerID)
	if err != nil {
		return nil, fmt.Errorf("list idea lists: %w", err)
	}
	lists, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.IdeaList, error) {
		var list domain.IdeaList
		var rank int
		err := row.Scan(&list.Owner.ID, &list.Owner.DisplayName, &list.Owner.Email, &list.Role, &rank)
		return list, err
	})
	if err != nil {
		return nil, fmt.Errorf("read idea lists: %w", err)
	}
	return lists, nil
}

// ListRole - tells what a person may do with somebody's list of ideas.
//
// Arguments:
//   - ctx: context bounding the query.
//   - readerID: the person.
//   - ownerID: the owner of the list.
//
// Returns:
//   - owner for the person's own list, or their role as a member.
//   - domain.ErrNotFound when the list is not shared with them.
func (r *IdeaRepository) ListRole(ctx context.Context, readerID, ownerID uuid.UUID) (domain.TripRole, error) {
	if readerID == ownerID {
		return domain.RoleOwner, nil
	}
	var role domain.TripRole
	err := r.pool.QueryRow(ctx, `SELECT role FROM idea_members WHERE owner_id = $1 AND user_id = $2`,
		ownerID, readerID).Scan(&role)
	return one(role, err, "read idea list role")
}

// Create - stores a new idea with its places and ways of getting there in
// its owner's list, and writes it down in the list's history.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - actorID: the person adding it, the owner or an editor of the list.
//   - idea: the validated idea with its identifier and owner set.
//
// Returns:
//   - domain.ErrNotFound when the actor may not add ideas to that list.
//   - an error if a statement fails.
func (r *IdeaRepository) Create(ctx context.Context, actorID uuid.UUID, idea domain.Idea) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if idea.OwnerID != actorID {
			var editor bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM idea_members WHERE owner_id = $1 AND user_id = $2 AND role = 'editor')`,
				idea.OwnerID, actorID).Scan(&editor); err != nil {
				return fmt.Errorf("check idea list role: %w", err)
			}
			if !editor {
				return domain.ErrNotFound
			}
		}
		args := append([]any{idea.ID, idea.OwnerID, actorID}, ideaParams(idea)...)
		if _, err := tx.Exec(ctx,
			`INSERT INTO ideas (id, owner_id, created_by, updated_by, title, countries, months, days_min, days_max,
			                    days_ideal, description_md, currency, cost_stay, cost_food, cost_other, visa)
			 VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::numeric, $13::numeric, $14::numeric, $15)`,
			args...); err != nil {
			return fmt.Errorf("create idea: %w", err)
		}
		if err := saveIdeaParts(ctx, tx, idea); err != nil {
			return err
		}
		return logIdeaChange(ctx, tx, idea.OwnerID, idea.ID, idea.Title, actorID, domain.IdeaCreated)
	})
}

// Update - stores every field of an idea, its places and ways of getting
// there included, and writes the change down in its list's history.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - actorID: the person changing it, the owner or an editor of the list.
//   - idea: the validated idea.
//
// Returns:
//   - domain.ErrNotFound when there is no such idea the actor may change.
func (r *IdeaRepository) Update(ctx context.Context, actorID uuid.UUID, idea domain.Idea) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, _, err := lockEditableIdea(ctx, tx, actorID, idea.ID)
		if err != nil {
			return err
		}
		args := append([]any{idea.ID, actorID}, ideaParams(idea)...)
		if _, err := tx.Exec(ctx,
			`UPDATE ideas
			 SET title = $3, countries = $4, months = $5, days_min = $6, days_max = $7,
			     days_ideal = $8, description_md = $9, currency = $10, cost_stay = $11::numeric,
			     cost_food = $12::numeric, cost_other = $13::numeric, visa = $14, updated_at = now(), updated_by = $2
			 WHERE id = $1`, args...); err != nil {
			return fmt.Errorf("update idea: %w", err)
		}
		if err := saveIdeaParts(ctx, tx, idea); err != nil {
			return err
		}
		return logIdeaChange(ctx, tx, ownerID, idea.ID, idea.Title, actorID, domain.IdeaUpdated)
	})
}

// Delete - removes an idea with its tags and photos, and writes its deletion
// down in its list's history, under the title it had.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - actorID: the person deleting it, the owner or an editor of the list.
//   - id: the idea.
//
// Returns:
//   - the media store objects of the idea's photos, to be deleted from disk.
//   - domain.ErrNotFound when there is no such idea the actor may change.
func (r *IdeaRepository) Delete(ctx context.Context, actorID, id uuid.UUID) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, title, err := lockEditableIdea(ctx, tx, actorID, id)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`SELECT unnest(ARRAY[storage_key, thumb_key]) FROM idea_photos WHERE idea_id = $1`, id)
		if err != nil {
			return fmt.Errorf("list idea photos: %w", err)
		}
		if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("read idea photo keys: %w", err)
		}
		// The entry names the idea until the idea goes, then keeps only its title.
		if err := logIdeaChange(ctx, tx, ownerID, id, title, actorID, domain.IdeaDeleted); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ideas WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete idea: %w", err)
		}
		return nil
	})
	return keys, err
}

// AddPhoto - puts a photo at the end of an idea's photos.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - actorID: the person adding it, the owner or an editor of the list.
//   - photo: the photo with its identifier, idea and keys set.
//
// Returns:
//   - domain.ErrNotFound when there is no such idea the actor may change.
//   - a *domain.ValidationError on photos when the idea holds MaxIdeaPhotos already.
func (r *IdeaRepository) AddPhoto(ctx context.Context, actorID uuid.UUID, photo domain.IdeaPhoto) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// The idea's row is locked, so two uploads at once count one after the other.
		ownerID, title, err := lockEditableIdea(ctx, tx, actorID, photo.IdeaID)
		if err != nil {
			return err
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
		if err := touchIdea(ctx, tx, photo.IdeaID, actorID); err != nil {
			return err
		}
		return logIdeaChange(ctx, tx, ownerID, photo.IdeaID, title, actorID, domain.IdeaPhotoAdded)
	})
}

// DeletePhoto - removes a photo of an idea and closes the gap it leaves.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - actorID: the person removing it, the owner or an editor of the list.
//   - ideaID: the idea.
//   - photoID: the photo.
//
// Returns:
//   - the photo as it was, whose files are to be deleted from disk.
//   - domain.ErrNotFound when there is no such photo the actor may remove.
func (r *IdeaRepository) DeletePhoto(ctx context.Context, actorID, ideaID, photoID uuid.UUID) (domain.IdeaPhoto, error) {
	var photo domain.IdeaPhoto
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, title, err := lockEditableIdea(ctx, tx, actorID, ideaID)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx,
			`DELETE FROM idea_photos WHERE id = $1 AND idea_id = $2
			 RETURNING id, idea_id, storage_key, thumb_key, width, height, created_at`,
			photoID, ideaID).Scan(&photo.ID, &photo.IdeaID, &photo.Key, &photo.ThumbKey, &photo.Width,
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
		if err := touchIdea(ctx, tx, ideaID, actorID); err != nil {
			return err
		}
		return logIdeaChange(ctx, tx, ownerID, ideaID, title, actorID, domain.IdeaPhotoRemoved)
	})
	return photo, err
}

// SetTags - replaces the reader's own tags on an idea they may open. The tags
// of the other members stay, and the idea does not count as changed: a tag is
// the reader's word, which nobody else sees.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - readerID: the person.
//   - id: the idea.
//   - tagIDs: the reader's tags the idea wears afterwards, without repeats.
//
// Returns:
//   - domain.ErrNotFound when the person may open no such idea.
//   - a *domain.ValidationError on tag_ids when one of them is not the person's.
func (r *IdeaRepository) SetTags(ctx context.Context, readerID, id uuid.UUID, tagIDs []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var readable bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM ideas i WHERE i.id = $1 AND (i.owner_id = $2 OR EXISTS (
			   SELECT 1 FROM idea_members m WHERE m.owner_id = i.owner_id AND m.user_id = $2)))`,
			id, readerID).Scan(&readable); err != nil {
			return fmt.Errorf("check idea: %w", err)
		}
		if !readable {
			return domain.ErrNotFound
		}
		var owned int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE user_id = $1 AND id = ANY($2)`,
			readerID, tagIDs).Scan(&owned); err != nil {
			return fmt.Errorf("check tags: %w", err)
		}
		if owned != len(tagIDs) {
			return domain.NewValidationError("tag_ids", "unknown_tag", "must name the reader's own tags")
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM idea_tags it USING tags tg
			 WHERE it.idea_id = $1 AND tg.id = it.tag_id AND tg.user_id = $2`, id, readerID); err != nil {
			return fmt.Errorf("clear idea tags: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO idea_tags (tag_id, idea_id) SELECT unnest($2::uuid[]), $1`, id, tagIDs); err != nil {
			return fmt.Errorf("tag idea: %w", err)
		}
		return nil
	})
}

// maxIdeaChanges bounds the history one request reads: the latest changes are
// the ones worth reading.
const maxIdeaChanges = 200

// History - returns the latest changes of a list of ideas, or of one idea of
// it, the newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - ownerID: the owner of the list.
//   - ideaID: the idea, or nil for the whole list.
//
// Returns:
//   - at most maxIdeaChanges entries.
//   - an error if the query fails.
func (r *IdeaRepository) History(ctx context.Context, ownerID uuid.UUID, ideaID *uuid.UUID) ([]domain.IdeaChange, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT c.id, c.owner_id, c.idea_id, c.idea_title, u.id, u.display_name, u.email, c.action, c.created_at
		 FROM idea_changes c LEFT JOIN users u ON u.id = c.user_id
		 WHERE c.owner_id = $1 AND ($2::uuid IS NULL OR c.idea_id = $2)
		 ORDER BY c.created_at DESC, c.id DESC LIMIT $3`, ownerID, ideaID, maxIdeaChanges)
	if err != nil {
		return nil, fmt.Errorf("list idea changes: %w", err)
	}
	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.IdeaChange, error) {
		var change domain.IdeaChange
		var user userRefRow
		err := row.Scan(&change.ID, &change.OwnerID, &change.IdeaID, &change.IdeaTitle,
			&user.ID, &user.DisplayName, &user.Email, &change.Action, &change.CreatedAt)
		change.User = user.user()
		return change, err
	})
	if err != nil {
		return nil, fmt.Errorf("read idea changes: %w", err)
	}
	return changes, nil
}

// Members - lists the people a person's list of ideas is shared with, by name.
//
// Arguments:
//   - ctx: context bounding the query.
//   - ownerID: the owner of the list.
//
// Returns:
//   - the members, the owner not among them.
//   - an error if the query fails.
func (r *IdeaRepository) Members(ctx context.Context, ownerID uuid.UUID) ([]domain.IdeaMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.owner_id, u.id, u.display_name, u.email, m.role, m.created_at
		 FROM idea_members m JOIN users u ON u.id = m.user_id
		 WHERE m.owner_id = $1 ORDER BY u.display_name, u.email`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list idea members: %w", err)
	}
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.IdeaMember, error) {
		return scanIdeaMember(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read idea members: %w", err)
	}
	return members, nil
}

// scanIdeaMember reads one member in the order the member queries select.
func scanIdeaMember(row pgx.Row) (domain.IdeaMember, error) {
	var member domain.IdeaMember
	err := row.Scan(&member.OwnerID, &member.User.ID, &member.User.DisplayName, &member.User.Email, &member.Role,
		&member.CreatedAt)
	return member, err
}

// ideaMember reads one person's access to a list of ideas inside a transaction.
func ideaMember(ctx context.Context, tx pgx.Tx, ownerID, userID uuid.UUID) (domain.IdeaMember, error) {
	return oneRow(scanIdeaMember, tx.QueryRow(ctx,
		`SELECT m.owner_id, u.id, u.display_name, u.email, m.role, m.created_at
		 FROM idea_members m JOIN users u ON u.id = m.user_id
		 WHERE m.owner_id = $1 AND m.user_id = $2`, ownerID, userID), "read idea member")
}

// AddMember - shares a person's list of ideas with an existing account.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the owner of the list.
//   - userID: the account to share it with.
//   - role: editor or viewer, already validated.
//
// Returns:
//   - the new member.
//   - domain.ErrAlreadyMember when the account is the owner or a member already.
//   - a *domain.ValidationError on user_id for an unknown or deactivated account.
func (r *IdeaRepository) AddMember(ctx context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error) {
	var added domain.IdeaMember
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if ownerID == userID {
			return domain.ErrAlreadyMember
		}
		if err := requireActiveUser(ctx, tx, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO idea_members (owner_id, user_id, role) VALUES ($1, $2, $3)`,
			ownerID, userID, role)
		if isUniqueViolation(err) {
			return domain.ErrAlreadyMember
		}
		if err != nil {
			return fmt.Errorf("add idea member: %w", err)
		}
		added, err = ideaMember(ctx, tx, ownerID, userID)
		return err
	})
	return added, err
}

// UpdateMember - switches a member of a list of ideas between editor and viewer.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the owner of the list.
//   - userID: the member.
//   - role: editor or viewer, already validated.
//
// Returns:
//   - the updated member.
//   - domain.ErrNotFound when the person is not a member.
func (r *IdeaRepository) UpdateMember(ctx context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error) {
	var updated domain.IdeaMember
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE idea_members SET role = $3 WHERE owner_id = $1 AND user_id = $2`,
			ownerID, userID, role)
		if err != nil {
			return fmt.Errorf("update idea member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		updated, err = ideaMember(ctx, tx, ownerID, userID)
		return err
	})
	return updated, err
}

// RemoveMember - takes a member's access to a list of ideas away: the owner
// removing them, or the member leaving. Their tags on its ideas go with it.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - ownerID: the owner of the list.
//   - userID: the member.
//
// Returns:
//   - domain.ErrNotFound when the person is not a member.
func (r *IdeaRepository) RemoveMember(ctx context.Context, ownerID, userID uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM idea_members WHERE owner_id = $1 AND user_id = $2`, ownerID, userID)
		if err != nil {
			return fmt.Errorf("remove idea member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM idea_tags it USING tags tg, ideas i
			 WHERE tg.id = it.tag_id AND tg.user_id = $2 AND i.id = it.idea_id AND i.owner_id = $1`,
			ownerID, userID); err != nil {
			return fmt.Errorf("clear former member's idea tags: %w", err)
		}
		return nil
	})
}
