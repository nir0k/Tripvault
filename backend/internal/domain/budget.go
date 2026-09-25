package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// The budget answers one question: what has the plan committed, next to what the
// trip is willing to spend. It is assembled from the trip's document rather than
// stored, so a cost typed on a place, a stay, a leg or a separate expense shows
// up in the totals with no extra bookkeeping.
//
// A report has a budget of its own, read from nothing but itself: the planned
// amounts are the snapshot it was copied with, or whatever was typed into it,
// and beside each one stands what was really spent. It never reads the plan, so
// deleting the plan leaves the report's figures exactly as they were. The
// overall budget of a report is measured against what was spent, since that is
// the question a finished journey asks.
//
// One kind of cost is kept apart from the committed total: a place not yet put on
// a day, which no day has agreed to pay for yet. A place marked optional counts
// in full, because the money still has to be there if it is visited.

// BudgetEntryKind says what carries a cost.
type BudgetEntryKind string

// Budget entry kinds.
const (
	BudgetPlace    BudgetEntryKind = "place"
	BudgetStay     BudgetEntryKind = "stay"
	BudgetTransfer BudgetEntryKind = "transfer"
	BudgetLeg      BudgetEntryKind = "leg"
	BudgetExpense  BudgetEntryKind = "expense"
)

// BudgetEntry is one line of the budget's expense list.
type BudgetEntry struct {
	Kind  BudgetEntryKind
	ID    uuid.UUID
	Label string
	// Category is the place's own category, accommodation for a stay and
	// transport for a leg or a transfer.
	Category CostCategory
	// DayID is the day the cost belongs to, nil for a stay, a transfer, an
	// unassigned place or an expense of the whole trip.
	DayID *uuid.UUID
	// Amount is what the cost comes to for the whole group: Unit multiplied by
	// the travellers when PerPerson is set.
	Amount Money
	// Unit is the amount as it was typed.
	Unit      Money
	PerPerson bool
	// IsOptional marks a place its day may skip. It is shown next to the cost,
	// but counted like any other.
	IsOptional bool
	// Unassigned marks a place with no day: it is listed, but left out of every
	// total except Budget.Unassigned.
	Unassigned bool
	// Actual is what was really spent for the whole group, in a report; nil in
	// a plan and where nothing was entered yet.
	Actual *Money
}

// BudgetCategory totals one budget category.
type BudgetCategory struct {
	Category CostCategory
	Planned  Money
	// Actual is what the category really cost; zero in a plan.
	Actual Money
}

// BudgetDay totals the costs of one day.
type BudgetDay struct {
	DayID    uuid.UUID
	Position int
	Date     *time.Time
	Planned  Money
	// Actual is what the day really cost; zero in a plan.
	Actual Money
}

// Budget is the money side of a trip's plan.
type Budget struct {
	// Kind is the document the budget was read from: a plan counts what is
	// committed, a report what was spent beside it.
	Kind      DocumentKind
	Currency  string
	Travelers int
	// Budget is the trip's overall budget, nil when it has none.
	Budget *Money
	// Planned is everything the plan commits to: the days, the stays, the
	// transfers and the expenses of the whole trip.
	Planned Money
	// Unassigned holds the places that are not on a day yet.
	Unassigned Money
	// Stays, Transfers and Untied are the parts of Planned that belong to no
	// single day.
	Stays     Money
	Transfers Money
	Untied    Money
	// Actual is everything a report records as spent; zero in a plan.
	Actual Money
	// PerPerson is the spending divided over the travellers: Planned in a plan,
	// Actual in a report.
	PerPerson Money
	// Remaining is Budget minus the spending - Planned in a plan, Actual in a
	// report - negative when it overspends the trip's budget; nil without a
	// budget.
	Remaining *Money
	// UsedPercent is how much of the budget the spending takes up, for a bar
	// that shows at a glance whether it still fits; nil without a budget, and
	// above 100 when it does not.
	UsedPercent *int
	// Categories lists every category, in CostCategories order.
	Categories []BudgetCategory
	// Days follow the plan's order.
	Days []BudgetDay
	// Entries are the individual costs, day by day and unassigned places last.
	Entries []BudgetEntry
}

// ItemLabel - names an element of a day. A stay mark carries no name of its
// own, so it borrows its stay's.
//
// Arguments:
//   - item: the place or stay mark.
//   - stays: the document's stays by ID.
//
// Returns:
//   - the name to show, empty when a mark points at a stay that is gone.
func ItemLabel(item Item, stays map[uuid.UUID]Stay) string {
	if item.Kind == ItemStayAnchor && item.StayID != nil {
		if stay, ok := stays[*item.StayID]; ok {
			return stay.Name
		}
		return ""
	}
	return item.Name
}

