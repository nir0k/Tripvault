package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A report made from a plan is a snapshot, not a view: every row is copied, and
// each copy remembers the row it came from. From then on the two are
// independent - they are separate trips - so editing or deleting the plan can
// never move the figures a finished report compares against.
//
// The copy runs in Go rather than in one large statement because every table
// needs the identifiers minted for the previous one - days for places, places for
// legs - and a map in memory says that far more plainly than nested CTEs would.
// It happens once per report, so the row-by-row inserts cost nothing that
// matters.
//
// Two things are deliberately left out. The unassigned places do not come along:
// a report describes what was actually done, and those were never put in a day.
// The actual amounts are not filled either; they start empty and are entered as
// the money is spent.

// CreateReport - writes a new report trip holding a copy of a plan.
//
// The report is a trip of its own: its row is written from the fields the
// caller copied off the plan's trip, and its document from the plan's content.
// Nothing else of the plan comes along - not its people, its links or its
// pictures - because the report is written by whoever travelled, and they
// choose who reads it.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - report: the validated report trip, with its ID, owner and source set.
//   - planID: the plan document to copy.
//
// Returns:
//   - domain.ErrNotFound when the plan does not exist.
//   - an error if a statement fails.
func (r *DocumentRepository) CreateReport(ctx context.Context, report domain.Trip, planID uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		plan, err := readContent(ctx, tx, planID)
		if err != nil {
			return err
		}
		if err := insertTrip(ctx, tx, report); err != nil {
			return err
		}
		_, err = copyPlan(ctx, tx, report.ID, plan)
		return err
	})
}

// copyPlan writes a report holding a copy of the plan and returns its identifier.
func copyPlan(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, plan domain.DocumentContent) (uuid.UUID, error) {
	reportID := uuid.Must(uuid.NewV7())
	_, err := tx.Exec(ctx,
		`INSERT INTO documents (id, trip_id, kind, source_document_id, intro_md, summary_md)
		 VALUES ($1, $2, 'report', $3, '', '')`, reportID, tripID, plan.Document.ID)
	if isUniqueViolation(err) {
		return uuid.Nil, domain.ErrAlreadyExists
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("create report: %w", err)
	}

	days, err := copyDays(ctx, tx, reportID, plan.Days)
	if err != nil {
		return uuid.Nil, err
	}
	if err := copyStays(ctx, tx, reportID, plan.Stays); err != nil {
		return uuid.Nil, err
	}
	places, err := copyPlaces(ctx, tx, reportID, plan.Items, days)
	if err != nil {
		return uuid.Nil, err
	}
	if err := copyExpenses(ctx, tx, reportID, plan.Expenses, days); err != nil {
		return uuid.Nil, err
	}

	// The stay marks are derived, so they are placed by the same code that
	// maintains them everywhere else; the legs then join the elements they frame.
	if err := syncAnchors(ctx, tx, tripID); err != nil {
		return uuid.Nil, err
	}
	anchors, err := anchorMap(ctx, tx, plan, reportID, days)
	if err != nil {
		return uuid.Nil, err
	}
	for id, copied := range anchors {
		places[id] = copied
	}
	if err := copyLegs(ctx, tx, reportID, plan.Legs, days, places); err != nil {
		return uuid.Nil, err
	}

	// A safety net rather than the mechanism: a copied leg keeps its calculation,
	// and this only fills a pair the copy could not match.
	return reportID, syncLegs(ctx, tx, tripID)
}

// copyDays writes the report's days and returns the plan day each one came from.
func copyDays(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, days []domain.Day) (map[uuid.UUID]uuid.UUID, error) {
	copies := make(map[uuid.UUID]uuid.UUID, len(days))
	for _, day := range days {
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx,
			`INSERT INTO days (id, document_id, position, date, title, notes_md, start_time, default_mode, timezone,
			                   morning_anchor, evening_anchor, no_overnight, source_day_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7::time, $8, $9, $10, $11, $12, $13)`,
			id, reportID, day.Position, day.Date, day.Title, day.NotesMD, day.StartTime.String(),
			day.DefaultMode, day.Timezone, day.MorningAnchor, day.EveningAnchor, day.NoOvernight, day.ID); err != nil {
			return nil, fmt.Errorf("copy day: %w", err)
		}
		copies[day.ID] = id
	}
	return copies, nil
}

// copyStays writes the report's stays with their planned costs as a snapshot.
//
// No mapping comes back: the stay marks are not copied but placed afresh by
// syncAnchors, which matches the report's own stays to its own days by date.
func copyStays(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, stays []domain.Stay) error {
	for _, stay := range stays {
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx,
			`INSERT INTO stays (id, document_id, name, kind, address, lat, lng, check_in_date, check_in_time,
			                    check_out_date, check_out_time, booking_ref, url, contacts, notes_md,
			                    planned_cost_amount, source_stay_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::time, $10, $11::time, $12, $13, $14, $15, $16::numeric, $17)`,
			id, reportID, stay.Name, stay.Kind, stay.Address, stay.Lat, stay.Lng,
			stay.CheckInDate, clockParam(stay.CheckInTime), stay.CheckOutDate, clockParam(stay.CheckOutTime),
			stay.BookingRef, stay.URL, stay.Contacts, stay.NotesMD, moneyParam(stay.PlannedCost),
			stay.ID); err != nil {
			return fmt.Errorf("copy stay: %w", err)
		}
	}
	return nil
}

