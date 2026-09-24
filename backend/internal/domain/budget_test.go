package domain

import (
	"testing"

	"github.com/google/uuid"
)

// money turns an amount in whole units into the stored hundredths.
func money(units int64) *Money {
	amount := Money(units * 100)
	return &amount
}

// budgetPlan builds a two-day plan carrying one cost of every kind: a
// per-person place, an optional place, a place with no day, a stay, a leg and
// two expenses, one on a day and one on the trip.
func budgetPlan(t *testing.T) (Trip, DocumentContent) {
	t.Helper()
	days := planDays(t, 2)
	hotel := stay(t, "2026-06-20", "2026-06-22")
	hotel.PlannedCost = money(240)

	museum := Item{ID: uuid.New(), DayID: &days[0].ID, Kind: ItemPlace, Name: "Museum",
		Category: CategoryMuseum, CostCategory: CostActivities, PlannedCost: money(15), CostPerPerson: true}
	cave := Item{ID: uuid.New(), DayID: &days[1].ID, Kind: ItemPlace, Name: "Cave",
		Category: CategoryNature, CostCategory: CostActivities, PlannedCost: money(50), IsOptional: true}
	maybe := Item{ID: uuid.New(), Kind: ItemPlace, Name: "Lagoon",
		Category: CategoryActivity, CostCategory: CostActivities, PlannedCost: money(80)}
	anchor := Item{ID: uuid.New(), DayID: &days[0].ID, Kind: ItemStayAnchor, Anchor: AnchorEvening, StayID: &hotel.ID}

	drive := Leg{ID: uuid.New(), DayID: days[0].ID, FromItemID: museum.ID, ToItemID: anchor.ID,
		Mode: ModeCar, PlannedCost: money(30)}

	tax := Expense{ID: uuid.New(), DayID: &days[0].ID, Category: CostOther, Planned: money(4), Note: "City tax"}
	fuel := Expense{ID: uuid.New(), Category: CostTransport, Planned: money(90), Note: "Fuel"}

	trip := Trip{Kind: DocumentPlan, Currency: "EUR", Travelers: 2, Budget: money(600)}
	content := DocumentContent{
		Days:     days,
		Items:    []Item{museum, anchor, cave, maybe},
		Stays:    []Stay{hotel},
		Legs:     []Leg{drive},
		Expenses: []Expense{tax, fuel},
	}
	return trip, content
}

// TestBuildBudgetTotals checks that the committed total adds up from the days,
// the stays and the trip's own expenses, with the optional place counted in and
// only the unassigned one left out.
func TestBuildBudgetTotals(t *testing.T) {
	trip, content := budgetPlan(t)
	budget := BuildBudget(trip, content)

	// 15 x 2 travellers + 30 drive + 4 tax + 240 stay + 90 fuel + 50 optional cave.
	if want := *money(444); budget.Planned != want {
		t.Errorf("planned %s, want %s", budget.Planned, want)
	}
	if want := *money(80); budget.Unassigned != want {
		t.Errorf("unassigned %s, want %s", budget.Unassigned, want)
	}
	if want := *money(240); budget.Stays != want {
		t.Errorf("stays %s, want %s", budget.Stays, want)
	}
	if want := *money(90); budget.Untied != want {
		t.Errorf("untied %s, want %s", budget.Untied, want)
	}
	if want := *money(222); budget.PerPerson != want {
		t.Errorf("per person %s, want %s", budget.PerPerson, want)
	}
	if budget.Remaining == nil || *budget.Remaining != *money(156) {
		t.Errorf("remaining %v, want 156.00", budget.Remaining)
	}
	// 444 of a 600 budget, rounded to whole percent.
	if budget.UsedPercent == nil || *budget.UsedPercent != 74 {
		t.Errorf("used %v%% of the budget, want 74", budget.UsedPercent)
	}
	if sum := budget.Days[0].Planned + budget.Days[1].Planned + budget.Stays + budget.Untied; sum != budget.Planned {
		t.Errorf("the days, the stays and the untied expenses add up to %s, not to %s", sum, budget.Planned)
	}
}

// TestBuildBudgetBreakdown checks the per-category and per-day rows.
func TestBuildBudgetBreakdown(t *testing.T) {
	trip, content := budgetPlan(t)
	budget := BuildBudget(trip, content)

	if len(budget.Categories) != len(CostCategories) {
		t.Fatalf("%d categories, want every one of the %d", len(budget.Categories), len(CostCategories))
	}
	rows := map[CostCategory]BudgetCategory{}
	for _, row := range budget.Categories {
		rows[row.Category] = row
	}
	if accommodation := rows[CostAccommodation]; accommodation.Planned != *money(240) {
		t.Errorf("accommodation %+v", accommodation)
	}
	// The drive and the fuel.
	if transport := rows[CostTransport]; transport.Planned != *money(120) {
		t.Errorf("transport %+v", transport)
	}
	// The museum and the optional cave count; only the lagoon, which has no day,
	// stays out.
	if activities := rows[CostActivities]; activities.Planned != *money(80) {
		t.Errorf("activities %+v", activities)
	}

	if len(budget.Days) != 2 {
		t.Fatalf("%d days, want 2", len(budget.Days))
	}
	if want := *money(64); budget.Days[0].Planned != want {
		t.Errorf("the first day plans %s, want %s", budget.Days[0].Planned, want)
	}
	if budget.Days[1].Planned != *money(50) {
		t.Errorf("the second day holds only the optional cave: %+v", budget.Days[1])
	}
}