// BuildBudget - assembles the budget of a trip's plan or report.
//
// Arguments:
//   - trip: the trip the document belongs to, for its kind, currency,
//     travellers and overall budget.
//   - content: the document with its days, places, stays, legs and expenses. A
//     zero value stands for a trip whose document could not be read.
//
// Returns:
//   - the budget with its totals, its per-category and per-day breakdown and
//     the list of individual costs.
func BuildBudget(trip Trip, content DocumentContent) Budget {
	travelers := max(trip.Travelers, 1)
	report := trip.Kind == DocumentReport
	budget := Budget{
		Kind:       trip.Kind,
		Currency:   trip.Currency,
		Travelers:  travelers,
		Budget:     trip.Budget,
		Categories: make([]BudgetCategory, 0, len(CostCategories)),
		Days:       make([]BudgetDay, 0, len(content.Days)),
		Entries:    []BudgetEntry{},
	}

	stays := make(map[uuid.UUID]Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	categories := make(map[CostCategory]*BudgetCategory, len(CostCategories))
	for _, category := range CostCategories {
		budget.Categories = append(budget.Categories, BudgetCategory{Category: category})
	}
	for index := range budget.Categories {
		categories[budget.Categories[index].Category] = &budget.Categories[index]
	}

	// The list follows the plan: each day with its places, legs and expenses,
	// then what belongs to no day, and the unassigned places last.
	for _, day := range content.Days {
		dayID := day.ID
		items := DayItems(content.Items, &dayID)
		total := BudgetDay{DayID: dayID, Position: day.Position, Date: day.Date}
		for _, item := range items {
			if entry, ok := placeEntry(item, travelers, report); ok {
				budget.Entries = append(budget.Entries, entry)
				addEntry(&budget, categories, &total, entry)
			}
		}
		for _, leg := range append([]*Leg{OpeningLeg(content.Legs, items)}, DayLegs(content.Legs, items)...) {
			if entry, ok := legEntry(leg, content.Items, stays, &dayID, report); ok {
				budget.Entries = append(budget.Entries, entry)
				addEntry(&budget, categories, &total, entry)
			}
		}
		for _, expense := range DayExpenses(content.Expenses, &dayID) {
			if entry, ok := expenseEntry(expense, report); ok {
				budget.Entries = append(budget.Entries, entry)
				addEntry(&budget, categories, &total, entry)
			}
		}
		budget.Days = append(budget.Days, total)
	}

	for _, stay := range content.Stays {
		entry, ok := stayEntry(stay, report)
		if !ok {
			continue
		}
		budget.Entries = append(budget.Entries, entry)
		addEntry(&budget, categories, nil, entry)
		budget.Stays += entry.Amount
	}
	for _, transfer := range content.Transfers {
		entry, ok := transferEntry(transfer, travelers, report)
		if !ok {
			continue
		}
		budget.Entries = append(budget.Entries, entry)
		addEntry(&budget, categories, nil, entry)
		budget.Transfers += entry.Amount
	}
	for _, expense := range DayExpenses(content.Expenses, nil) {
		entry, ok := expenseEntry(expense, report)
		if !ok {
			continue
		}
		budget.Entries = append(budget.Entries, entry)
		addEntry(&budget, categories, nil, entry)
		budget.Untied += entry.Amount
	}
	for _, item := range DayItems(content.Items, nil) {
		if entry, ok := placeEntry(item, travelers, report); ok {
			budget.Entries = append(budget.Entries, entry)
			addEntry(&budget, categories, nil, entry)
		}
	}

	spent := budget.Planned
	if report {
		spent = budget.Actual
	}
	budget.PerPerson = perPerson(spent, travelers)
	if trip.Budget != nil {
		remaining := *trip.Budget - spent
		budget.Remaining = &remaining
		budget.UsedPercent = usedPercent(spent, *trip.Budget)
	}
	return budget
}

// perPerson divides a total over the travellers, rounded to the nearest cent.
func perPerson(total Money, travelers int) Money {
	if total == 0 {
		return 0
	}
	return Money(math.Round(float64(total) / float64(travelers)))
}

// usedPercent reports how much of a budget the plan takes up, rounded to whole
// percent. A budget of zero is treated as fully used by anything at all, since
// no share of it is left to spend.
func usedPercent(planned, budget Money) *int {
	if budget <= 0 {
		used := 0
		if planned > 0 {
			used = 100
		}
		return &used
	}
	used := int(math.Round(float64(planned) / float64(budget) * 100))
	return &used
}

// placeEntry turns a place that carries a cost into an entry: a planned cost,
// or in a report an actual one as well. A stay mark and a place without a cost
// produce nothing.
func placeEntry(item Item, travelers int, report bool) (BudgetEntry, bool) {
	if !item.Kind.IsVisit() || !hasCost(item.PlannedCost, item.ActualCost, report) {
		return BudgetEntry{}, false
	}
	entry := BudgetEntry{
		Kind:       BudgetPlace,
		ID:         item.ID,
		Label:      item.Name,
		Category:   item.CostCategory,
		DayID:      item.DayID,
		Amount:     item.PlannedCostTotal(travelers),
		Unit:       moneyOrZero(item.PlannedCost),
		PerPerson:  item.CostPerPerson,
		IsOptional: item.IsOptional,
		Unassigned: item.DayID == nil,
	}
	if report && item.ActualCost != nil {
		actual := item.ActualCostTotal(travelers)
		entry.Actual = &actual
	}
	return entry, true
}

// hasCost says whether something carries an amount the budget lists: a planned
// one, or in a report an actual one.
func hasCost(planned, actual *Money, report bool) bool {
	return planned != nil || (report && actual != nil)
}

// actualOf keeps an actual amount for a report and drops it for a plan, which
// never carries one.
func actualOf(actual *Money, report bool) *Money {
	if !report {
		return nil
	}
	return actual
}

// stayEntry turns a stay that carries a cost into an entry.
func stayEntry(stay Stay, report bool) (BudgetEntry, bool) {
	if !hasCost(stay.PlannedCost, stay.ActualCost, report) {
		return BudgetEntry{}, false
	}
	return BudgetEntry{
		Kind:     BudgetStay,
		ID:       stay.ID,
		Label:    stay.Name,
		Category: CostAccommodation,
		Amount:   moneyOrZero(stay.PlannedCost),
		Unit:     moneyOrZero(stay.PlannedCost),
		Actual:   actualOf(stay.ActualCost, report),
	}, true
}

// transferEntry turns a transfer that carries a cost into an entry. A ticket
// is usually priced per head, so the per-person flag applies as on a place.
func transferEntry(transfer Transfer, travelers int, report bool) (BudgetEntry, bool) {
	if !hasCost(transfer.PlannedCost, transfer.ActualCost, report) {
		return BudgetEntry{}, false
	}
	entry := BudgetEntry{
		Kind:      BudgetTransfer,
		ID:        transfer.ID,
		Label:     transfer.FromName + " → " + transfer.ToName,
		Category:  CostTransport,
		Amount:    transfer.PlannedCostTotal(travelers),
		Unit:      moneyOrZero(transfer.PlannedCost),
		PerPerson: transfer.CostPerPerson,
	}
	if report && transfer.ActualCost != nil {
		actual := transfer.ActualCostTotal(travelers)
		entry.Actual = &actual
	}
	return entry, true
}

// legEntry turns a leg that carries a cost into an entry.
func legEntry(leg *Leg, items []Item, stays map[uuid.UUID]Stay, dayID *uuid.UUID, report bool) (BudgetEntry, bool) {
	if leg == nil || !hasCost(leg.PlannedCost, leg.ActualCost, report) {
		return BudgetEntry{}, false
	}
	return BudgetEntry{
		Kind:     BudgetLeg,
		ID:       leg.ID,
		Label:    legLabel(*leg, items, stays),
		Category: CostTransport,
		DayID:    dayID,
		Amount:   moneyOrZero(leg.PlannedCost),
		Unit:     moneyOrZero(leg.PlannedCost),
		Actual:   actualOf(leg.ActualCost, report),
	}, true
}

// expenseEntry turns a separate expense that carries an amount into an entry.
func expenseEntry(expense Expense, report bool) (BudgetEntry, bool) {
	if !hasCost(expense.Planned, expense.Actual, report) {
		return BudgetEntry{}, false
	}
	return BudgetEntry{
		Kind:     BudgetExpense,
		ID:       expense.ID,
		Label:    expense.Note,
		Category: expense.Category,
		DayID:    expense.DayID,
		Amount:   moneyOrZero(expense.Planned),
		Unit:     moneyOrZero(expense.Planned),
		Actual:   actualOf(expense.Actual, report),
	}, true
}

// addEntry adds one cost to the totals it belongs to. An unassigned place only
// reaches Budget.Unassigned, since no day has taken it on.
func addEntry(budget *Budget, categories map[CostCategory]*BudgetCategory, day *BudgetDay, entry BudgetEntry) {
	if entry.Unassigned {
		budget.Unassigned += entry.Amount
		return
	}
	actual := moneyOrZero(entry.Actual)
	budget.Planned += entry.Amount
	budget.Actual += actual
	if category := categories[entry.Category]; category != nil {
		category.Planned += entry.Amount
		category.Actual += actual
	}
	if day != nil {
		day.Planned += entry.Amount
		day.Actual += actual
	}
}

// legLabel names a leg by the elements it joins, such as "Hotel - Museum".
func legLabel(leg Leg, items []Item, stays map[uuid.UUID]Stay) string {
	from, to := "", ""
	for _, item := range items {
		switch item.ID {
		case leg.FromItemID:
			from = ItemLabel(item, stays)
		case leg.ToItemID:
			to = ItemLabel(item, stays)
		}
	}
	return from + " - " + to
}