// copyPlaces writes the report's places. A place with no day is a plan's
// unassigned idea and is not copied; stay marks are derived and are not either.
func copyPlaces(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, items []domain.Item,
	days map[uuid.UUID]uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	copies := make(map[uuid.UUID]uuid.UUID, len(items))
	for _, item := range items {
		if !item.Kind.IsVisit() || item.DayID == nil {
			continue
		}
		dayID, ok := days[*item.DayID]
		if !ok {
			continue
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx,
			`INSERT INTO items (id, document_id, day_id, position, kind, name, category, lat, lng, address, osm_ref,
			                    description_md, url, desired_time, visit_minutes, is_optional, booking_ref,
			                    planned_cost_amount, cost_per_person, cost_category, status, source_item_id,
			                    activity_type, difficulty)
			 VALUES ($1, $2, $3, $4, $21, $5, $6, $7, $8, $9, nullif($10, ''), $11, $12, $13::time, $14, $15, $16,
			         $17::numeric, $18, $19, 'visited', $20, nullif($22, ''), $23)`,
			id, reportID, dayID, item.Position, item.Name, item.Category, item.Lat, item.Lng, item.Address,
			item.OSMRef, item.DescriptionMD, item.URL, clockParam(item.DesiredTime), item.VisitMinutes,
			item.IsOptional, item.BookingRef, moneyParam(item.PlannedCost), item.CostPerPerson, item.CostCategory,
			item.ID, item.Kind, item.ActivityType, item.Difficulty); err != nil {
			return nil, fmt.Errorf("copy place: %w", err)
		}
		copies[item.ID] = id
	}
	return copies, nil
}

// copyExpenses writes the report's separate expenses with their planned amounts.
func copyExpenses(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, expenses []domain.Expense,
	days map[uuid.UUID]uuid.UUID) error {
	for _, expense := range expenses {
		var dayID *uuid.UUID
		if expense.DayID != nil {
			copied, ok := days[*expense.DayID]
			if !ok {
				continue
			}
			dayID = &copied
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO expenses (id, document_id, day_id, category, planned_amount, note, source_expense_id)
			 VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)`,
			uuid.Must(uuid.NewV7()), reportID, dayID, expense.Category, moneyParam(expense.Planned),
			expense.Note, expense.ID); err != nil {
			return fmt.Errorf("copy expense: %w", err)
		}
	}
	return nil
}

// anchorMap pairs each stay mark of the plan with the report's mark in the same
// slot of the copied day, so a leg that starts or ends at a mark can be copied.
func anchorMap(ctx context.Context, tx pgx.Tx, plan domain.DocumentContent, reportID uuid.UUID,
	days map[uuid.UUID]uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	report, err := readContent(ctx, tx, reportID)
	if err != nil {
		return nil, err
	}
	bySlot := make(map[domain.AnchorKey]uuid.UUID, len(report.Items))
	for _, item := range report.Items {
		if item.Kind == domain.ItemStayAnchor && item.DayID != nil {
			bySlot[domain.AnchorKey{DayID: *item.DayID, Slot: item.Anchor}] = item.ID
		}
	}

	copies := make(map[uuid.UUID]uuid.UUID, len(bySlot))
	for _, item := range plan.Items {
		if item.Kind != domain.ItemStayAnchor || item.DayID == nil {
			continue
		}
		dayID, ok := days[*item.DayID]
		if !ok {
			continue
		}
		if copied, ok := bySlot[domain.AnchorKey{DayID: dayID, Slot: item.Anchor}]; ok {
			copies[item.ID] = copied
		}
	}
	return copies, nil
}

// copyLegs writes the report's legs, carrying the plan's calculation over so the
// report opens with the distances and times the plan had rather than waiting for
// the routing provider again. A leg whose endpoints were not copied is skipped;
// syncLegs then creates whatever the elements need.
func copyLegs(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, legs []domain.Leg,
	days, items map[uuid.UUID]uuid.UUID) error {
	for _, leg := range legs {
		dayID, hasDay := days[leg.DayID]
		from, hasFrom := items[leg.FromItemID]
		to, hasTo := items[leg.ToItemID]
		if !hasDay || !hasFrom || !hasTo {
			continue
		}
		var geometry *string
		if leg.Geometry != "" {
			geometry = &leg.Geometry
		}
		var calcError *string
		if leg.Error != "" {
			calcError = &leg.Error
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO legs (id, document_id, day_id, from_item_id, to_item_id, mode, distance_m, duration_s,
			                   geometry, calc_source, calc_error, calc_input, calculated_at,
			                   manual_distance_m, manual_duration_s, planned_cost_amount)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::numeric)`,
			uuid.Must(uuid.NewV7()), reportID, dayID, from, to, leg.Mode, leg.DistanceM, leg.DurationS,
			geometry, leg.Source, calcError, leg.Input, leg.CalculatedAt,
			leg.ManualDistanceM, leg.ManualDurationS, moneyParam(leg.PlannedCost)); err != nil {
			return fmt.Errorf("copy leg: %w", err)
		}
	}
	return nil
}

// UpdateDocument - stores a document's introduction and closing words.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the document.
//   - intro: the opening text, in Markdown.
//   - summary: the closing text, in Markdown.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
func (r *DocumentRepository) UpdateDocument(ctx context.Context, id uuid.UUID, intro, summary string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE documents SET intro_md = $2, summary_md = $3, updated_at = now() WHERE id = $1`, id, intro, summary)
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
