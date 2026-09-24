package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// An expense is the way a cost that belongs to nothing in particular - a city
// tax, a souvenir, a tank of fuel - reaches the budget. Like every other change
// to a document, writing one answers with the whole document, because a day's
// total moves with it.

// applyNullableDate applies an optional calendar date; null clears it.
func applyNullableDate(field string, change optional[string], target **time.Time) error {
	if !change.Set {
		return nil
	}
	*target = nil
	if change.Null {
		return nil
	}
	date, err := domain.ParseDate(field, change.Value)
	if err != nil {
		return err
	}
	*target = &date
	return nil
}

// applyNullableUUID applies an optional identifier; null clears it.
func applyNullableUUID(field string, change optional[string], target **uuid.UUID) error {
	if !change.Set {
		return nil
	}
	*target = nil
	if change.Null {
		return nil
	}
	id, err := uuid.Parse(change.Value)
	if err != nil {
		return domain.NewValidationError(field, "invalid_id", "must be an identifier")
	}
	*target = &id
	return nil
}

// expenseFields are the editable fields of an expense, shared by creation and
// change. A null day_id moves the expense to the trip as a whole.
type expenseFields struct {
	DayID         optional[string] `json:"day_id"`
	Category      optional[string] `json:"category"`
	PlannedAmount optional[string] `json:"planned_amount"`
	ActualAmount  optional[string] `json:"actual_amount"`
	SpentOn       optional[string] `json:"spent_on"`
	Note          optional[string] `json:"note"`
}

// apply writes the given fields onto an expense and validates the result.
func (f expenseFields) apply(expense domain.Expense, kind domain.DocumentKind) (domain.Expense, error) {
	if err := applyNullableUUID("day_id", f.DayID, &expense.DayID); err != nil {
		return expense, err
	}
	if f.Category.Set {
		expense.Category = domain.CostCategory(f.Category.Value)
	}
	if err := applyNullableMoney("planned_amount", f.PlannedAmount, &expense.Planned); err != nil {
		return expense, err
	}
	if err := applyNullableMoney("actual_amount", f.ActualAmount, &expense.Actual); err != nil {
		return expense, err
	}
	if err := applyNullableDate("spent_on", f.SpentOn, &expense.SpentOn); err != nil {
		return expense, err
	}
	if f.Note.Set {
		expense.Note = f.Note.Value
	}
	return expense.Normalize(kind)
}

// handleCreateExpense adds an expense to a document, tied to one day or to the
// whole trip.
func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	document, ok := s.documentFor(w, r, documentID, domain.ActionEdit)
	if !ok {
		return
	}
	var body expenseFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	expense, err := body.apply(domain.Expense{ID: uuid.Must(uuid.NewV7()), DocumentID: documentID}, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate expense", err)
		return
	}
	if err := s.documents.CreateExpense(r.Context(), expense); err != nil {
		s.writeDomainError(w, r, "create expense", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, documentID)
}

// expenseFor loads the expense named in the path and checks the role on its trip.
func (s *Server) expenseFor(w http.ResponseWriter, r *http.Request,
	action domain.TripAction) (domain.Expense, domain.Document, bool) {
	expenseID, ok := s.pathUUID(w, r, "expenseID")
	if !ok {
		return domain.Expense{}, domain.Document{}, false
	}
	expense, err := s.documents.Expense(r.Context(), expenseID)
	if err != nil {
		s.writeDomainError(w, r, "get expense", err)
		return domain.Expense{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, expense.DocumentID, action)
	return expense, document, ok
}

// handleUpdateExpense changes an expense.
func (s *Server) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	expense, document, ok := s.expenseFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body expenseFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := body.apply(expense, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate expense", err)
		return
	}
	if err := s.documents.UpdateExpense(r.Context(), updated); err != nil {
		s.writeDomainError(w, r, "update expense", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, expense.DocumentID)
}

// handleDeleteExpense removes an expense.
func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	expense, _, ok := s.expenseFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	if err := s.documents.DeleteExpense(r.Context(), expense.ID); err != nil {
		s.writeDomainError(w, r, "delete expense", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, expense.DocumentID)
}
