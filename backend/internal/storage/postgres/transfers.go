package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A transfer, like an expense, touches nothing else in the document: it places
// no mark in a day and joins no two places, so it needs neither the trip lock
// nor a resynchronisation of the marks and legs. The days show it by its dates,
// which is worked out on every read.

var transferColumns = `t.id, t.document_id, t.kind, t.name, t.from_name, t.from_address, t.from_lat, t.from_lng,
	t.to_name, t.to_address, t.to_lat, t.to_lng, t.departure_date, ` + clockColumn("t.departure_time") + `,
	t.arrival_date, ` + clockColumn("t.arrival_time") + `, t.booking_ref, t.url, t.notes_md,
	(t.planned_cost_amount * 100)::bigint, t.cost_per_person, (t.actual_cost_amount * 100)::bigint,
	t.source_transfer_id, t.created_at, t.updated_at`

// transferOrder is the order a document's transfers are read in: by departure,
// a transfer with no time last on its day.
const transferOrder = `t.departure_date, t.departure_time NULLS LAST, t.id`

// scanTransfer reads one row in the order of transferColumns.
func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(&t.ID, &t.DocumentID, &t.Kind, &t.Name, &t.FromName, &t.FromAddress, &t.FromLat, &t.FromLng,
		&t.ToName, &t.ToAddress, &t.ToLat, &t.ToLng, &t.DepartureDate, &t.DepartureTime,
		&t.ArrivalDate, &t.ArrivalTime, &t.BookingRef, &t.URL, &t.NotesMD,
		&t.PlannedCost, &t.CostPerPerson, &t.ActualCost, &t.SourceTransferID, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// Transfer - reads one transfer.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the transfer.
//
// Returns:
//   - the transfer.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Transfer(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return oneRow(scanTransfer, r.pool.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers t WHERE t.id = $1`, id),
		"get transfer")
}

// transferArgs lists a transfer's editable columns in the order both
// statements of writeTransfer number them.
func transferArgs(transfer domain.Transfer) []any {
	return []any{transfer.ID, transfer.DocumentID, transfer.Kind, transfer.Name,
		transfer.FromName, transfer.FromAddress, transfer.FromLat, transfer.FromLng,
		transfer.ToName, transfer.ToAddress, transfer.ToLat, transfer.ToLng,
		transfer.DepartureDate, clockParam(transfer.DepartureTime), transfer.ArrivalDate, clockParam(transfer.ArrivalTime),
		transfer.BookingRef, transfer.URL, transfer.NotesMD,
		moneyParam(transfer.PlannedCost), transfer.CostPerPerson, moneyParam(transfer.ActualCost)}
}

// CreateTransfer - adds a transfer to a document.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - transfer: the validated transfer with its ID and document set.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
func (r *DocumentRepository) CreateTransfer(ctx context.Context, transfer domain.Transfer) error {
	// Selecting from the document makes a missing one insert nothing, which
	// reads as not found rather than as a failed constraint.
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO transfers (id, document_id, kind, name, from_name, from_address, from_lat, from_lng,
		                        to_name, to_address, to_lat, to_lng, departure_date, departure_time,
		                        arrival_date, arrival_time, booking_ref, url, notes_md,
		                        planned_cost_amount, cost_per_person, actual_cost_amount)
		 SELECT $1, doc.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::time, $15, $16::time, $17, $18, $19,
		        $20::numeric, $21, $22::numeric
		 FROM documents doc WHERE doc.id = $2`,
		transferArgs(transfer)...)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// UpdateTransfer - stores a transfer's fields.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - transfer: the validated transfer.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) UpdateTransfer(ctx context.Context, transfer domain.Transfer) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE transfers SET kind = $3, name = $4, from_name = $5, from_address = $6, from_lat = $7, from_lng = $8,
		                      to_name = $9, to_address = $10, to_lat = $11, to_lng = $12, departure_date = $13,
		                      departure_time = $14::time, arrival_date = $15, arrival_time = $16::time,
		                      booking_ref = $17, url = $18, notes_md = $19, planned_cost_amount = $20::numeric,
		                      cost_per_person = $21, actual_cost_amount = $22::numeric, updated_at = now()
		 WHERE id = $1 AND document_id = $2`,
		transferArgs(transfer)...)
	if err != nil {
		return fmt.Errorf("update transfer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteTransfer - removes a transfer.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the transfer.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) DeleteTransfer(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM transfers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete transfer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// copyTransfers writes the report's transfers with their planned costs as a
// snapshot, each remembering the plan's transfer it came from.
func copyTransfers(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, transfers []domain.Transfer) error {
	for _, transfer := range transfers {
		if _, err := tx.Exec(ctx,
			`INSERT INTO transfers (id, document_id, kind, name, from_name, from_address, from_lat, from_lng,
			                        to_name, to_address, to_lat, to_lng, departure_date, departure_time,
			                        arrival_date, arrival_time, booking_ref, url, notes_md,
			                        planned_cost_amount, cost_per_person, source_transfer_id)
			 SELECT $1, $2, kind, name, from_name, from_address, from_lat, from_lng,
			        to_name, to_address, to_lat, to_lng, departure_date, departure_time,
			        arrival_date, arrival_time, booking_ref, url, notes_md,
			        planned_cost_amount, cost_per_person, id
			 FROM transfers WHERE id = $3`,
			uuid.Must(uuid.NewV7()), reportID, transfer.ID); err != nil {
			return fmt.Errorf("copy transfer: %w", err)
		}
	}
	return nil
}
