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

// The catalogue of the files a trip carries. The bytes themselves live in the
// media store; a row here says which trip they belong to, what is known about
// them and where they are shown. Nothing is served without a row, so a file
// whose row is gone is unreachable even while its bytes are still on disk.
type MediaRepository struct {
	pool *pgxpool.Pool
}

// NewMediaRepository - creates the media repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewMediaRepository(pool *pgxpool.Pool) *MediaRepository {
	return &MediaRepository{pool: pool}
}

var mediaColumns = `m.id, m.trip_id, m.storage_key, m.original_name, m.mime, m.size, m.checksum,
	coalesce(m.width, 0), coalesce(m.height, 0), m.taken_at, m.lat, m.lng, m.is_private, m.status,
	m.uploaded_by, m.created_at`

// scanMedia reads one row in the order of mediaColumns.
func scanMedia(row pgx.Row) (domain.Media, error) {
	var m domain.Media
	err := row.Scan(&m.ID, &m.TripID, &m.StorageKey, &m.OriginalName, &m.MIME, &m.Size, &m.Checksum,
		&m.Width, &m.Height, &m.TakenAt, &m.Lat, &m.Lng, &m.IsPrivate, &m.Status, &m.UploadedBy, &m.CreatedAt)
	return m, err
}

// Create - stores the catalogue row of a file that is already in the store.
//
// The trip's allowance is checked again here, under a lock on the trip, so two
// uploads arriving together cannot both fit under what only one of them may
// use: the second waits for the first and counts its file.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - media: the validated file with its ID, trip and uploader set.
//   - quota: the most a trip's files may take up in bytes; zero means no limit.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist.
//   - domain.ErrMediaQuota when the file does not fit the trip's allowance.
func (r *MediaRepository) Create(ctx context.Context, media domain.Media, quota int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// NO KEY UPDATE serialises uploads to one trip without blocking the
		// key checks other tables make against the row.
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM trips WHERE id = $1 FOR NO KEY UPDATE`, media.TripID).
			Scan(&locked); err != nil {
			_, err = one(locked, err, "lock trip")
			return err
		}
		if quota > 0 {
			var used int64
			if err := tx.QueryRow(ctx,
				`SELECT coalesce(sum(size), 0) FROM media WHERE trip_id = $1`, media.TripID).Scan(&used); err != nil {
				return fmt.Errorf("sum media size: %w", err)
			}
			if used+media.Size > quota {
				return domain.ErrMediaQuota
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO media (id, trip_id, storage_key, original_name, mime, size, checksum, width, height,
			                    taken_at, lat, lng, is_private, status, uploaded_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, 0), nullif($9, 0), $10, $11, $12, $13, $14, $15)`,
			media.ID, media.TripID, media.StorageKey, media.OriginalName, media.MIME, media.Size, media.Checksum,
			media.Width, media.Height, media.TakenAt, media.Lat, media.Lng, media.IsPrivate, media.Status,
			media.UploadedBy); err != nil {
			return fmt.Errorf("create media: %w", err)
		}
		return nil
	})
}

// Get - reads one file's catalogue row.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the file.
//
// Returns:
//   - the file.
//   - domain.ErrNotFound when it does not exist.
func (r *MediaRepository) Get(ctx context.Context, id uuid.UUID) (domain.Media, error) {
	return oneRow(scanMedia, r.pool.QueryRow(ctx,
		`SELECT `+mediaColumns+`
		 FROM media m WHERE m.id = $1`, id), "get media")
}

// ListByTrip - lists a trip's files, newest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - the files, which is an empty slice for a trip without any.
func (r *MediaRepository) ListByTrip(ctx context.Context, tripID uuid.UUID) ([]domain.Media, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+mediaColumns+` FROM media m WHERE m.trip_id = $1 ORDER BY m.created_at DESC, m.id`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Media, 0)
	for rows.Next() {
		media, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		items = append(items, media)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	return items, nil
}

