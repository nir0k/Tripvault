package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// DocumentRepository reads and writes documents with their days, places and
// stays.
//
// Every change that touches more than one row runs in a transaction that first
// locks the trip row. That serialises structural edits of one trip - reorders,
// moves, day and date changes - so positions, dates and stay marks stay
// consistent under concurrent editors, while ordinary field edits follow
// last-write-wins.
type DocumentRepository struct {
	pool *pgxpool.Pool
}

// NewDocumentRepository - creates the document repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	return &DocumentRepository{pool: pool}
}

// querier is what both the pool and a transaction offer, so reads can run
// inside or outside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// clockColumn converts a time column into minutes after midnight.
func clockColumn(column string) string {
	return "(extract(hour FROM " + column + ") * 60 + extract(minute FROM " + column + "))::int"
}

// clockParam converts an optional time of day into the value a time column takes.
func clockParam(clock *domain.ClockTime) *string {
	if clock == nil {
		return nil
	}
	value := clock.String()
	return &value
}

// oneRow scans a single-row result with scan, mapping "no rows" to
// domain.ErrNotFound.
func oneRow[T any](scan func(pgx.Row) (T, error), row pgx.Row, action string) (T, error) {
	value, err := scan(row)
	return one(value, err, action)
}

// one maps "no rows" to domain.ErrNotFound and wraps any other failure.
func one[T any](value T, err error, action string) (T, error) {
	var zero T
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, domain.ErrNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("%s: %w", action, err)
	}
	return value, nil
}

const documentColumns = `doc.id, doc.trip_id, doc.kind, doc.source_document_id, doc.intro_md, doc.summary_md,
	doc.created_at, doc.updated_at`