// TestBuildBudgetEntries checks every cost is listed once, with the group total
// next to the amount that was typed.
func TestBuildBudgetEntries(t *testing.T) {
	trip, content := budgetPlan(t)
	budget := BuildBudget(trip, content)

	if len(budget.Entries) != 7 {
		t.Fatalf("%d entries, want 7: %+v", len(budget.Entries), budget.Entries)
	}
	kinds := map[BudgetEntryKind]int{}
	for _, entry := range budget.Entries {
		kinds[entry.Kind]++
	}
	if kinds[BudgetPlace] != 3 || kinds[BudgetStay] != 1 || kinds[BudgetLeg] != 1 || kinds[BudgetExpense] != 2 {
		t.Errorf("entries by kind: %v", kinds)
	}
	museum := budget.Entries[0]
	if museum.Kind != BudgetPlace || museum.Label != "Museum" || !museum.PerPerson ||
		museum.Unit != *money(15) || museum.Amount != *money(30) {
		t.Errorf("the per-person museum is listed as %+v", museum)
	}
	if last := budget.Entries[len(budget.Entries)-1]; last.Label != "Lagoon" || !last.Unassigned {
		t.Errorf("the unassigned place comes last, got %+v", last)
	}
}

// TestBuildBudgetWithoutPlan checks a trip with no plan still reports its budget.
func TestBuildBudgetWithoutPlan(t *testing.T) {
	trip := Trip{Currency: "EUR", Travelers: 3, Budget: money(600)}
	budget := BuildBudget(trip, DocumentContent{})

	if budget.Planned != 0 || budget.PerPerson != 0 {
		t.Errorf("nothing is planned yet: %s, %s", budget.Planned, budget.PerPerson)
	}
	if budget.Remaining == nil || *budget.Remaining != *money(600) {
		t.Errorf("the whole budget is left, got %v", budget.Remaining)
	}
	if budget.UsedPercent == nil || *budget.UsedPercent != 0 {
		t.Errorf("none of the budget is used, got %v", budget.UsedPercent)
	}
	if len(budget.Days) != 0 || len(budget.Entries) != 0 {
		t.Errorf("a trip without a plan has no days or entries")
	}
	if len(budget.Categories) != len(CostCategories) {
		t.Errorf("%d category rows, want every one", len(budget.Categories))
	}
}

// TestBuildBudgetWithoutABudget checks a trip that set no amount aside reports
// no share used rather than a made-up one.
func TestBuildBudgetWithoutABudget(t *testing.T) {
	trip, content := budgetPlan(t)
	trip.Budget = nil
	budget := BuildBudget(trip, content)

	if budget.Remaining != nil || budget.UsedPercent != nil {
		t.Errorf("remaining %v, used %v", budget.Remaining, budget.UsedPercent)
	}
	if budget.Planned != *money(444) {
		t.Errorf("the plan still adds up: %s", budget.Planned)
	}
}

// TestBuildBudgetReport checks a report counts what was spent beside the
// snapshot it carries, lists a cost entered only as spent, and measures the
// overall budget against the spending rather than the plan.
func TestBuildBudgetReport(t *testing.T) {
	trip, content := budgetPlan(t)
	trip.Kind = DocumentReport
	// The museum was paid per person, 20 each instead of 15; the tax was never
	// charged, and a souvenir nobody planned for cost 25.
	content.Items[0].ActualCost = money(20)
	content.Stays[0].ActualCost = money(260)
	souvenir := Expense{ID: uuid.New(), DayID: &content.Days[1].ID, Category: CostShopping, Actual: money(25),
		Note: "Souvenir"}
	content.Expenses = append(content.Expenses, souvenir)

	budget := BuildBudget(trip, content)
	if budget.Kind != DocumentReport {
		t.Errorf("kind %q", budget.Kind)
	}
	// The planned side is unchanged by what was spent.
	if want := *money(444); budget.Planned != want {
		t.Errorf("planned %s, want %s", budget.Planned, want)
	}
	// 20 x 2 travellers + 260 stay + 25 souvenir.
	if want := *money(325); budget.Actual != want {
		t.Errorf("actual %s, want %s", budget.Actual, want)
	}
	if budget.Remaining == nil || *budget.Remaining != *money(275) {
		t.Errorf("remaining %v, want 275 of the 600 left after spending", budget.Remaining)
	}
	var listed bool
	for _, entry := range budget.Entries {
		if entry.ID == souvenir.ID {
			listed = entry.Actual != nil && *entry.Actual == *money(25) && entry.Amount == 0
		}
	}
	if !listed {
		t.Error("a cost entered only as spent is missing from the list")
	}

	// The same content read as a plan carries no actual amounts at all.
	trip.Kind = DocumentPlan
	if plan := BuildBudget(trip, content); plan.Actual != 0 {
		t.Errorf("a plan counts %s as spent", plan.Actual)
	}
}
