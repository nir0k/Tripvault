package httpapi

import (
	"net/http"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The budget is derived, never stored: it is assembled from the trip's document
// every time it is asked for, so a cost typed anywhere is in the totals at once.
//
// A plan's budget counts what is committed. A report's counts the same planned
// amounts - the snapshot it was copied with - and what was really spent beside
// them, read from the report alone, so the plan it came from may be deleted
// without moving a figure.

// budgetCategoryResponse is one row of the per-category breakdown.
type budgetCategoryResponse struct {
	Category string `json:"category"`
	Planned  string `json:"planned"`
	// Actual is what the category really cost; "0.00" in a plan.
	Actual string `json:"actual"`
}

// budgetDayResponse totals one day of the plan.
type budgetDayResponse struct {
	DayID    string  `json:"day_id"`
	Position int     `json:"position"`
	Date     *string `json:"date"`
	Planned  string  `json:"planned"`
	// Actual is what the day really cost; "0.00" in a plan.
	Actual string `json:"actual"`
}

// budgetEntryResponse is one cost of the plan, whatever carries it.
type budgetEntryResponse struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Category string  `json:"category"`
	DayID    *string `json:"day_id"`
	// Amount is the cost for the whole group; UnitAmount is what was typed.
	Amount     string `json:"amount"`
	UnitAmount string `json:"unit_amount"`
	PerPerson  bool   `json:"per_person"`
	// IsOptional marks a place its day may skip; it counts towards Planned all
	// the same.
	IsOptional bool `json:"is_optional"`
	Unassigned bool `json:"unassigned"`
	// ActualAmount is what was really spent for the whole group, in a report;
	// null in a plan and where nothing was entered.
	ActualAmount *string `json:"actual_amount"`
}

// budgetResponse is the money side of a trip's plan or report.
type budgetResponse struct {
	// Kind is the document the figures come from: plan or report.
	Kind string `json:"kind"`
	// DocumentID is the document the figures come from.
	DocumentID *string `json:"document_id"`
	Currency   string  `json:"currency"`
	Travelers  int     `json:"travelers"`
	// BudgetAmount is what the trip is willing to spend, null when unset.
	BudgetAmount *string `json:"budget_amount"`
	// Planned is everything the plan commits to; Unassigned, the places not on a
	// day yet, is kept out of it.
	Planned    string `json:"planned"`
	Unassigned string `json:"unassigned"`
	// Stays and Untied are the parts of Planned that belong to no single day.
	Stays  string `json:"stays"`
	Untied string `json:"untied"`
	// Actual is everything a report records as spent; "0.00" in a plan.
	Actual string `json:"actual"`
	// PerPerson divides the spending over the travellers: Planned in a plan,
	// Actual in a report.
	PerPerson string `json:"per_person"`
	// Remaining is the budget minus the spending, negative when overspent; null
	// without a budget.
	Remaining *string `json:"remaining"`
	// UsedPercent is the share of the budget the spending takes up, for a bar;
	// null without a budget, and above 100 when it does not fit.
	UsedPercent *int                     `json:"used_percent"`
	Categories  []budgetCategoryResponse `json:"categories"`
	Days        []budgetDayResponse      `json:"days"`
	Entries     []budgetEntryResponse    `json:"entries"`
}

// newBudgetResponse maps a budget onto the wire.
func newBudgetResponse(budget domain.Budget, documentID *string) budgetResponse {
	response := budgetResponse{
		Kind:         string(budget.Kind),
		DocumentID:   documentID,
		Currency:     budget.Currency,
		Travelers:    budget.Travelers,
		BudgetAmount: formatMoney(budget.Budget),
		Planned:      budget.Planned.String(),
		Unassigned:   budget.Unassigned.String(),
		Stays:        budget.Stays.String(),
		Untied:       budget.Untied.String(),
		Actual:       budget.Actual.String(),
		PerPerson:    budget.PerPerson.String(),
		Remaining:    formatMoney(budget.Remaining),
		UsedPercent:  budget.UsedPercent,
		Categories:   make([]budgetCategoryResponse, 0, len(budget.Categories)),
		Days:         make([]budgetDayResponse, 0, len(budget.Days)),
		Entries:      make([]budgetEntryResponse, 0, len(budget.Entries)),
	}
	for _, category := range budget.Categories {
		response.Categories = append(response.Categories, budgetCategoryResponse{
			Category: string(category.Category),
			Planned:  category.Planned.String(),
			Actual:   category.Actual.String(),
		})
	}
	for _, day := range budget.Days {
		response.Days = append(response.Days, budgetDayResponse{
			DayID:    day.DayID.String(),
			Position: day.Position,
			Date:     formatDate(day.Date),
			Planned:  day.Planned.String(),
			Actual:   day.Actual.String(),
		})
	}
	for _, entry := range budget.Entries {
		response.Entries = append(response.Entries, budgetEntryResponse{
			Kind:         string(entry.Kind),
			ID:           entry.ID.String(),
			Label:        entry.Label,
			Category:     string(entry.Category),
			DayID:        formatID(entry.DayID),
			Amount:       entry.Amount.String(),
			UnitAmount:   entry.Unit.String(),
			PerPerson:    entry.PerPerson,
			IsOptional:   entry.IsOptional,
			Unassigned:   entry.Unassigned,
			ActualAmount: formatMoney(entry.Actual),
		})
	}
	return response
}

// handleGetBudget returns the budget of a trip's plan or report: its totals,
// the breakdown by category and by day, and every individual cost.
func (s *Server) handleGetBudget(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	var content domain.DocumentContent
	documentID := trip.DocumentID()
	if documentID != nil {
		var err error
		if content, err = s.documents.Content(r.Context(), *documentID); err != nil {
			s.writeDomainError(w, r, "read document", err)
			return
		}
	}
	writeJSON(w, s.logger, http.StatusOK, newBudgetResponse(domain.BuildBudget(trip.Trip, content), formatID(documentID)))
}
