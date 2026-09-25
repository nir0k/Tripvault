package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TripRepository reads and writes trips, their members and ownership.
type TripRepository struct {
	pool *pgxpool.Pool
}

// NewTripRepository - creates the trip repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	return &TripRepository{pool: pool}
}

// tripSummaryColumns is the shared select list, kept in step with scanTripSummary.
// Amounts are read in hundredths so they never pass through a float. The query
// joins the owner as "o"; the columns are named so the list can wrap them in a
// subquery without ambiguity.
//
// reader is the query parameter holding the person the trip is read for, or ""
// when it is read on nobody's behalf. It decides only whether the plan a report
// was copied from may be pointed at: somebody who cannot open that plan is not
// told which it is.
func tripSummaryColumns(reader string) string {
	sourceVisible := "false"
	if reader != "" {
		sourceVisible = `EXISTS (SELECT 1 FROM trips st
		   LEFT JOIN trip_members sm ON sm.trip_id = st.id AND sm.user_id = ` + reader + `
		   WHERE st.id = t.source_trip_id AND (st.owner_id = ` + reader + ` OR sm.user_id IS NOT NULL))`
	}
	return `t.id, t.owner_id, t.kind, t.source_trip_id, t.title, t.summary, t.start_date, t.end_date,
	t.timezone, t.currency, t.travelers, (t.budget_amount * 100)::bigint, t.cover_media_id,
	t.cover_crop_x, t.cover_crop_y, t.cover_crop_w, t.cover_crop_h, t.languages,
	(SELECT jsonb_object_agg(tr.lang, tr.fields) FROM (
	   SELECT lang, jsonb_object_agg(field, value) AS fields FROM translations
	   WHERE trip_id = t.id AND num_nonnulls(document_id, day_id, stay_id, item_id, leg_id, transfer_id) = 0
	   GROUP BY lang) tr) AS translations,
	t.created_at, t.updated_at, o.display_name, o.email,
	(SELECT d.id FROM documents d WHERE d.trip_id = t.id AND d.kind = 'plan') AS plan_id,
	(SELECT d.id FROM documents d WHERE d.trip_id = t.id AND d.kind = 'report') AS report_id,
	` + sourceVisible + ` AS source_visible`
}

// scanTripSummary reads one row in the order of tripSummaryColumns followed by
// the reader's role.
func scanTripSummary(row pgx.Row, extra ...any) (domain.TripSummary, error) {
	var (
		s      domain.TripSummary
		budget *int64
		role   *string
		crop   [4]*float64
	)
	dest := []any{&s.ID, &s.OwnerID, &s.Kind, &s.SourceTripID, &s.Title, &s.Summary, &s.StartDate, &s.EndDate,
		&s.Timezone, &s.Currency, &s.Travelers, &budget, &s.CoverMediaID,
		&crop[0], &crop[1], &crop[2], &crop[3], &s.Languages, &s.Translations,
		&s.CreatedAt, &s.UpdatedAt, &s.Owner.DisplayName, &s.Owner.Email,
		&s.PlanID, &s.ReportID, &s.SourceVisible, &role}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return domain.TripSummary{}, err
	}
	s.Owner.ID = s.OwnerID
	if crop[0] != nil && crop[1] != nil && crop[2] != nil && crop[3] != nil {
		s.CoverCrop = &domain.CoverCrop{X: *crop[0], Y: *crop[1], W: *crop[2], H: *crop[3]}
	}
	if budget != nil {
		amount := domain.Money(*budget)
		s.Budget = &amount
	}
	if role != nil {
		s.Role = domain.TripRole(*role)
	}
	return s, nil
}

// readerRole is the select expression of the reader's role, given the reader
// as a query parameter and the membership joined as "m".
func readerRole(param string) string {
	return `CASE WHEN t.owner_id = ` + param + ` THEN 'owner' ELSE m.role END`
}

// moneyParam converts an optional amount into the value a numeric column takes.
func moneyParam(amount *domain.Money) *string {
	if amount == nil {
		return nil
	}
	value := amount.String()
	return &value
}

// languagesParam gives a trip's languages as the array column takes them: a
// plan's none is an empty array, never NULL.
func languagesParam(languages []string) []string {
	if languages == nil {
		return []string{}
	}
	return languages
}

