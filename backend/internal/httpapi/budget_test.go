package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// decodeBudget reads a budget response, failing the test on anything else.
func decodeBudget(t *testing.T, body []byte) budgetResponse {
	t.Helper()
	var budget budgetResponse
	if err := json.Unmarshal(body, &budget); err != nil {
		t.Fatalf("response %q is not a budget: %v", body, err)
	}
	return budget
}

// TestBudgetIsReadableByEveryRole checks the budget is part of what a viewer may
// see; nothing on the screen writes to it, so there is no role to refuse.
func TestBudgetIsReadableByEveryRole(t *testing.T) {
	for _, role := range []domain.TripRole{domain.RoleViewer, domain.RoleEditor, domain.RoleOwner} {
		s, trips := newTripServer(role)
		recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/budget", "good", "")
		if recorder.Code != http.StatusOK {
			t.Errorf("%s reads the budget: %d %s", role, recorder.Code, recorder.Body.String())
		}
		budget := decodeBudget(t, recorder.Body.Bytes())
		// The fake trip has no plan, so only its own budget is known.
		if budget.DocumentID != nil || budget.Planned != "0.00" || budget.BudgetAmount == nil {
			t.Errorf("%s: %+v", role, budget)
		}
		if len(budget.Categories) != len(domain.CostCategories) {
			t.Errorf("%d category rows, want every one", len(budget.Categories))
		}
	}
}

// TestBudgetReadsThePlan checks a cost typed in the plan reaches the budget,
// each kind of cost in its own place.
func TestBudgetReadsThePlan(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	trip := docs.document.TripID
	cost := domain.Money(2500)
	docs.place.PlannedCost = &cost
	docs.place.CostPerPerson = true
	docs.place.CostCategory = domain.CostActivities
	docs.stay.PlannedCost = &cost

	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trip.String()+"/budget", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("read the budget: %d %s", recorder.Code, recorder.Body.String())
	}
	budget := decodeBudget(t, recorder.Body.Bytes())
	// The place costs 25.00 each for two travellers, the stay 25.00 once.
	if budget.Planned != "75.00" || budget.Stays != "25.00" || budget.PerPerson != "37.50" {
		t.Errorf("planned %s, stays %s, per person %s", budget.Planned, budget.Stays, budget.PerPerson)
	}
	if budget.Remaining == nil || *budget.Remaining != "2925.00" {
		t.Errorf("remaining %v of a 3000.00 budget", budget.Remaining)
	}
	if len(budget.Entries) != 2 {
		t.Fatalf("%d entries, want the place and the stay: %+v", len(budget.Entries), budget.Entries)
	}
	if budget.Entries[0].Label != "Seljalandsfoss" || budget.Entries[0].Amount != "50.00" ||
		!budget.Entries[0].PerPerson {
		t.Errorf("the per-person place is listed as %+v", budget.Entries[0])
	}
}

// TestExpenseLifecycle checks an expense is created, changed and deleted, and
// that it lands in the day it names.
func TestExpenseLifecycle(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	base := "/api/v1/documents/" + docs.document.ID.String()

	recorder := send(s, http.MethodPost, base+"/expenses", "good",
		`{"day_id":"`+docs.day.ID.String()+`","category":"food","planned_amount":"32.50","note":"Dinner"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create the expense: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.expense == nil || docs.expense.Planned == nil || docs.expense.Planned.String() != "32.50" ||
		docs.expense.DayID == nil || *docs.expense.DayID != docs.day.ID {
		t.Fatalf("the stored expense is %+v", docs.expense)
	}
	// The day's own total carries it, so the plan and the budget agree.
	var document documentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("response is not a document: %v", err)
	}
	if len(document.Expenses) != 1 || document.Days[0].Summary.PlannedCost != "32.50" {
		t.Errorf("the day totals %s with %d expenses", document.Days[0].Summary.PlannedCost, len(document.Expenses))
	}

	path := "/api/v1/expenses/" + docs.expense.ID.String()
	recorder = send(s, http.MethodPatch, path, "good", `{"day_id":null,"planned_amount":"40"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("change the expense: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.expense.DayID != nil || docs.expense.Planned.String() != "40.00" || docs.expense.Note != "Dinner" {
		t.Errorf("the changed expense is %+v", docs.expense)
	}

	if recorder := send(s, http.MethodDelete, path, "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("delete the expense: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.expense != nil {
		t.Error("the expense survived its deletion")
	}
}

// TestExpenseValidation checks an expense needs an amount and a known category.
func TestExpenseValidation(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	base := "/api/v1/documents/" + docs.document.ID.String() + "/expenses"

	for _, body := range []string{
		`{"note":"Nothing in particular"}`,
		`{"category":"flights","planned_amount":"10"}`,
		`{"planned_amount":"10","day_id":"not-an-id"}`,
	} {
		recorder := send(s, http.MethodPost, base, "good", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if docs.expense != nil {
		t.Error("an invalid expense was stored")
	}
}

// TestExpenseRolesAreEnforced checks only editors write expenses.
func TestExpenseRolesAreEnforced(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleViewer)
	recorder := send(s, http.MethodPost, "/api/v1/documents/"+docs.document.ID.String()+"/expenses", "good",
		`{"planned_amount":"10"}`)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer added an expense: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.expense != nil {
		t.Error("a viewer's expense was stored")
	}
}