// ListAll - lists every stored file of every trip, newest first.
//
// It is what the service walks at start-up to render the previews nobody has
// asked for yet, and newest first puts the trips being worked on at the front.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the files, which is an empty slice for an instance without any.
func (r *MediaRepository) ListAll(ctx context.Context) ([]domain.Media, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+mediaColumns+` FROM media m ORDER BY m.created_at DESC, m.id`)
	if err != nil {
		return nil, fmt.Errorf("list all media: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Media, 0)
	for rows.Next() {
		media, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		items = append(items, media)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all media: %w", err)
	}
	return items, nil
}

// Update - changes what can be changed about a stored file.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the file.
//   - changes: the fields to change; an unset field is left alone.
//
// Returns:
//   - the file as it now stands.
//   - domain.ErrNotFound when it does not exist.
func (r *MediaRepository) Update(ctx context.Context, id uuid.UUID, changes domain.MediaChanges) (domain.Media, error) {
	if changes.IsPrivate != nil {
		tag, err := r.pool.Exec(ctx, `UPDATE media SET is_private = $2 WHERE id = $1`, id, *changes.IsPrivate)
		if err != nil {
			return domain.Media{}, fmt.Errorf("update media: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.Media{}, domain.ErrNotFound
		}
	}
	return r.Get(ctx, id)
}

// Delete - removes a file from the catalogue, with every link to it.
//
// The bytes are the caller's to remove: the row goes first, so a file that can
// no longer be reached never waits on the filesystem.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the file.
//
// Returns:
//   - the storage key of the removed file, for the caller to delete.
//   - domain.ErrNotFound when it does not exist.
func (r *MediaRepository) Delete(ctx context.Context, id uuid.UUID) (string, error) {
	var key string
	err := r.pool.QueryRow(ctx, `DELETE FROM media WHERE id = $1 RETURNING storage_key`, id).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("delete media: %w", err)
	}
	return key, nil
}

// UsedBytes - reports how much of a trip's allowance its files take up.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - the total size of its files in bytes.
func (r *MediaRepository) UsedBytes(ctx context.Context, tripID uuid.UUID) (int64, error) {
	var used int64
	if err := r.pool.QueryRow(ctx,
		`SELECT coalesce(sum(size), 0) FROM media WHERE trip_id = $1`, tripID).Scan(&used); err != nil {
		return 0, fmt.Errorf("sum media size: %w", err)
	}
	return used, nil
}

// SetLinks - makes a target show exactly these files, in this order.
//
// The call replaces whatever the target showed before, so the client sends the
// gallery it wants rather than a difference, and repeating a call changes
// nothing. Files of another trip are refused: a link is how a file is reached,
// and one that crossed trips would hand a reader something they cannot open.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - target: trip, day or item.
//   - targetID: which one.
//   - mediaIDs: the files in the order they should appear.
//
// Returns:
//   - domain.ErrNotFound when the target does not exist.
//   - a *domain.ValidationError when a file belongs to another trip.
func (r *MediaRepository) SetLinks(ctx context.Context, target domain.MediaTarget, targetID uuid.UUID,
	mediaIDs []uuid.UUID) error {
	column, err := linkColumn(target)
	if err != nil {
		return err
	}
	return r.inTx(ctx, func(tx pgx.Tx) error {
		tripID, err := linkTargetTrip(ctx, tx, target, targetID)
		if err != nil {
			return err
		}
		if len(mediaIDs) > 0 {
			var known int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM media WHERE id = ANY($1) AND trip_id = $2`, mediaIDs, tripID).Scan(&known); err != nil {
				return fmt.Errorf("check media: %w", err)
			}
			if known != len(distinct(mediaIDs)) {
				return domain.NewValidationError("media_ids", "unknown_media",
					"every file must belong to the same trip as the target")
			}
		}
		// The gallery is replaced whole, so which of its pictures were the
		// report's favourites is read first and written back with them; a file
		// taken out of the gallery loses that along with its place in it.
		favorites, err := favoritesOf(ctx, tx, column, targetID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM media_links WHERE `+column+` = $1`, targetID); err != nil {
			return fmt.Errorf("clear media links: %w", err)
		}
		for position, mediaID := range mediaIDs {
			_, favorite := favorites[mediaID]
			if _, err := tx.Exec(ctx,
				`INSERT INTO media_links (media_id, `+column+`, position, is_favorite) VALUES ($1, $2, $3, $4)
				 ON CONFLICT DO NOTHING`, mediaID, targetID, position, favorite); err != nil {
				return fmt.Errorf("link media: %w", err)
			}
		}
		return nil
	})
}

// favoritesOf reads which files a target shows as its favourites.
func favoritesOf(ctx context.Context, q querier, column string, targetID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	rows, err := q.Query(ctx,
		`SELECT media_id FROM media_links WHERE `+column+` = $1 AND is_favorite`, targetID)
	if err != nil {
		return nil, fmt.Errorf("read favourite media: %w", err)
	}
	defer rows.Close()

	favorites := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var mediaID uuid.UUID
		if err := rows.Scan(&mediaID); err != nil {
			return nil, fmt.Errorf("scan favourite media: %w", err)
		}
		favorites[mediaID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read favourite media: %w", err)
	}
	return favorites, nil
}

// SetFavorites - makes a day or a place of a report show exactly these files as
// its favourites, out of the gallery it already holds.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - target: day or item; a trip's own gallery has no favourites.
//   - targetID: which one.
//   - mediaIDs: the files to mark, at most domain.MediaFavoriteLimit of them.
//
// Returns:
//   - domain.ErrNotFound when the target does not exist.
//   - domain.ErrMediaFavoriteTarget when it does not belong to a report.
//   - domain.ErrMediaFavoriteLimit when more files are named than a report shows.
//   - domain.ErrMediaFavoriteUnlinked when a named file does not hang there.
func (r *MediaRepository) SetFavorites(ctx context.Context, target domain.MediaTarget, targetID uuid.UUID,
	mediaIDs []uuid.UUID) error {
	column, err := favoriteColumn(target)
	if err != nil {
		return err
	}
	unique := distinct(mediaIDs)
	if len(unique) > domain.MediaFavoriteLimit {
		return domain.ErrMediaFavoriteLimit
	}
	return r.inTx(ctx, func(tx pgx.Tx) error {
		if err := checkReportTarget(ctx, tx, target, targetID); err != nil {
			return err
		}
		var linked int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM media_links WHERE `+column+` = $1 AND media_id = ANY($2)`,
			targetID, unique).Scan(&linked); err != nil {
			return fmt.Errorf("check media links: %w", err)
		}
		if linked != len(unique) {
			return domain.ErrMediaFavoriteUnlinked
		}
		if _, err := tx.Exec(ctx,
			`UPDATE media_links SET is_favorite = (media_id = ANY($2)) WHERE `+column+` = $1`,
			targetID, unique); err != nil {
			return fmt.Errorf("set favourite media: %w", err)
		}
		return nil
	})
}

// SetFavorite - marks these files as favourites, or stops marking them,
// wherever they hang in a report: the trip's gallery names the files rather
// than the places they are shown in.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - mediaIDs: the files.
//   - favorite: true to mark them, false to take the mark away.
//
// Returns:
//   - domain.ErrMediaFavoriteLimit when a day or a place would end up showing
//     more favourites than a report has room for; nothing is changed then.
func (r *MediaRepository) SetFavorite(ctx context.Context, mediaIDs []uuid.UUID, favorite bool) error {
	unique := distinct(mediaIDs)
	if len(unique) == 0 {
		return nil
	}
	return r.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE media_links l SET is_favorite = $2
			 WHERE l.media_id = ANY($1)
			   AND (
			     EXISTS (SELECT 1 FROM days d JOIN documents doc ON doc.id = d.document_id
			             WHERE d.id = l.day_id AND doc.kind = 'report')
			     OR EXISTS (SELECT 1 FROM items i JOIN documents doc ON doc.id = i.document_id
			                WHERE i.id = l.item_id AND doc.kind = 'report')
			   )`, unique, favorite); err != nil {
			return fmt.Errorf("set favourite media: %w", err)
		}
		if !favorite {
			// Taking a mark away cannot leave a day or a place over the limit.
			return nil
		}
		return checkFavoriteLimits(ctx, tx, unique)
	})
}