// cropParams gives a cover's frame as its four columns, all nil for a cover
// shown by its middle.
func cropParams(crop *domain.CoverCrop) (x, y, w, h *float64) {
	if crop == nil {
		return nil, nil, nil, nil
	}
	return &crop.X, &crop.Y, &crop.W, &crop.H
}

// Create - inserts a trip with its document, empty days one per date: a plan
// for a plan, a report for a report.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - trip: the validated trip with its ID, owner and kind set.
//
// Returns:
//   - the trip as its owner sees it.
//   - an error if a statement fails.
func (r *TripRepository) Create(ctx context.Context, trip domain.Trip) (domain.TripSummary, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := insertTrip(ctx, tx, trip); err != nil {
			return err
		}
		_, err := insertDocument(ctx, tx, trip.ID, trip.Kind, trip.StartDate, trip.EndDate)
		return err
	})
	if err != nil {
		return domain.TripSummary{}, err
	}
	return r.Get(ctx, trip.ID, trip.OwnerID)
}

// insertTrip writes a trip's own row, without a document.
func insertTrip(ctx context.Context, tx pgx.Tx, trip domain.Trip) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO trips (id, owner_id, kind, source_trip_id, title, summary, start_date, end_date, timezone,
		                    currency, travelers, budget_amount, languages)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::numeric, $13)`,
		trip.ID, trip.OwnerID, trip.Kind, trip.SourceTripID, trip.Title, trip.Summary, trip.StartDate,
		trip.EndDate, trip.Timezone, trip.Currency, trip.Travelers, moneyParam(trip.Budget), languagesParam(trip.Languages))
	if err != nil {
		return fmt.Errorf("create trip: %w", err)
	}
	return nil
}

// Get - reads a trip on behalf of a person, with their role on it.
//
// A trip the person has no access to is reported exactly like a missing one,
// so identifiers cannot be probed for existence.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//   - userID: the reader.
//
// Returns:
//   - the trip with the reader's role.
//   - domain.ErrNotFound when it does not exist, is deleted or is not shared
//     with the reader.
func (r *TripRepository) Get(ctx context.Context, tripID, userID uuid.UUID) (domain.TripSummary, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+tripSummaryColumns("$2")+`, `+readerRole("$2")+`
		 FROM trips t
		 JOIN users o ON o.id = t.owner_id
		 LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = $2
		 WHERE t.id = $1 AND (t.owner_id = $2 OR m.user_id IS NOT NULL)`,
		tripID, userID)
	summary, err := scanTripSummary(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TripSummary{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TripSummary{}, fmt.Errorf("get trip: %w", err)
	}
	return summary, nil
}

// tripCursor is the position after the last row of a page of a person's trips.
type tripCursor struct {
	group int
	key   int64
	id    uuid.UUID
}

// encodeCursor packs cursor fields into an opaque URL-safe string.
func encodeCursor(fields ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, ":")))
}

// decodeCursor unpacks an opaque cursor into its expected number of fields.
func decodeCursor(cursor string, count int) ([]string, error) {
	invalid := domain.NewValidationError("cursor", "invalid_cursor", "is not a cursor this list returned")
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, invalid
	}
	fields := strings.Split(string(raw), ":")
	if len(fields) != count {
		return nil, invalid
	}
	return fields, nil
}

// parseTripCursor decodes the cursor of a person's trip list.
func parseTripCursor(cursor string) (tripCursor, error) {
	fields, err := decodeCursor(cursor, 3)
	if err != nil {
		return tripCursor{}, err
	}
	group, groupErr := strconv.Atoi(fields[0])
	key, keyErr := strconv.ParseInt(fields[1], 10, 64)
	id, idErr := uuid.Parse(fields[2])
	if groupErr != nil || keyErr != nil || idErr != nil {
		return tripCursor{}, domain.NewValidationError("cursor", "invalid_cursor", "is not a cursor this list returned")
	}
	return tripCursor{group: group, key: key, id: id}, nil
}

// tripActivity is the moment the content of trip "t" last changed, whoever
// changed it: the trip's own row, its document and every record inside it,
// and the files uploaded to it. A translation touches its document, so it
// counts through that. A record removed leaves no trace, so a deletion alone
// does not move a trip up.
const tripActivity = `GREATEST(t.updated_at,
	(SELECT max(d.updated_at) FROM documents d WHERE d.trip_id = t.id),
	(SELECT max(x.updated_at) FROM days x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.updated_at) FROM stays x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.updated_at) FROM items x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.updated_at) FROM legs x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.created_at) FROM tracks x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.updated_at) FROM expenses x JOIN documents d ON d.id = x.document_id WHERE d.trip_id = t.id),
	(SELECT max(x.created_at) FROM media x WHERE x.trip_id = t.id))`

// likePattern turns free text into a substring pattern for ILIKE, escaping the
// wildcard characters so they match literally.
func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// List - returns one page of the trips a person owns or was given access to.
//
// By relevance the order puts what matters now first: ongoing trips, then
// upcoming ones by start date, then trips without dates by creation, newest
// first, then completed trips from the most recent. By update the trip whose
// content changed last comes first, in a single group. Either order is
// expressed as a (group, key, id) triple sorted ascending, which is also the
// keyset of the cursor.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the reader.
//   - filter: the validated filter.
//   - now: the instant "today" is derived from in each trip's time zone.
//
// Returns:
//   - the page and the cursor of the next one.
//   - a *domain.ValidationError for a malformed cursor.
func (r *TripRepository) List(ctx context.Context, userID uuid.UUID, filter domain.TripFilter,
	now time.Time) (domain.TripPage, error) {
	args := []any{userID}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	// Only the relevance order asks what day it is, and a parameter the query
	// never mentions has no type Postgres could infer, so "now" is passed only
	// then.
	group := "0"
	key := `-(extract(epoch FROM ` + tripActivity + `) * 1000000)::bigint`
	if filter.Sort != domain.SortUpdated {
		today := arg(now)
		group = `CASE
		       WHEN t.start_date IS NULL THEN 2
		       WHEN (` + today + `::timestamptz AT TIME ZONE t.timezone)::date < t.start_date THEN 1
		       WHEN (` + today + `::timestamptz AT TIME ZONE t.timezone)::date > t.end_date THEN 3
		       ELSE 0
		     END`
		key = `CASE g.sort_group
		       WHEN 2 THEN -(extract(epoch FROM t.created_at) * 1000000)::bigint
		       WHEN 3 THEN -(t.end_date - DATE '2000-01-01')::bigint
		       ELSE (t.start_date - DATE '2000-01-01')::bigint
		     END`
	}

	conditions := []string{"true"}
	switch filter.Scope {
	case domain.ScopeOwned:
		conditions = append(conditions, "t.owner_id = $1")
	case domain.ScopeShared:
		conditions = append(conditions, "m.user_id IS NOT NULL")
	default:
		conditions = append(conditions, "(t.owner_id = $1 OR m.user_id IS NOT NULL)")
	}
	if filter.Kind != "" {
		conditions = append(conditions, "t.kind = "+arg(filter.Kind))
	}
	if filter.Year != 0 {
		year := arg(filter.Year)
		conditions = append(conditions,
			"t.start_date <= make_date("+year+", 12, 31) AND t.end_date >= make_date("+year+", 1, 1)")
	}
	if filter.Query != "" {
		// A translated report is found by its title in any of its languages.
		pattern := arg(likePattern(filter.Query))
		conditions = append(conditions, `(t.title ILIKE `+pattern+` ESCAPE '\' OR EXISTS (
		  SELECT 1 FROM translations x
		  WHERE x.trip_id = t.id AND x.field = 'title'
		    AND num_nonnulls(x.document_id, x.day_id, x.stay_id, x.item_id, x.leg_id, x.transfer_id) = 0
		    AND x.value ILIKE `+pattern+` ESCAPE '\'))`)
	}

	outer := ""
	if filter.Cursor != "" {
		cursor, err := parseTripCursor(filter.Cursor)
		if err != nil {
			return domain.TripPage{}, err
		}
		outer = "WHERE (sort_group, sort_key, id) > (" + arg(cursor.group) + ", " + arg(cursor.key) + ", " +
			arg(cursor.id) + ")"
	}
	limit := arg(filter.Limit + 1)

	rows, err := r.pool.Query(ctx,
		`SELECT * FROM (
		   SELECT `+tripSummaryColumns("$1")+`, `+readerRole("$1")+` AS reader_role, g.sort_group, k.sort_key
		   FROM trips t
		   JOIN users o ON o.id = t.owner_id
		   LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = $1
		   CROSS JOIN LATERAL (SELECT `+group+` AS sort_group) g
		   CROSS JOIN LATERAL (SELECT `+key+` AS sort_key) k
		   WHERE `+strings.Join(conditions, " AND ")+`
		 ) page `+outer+`
		 ORDER BY sort_group, sort_key, id
		 LIMIT `+limit,
		args...)
	if err != nil {
		return domain.TripPage{}, fmt.Errorf("list trips: %w", err)
	}

	type positioned struct {
		summary domain.TripSummary
		cursor  tripCursor
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (positioned, error) {
		var p positioned
		var err error
		p.summary, err = scanTripSummary(row, &p.cursor.group, &p.cursor.key)
		p.cursor.id = p.summary.ID
		return p, err
	})
	if err != nil {
		return domain.TripPage{}, fmt.Errorf("read trips: %w", err)
	}

	page := domain.TripPage{Items: make([]domain.TripSummary, 0, len(items))}
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		last := items[len(items)-1].cursor
		page.NextCursor = encodeCursor(strconv.Itoa(last.group), strconv.FormatInt(last.key, 10), last.id.String())
	}
	for _, item := range items {
		page.Items = append(page.Items, item.summary)
	}
	return page, nil
}

