package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// segmentColumns is the select list of a leg's part, kept in step with scanSegment.
const segmentColumns = `g.id, g.leg_id, g.position, g.mode, g.distance_m, g.duration_s, coalesce(g.geometry, ''),
	g.calc_source, coalesce(g.calc_error, ''), g.calc_input, g.calculated_at, g.manual_distance_m,
	g.manual_duration_s, g.ticket_id, g.stop_name, g.stop_lat, g.stop_lng, g.wait_minutes`

// scanSegment reads one row in the order of segmentColumns.
func scanSegment(row pgx.Row) (domain.LegSegment, error) {
	var s domain.LegSegment
	err := row.Scan(&s.ID, &s.LegID, &s.Position, &s.Mode, &s.DistanceM, &s.DurationS, &s.Geometry,
		&s.Source, &s.Error, &s.Input, &s.CalculatedAt, &s.ManualDistanceM, &s.ManualDurationS, &s.TicketID,
		&s.StopName, &s.StopLat, &s.StopLng, &s.WaitMinutes)
	return s, err
}

// ticketColumns is the select list of a leg's ticket, kept in step with scanTicket.
const ticketColumns = `k.id, k.leg_id, k.position, k.name, (k.planned_cost_amount * 100)::bigint,
	(k.actual_cost_amount * 100)::bigint`

// scanTicket reads one row in the order of ticketColumns.
func scanTicket(row pgx.Row) (domain.LegTicket, error) {
	var t domain.LegTicket
	err := row.Scan(&t.ID, &t.LegID, &t.Position, &t.Name, &t.PlannedCost, &t.ActualCost)
	return t, err
}

// attachLegParts reads the parts and tickets of some legs, hangs them on their
// legs and folds every composite leg, so whatever reads the legs reads their
// totals. where selects the legs' identifiers, given the arguments.
func attachLegParts(ctx context.Context, q querier, legs []domain.Leg, where string, args ...any) error {
	if len(legs) == 0 {
		return nil
	}
	segments, err := collect(ctx, q, scanSegment,
		`SELECT `+segmentColumns+` FROM leg_segments g WHERE g.leg_id IN (`+where+`) ORDER BY g.leg_id, g.position`,
		args...)
	if err != nil {
		return err
	}
	tickets, err := collect(ctx, q, scanTicket,
		`SELECT `+ticketColumns+` FROM leg_tickets k WHERE k.leg_id IN (`+where+`) ORDER BY k.leg_id, k.position`,
		args...)
	if err != nil {
		return err
	}
	byLeg := make(map[uuid.UUID]int, len(legs))
	for index := range legs {
		byLeg[legs[index].ID] = index
	}
	for _, segment := range segments {
		if index, ok := byLeg[segment.LegID]; ok {
			legs[index].Segments = append(legs[index].Segments, segment)
		}
	}
	for _, ticket := range tickets {
		if index, ok := byLeg[ticket.LegID]; ok {
			legs[index].Tickets = append(legs[index].Tickets, ticket)
		}
	}
	for index := range legs {
		legs[index] = legs[index].Fold()
	}
	return nil
}

// SaveLegParts - replaces the parts and tickets of a leg.
//
// With more than one part the leg becomes composite: its parts and tickets are
// written as given - a part that kept its ends carries its calculation over -
// and the leg's own mode becomes the first part's, which is what a new leg
// after it takes. With one part or none it is a plain leg again: the parts go,
// and the leg takes the fields given on it. Either way the trip's legs are then
// reconciled, which sends every part and leg whose input changed back to
// pending.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - leg: the validated leg with its parts and tickets, or with its own fields
//     when it is plain again.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) SaveLegParts(ctx context.Context, leg domain.Leg) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, leg.DocumentID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM leg_segments WHERE leg_id = $1`, leg.ID); err != nil {
			return fmt.Errorf("clear leg parts: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM leg_tickets WHERE leg_id = $1`, leg.ID); err != nil {
			return fmt.Errorf("clear leg tickets: %w", err)
		}
		if !leg.Composite() {
			via, err := viaParam(leg.Via)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx,
				`UPDATE legs SET mode = $2, manual_distance_m = $3, manual_duration_s = $4,
				                 planned_cost_amount = $5::numeric, actual_cost_amount = $6::numeric,
				                 route_preference = $7, via = $8, updated_at = now()
				 WHERE id = $1`,
				leg.ID, leg.Mode, leg.ManualDistanceM, leg.ManualDurationS, moneyParam(leg.PlannedCost),
				moneyParam(leg.ActualCost), leg.Route().Preference, via)
			if err != nil {
				return fmt.Errorf("update leg: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return domain.ErrNotFound
			}
			return syncTrip(ctx, tx, document.TripID)
		}
		tag, err := tx.Exec(ctx, `UPDATE legs SET mode = $2, updated_at = now() WHERE id = $1`,
			leg.ID, leg.Segments[0].Mode)
		if err != nil {
			return fmt.Errorf("update leg: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if err := insertLegParts(ctx, tx, leg.ID, leg.Segments, leg.Tickets, nil); err != nil {
			return err
		}
		return syncTrip(ctx, tx, document.TripID)
	})
}

