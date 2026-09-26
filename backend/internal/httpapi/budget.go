package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

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

// balanceResponse is where one member stands across the split costs.
type balanceResponse struct {
	UserID string `json:"user_id"`
	// Name is the member's display name; empty for an account that is gone.
	Name  string `json:"name"`
	Paid  string `json:"paid"`
	Share string `json:"share"`
	// Net is what the others owe the member, negative when the member owes.
	Net string `json:"net"`
}

// settlementResponse is one payment that squares the members' accounts.
type settlementResponse struct {
	FromUserID string `json:"from_user_id"`
	FromName   string `json:"from_name"`
	ToUserID   string `json:"to_user_id"`
	ToName     string `json:"to_name"`
	Amount     string `json:"amount"`
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
	// Stays, Transfers and Untied are the parts of Planned that belong to no
	// single day.
	Stays     string `json:"stays"`
	Transfers string `json:"transfers"`
	Untied    string `json:"untied"`
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
	// Balances and Settlements say who owes whom for the split costs; empty
	// while nothing is split.
	Balances    []balanceResponse    `json:"balances"`
	Settlements []settlementResponse `json:"settlements"`
}

// newBudgetResponse maps a budget onto the wire, naming the members of its
// balances by names.
func newBudgetResponse(budget domain.Budget, documentID *string, names map[uuid.UUID]string) budgetResponse {
	response := budgetResponse{
		Kind:         string(budget.Kind),
		DocumentID:   documentID,
		Currency:     budget.Currency,
		Travelers:    budget.Travelers,
		BudgetAmount: formatMoney(budget.Budget),
		Planned:      budget.Planned.String(),
		Unassigned:   budget.Unassigned.String(),
		Stays:        budget.Stays.String(),
		Transfers:    budget.Transfers.String(),
		Untied:       budget.Untied.String(),
		Actual:       budget.Actual.String(),
		PerPerson:    budget.PerPerson.String(),
		Remaining:    formatMoney(budget.Remaining),
		UsedPercent:  budget.UsedPercent,
		Categories:   make([]budgetCategoryResponse, 0, len(budget.Categories)),
		Days:         make([]budgetDayResponse, 0, len(budget.Days)),
		Entries:      make([]budgetEntryResponse, 0, len(budget.Entries)),
		Balances:     make([]balanceResponse, 0, len(budget.Balances)),
		Settlements:  make([]settlementResponse, 0, len(budget.Settlements)),
	}
	for _, balance := range budget.Balances {
		response.Balances = append(response.Balances, balanceResponse{
			UserID: balance.UserID.String(),
			Name:   names[balance.UserID],
			Paid:   balance.Paid.String(),
			Share:  balance.Share.String(),
			Net:    balance.Net().String(),
		})
	}
	for _, settlement := range budget.Settlements {
		response.Settlements = append(response.Settlements, settlementResponse{
			FromUserID: settlement.From.String(),
			FromName:   names[settlement.From],
			ToUserID:   settlement.To.String(),
			ToName:     names[settlement.To],
			Amount:     settlement.Amount.String(),
		})
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
	budget := domain.BuildBudget(trip.Trip, content)
	names, err := s.balanceNames(r.Context(), trip.ID, budget.Balances)
	if err != nil {
		s.writeDomainError(w, r, "name members", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newBudgetResponse(budget, formatID(documentID), names))
}

// balanceNames names the members of a budget's balances: the trip's members
// by their display names, and anybody who has left it by their account's, as
// long as it is still there.
func (s *Server) balanceNames(ctx context.Context, tripID uuid.UUID, balances []domain.MemberBalance) (map[uuid.UUID]string, error) {
	names := make(map[uuid.UUID]string, len(balances))
	if len(balances) == 0 {
		return names, nil
	}
	members, err := s.trips.Members(ctx, tripID)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		names[member.User.ID] = member.User.DisplayName
	}
	for _, balance := range balances {
		if _, ok := names[balance.UserID]; ok {
			continue
		}
		user, err := s.users.GetByID(ctx, balance.UserID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			names[balance.UserID] = ""
		case err != nil:
			return nil, err
		default:
			names[balance.UserID] = user.DisplayName
		}
	}
	return names, nil
}