// Years - lists the calendar years a person's plans or reports touch, newest
// first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - userID: the reader.
//   - kind: the plans or the reports; "" counts both.
//
// Returns:
//   - the years, feeding the year filter of the list.
//   - an error if the query fails.
func (r *TripRepository) Years(ctx context.Context, userID uuid.UUID, kind domain.DocumentKind) ([]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT generate_series(extract(year FROM t.start_date)::int, extract(year FROM t.end_date)::int) AS year
		 FROM trips t
		 LEFT JOIN trip_members m ON m.trip_id = t.id AND m.user_id = $1
		 WHERE t.start_date IS NOT NULL AND (t.owner_id = $1 OR m.user_id IS NOT NULL)
		   AND ($2 = '' OR t.kind = $2)
		 ORDER BY year DESC`, userID, string(kind))
	if err != nil {
		return nil, fmt.Errorf("list trip years: %w", err)
	}
	years, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, fmt.Errorf("read trip years: %w", err)
	}
	return years, nil
}

// Update - stores a trip's editable fields. Field changes follow
// last-write-wins; a change of dates also reshapes the documents in the same
// transaction.
//
// A language taken off a report takes its translations with it, and so does the
// original: the report's own columns already hold that language.
//
// Moving the start moves every day and stay by the same number of days. A
// period of a different length adds empty days at the end of each document or
// removes days from the end; removing a day with content needs confirm.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - trip: the validated trip.
//   - confirm: whether removing days with content was confirmed.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist or is deleted.
//   - a *domain.DaysWouldBeRemovedError when confirmation is needed.
func (r *TripRepository) Update(ctx context.Context, trip domain.Trip, confirm bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		oldStart, oldEnd, err := tripDates(ctx, tx, trip.ID)
		if err != nil {
			return err
		}
		cropX, cropY, cropW, cropH := cropParams(trip.CoverCrop)
		if _, err := tx.Exec(ctx,
			`UPDATE trips
			 SET title = $2, summary = $3, start_date = $4, end_date = $5, timezone = $6, currency = $7,
			     travelers = $8, budget_amount = $9::numeric, cover_media_id = $10, languages = $11,
			     cover_crop_x = $12, cover_crop_y = $13, cover_crop_w = $14, cover_crop_h = $15,
			     updated_at = now()
			 WHERE id = $1`,
			trip.ID, trip.Title, trip.Summary, trip.StartDate, trip.EndDate, trip.Timezone, trip.Currency,
			trip.Travelers, moneyParam(trip.Budget), trip.CoverMediaID, languagesParam(trip.Languages),
			cropX, cropY, cropW, cropH); err != nil {
			return fmt.Errorf("update trip: %w", err)
		}
		translated := []string{}
		if len(trip.Languages) > 1 {
			translated = trip.Languages[1:]
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM translations WHERE trip_id = $1 AND NOT (lang = ANY($2))`,
			trip.ID, translated); err != nil {
			return fmt.Errorf("drop translations of removed languages: %w", err)
		}
		if sameDate(oldStart, trip.StartDate) && sameDate(oldEnd, trip.EndDate) {
			return nil
		}
		return reshapeTrip(ctx, tx, trip.ID, oldStart, trip.StartDate, trip.EndDate, confirm)
	})
}