// insertLegParts writes a leg's tickets and then its parts, which name them.
// fresh gives the parts and tickets new identifiers - for a copy - when set;
// otherwise their own are kept.
func insertLegParts(ctx context.Context, tx pgx.Tx, legID uuid.UUID, segments []domain.LegSegment,
	tickets []domain.LegTicket, fresh func() uuid.UUID) error {
	ticketIDs := make(map[uuid.UUID]uuid.UUID, len(tickets))
	for _, ticket := range tickets {
		id := ticket.ID
		if fresh != nil {
			id = fresh()
		}
		ticketIDs[ticket.ID] = id
		if _, err := tx.Exec(ctx,
			`INSERT INTO leg_tickets (id, leg_id, position, name, planned_cost_amount, actual_cost_amount)
			 VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric)`,
			id, legID, ticket.Position, ticket.Name, moneyParam(ticket.PlannedCost), moneyParam(ticket.ActualCost)); err != nil {
			return fmt.Errorf("add leg ticket: %w", err)
		}
	}
	for _, segment := range segments {
		id := segment.ID
		if fresh != nil {
			id = fresh()
		}
		var ticketID *uuid.UUID
		if segment.TicketID != nil {
			if mapped, ok := ticketIDs[*segment.TicketID]; ok {
				ticketID = &mapped
			}
		}
		source := segment.Source
		if source == "" {
			source = domain.LegPending
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO leg_segments (id, leg_id, position, mode, distance_m, duration_s, geometry, calc_source,
			                           calc_error, calc_input, calculated_at, manual_distance_m, manual_duration_s,
			                           ticket_id, stop_name, stop_lat, stop_lng, wait_minutes)
			 VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8, nullif($9, ''), $10, $11, $12, $13, $14, $15, $16,
			         $17, $18)`,
			id, legID, segment.Position, segment.Mode, segment.DistanceM, segment.DurationS, segment.Geometry,
			source, segment.Error, segment.Input, segment.CalculatedAt, segment.ManualDistanceM,
			segment.ManualDurationS, ticketID, segment.StopName, segment.StopLat, segment.StopLng,
			segment.WaitMinutes); err != nil {
			return fmt.Errorf("add leg part: %w", err)
		}
	}
	return nil
}

// resetSegment sends a part whose ends moved back to pending with its new input.
func resetSegment(ctx context.Context, tx pgx.Tx, segment domain.LegSegment) error {
	if _, err := tx.Exec(ctx,
		`UPDATE leg_segments SET calc_input = $2, calc_source = 'pending', distance_m = NULL, duration_s = NULL,
		                         geometry = NULL, calc_error = NULL, calculated_at = NULL
		 WHERE id = $1`, segment.ID, segment.Input); err != nil {
		return fmt.Errorf("reset leg part: %w", err)
	}
	return nil
}

// SaveSegmentCalculation - stores a part's calculation, unless the part changed
// while it ran, exactly as SaveLegCalculation does for a leg.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - segmentID: the part.
//   - input: the input the calculation used.
//   - calculation: the result.
//   - at: when it was calculated.
//
// Returns:
//   - whether the result was stored; false when the part changed meanwhile.
//   - an error if the statement fails.
func (r *DocumentRepository) SaveSegmentCalculation(ctx context.Context, segmentID uuid.UUID, input string,
	calculation domain.LegCalculation, at time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE leg_segments SET distance_m = $3, duration_s = $4, geometry = nullif($5, ''), calc_source = $6,
		                         calc_error = nullif($7, ''), calculated_at = $8
		 WHERE id = $1 AND calc_input = $2`,
		segmentID, input, calculation.DistanceM, calculation.DurationS, calculation.Geometry, calculation.Source,
		calculation.Error, at)
	if err != nil {
		return false, fmt.Errorf("save leg part calculation: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UpdateLegNote - stores the note of a leg, which is all of a leg with changes
// that is kept on the leg itself.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - legID: the leg.
//   - note: the normalised note.
//
// Returns:
//   - domain.ErrNotFound when the leg does not exist.
func (r *DocumentRepository) UpdateLegNote(ctx context.Context, legID uuid.UUID, note string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE legs SET note = $2, updated_at = now() WHERE id = $1`, legID, note)
	if err != nil {
		return fmt.Errorf("update leg note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