// checkFavoriteLimits refuses the transaction when one of the days or places
// these files hang on is left showing more favourites than a report has room
// for. Only those targets are counted: the rest of the trip was not touched.
func checkFavoriteLimits(ctx context.Context, q querier, mediaIDs []uuid.UUID) error {
	var over int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM (
		   SELECT day_id AS target FROM media_links
		   WHERE is_favorite AND day_id IS NOT NULL AND day_id IN (
		     SELECT day_id FROM media_links WHERE media_id = ANY($2) AND day_id IS NOT NULL)
		   UNION ALL
		   SELECT item_id AS target FROM media_links
		   WHERE is_favorite AND item_id IS NOT NULL AND item_id IN (
		     SELECT item_id FROM media_links WHERE media_id = ANY($2) AND item_id IS NOT NULL)
		 ) t GROUP BY target HAVING count(*) > $1 LIMIT 1`,
		domain.MediaFavoriteLimit, mediaIDs).Scan(&over)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("count favourite media: %w", err)
	}
	return domain.ErrMediaFavoriteLimit
}

// checkReportTarget refuses a day or a place that belongs to a plan rather than
// to a report, and one that is not there at all.
func checkReportTarget(ctx context.Context, q querier, target domain.MediaTarget, targetID uuid.UUID) error {
	var query string
	switch target {
	case domain.MediaTargetDay:
		query = `SELECT doc.kind FROM days d JOIN documents doc ON doc.id = d.document_id WHERE d.id = $1`
	case domain.MediaTargetItem:
		query = `SELECT doc.kind FROM items i JOIN documents doc ON doc.id = i.document_id WHERE i.id = $1`
	default:
		return domain.ErrMediaFavoriteTarget
	}
	var kind string
	err := q.QueryRow(ctx, query, targetID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find media target: %w", err)
	}
	if kind != string(domain.DocumentReport) {
		return domain.ErrMediaFavoriteTarget
	}
	return nil
}

// favoriteColumn maps a favourite's target onto its column, refusing the
// targets that have no favourites at all.
func favoriteColumn(target domain.MediaTarget) (string, error) {
	switch target {
	case domain.MediaTargetDay:
		return "day_id", nil
	case domain.MediaTargetItem:
		return "item_id", nil
	default:
		return "", domain.ErrMediaFavoriteTarget
	}
}

// LinksOfTrip - reads every link of a trip, so one query serves a whole
// document: its days, its places and the trip itself.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - the links, ordered by target and position.
func (r *MediaRepository) LinksOfTrip(ctx context.Context, tripID uuid.UUID) ([]domain.MediaLink, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT l.media_id, coalesce(l.trip_id, l.day_id, l.item_id),
		        CASE WHEN l.trip_id IS NOT NULL THEN 'trip'
		             WHEN l.day_id IS NOT NULL THEN 'day' ELSE 'item' END,
		        l.position, l.is_favorite
		 FROM media_links l JOIN media m ON m.id = l.media_id
		 WHERE m.trip_id = $1
		 ORDER BY l.position, l.media_id`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list media links: %w", err)
	}
	defer rows.Close()

	links := make([]domain.MediaLink, 0)
	for rows.Next() {
		var link domain.MediaLink
		if err := rows.Scan(&link.MediaID, &link.TargetID, &link.Target, &link.Position, &link.IsFavorite); err != nil {
			return nil, fmt.Errorf("scan media link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list media links: %w", err)
	}
	return links, nil
}