// sameDate compares two optional calendar dates.
func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Delete - removes a trip and everything in it, for good.
//
// There is no undo: a trip is deleted when its owner types its name to say so,
// and nothing is kept afterwards. The keys of its files come back so the caller
// can remove the bytes too; the rows go with the trip through the cascades.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tripID: the trip.
//
// Returns:
//   - the storage keys of the trip's files, for the caller to delete.
//   - domain.ErrNotFound when the trip does not exist.
func (r *TripRepository) Delete(ctx context.Context, tripID uuid.UUID) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT storage_key FROM media WHERE trip_id = $1`, tripID)
		if err != nil {
			return fmt.Errorf("list media of the trip: %w", err)
		}
		keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("read media keys: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM trips WHERE id = $1`, tripID)
		if err != nil {
			return fmt.Errorf("delete trip: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// Members - lists everybody with access to a trip, the owner first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - the owner followed by the members ordered by name.
//   - an error if the query fails.
func (r *TripRepository) Members(ctx context.Context, tripID uuid.UUID) ([]domain.TripMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.display_name, u.email, 'owner', t.created_at, 0 AS rank
		 FROM trips t JOIN users u ON u.id = t.owner_id
		 WHERE t.id = $1
		 UNION ALL
		 SELECT u.id, u.display_name, u.email, m.role, m.created_at, 1
		 FROM trip_members m JOIN users u ON u.id = m.user_id
		 WHERE m.trip_id = $1
		 ORDER BY rank, 2, 3`, tripID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TripMember, error) {
		var member domain.TripMember
		var rank int
		err := row.Scan(&member.User.ID, &member.User.DisplayName, &member.User.Email, &member.Role,
			&member.CreatedAt, &rank)
		return member, err
	})
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}
	return members, nil
}

// lockTrip locks a live trip's row for the rest of the transaction and returns
// its owner, so membership and ownership changes on one trip run one at a time.
func lockTrip(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) (uuid.UUID, error) {
	var ownerID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT owner_id FROM trips WHERE id = $1 FOR UPDATE`, tripID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock trip: %w", err)
	}
	return ownerID, nil
}

// requireActiveUser checks inside a transaction that a person can be given a
// trip, reporting the problem against the user_id field.
func requireActiveUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT is_active FROM users WHERE id = $1`, userID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NewValidationError("user_id", "unknown_user", "no such account")
	}
	if err != nil {
		return fmt.Errorf("read user: %w", err)
	}
	if !active {
		return domain.NewValidationError("user_id", "inactive_user", "the account is deactivated")
	}
	return nil
}

// member reads one person's access to a trip inside a transaction.
func member(ctx context.Context, tx pgx.Tx, tripID, userID uuid.UUID) (domain.TripMember, error) {
	var m domain.TripMember
	err := tx.QueryRow(ctx,
		`SELECT u.id, u.display_name, u.email, m.role, m.created_at
		 FROM trip_members m JOIN users u ON u.id = m.user_id
		 WHERE m.trip_id = $1 AND m.user_id = $2`, tripID, userID).
		Scan(&m.User.ID, &m.User.DisplayName, &m.User.Email, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TripMember{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TripMember{}, fmt.Errorf("read member: %w", err)
	}
	return m, nil
}

// AddMember - gives a person access to a trip.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tripID: the trip.
//   - userID: the person to add.
//   - role: editor or viewer, already validated.
//
// Returns:
//   - the new member.
//   - domain.ErrNotFound when the trip does not exist.
//   - domain.ErrAlreadyMember when the person is the owner or already a member.
//   - a *domain.ValidationError when the account is unknown or deactivated.
func (r *TripRepository) AddMember(ctx context.Context, tripID, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error) {
	var added domain.TripMember
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		ownerID, err := lockTrip(ctx, tx, tripID)
		if err != nil {
			return err
		}
		if ownerID == userID {
			return domain.ErrAlreadyMember
		}
		if err := requireActiveUser(ctx, tx, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)`,
			tripID, userID, role)
		if isUniqueViolation(err) {
			return domain.ErrAlreadyMember
		}
		if err != nil {
			return fmt.Errorf("add member: %w", err)
		}
		added, err = member(ctx, tx, tripID, userID)
		return err
	})
	if err != nil {
		return domain.TripMember{}, err
	}
	return added, nil
}

