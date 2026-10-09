package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The stops along the lines of activities. A stop hangs on its activity's
// track - its foreign key names the track's activity - so removing the line, or
// the activity, removes its stops with it, while a line replaced by another
// import keeps them, to be moved onto the new line by MoveStops.

var stopColumns = `s.id, s.document_id, s.item_id, s.kind, s.name, s.note_md, s.lat, s.lng, s.distance_m,
	s.grades_to, ` + clockColumn("s.actual_time") + `, (s.planned_cost_amount * 100)::bigint,
	(s.actual_cost_amount * 100)::bigint, s.cost_per_person, s.cost_category, s.cost_note, s.paid_by, s.cost_split,
	s.created_at`

// stopOrder lists stops along their lines.
const stopOrder = `s.item_id, s.distance_m, s.id`

// scanStop reads one row in the order of stopColumns.
func scanStop(row pgx.Row) (domain.Stop, error) {
	var s domain.Stop
	err := row.Scan(&s.ID, &s.DocumentID, &s.ItemID, &s.Kind, &s.Name, &s.NoteMD, &s.Point.Lat, &s.Point.Lng,
		&s.DistanceM, &s.GradesTo, &s.ActualTime, &s.PlannedCost, &s.ActualCost, &s.CostPerPerson,
		&s.CostCategory, &s.CostNote, &s.PaidBy, &s.CostSplit, &s.CreatedAt)
	return s, err
}

// readStops reads the stops of a document with the members their costs are
// shared among, along their lines.
func readStops(ctx context.Context, q querier, documentID uuid.UUID) ([]domain.Stop, error) {
	stops, err := collect(ctx, q, scanStop,
		`SELECT `+stopColumns+` FROM activity_stops s WHERE s.document_id = $1 ORDER BY `+stopOrder, documentID)
	if err != nil {
		return nil, err
	}
	shares, err := readShares(ctx, q, stopShares, `s.stop_id IN (SELECT id FROM activity_stops WHERE document_id = $1)`,
		documentID)
	if err != nil {
		return nil, err
	}
	for index := range stops {
		stops[index].CostShares = shares[stops[index].ID]
	}
	return stops, nil
}

// attachStops hangs every stop on the track of its activity.
func attachStops(tracks []domain.Track, stops []domain.Stop) {
	for index := range tracks {
		for _, stop := range stops {
			if stop.ItemID == tracks[index].ItemID {
				tracks[index].Stops = append(tracks[index].Stops, stop)
			}
		}
	}
}

// Stop - reads one stop with the members its cost is shared among.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - id: the stop.
//
// Returns:
//   - the stop, which names the document to check access against.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Stop(ctx context.Context, id uuid.UUID) (domain.Stop, error) {
	stop, err := oneRow(scanStop,
		r.pool.QueryRow(ctx, `SELECT `+stopColumns+` FROM activity_stops s WHERE s.id = $1`, id), "get stop")
	if err != nil {
		return stop, err
	}
	shares, err := readShares(ctx, r.pool, stopShares, `s.stop_id = $1`, id)
	stop.CostShares = shares[id]
	return stop, err
}

// CreateStop - adds a stop to the line of an activity.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stop: the validated stop, placed on the line, with its ID, document and
//     activity set.
//
// Returns:
//   - domain.ErrNotFound when the activity has no line in the document.
//   - a *domain.ValidationError when the activity has as many stops as it may.
func (r *DocumentRepository) CreateStop(ctx context.Context, stop domain.Stop) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, _, err := lockDocument(ctx, tx, stop.DocumentID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM activity_stops WHERE item_id = $1`, stop.ItemID).
			Scan(&count); err != nil {
			return fmt.Errorf("count stops: %w", err)
		}
		if count >= domain.MaxActivityStops {
			return domain.NewValidationError("item_id", "too_many_stops",
				fmt.Sprintf("an activity holds at most %d stops", domain.MaxActivityStops))
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO activity_stops (id, document_id, item_id, kind, name, note_md, lat, lng, distance_m,
			                             grades_to, actual_time, planned_cost_amount, actual_cost_amount,
			                             cost_per_person, cost_category, cost_note, paid_by, cost_split)
			 SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::time, $12::numeric, $13::numeric, $14, $15, $16,
			        $17, $18
			 WHERE EXISTS (SELECT 1 FROM tracks WHERE item_id = $3 AND document_id = $2)`,
			stop.ID, stop.DocumentID, stop.ItemID, stop.Kind, stop.Name, stop.NoteMD, stop.Point.Lat, stop.Point.Lng,
			stop.DistanceM, stop.GradesTo, clockParam(stop.ActualTime), moneyParam(stop.PlannedCost),
			moneyParam(stop.ActualCost), stop.CostPerPerson, stop.CostCategory, stop.CostNote, stop.PaidBy,
			splitParam(stop.CostSplit))
		if err != nil {
			return fmt.Errorf("add stop: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return writeShares(ctx, tx, stopShares, stop.ID, stop.CostShares)
	})
}