// TripOfTarget - says which trip a link target belongs to, so a caller can
// check the reader's role on it before touching the gallery.
//
// Arguments:
//   - ctx: context bounding the query.
//   - target: trip, day or item.
//   - targetID: which one.
//
// Returns:
//   - the trip that owns the target.
//   - domain.ErrNotFound when the target does not exist.
func (r *MediaRepository) TripOfTarget(ctx context.Context, target domain.MediaTarget,
	targetID uuid.UUID) (uuid.UUID, error) {
	return linkTargetTrip(ctx, r.pool, target, targetID)
}

// linkColumn maps a target onto the column that holds it.
func linkColumn(target domain.MediaTarget) (string, error) {
	switch target {
	case domain.MediaTargetTrip:
		return "trip_id", nil
	case domain.MediaTargetDay:
		return "day_id", nil
	case domain.MediaTargetItem:
		return "item_id", nil
	default:
		return "", domain.NewValidationError("target_type", "invalid_target", "must be trip, day or item")
	}
}

// linkTargetTrip finds the trip behind a link target.
func linkTargetTrip(ctx context.Context, q querier, target domain.MediaTarget,
	targetID uuid.UUID) (uuid.UUID, error) {
	var query string
	switch target {
	case domain.MediaTargetTrip:
		query = `SELECT id FROM trips WHERE id = $1`
	case domain.MediaTargetDay:
		query = `SELECT doc.trip_id FROM days d JOIN documents doc ON doc.id = d.document_id WHERE d.id = $1`
	case domain.MediaTargetItem:
		query = `SELECT doc.trip_id FROM items i JOIN documents doc ON doc.id = i.document_id WHERE i.id = $1`
	default:
		return uuid.Nil, domain.NewValidationError("target_type", "invalid_target", "must be trip, day or item")
	}
	var tripID uuid.UUID
	err := q.QueryRow(ctx, query, targetID).Scan(&tripID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("find media target: %w", err)
	}
	return tripID, nil
}

// distinct removes repeated identifiers, so a list that names one file twice
// still matches the count the database reports.
func distinct(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, found := seen[id]; found {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// inTx runs fn inside a transaction, rolling it back on any failure.
func (r *MediaRepository) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