// UpdateMember - changes a member's role.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tripID: the trip.
//   - userID: the member.
//   - role: editor or viewer, already validated.
//
// Returns:
//   - the updated member.
//   - domain.ErrNotFound when the trip does not exist or the person is not a
//     member; the owner is not a member.
func (r *TripRepository) UpdateMember(ctx context.Context, tripID, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error) {
	var updated domain.TripMember
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, tripID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE trip_members SET role = $3 WHERE trip_id = $1 AND user_id = $2`,
			tripID, userID, role)
		if err != nil {
			return fmt.Errorf("update member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		updated, err = member(ctx, tx, tripID, userID)
		return err
	})
	if err != nil {
		return domain.TripMember{}, err
	}
	return updated, nil
}

// RemoveMember - takes a member's access to a trip away.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - tripID: the trip.
//   - userID: the member.
//
// Returns:
//   - domain.ErrNotFound when the person is not a member.
func (r *TripRepository) RemoveMember(ctx context.Context, tripID, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM trip_members WHERE trip_id = $1 AND user_id = $2`, tripID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// AdminList - returns one page of every live trip, newest first, whoever owns
// it. It looks past memberships, so it serves the service itself - seeding asks
// it whether the instance is empty - and is not reachable through the API.
//
// Arguments:
//   - ctx: context bounding the query.
//   - filter: the validated filter.
//
// Returns:
//   - the page and the cursor of the next one.
//   - a *domain.ValidationError for a malformed cursor.
func (r *TripRepository) AdminList(ctx context.Context, filter domain.AdminTripFilter) (domain.TripPage, error) {
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	conditions := []string{"true"}
	if filter.Query != "" {
		pattern := arg(likePattern(filter.Query))
		conditions = append(conditions,
			`(t.title ILIKE `+pattern+` ESCAPE '\' OR o.display_name ILIKE `+pattern+` ESCAPE '\' OR o.email ILIKE `+
				pattern+` ESCAPE '\')`)
	}
	if filter.Cursor != "" {
		fields, err := decodeCursor(filter.Cursor, 2)
		if err != nil {
			return domain.TripPage{}, err
		}
		micros, microsErr := strconv.ParseInt(fields[0], 10, 64)
		id, idErr := uuid.Parse(fields[1])
		if microsErr != nil || idErr != nil {
			return domain.TripPage{}, domain.NewValidationError("cursor", "invalid_cursor", "is not a cursor this list returned")
		}
		conditions = append(conditions, "(t.created_at, t.id) < ("+arg(time.UnixMicro(micros))+", "+arg(id)+")")
	}
	limit := arg(filter.Limit + 1)

	rows, err := r.pool.Query(ctx,
		`SELECT `+tripSummaryColumns("")+`, NULL::text
		 FROM trips t JOIN users o ON o.id = t.owner_id
		 WHERE `+strings.Join(conditions, " AND ")+`
		 ORDER BY t.created_at DESC, t.id DESC
		 LIMIT `+limit, args...)
	if err != nil {
		return domain.TripPage{}, fmt.Errorf("list all trips: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TripSummary, error) {
		return scanTripSummary(row)
	})
	if err != nil {
		return domain.TripPage{}, fmt.Errorf("read all trips: %w", err)
	}

	page := domain.TripPage{Items: items}
	if len(items) > filter.Limit {
		page.Items = items[:filter.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(strconv.FormatInt(last.CreatedAt.UnixMicro(), 10), last.ID.String())
	}
	return page, nil
}