// UpdateStop - stores a stop's fields, its place on the line included.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stop: the validated stop.
//
// Returns:
//   - domain.ErrNotFound when the stop does not exist.
func (r *DocumentRepository) UpdateStop(ctx context.Context, stop domain.Stop) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, _, err := lockDocument(ctx, tx, stop.DocumentID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE activity_stops SET kind = $2, name = $3, note_md = $4, lat = $5, lng = $6, distance_m = $7,
			                           grades_to = $8, actual_time = $9::time, planned_cost_amount = $10::numeric,
			                           actual_cost_amount = $11::numeric, cost_per_person = $12, cost_category = $13,
			                           cost_note = $14, paid_by = $15, cost_split = $16
			 WHERE id = $1`,
			stop.ID, stop.Kind, stop.Name, stop.NoteMD, stop.Point.Lat, stop.Point.Lng, stop.DistanceM, stop.GradesTo,
			clockParam(stop.ActualTime), moneyParam(stop.PlannedCost), moneyParam(stop.ActualCost),
			stop.CostPerPerson, stop.CostCategory, stop.CostNote, stop.PaidBy, splitParam(stop.CostSplit))
		if err != nil {
			return fmt.Errorf("update stop: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return writeShares(ctx, tx, stopShares, stop.ID, stop.CostShares)
	})
}

// MoveStops - puts stops at new places on their line, as a new import of the
// line asks: only their positions change.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - stops: the stops with their new point, distance and slopes.
//
// Returns:
//   - an error if a statement fails; a stop deleted meanwhile is no error.
func (r *DocumentRepository) MoveStops(ctx context.Context, stops []domain.Stop) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		for _, stop := range stops {
			if _, err := tx.Exec(ctx,
				`UPDATE activity_stops SET lat = $2, lng = $3, distance_m = $4, grades_to = $5 WHERE id = $1`,
				stop.ID, stop.Point.Lat, stop.Point.Lng, stop.DistanceM, stop.GradesTo); err != nil {
				return fmt.Errorf("move stop: %w", err)
			}
		}
		return nil
	})
}

// DeleteStop - removes a stop.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the stop.
//
// Returns:
//   - domain.ErrNotFound when the stop does not exist.
func (r *DocumentRepository) DeleteStop(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM activity_stops WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete stop: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// copyStops gives the report's activities the stops their plan's lines had,
// planned costs as a snapshot and without who pays or shares them, whose
// members stay with the plan. A stop of an activity that was not copied stays
// behind.
func copyStops(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, tracks []domain.Track,
	places map[uuid.UUID]uuid.UUID) error {
	for _, track := range tracks {
		itemID, ok := places[track.ItemID]
		if !ok {
			continue
		}
		for _, stop := range track.Stops {
			copied := stop.ForReport(uuid.Must(uuid.NewV7()), reportID, itemID)
			if _, err := tx.Exec(ctx,
				`INSERT INTO activity_stops (id, document_id, item_id, kind, name, note_md, lat, lng, distance_m,
				                             grades_to, planned_cost_amount, cost_per_person, cost_category,
				                             cost_note)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12, $13, $14)`,
				copied.ID, copied.DocumentID, copied.ItemID, copied.Kind, copied.Name, copied.NoteMD,
				copied.Point.Lat, copied.Point.Lng, copied.DistanceM, copied.GradesTo, moneyParam(copied.PlannedCost),
				copied.CostPerPerson, copied.CostCategory, copied.CostNote); err != nil {
				return fmt.Errorf("copy stop: %w", err)
			}
		}
	}
	return nil
}