// scanDocument reads one row in the order of documentColumns.
func scanDocument(row pgx.Row) (domain.Document, error) {
	var d domain.Document
	err := row.Scan(&d.ID, &d.TripID, &d.Kind, &d.SourceDocumentID, &d.IntroMD, &d.SummaryMD, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

var dayColumns = `d.id, d.document_id, d.position, d.date, d.title, d.notes_md, ` + clockColumn("d.start_time") + `,
	d.default_mode, d.timezone, d.morning_anchor, d.evening_anchor, d.no_overnight, d.cover_media_id,
	d.created_at, d.updated_at`

// scanDay reads one row in the order of dayColumns.
func scanDay(row pgx.Row) (domain.Day, error) {
	var d domain.Day
	err := row.Scan(&d.ID, &d.DocumentID, &d.Position, &d.Date, &d.Title, &d.NotesMD, &d.StartTime,
		&d.DefaultMode, &d.Timezone, &d.MorningAnchor, &d.EveningAnchor, &d.NoOvernight, &d.CoverMediaID,
		&d.CreatedAt, &d.UpdatedAt)
	return d, err
}

var stayColumns = `s.id, s.document_id, s.name, s.kind, s.address, s.lat, s.lng, s.check_in_date, ` +
	clockColumn("s.check_in_time") + `, s.check_out_date, ` + clockColumn("s.check_out_time") + `,
	s.booking_ref, s.url, s.contacts, s.notes_md, (s.planned_cost_amount * 100)::bigint,
	(s.actual_cost_amount * 100)::bigint, s.source_stay_id, s.created_at, s.updated_at`

// scanStay reads one row in the order of stayColumns.
func scanStay(row pgx.Row) (domain.Stay, error) {
	var s domain.Stay
	err := row.Scan(&s.ID, &s.DocumentID, &s.Name, &s.Kind, &s.Address, &s.Lat, &s.Lng, &s.CheckInDate,
		&s.CheckInTime, &s.CheckOutDate, &s.CheckOutTime, &s.BookingRef, &s.URL, &s.Contacts, &s.NotesMD,
		&s.PlannedCost, &s.ActualCost, &s.SourceStayID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

var legColumns = `l.id, l.document_id, l.day_id, l.from_item_id, l.to_item_id, l.mode, l.distance_m, l.duration_s,
	coalesce(l.geometry, ''), l.calc_source, coalesce(l.calc_error, ''), l.calc_input, l.calculated_at,
	l.manual_distance_m, l.manual_duration_s, (l.planned_cost_amount * 100)::bigint,
	(l.actual_cost_amount * 100)::bigint, l.note, l.created_at, l.updated_at`

// scanLeg reads one row in the order of legColumns.
func scanLeg(row pgx.Row) (domain.Leg, error) {
	var l domain.Leg
	err := row.Scan(&l.ID, &l.DocumentID, &l.DayID, &l.FromItemID, &l.ToItemID, &l.Mode, &l.DistanceM, &l.DurationS,
		&l.Geometry, &l.Source, &l.Error, &l.Input, &l.CalculatedAt,
		&l.ManualDistanceM, &l.ManualDurationS, &l.PlannedCost, &l.ActualCost, &l.Note, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

var itemColumns = `i.id, i.document_id, i.day_id, i.position, i.kind, coalesce(i.anchor, ''), i.stay_id, i.name,
	i.category, coalesce(i.activity_type, ''), i.lat, i.lng, i.address, coalesce(i.osm_ref, ''), i.description_md, i.url, ` +
	clockColumn("i.desired_time") + `, i.visit_minutes, i.is_optional, i.booking_ref,
	(i.planned_cost_amount * 100)::bigint, i.cost_per_person, i.cost_category,
	i.status, i.story_md, ` + clockColumn("i.actual_time") + `, ` + clockColumn("i.actual_end_time") + `, i.rating,
	(i.actual_cost_amount * 100)::bigint, i.cover_media_id, i.source_item_id, i.difficulty, i.created_at, i.updated_at`

// scanItem reads one row in the order of itemColumns.
func scanItem(row pgx.Row) (domain.Item, error) {
	var i domain.Item
	err := row.Scan(&i.ID, &i.DocumentID, &i.DayID, &i.Position, &i.Kind, &i.Anchor, &i.StayID, &i.Name,
		&i.Category, &i.ActivityType, &i.Lat, &i.Lng, &i.Address, &i.OSMRef, &i.DescriptionMD, &i.URL,
		&i.DesiredTime, &i.VisitMinutes, &i.IsOptional, &i.BookingRef,
		&i.PlannedCost, &i.CostPerPerson, &i.CostCategory,
		&i.Status, &i.StoryMD, &i.ActualTime, &i.ActualEndTime, &i.Rating, &i.ActualCost, &i.CoverMediaID,
		&i.SourceItemID, &i.Difficulty, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

// Document - reads a document without its content.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the document.
//
// Returns:
//   - the document.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Document(ctx context.Context, id uuid.UUID) (domain.Document, error) {
	return oneRow(scanDocument, r.pool.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents doc WHERE doc.id = $1`, id),
		"get document")
}

// Day - reads one day.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the day.
//
// Returns:
//   - the day.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Day(ctx context.Context, id uuid.UUID) (domain.Day, error) {
	return oneRow(scanDay, r.pool.QueryRow(ctx, `SELECT `+dayColumns+` FROM days d WHERE d.id = $1`, id), "get day")
}

// Item - reads one place or stay mark.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the item.
//
// Returns:
//   - the item.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Item(ctx context.Context, id uuid.UUID) (domain.Item, error) {
	return oneRow(scanItem, r.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM items i WHERE i.id = $1`, id), "get item")
}

// Stay - reads one stay.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the stay.
//
// Returns:
//   - the stay.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Stay(ctx context.Context, id uuid.UUID) (domain.Stay, error) {
	return oneRow(scanStay, r.pool.QueryRow(ctx, `SELECT `+stayColumns+` FROM stays s WHERE s.id = $1`, id), "get stay")
}

// Content - reads a whole document: its days, places, marks, stays and
// transfers.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - id: the document.
//
// Returns:
//   - the content.
//   - domain.ErrNotFound when the document does not exist.
func (r *DocumentRepository) Content(ctx context.Context, id uuid.UUID) (domain.DocumentContent, error) {
	var content domain.DocumentContent
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead},
		func(tx pgx.Tx) error {
			var err error
			content, err = readContent(ctx, tx, id)
			return err
		})
	return content, err
}

// readContent reads a whole document through q.
func readContent(ctx context.Context, q querier, id uuid.UUID) (domain.DocumentContent, error) {
	var content domain.DocumentContent
	var err error
	content.Document, err = oneRow(scanDocument,
		q.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents doc WHERE doc.id = $1`, id), "get document")
	if err != nil {
		return content, err
	}
	if content.Days, err = collect(ctx, q, scanDay,
		`SELECT `+dayColumns+` FROM days d WHERE d.document_id = $1 ORDER BY d.position`, id); err != nil {
		return content, err
	}
	if content.Items, err = collect(ctx, q, scanItem,
		`SELECT `+itemColumns+` FROM items i WHERE i.document_id = $1 ORDER BY i.position, i.created_at`, id); err != nil {
		return content, err
	}
	if content.Stays, err = collect(ctx, q, scanStay,
		`SELECT `+stayColumns+` FROM stays s WHERE s.document_id = $1 ORDER BY s.check_in_date, s.id`, id); err != nil {
		return content, err
	}
	if content.Transfers, err = collect(ctx, q, scanTransfer,
		`SELECT `+transferColumns+` FROM transfers t WHERE t.document_id = $1 ORDER BY `+transferOrder, id); err != nil {
		return content, err
	}
	if content.Legs, err = collect(ctx, q, scanLeg,
		`SELECT `+legColumns+` FROM legs l WHERE l.document_id = $1`, id); err != nil {
		return content, err
	}
	if content.Expenses, err = collect(ctx, q, scanExpense,
		`SELECT `+expenseColumns+` FROM expenses e WHERE e.document_id = $1 ORDER BY e.created_at, e.id`, id); err != nil {
		return content, err
	}
	if content.Tracks, err = collect(ctx, q, scanTrack,
		`SELECT `+trackColumns+` FROM tracks t WHERE t.document_id = $1`, id); err != nil {
		return content, err
	}
	content.Translations, err = documentTranslations(ctx, q, content.Document.TripID)
	return content, err
}

// collect runs a query and scans every row with scan.
func collect[T any](ctx context.Context, q querier, scan func(pgx.Row) (T, error), sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	values, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
	if err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return values, nil
}

// tripDates locks a live trip for the rest of the transaction and returns its
// dates, which every structural change of its documents depends on.
func tripDates(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) (start, end *time.Time, err error) {
	err = tx.QueryRow(ctx,
		`SELECT start_date, end_date FROM trips WHERE id = $1 FOR UPDATE`, tripID).
		Scan(&start, &end)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lock trip: %w", err)
	}
	return start, end, nil
}

// lockDocument locks the trip of a document and returns the document with the
// trip's start date.
func lockDocument(ctx context.Context, tx pgx.Tx, documentID uuid.UUID) (domain.Document, *time.Time, error) {
	var tripID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT trip_id FROM documents WHERE id = $1`, documentID).Scan(&tripID); err != nil {
		_, err = one(tripID, err, "find document")
		return domain.Document{}, nil, err
	}
	start, _, err := tripDates(ctx, tx, tripID)
	if err != nil {
		return domain.Document{}, nil, err
	}
	document, err := oneRow(scanDocument,
		tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents doc WHERE doc.id = $1`, documentID), "get document")
	return document, start, err
}

// insertDocument creates a document with its first days: one per date of a
// trip with dates, or a single day otherwise.
func insertDocument(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, kind domain.DocumentKind,
	start, end *time.Time) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	_, err := tx.Exec(ctx, `INSERT INTO documents (id, trip_id, kind) VALUES ($1, $2, $3)`, id, tripID, kind)
	if isUniqueViolation(err) {
		return uuid.Nil, domain.ErrAlreadyExists
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("create document: %w", err)
	}
	count := 1
	if days := (domain.Trip{StartDate: start, EndDate: end}).DayCount(); days != nil {
		count = *days
	}
	if err := appendDays(ctx, tx, id, 0, count); err != nil {
		return uuid.Nil, err
	}
	return id, layoutDates(ctx, tx, tripID)
}

// appendDays adds empty days at positions from..to-1.
func appendDays(ctx context.Context, tx pgx.Tx, documentID uuid.UUID, from, to int) error {
	for position := from; position < to; position++ {
		if _, err := tx.Exec(ctx, `INSERT INTO days (id, document_id, position) VALUES ($1, $2, $3)`,
			uuid.Must(uuid.NewV7()), documentID, position); err != nil {
			return fmt.Errorf("add day: %w", err)
		}
	}
	return nil
}

// layoutDates gives every day of a trip's documents the date its position
// implies: the trip start plus the position, or none without a start.
func layoutDates(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE days d SET date = t.start_date + d.position
		 FROM documents doc JOIN trips t ON t.id = doc.trip_id
		 WHERE d.document_id = doc.id AND doc.trip_id = $1 AND d.date IS DISTINCT FROM t.start_date + d.position`,
		tripID)
	if err != nil {
		return fmt.Errorf("lay out day dates: %w", err)
	}
	return nil
}

// fitDocuments makes every document of a trip hold exactly length days,
// adding empty days at the end or removing days from the end.
//
// Removing a day that holds content needs confirm; without it the whole change
// is refused with a *domain.DaysWouldBeRemovedError listing those days. A
// removed plan day hands its places to the unassigned list rather than losing
// them.
func fitDocuments(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, length int, confirm bool) error {
	type document struct {
		id   uuid.UUID
		kind domain.DocumentKind
		days int
	}
	documents, err := collect(ctx, tx, func(row pgx.Row) (document, error) {
		var d document
		return d, row.Scan(&d.id, &d.kind, &d.days)
	}, `SELECT doc.id, doc.kind, (SELECT count(*) FROM days d WHERE d.document_id = doc.id)
	    FROM documents doc WHERE doc.trip_id = $1`, tripID)
	if err != nil {
		return err
	}

	removed, err := collect(ctx, tx, func(row pgx.Row) (domain.RemovedDay, error) {
		var day domain.RemovedDay
		return day, row.Scan(&day.DocumentKind, &day.Position, &day.Date, &day.Title)
	}, `SELECT doc.kind, d.position, d.date, d.title
	    FROM days d JOIN documents doc ON doc.id = d.document_id
	    WHERE doc.trip_id = $1 AND d.position >= $2
	      AND (d.title <> '' OR btrim(d.notes_md) <> ''
	           OR EXISTS (SELECT 1 FROM items i WHERE i.day_id = d.id AND i.kind <> 'stay_anchor'))
	    ORDER BY doc.kind, d.position`, tripID, length)
	if err != nil {
		return err
	}
	if len(removed) > 0 && !confirm {
		return &domain.DaysWouldBeRemovedError{Days: removed}
	}

	for _, doc := range documents {
		switch {
		case doc.days < length:
			if err := appendDays(ctx, tx, doc.id, doc.days, length); err != nil {
				return err
			}
		case doc.days > length:
			if err := dropTailDays(ctx, tx, doc.id, doc.kind, length); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropTailDays removes a document's days from position from onwards.
func dropTailDays(ctx context.Context, tx pgx.Tx, documentID uuid.UUID, kind domain.DocumentKind, from int) error {
	if kind == domain.DocumentPlan {
		if _, err := tx.Exec(ctx,
			`UPDATE items i
			 SET day_id = NULL, updated_at = now(),
			     position = (SELECT coalesce(max(u.position) + 1, 0) FROM items u
			                 WHERE u.document_id = $1 AND u.day_id IS NULL) + (d.position * 1000) + i.position
			 FROM days d
			 WHERE i.day_id = d.id AND d.document_id = $1 AND d.position >= $2 AND i.kind <> 'stay_anchor'`,
			documentID, from); err != nil {
			return fmt.Errorf("unassign places of removed days: %w", err)
		}
		if err := renumberPlaces(ctx, tx, documentID, nil); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM days WHERE document_id = $1 AND position >= $2`, documentID, from); err != nil {
		return fmt.Errorf("remove days: %w", err)
	}
	return nil
}

// renumberPlaces closes the gaps in the positions of one list of places: a
// day's, or the unassigned list when dayID is nil.
func renumberPlaces(ctx context.Context, tx pgx.Tx, documentID uuid.UUID, dayID *uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`UPDATE items i SET position = r.rn - 1
		 FROM (SELECT id, row_number() OVER (ORDER BY position, created_at, id) AS rn FROM items
		       WHERE document_id = $1 AND day_id IS NOT DISTINCT FROM $2 AND kind <> 'stay_anchor') r
		 WHERE i.id = r.id AND i.position <> r.rn - 1`, documentID, dayID)
	if err != nil {
		return fmt.Errorf("renumber places: %w", err)
	}
	return nil
}

// openSlot makes room in a list of places and returns the position to use. A
// nil or out-of-range position means the end of the list. except is left out
// of the list, for a place moving within it.
func openSlot(ctx context.Context, tx pgx.Tx, documentID uuid.UUID, dayID *uuid.UUID, position *int, except uuid.UUID) (int, error) {
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM items WHERE document_id = $1 AND day_id IS NOT DISTINCT FROM $2 AND kind <> 'stay_anchor' AND id <> $3`,
		documentID, dayID, except).Scan(&count); err != nil {
		return 0, fmt.Errorf("count places: %w", err)
	}
	slot := count
	if position != nil && *position >= 0 && *position < count {
		slot = *position
	}
	if _, err := tx.Exec(ctx,
		`UPDATE items SET position = position + 1
		 WHERE document_id = $1 AND day_id IS NOT DISTINCT FROM $2 AND kind <> 'stay_anchor' AND id <> $3 AND position >= $4`,
		documentID, dayID, except, slot); err != nil {
		return 0, fmt.Errorf("open a slot: %w", err)
	}
	return slot, nil
}

// syncAnchors places, moves and removes the stay marks of every document of a
// trip so they match the days and stays. It runs after any change to either.
func syncAnchors(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) error {
	documentIDs, err := collect(ctx, tx, func(row pgx.Row) (uuid.UUID, error) {
		var id uuid.UUID
		return id, row.Scan(&id)
	}, `SELECT id FROM documents WHERE trip_id = $1`, tripID)
	if err != nil {
		return err
	}

	for _, documentID := range documentIDs {
		content, err := readContent(ctx, tx, documentID)
		if err != nil {
			return err
		}
		wanted := domain.AnchorAssignments(content.Days, content.Stays)

		for _, item := range content.Items {
			if item.Kind != domain.ItemStayAnchor {
				continue
			}
			key := domain.AnchorKey{DayID: *item.DayID, Slot: item.Anchor}
			stayID, keep := wanted[key]
			switch {
			case !keep:
				if _, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1`, item.ID); err != nil {
					return fmt.Errorf("remove stay mark: %w", err)
				}
			case *item.StayID != stayID:
				if _, err := tx.Exec(ctx, `UPDATE items SET stay_id = $2, updated_at = now() WHERE id = $1`,
					item.ID, stayID); err != nil {
					return fmt.Errorf("move stay mark: %w", err)
				}
			}
			delete(wanted, key)
		}

		for key, stayID := range wanted {
			if _, err := tx.Exec(ctx,
				`INSERT INTO items (id, document_id, day_id, kind, anchor, stay_id, cost_category)
				 VALUES ($1, $2, $3, 'stay_anchor', $4, $5, 'accommodation')`,
				uuid.Must(uuid.NewV7()), documentID, key.DayID, key.Slot, stayID); err != nil {
				return fmt.Errorf("place stay mark: %w", err)
			}
		}
	}
	return nil
}

// syncTrip brings the derived parts of every document of a trip in line after a
// change: the stay marks first, then the legs between the elements they frame.
func syncTrip(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) error {
	if err := syncAnchors(ctx, tx, tripID); err != nil {
		return err
	}
	return syncLegs(ctx, tx, tripID)
}

// syncLegs creates, resets and deletes legs so every pair of neighbouring
// elements has exactly one, and a leg whose mode or points changed waits for a
// new calculation.
func syncLegs(ctx context.Context, tx pgx.Tx, tripID uuid.UUID) error {
	documentIDs, err := collect(ctx, tx, func(row pgx.Row) (uuid.UUID, error) {
		var id uuid.UUID
		return id, row.Scan(&id)
	}, `SELECT id FROM documents WHERE trip_id = $1`, tripID)
	if err != nil {
		return err
	}

	for _, documentID := range documentIDs {
		content, err := readContent(ctx, tx, documentID)
		if err != nil {
			return err
		}
		plan := domain.ReconcileLegs(content, content.Legs, func() uuid.UUID { return uuid.Must(uuid.NewV7()) })
		if len(plan.Delete) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM legs WHERE id = ANY($1)`, plan.Delete); err != nil {
				return fmt.Errorf("remove legs: %w", err)
			}
		}
		for _, leg := range plan.Reset {
			if _, err := tx.Exec(ctx,
				`UPDATE legs SET calc_input = $2, calc_source = 'pending', distance_m = NULL, duration_s = NULL,
				                 geometry = NULL, calc_error = NULL, calculated_at = NULL, updated_at = now()
				 WHERE id = $1`, leg.ID, leg.Input); err != nil {
				return fmt.Errorf("reset leg: %w", err)
			}
		}
		for _, leg := range plan.Create {
			if _, err := tx.Exec(ctx,
				`INSERT INTO legs (id, document_id, day_id, from_item_id, to_item_id, mode, calc_input)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				leg.ID, documentID, leg.DayID, leg.FromItemID, leg.ToItemID, leg.Mode, leg.Input); err != nil {
				return fmt.Errorf("add leg: %w", err)
			}
		}
	}
	return nil
}

// reshapeTrip brings a trip's documents in line after its dates changed: stays
// and transfers move with a shifted start, documents get as many days as the period has, and
// dates and stay marks follow.
func reshapeTrip(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, oldStart, newStart, newEnd *time.Time, confirm bool) error {
	if oldStart != nil && newStart != nil && !oldStart.Equal(*newStart) {
		shift := int(newStart.Sub(*oldStart).Hours() / 24)
		if _, err := tx.Exec(ctx,
			`UPDATE stays s SET check_in_date = check_in_date + $2::int, check_out_date = check_out_date + $2::int, updated_at = now()
			 FROM documents doc WHERE s.document_id = doc.id AND doc.trip_id = $1`, tripID, shift); err != nil {
			return fmt.Errorf("shift stays: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE transfers t SET departure_date = departure_date + $2::int, arrival_date = arrival_date + $2::int,
			                        updated_at = now()
			 FROM documents doc WHERE t.document_id = doc.id AND doc.trip_id = $1`, tripID, shift); err != nil {
			return fmt.Errorf("shift transfers: %w", err)
		}
	}
	if length := (domain.Trip{StartDate: newStart, EndDate: newEnd}).DayCount(); length != nil {
		if err := fitDocuments(ctx, tx, tripID, *length, confirm); err != nil {
			return err
		}
	}
	if err := layoutDates(ctx, tx, tripID); err != nil {
		return err
	}
	return syncTrip(ctx, tx, tripID)
}

// resizeTrip changes the day count of a trip with dates by delta, moving its
// end date and fitting the other documents to the new length.
func resizeTrip(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, start *time.Time, length int, confirm bool) error {
	if start == nil {
		return nil
	}
	if length > domain.MaxTripDays {
		return domain.NewValidationError("end_date", "trip_too_long", "a trip lasts at most 366 days")
	}
	if _, err := tx.Exec(ctx, `UPDATE trips SET end_date = start_date + $2::int - 1, updated_at = now() WHERE id = $1`,
		tripID, length); err != nil {
		return fmt.Errorf("move trip end: %w", err)
	}
	return fitDocuments(ctx, tx, tripID, length, confirm)
}

// countDays returns how many days a document has.
func countDays(ctx context.Context, tx pgx.Tx, documentID uuid.UUID) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM days WHERE document_id = $1`, documentID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count days: %w", err)
	}
	return count, nil
}

// insertDayAt opens a position among a document's days and inserts the day there.
func insertDayAt(ctx context.Context, tx pgx.Tx, day domain.Day, count int, position *int) error {
	slot := count
	if position != nil && *position >= 0 && *position < count {
		slot = *position
	}
	if _, err := tx.Exec(ctx, `UPDATE days SET position = position + 1 WHERE document_id = $1 AND position >= $2`,
		day.DocumentID, slot); err != nil {
		return fmt.Errorf("open a day slot: %w", err)
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO days (id, document_id, position, title, notes_md, start_time, default_mode, timezone,
		                   morning_anchor, evening_anchor, no_overnight)
		 VALUES ($1, $2, $3, $4, $5, $6::time, $7, $8, $9, $10, $11)`,
		day.ID, day.DocumentID, slot, day.Title, day.NotesMD, day.StartTime.String(), day.DefaultMode, day.Timezone,
		day.MorningAnchor, day.EveningAnchor, day.NoOvernight)
	if err != nil {
		return fmt.Errorf("add day: %w", err)
	}
	return nil
}

// AddDay - inserts a day into a document.
//
// On a trip with dates every day has a date, so a new day lengthens the trip by
// one: the end date moves and the other document gains an empty day at its end.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - day: the validated day with its ID and document set.
//   - position: where to insert it; nil or out of range appends.
//
// Returns:
//   - domain.ErrNotFound when the document or its trip does not exist.
//   - a *domain.ValidationError when the trip would grow past its maximum length.
func (r *DocumentRepository) AddDay(ctx context.Context, day domain.Day, position *int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, start, err := lockDocument(ctx, tx, day.DocumentID)
		if err != nil {
			return err
		}
		count, err := countDays(ctx, tx, document.ID)
		if err != nil {
			return err
		}
		if err := insertDayAt(ctx, tx, day, count, position); err != nil {
			return err
		}
		if err := resizeTrip(ctx, tx, document.TripID, start, count+1, true); err != nil {
			return err
		}
		if err := layoutDates(ctx, tx, document.TripID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// DuplicateDay - copies a day with its places right after it.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - sourceID: the day to copy.
//
// Returns:
//   - the identifier of the copy.
//   - domain.ErrNotFound when the day does not exist.
//   - a *domain.ValidationError when the trip would grow past its maximum length.
func (r *DocumentRepository) DuplicateDay(ctx context.Context, sourceID uuid.UUID) (uuid.UUID, error) {
	copyID := uuid.Must(uuid.NewV7())
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		source, err := oneRow(scanDay, tx.QueryRow(ctx, `SELECT `+dayColumns+` FROM days d WHERE d.id = $1`, sourceID), "get day")
		if err != nil {
			return err
		}
		document, start, err := lockDocument(ctx, tx, source.DocumentID)
		if err != nil {
			return err
		}
		count, err := countDays(ctx, tx, document.ID)
		if err != nil {
			return err
		}
		duplicate := source
		duplicate.ID = copyID
		position := source.Position + 1
		if err := insertDayAt(ctx, tx, duplicate, count, &position); err != nil {
			return err
		}
		// The copies are named up front, so the translations of each place can
		// follow it to its copy in the same statement.
		if _, err := tx.Exec(ctx,
			`WITH source AS (
			   SELECT id, gen_random_uuid() AS copy_id FROM items WHERE day_id = $1 AND kind <> 'stay_anchor'
			 ), copied AS (
			   INSERT INTO items (id, document_id, day_id, position, kind, name, category, activity_type, lat, lng,
			                      address, osm_ref,
			                      description_md, url, desired_time, visit_minutes, is_optional, booking_ref,
			                      planned_cost_amount, cost_per_person, cost_category, difficulty)
			   SELECT source.copy_id, document_id, $2, position, kind, name, category, activity_type, lat, lng,
			          address, osm_ref,
			          description_md, url, desired_time, visit_minutes, is_optional, booking_ref,
			          planned_cost_amount, cost_per_person, cost_category, difficulty
			   FROM items JOIN source ON source.id = items.id
			 )
			 INSERT INTO translations (trip_id, item_id, field, lang, value)
			 SELECT tr.trip_id, source.copy_id, tr.field, tr.lang, tr.value
			 FROM translations tr JOIN source ON source.id = tr.item_id
			 WHERE tr.field IN ('name', 'description_md')`, sourceID, copyID); err != nil {
			return fmt.Errorf("copy places: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO translations (trip_id, day_id, field, lang, value)
			 SELECT trip_id, $2, field, lang, value FROM translations WHERE day_id = $1`,
			sourceID, copyID); err != nil {
			return fmt.Errorf("copy day translations: %w", err)
		}
		if err := resizeTrip(ctx, tx, document.TripID, start, count+1, true); err != nil {
			return err
		}
		if err := layoutDates(ctx, tx, document.TripID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
	return copyID, err
}

// UpdateDay - stores a day's editable fields and refreshes its stay marks,
// which depend on the mark switches and the "no overnight" flag.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - day: the validated day.
//
// Returns:
//   - domain.ErrNotFound when the day does not exist.
func (r *DocumentRepository) UpdateDay(ctx context.Context, day domain.Day) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, day.DocumentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE days SET title = $2, notes_md = $3, start_time = $4::time, default_mode = $5, timezone = $6,
			                 morning_anchor = $7, evening_anchor = $8, no_overnight = $9, cover_media_id = $10,
			                 updated_at = now()
			 WHERE id = $1`,
			day.ID, day.Title, day.NotesMD, day.StartTime.String(), day.DefaultMode, day.Timezone,
			day.MorningAnchor, day.EveningAnchor, day.NoOvernight, day.CoverMediaID)
		if err != nil {
			return fmt.Errorf("update day: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// DeleteDay - removes a day.
//
// A plan day hands its places to the unassigned list. On a trip with dates the
// trip becomes one day shorter; the other document then loses its last day,
// which needs confirm when that day holds content. A document keeps at least
// one day.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - dayID: the day.
//   - confirm: whether removing content elsewhere was confirmed.
//
// Returns:
//   - domain.ErrNotFound when the day does not exist.
//   - a *domain.ValidationError for the last day of a document.
//   - a *domain.DaysWouldBeRemovedError when confirmation is needed.
func (r *DocumentRepository) DeleteDay(ctx context.Context, dayID uuid.UUID, confirm bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		day, err := oneRow(scanDay, tx.QueryRow(ctx, `SELECT `+dayColumns+` FROM days d WHERE d.id = $1`, dayID), "get day")
		if err != nil {
			return err
		}
		document, start, err := lockDocument(ctx, tx, day.DocumentID)
		if err != nil {
			return err
		}
		count, err := countDays(ctx, tx, document.ID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return domain.NewValidationError("day_id", "last_day", "a document keeps at least one day")
		}

		// Moving the day to the end and dropping the tail reuses the rule that
		// hands a plan day's places to the unassigned list.
		if _, err := tx.Exec(ctx,
			`UPDATE days SET position = CASE WHEN id = $2 THEN $3 - 1 ELSE position - 1 END
			 WHERE document_id = $1 AND (id = $2 OR position > $4)`,
			document.ID, dayID, count, day.Position); err != nil {
			return fmt.Errorf("move day to the end: %w", err)
		}
		if err := dropTailDays(ctx, tx, document.ID, document.Kind, count-1); err != nil {
			return err
		}
		if err := resizeTrip(ctx, tx, document.TripID, start, count-1, confirm); err != nil {
			return err
		}
		if err := layoutDates(ctx, tx, document.TripID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// ReorderDays - puts a document's days in a new order. Dates stay in calendar
// order: they follow positions, so the content moves between dates.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - documentID: the document.
//   - order: every day of the document, each once, in the new order.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
//   - a *domain.ValidationError when order is not exactly the document's days.
func (r *DocumentRepository) ReorderDays(ctx context.Context, documentID uuid.UUID, order []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, documentID)
		if err != nil {
			return err
		}
		var matching int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM days WHERE document_id = $1 AND id = ANY($2)`, documentID, order).Scan(&matching); err != nil {
			return fmt.Errorf("check day order: %w", err)
		}
		count, err := countDays(ctx, tx, documentID)
		if err != nil {
			return err
		}
		if matching != count || len(order) != count || hasDuplicates(order) {
			return domain.NewValidationError("day_ids", "order_mismatch", "must list every day of the document once")
		}
		if _, err := tx.Exec(ctx,
			`UPDATE days d SET position = o.ord - 1, updated_at = now()
			 FROM unnest($2::uuid[]) WITH ORDINALITY AS o(id, ord)
			 WHERE d.id = o.id AND d.document_id = $1 AND d.position <> o.ord - 1`, documentID, order); err != nil {
			return fmt.Errorf("reorder days: %w", err)
		}
		if err := layoutDates(ctx, tx, document.TripID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// hasDuplicates reports whether a list names an identifier twice.
func hasDuplicates(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// checkPlaceTarget verifies a day belongs to the document, and that only a
// plan uses the unassigned list.
func checkPlaceTarget(ctx context.Context, tx pgx.Tx, document domain.Document, dayID *uuid.UUID) error {
	if dayID == nil {
		if document.Kind != domain.DocumentPlan {
			return domain.NewValidationError("day_id", "unassigned_plan_only", "only a plan has unassigned places")
		}
		return nil
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT document_id FROM days WHERE id = $1`, *dayID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != document.ID) {
		return domain.NewValidationError("day_id", "unknown_day", "the day is not part of this document")
	}
	if err != nil {
		return fmt.Errorf("find day: %w", err)
	}
	return nil
}

// insertPlace writes a place at an already opened position.
func insertPlace(ctx context.Context, tx pgx.Tx, place domain.Item, position int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO items (id, document_id, day_id, position, kind, name, category, lat, lng, address, osm_ref,
		                    description_md, url, desired_time, visit_minutes, is_optional, booking_ref,
		                    planned_cost_amount, cost_per_person, cost_category,
		                    status, story_md, actual_time, rating, actual_cost_amount, source_item_id,
		                    activity_type, actual_end_time, difficulty)
		 VALUES ($1, $2, $3, $4, $26, $5, $6, $7, $8, $9, nullif($10, ''), $11, $12, $13::time, $14, $15, $16,
		         $17::numeric, $18, $19, $20, $21, $22::time, $23, $24::numeric, $25, nullif($27, ''),
		         $28::time, $29)`,
		place.ID, place.DocumentID, place.DayID, position, place.Name, place.Category, place.Lat, place.Lng,
		place.Address, place.OSMRef, place.DescriptionMD, place.URL, clockParam(place.DesiredTime),
		place.VisitMinutes, place.IsOptional, place.BookingRef, moneyParam(place.PlannedCost),
		place.CostPerPerson, place.CostCategory,
		place.Status, place.StoryMD, clockParam(place.ActualTime), place.Rating, moneyParam(place.ActualCost),
		place.SourceItemID, place.Kind, place.ActivityType, clockParam(place.ActualEndTime), place.Difficulty)
	if err != nil {
		return fmt.Errorf("add place: %w", err)
	}
	return nil
}

// CreatePlace - adds a place to a day or to the unassigned list.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - place: the validated place with its ID, document and day set.
//   - position: where in the list; nil or out of range appends.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
//   - a *domain.ValidationError when the day is not in the document, or the
//     unassigned list is used outside a plan.
func (r *DocumentRepository) CreatePlace(ctx context.Context, place domain.Item, position *int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, place.DocumentID)
		if err != nil {
			return err
		}
		if err := checkPlaceTarget(ctx, tx, document, place.DayID); err != nil {
			return err
		}
		slot, err := openSlot(ctx, tx, document.ID, place.DayID, position, place.ID)
		if err != nil {
			return err
		}
		if err := insertPlace(ctx, tx, place, slot); err != nil {
			return err
		}
		if err := orderByTime(ctx, tx, document, place.DayID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// orderByTime puts the places of a report's day in the order they were
// reached, as domain.OrderByActualTime decides. A plan is left alone: its times
// are wishes a schedule is worked out from, and its order is the route.
func orderByTime(ctx context.Context, tx pgx.Tx, document domain.Document, dayID *uuid.UUID) error {
	if document.Kind != domain.DocumentReport || dayID == nil {
		return nil
	}
	places, err := collect(ctx, tx, scanItem,
		`SELECT `+itemColumns+` FROM items i WHERE i.day_id = $1 AND i.kind <> 'stay_anchor'
		 ORDER BY i.position, i.created_at, i.id`, *dayID)
	if err != nil {
		return err
	}
	for position, id := range domain.OrderByActualTime(places) {
		if places[position].ID == id {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE items SET position = $2, updated_at = now() WHERE id = $1`,
			id, position); err != nil {
			return fmt.Errorf("order places by time: %w", err)
		}
	}
	return nil
}

// UpdatePlace - stores a place's editable fields. Its day and position change
// only through MovePlace. New coordinates send its legs back to pending.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - place: the validated place.
//
// Returns:
//   - domain.ErrNotFound when no such place exists.
func (r *DocumentRepository) UpdatePlace(ctx context.Context, place domain.Item) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, place.DocumentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE items SET name = $2, category = $3, lat = $4, lng = $5, address = $6, osm_ref = nullif($7, ''),
			                  description_md = $8, url = $9, desired_time = $10::time, visit_minutes = $11,
			                  is_optional = $12, booking_ref = $13, planned_cost_amount = $14::numeric,
			                  cost_per_person = $15, cost_category = $16, status = $17, story_md = $18,
			                  actual_time = $19::time, rating = $20, actual_cost_amount = $21::numeric,
			                  cover_media_id = $22, kind = $23, activity_type = nullif($24, ''),
			                  actual_end_time = $25::time, difficulty = $26, updated_at = now()
			 WHERE id = $1 AND kind <> 'stay_anchor'`,
			place.ID, place.Name, place.Category, place.Lat, place.Lng, place.Address, place.OSMRef,
			place.DescriptionMD, place.URL, clockParam(place.DesiredTime), place.VisitMinutes, place.IsOptional,
			place.BookingRef, moneyParam(place.PlannedCost), place.CostPerPerson, place.CostCategory,
			place.Status, place.StoryMD, clockParam(place.ActualTime), place.Rating,
			moneyParam(place.ActualCost), place.CoverMediaID, place.Kind, place.ActivityType,
			clockParam(place.ActualEndTime), place.Difficulty)
		if err != nil {
			return fmt.Errorf("update place: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if err := orderByTime(ctx, tx, document, place.DayID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// DeletePlace - removes a place and closes the gap it leaves.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - place: the place as read, which names its document and list.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) DeletePlace(ctx context.Context, place domain.Item) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, place.DocumentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1 AND kind <> 'stay_anchor'`, place.ID)
		if err != nil {
			return fmt.Errorf("delete place: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if err := renumberPlaces(ctx, tx, place.DocumentID, place.DayID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// MovePlace - moves a place within its list, to another day, or between a day
// and the unassigned list, in one transaction.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - documentID: the document the place must belong to.
//   - placeID: the place.
//   - dayID: the target day, or nil for the unassigned list.
//   - position: the position in the target list; out of range appends.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
//   - a *domain.ValidationError when the place or the day is not in the
//     document, or the unassigned list is used outside a plan.
func (r *DocumentRepository) MovePlace(ctx context.Context, documentID, placeID uuid.UUID, dayID *uuid.UUID, position int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, documentID)
		if err != nil {
			return err
		}
		place, err := scanItem(tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM items i WHERE i.id = $1`, placeID))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (place.DocumentID != documentID || !place.Kind.IsVisit())) {
			return domain.NewValidationError("item_id", "unknown_item", "the place is not part of this document")
		}
		if err != nil {
			return fmt.Errorf("find place: %w", err)
		}
		if err := checkPlaceTarget(ctx, tx, document, dayID); err != nil {
			return err
		}
		slot, err := openSlot(ctx, tx, documentID, dayID, &position, placeID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE items SET day_id = $2, position = $3, updated_at = now() WHERE id = $1`,
			placeID, dayID, slot); err != nil {
			return fmt.Errorf("move place: %w", err)
		}
		if err := renumberPlaces(ctx, tx, documentID, place.DayID); err != nil {
			return err
		}
		if err := renumberPlaces(ctx, tx, documentID, dayID); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// CopyPlace - copies a place into a day or the unassigned list.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - source: the place as read.
//   - copyID: the identifier of the copy.
//   - dayID: the target day, or nil for the unassigned list.
//   - position: where in the target list; nil or out of range appends.
//
// Returns:
//   - a *domain.ValidationError when the day is not in the document, or the
//     unassigned list is used outside a plan.
func (r *DocumentRepository) CopyPlace(ctx context.Context, source domain.Item, copyID uuid.UUID, dayID *uuid.UUID, position *int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, source.DocumentID)
		if err != nil {
			return err
		}
		if err := checkPlaceTarget(ctx, tx, document, dayID); err != nil {
			return err
		}
		slot, err := openSlot(ctx, tx, document.ID, dayID, position, copyID)
		if err != nil {
			return err
		}
		place := source
		place.ID = copyID
		place.DayID = dayID
		if err := insertPlace(ctx, tx, place, slot); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO translations (trip_id, item_id, field, lang, value)
			 SELECT trip_id, $2, field, lang, value FROM translations WHERE item_id = $1`,
			source.ID, copyID); err != nil {
			return fmt.Errorf("copy place translations: %w", err)
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// writeStay inserts or updates a stay and refreshes the trip's stay marks.
func (r *DocumentRepository) writeStay(ctx context.Context, stay domain.Stay, insert bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, stay.DocumentID)
		if err != nil {
			return err
		}
		args := []any{stay.ID, stay.DocumentID, stay.Name, stay.Kind, stay.Address, stay.Lat, stay.Lng,
			stay.CheckInDate, clockParam(stay.CheckInTime), stay.CheckOutDate, clockParam(stay.CheckOutTime),
			stay.BookingRef, stay.URL, stay.Contacts, stay.NotesMD, moneyParam(stay.PlannedCost),
			moneyParam(stay.ActualCost)}
		var tag pgconn.CommandTag
		if insert {
			tag, err = tx.Exec(ctx,
				`INSERT INTO stays (id, document_id, name, kind, address, lat, lng, check_in_date, check_in_time,
				                    check_out_date, check_out_time, booking_ref, url, contacts, notes_md,
				                    planned_cost_amount, actual_cost_amount)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::time, $10, $11::time, $12, $13, $14, $15, $16::numeric,
				         $17::numeric)`,
				args...)
		} else {
			tag, err = tx.Exec(ctx,
				`UPDATE stays SET name = $3, kind = $4, address = $5, lat = $6, lng = $7, check_in_date = $8,
				                  check_in_time = $9::time, check_out_date = $10, check_out_time = $11::time,
				                  booking_ref = $12, url = $13, contacts = $14, notes_md = $15,
				                  planned_cost_amount = $16::numeric, actual_cost_amount = $17::numeric,
				                  updated_at = now()
				 WHERE id = $1 AND document_id = $2`, args...)
		}
		if err != nil {
			return fmt.Errorf("write stay: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// CreateStay - adds a stay and places its marks in the days it covers.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stay: the validated stay with its ID and document set.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
func (r *DocumentRepository) CreateStay(ctx context.Context, stay domain.Stay) error {
	return r.writeStay(ctx, stay, true)
}

// UpdateStay - stores a stay's fields and moves its marks to match its dates.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stay: the validated stay.
//
// Returns:
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) UpdateStay(ctx context.Context, stay domain.Stay) error {
	return r.writeStay(ctx, stay, false)
}

// DeleteStay - removes a stay with its marks.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stay: the stay as read.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) DeleteStay(ctx context.Context, stay domain.Stay) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, stay.DocumentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM stays WHERE id = $1`, stay.ID)
		if err != nil {
			return fmt.Errorf("delete stay: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// Leg - reads one leg.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the leg.
//
// Returns:
//   - the leg.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Leg(ctx context.Context, id uuid.UUID) (domain.Leg, error) {
	return oneRow(scanLeg, r.pool.QueryRow(ctx, `SELECT `+legColumns+` FROM legs l WHERE l.id = $1`, id), "get leg")
}

// UpdateLeg - stores a leg's mode, typed values, cost and note. A new mode
// sends the leg back to pending.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - leg: the validated leg.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) UpdateLeg(ctx context.Context, leg domain.Leg) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, leg.DocumentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE legs SET mode = $2, manual_distance_m = $3, manual_duration_s = $4,
			                 planned_cost_amount = $5::numeric, actual_cost_amount = $6::numeric, note = $7,
			                 updated_at = now()
			 WHERE id = $1`,
			leg.ID, leg.Mode, leg.ManualDistanceM, leg.ManualDurationS, moneyParam(leg.PlannedCost),
			moneyParam(leg.ActualCost), leg.Note)
		if err != nil {
			return fmt.Errorf("update leg: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// SaveLegCalculation - stores a calculation, unless the leg changed while it ran.
//
// The calculation talks to the provider outside any transaction, so the leg may
// have moved or changed mode meanwhile; the result is stored only while the
// leg's input is still the one it was calculated for.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - legID: the leg.
//   - input: the input the calculation used.
//   - calculation: the result.
//   - at: when it was calculated.
//
// Returns:
//   - an error if the statement fails; a stale result is dropped silently.
func (r *DocumentRepository) SaveLegCalculation(ctx context.Context, legID uuid.UUID, input string,
	calculation domain.LegCalculation, at time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE legs SET distance_m = $3, duration_s = $4, geometry = nullif($5, ''), calc_source = $6,
		                 calc_error = nullif($7, ''), calculated_at = $8, updated_at = now()
		 WHERE id = $1 AND calc_input = $2`,
		legID, input, calculation.DistanceM, calculation.DurationS, calculation.Geometry, calculation.Source,
		calculation.Error, at)
	if err != nil {
		return fmt.Errorf("save leg calculation: %w", err)
	}
	return nil
}
