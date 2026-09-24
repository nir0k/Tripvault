package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// An expense touches nothing else in the document: it moves no stay mark and
// joins no two places, so it needs neither the trip lock nor a resynchronisation
// of the marks and legs. Only the day it hangs on has to belong to the same
// document, which is checked in the same transaction as the write.

var expenseColumns = `e.id, e.document_id, e.day_id, e.category, (e.planned_amount * 100)::bigint,
	(e.actual_amount * 100)::bigint, e.spent_on, e.note, e.source_expense_id, e.created_at, e.updated_at`

// scanExpense reads one row in the order of expenseColumns.
func scanExpense(row pgx.Row) (domain.Expense, error) {
	var e domain.Expense
	err := row.Scan(&e.ID, &e.DocumentID, &e.DayID, &e.Category, &e.Planned, &e.Actual, &e.SpentOn, &e.Note,
		&e.SourceExpenseID, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// checkExpenseDay refuses a day that is not part of the document the expense
// belongs to. A nil day means the expense belongs to the whole trip.
func checkExpenseDay(ctx context.Context, tx pgx.Tx, documentID uuid.UUID, dayID *uuid.UUID) error {
	if dayID == nil {
		return nil
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT document_id FROM days WHERE id = $1`, *dayID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != documentID) {
		return domain.NewValidationError("day_id", "unknown_day", "the day is not part of this document")
	}
	if err != nil {
		return fmt.Errorf("find day: %w", err)
	}
	return nil
}

// Expense - reads one expense.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the expense.
//
// Returns:
//   - the expense.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Expense(ctx context.Context, id uuid.UUID) (domain.Expense, error) {
	return oneRow(scanExpense, r.pool.QueryRow(ctx, `SELECT `+expenseColumns+` FROM expenses e WHERE e.id = $1`, id),
		"get expense")
}

// writeExpense inserts or updates an expense after checking its day.
func (r *DocumentRepository) writeExpense(ctx context.Context, expense domain.Expense, insert bool) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM documents WHERE id = $1`, expense.DocumentID).Scan(&exists); err != nil {
			_, err = one(exists, err, "find document")
			return err
		}
		if err := checkExpenseDay(ctx, tx, expense.DocumentID, expense.DayID); err != nil {
			return err
		}
		args := []any{expense.ID, expense.DocumentID, expense.DayID, expense.Category,
			moneyParam(expense.Planned), moneyParam(expense.Actual), expense.SpentOn, expense.Note}
		var sql string
		if insert {
			// The link to the plan's expense is written once and never changes, so
			// only the insert carries it; the update must not be handed an argument
			// its statement has no place for.
			args = append(args, expense.SourceExpenseID)
			sql = `INSERT INTO expenses (id, document_id, day_id, category, planned_amount, actual_amount,
			                             spent_on, note, source_expense_id)
			       VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8, $9)`
		} else {
			sql = `UPDATE expenses SET day_id = $3, category = $4, planned_amount = $5::numeric,
			                           actual_amount = $6::numeric, spent_on = $7, note = $8, updated_at = now()
			       WHERE id = $1 AND document_id = $2`
		}
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("write expense: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
}

// CreateExpense - adds an expense to a document, optionally tied to one day.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - expense: the validated expense with its ID and document set.
//
// Returns:
//   - domain.ErrNotFound when the document does not exist.
//   - a *domain.ValidationError when the day is not part of the document.
func (r *DocumentRepository) CreateExpense(ctx context.Context, expense domain.Expense) error {
	return r.writeExpense(ctx, expense, true)
}

// UpdateExpense - stores an expense's fields.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - expense: the validated expense.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
//   - a *domain.ValidationError when the day is not part of the document.
func (r *DocumentRepository) UpdateExpense(ctx context.Context, expense domain.Expense) error {
	return r.writeExpense(ctx, expense, false)
}

// DeleteExpense - removes an expense.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the expense.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *DocumentRepository) DeleteExpense(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete expense: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
