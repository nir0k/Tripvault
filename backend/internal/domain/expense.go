package domain

import (
	"time"

	"github.com/google/uuid"
)

// CostCategories is the order budget categories are listed in. Accommodation
// and transport come first because they dominate a trip's cost.
var CostCategories = []CostCategory{CostAccommodation, CostTransport, CostFood, CostActivities,
	CostShopping, CostOther}

// ValidateCostCategory - checks that a value names a budget category.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - category: the value to check.
//
// Returns:
//   - a *ValidationError when the value is not a known category.
func ValidateCostCategory(field string, category CostCategory) error {
	switch category {
	case CostAccommodation, CostTransport, CostFood, CostActivities, CostShopping, CostOther:
		return nil
	default:
		return NewValidationError(field, "unsupported", "is not a known budget category")
	}
}

// Expense is a cost that belongs to no place, stay or leg: a city tax, a
// souvenir, a tank of fuel. It hangs on a day, or on the document as a whole
// when DayID is nil.
type Expense struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	DayID      *uuid.UUID
	Category   CostCategory
	// Planned is what the plan sets aside; Actual is what was really spent and
	// stays empty in a plan.
	Planned *Money
	Actual  *Money
	// SpentOn is the date the money left, known only in a report.
	SpentOn *time.Time
	Note    string
	// SourceExpenseID is the expense of the plan this one was copied from.
	SourceExpenseID *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Normalize - trims an expense's note, fills its default category and checks
// its fields.
//
// Arguments:
//   - kind: the document the expense belongs to; a plan carries no actual amount
//     and no date of spending.
//
// Returns:
//   - the normalised expense.
//   - the first *ValidationError found.
func (e Expense) Normalize(kind DocumentKind) (Expense, error) {
	if kind == DocumentPlan {
		if e.Actual != nil {
			return e, reportOnly("actual_amount")
		}
		if e.SpentOn != nil {
			return e, reportOnly("spent_on")
		}
	}
	if e.Category == "" {
		e.Category = CostOther
	}
	if err := ValidateCostCategory("category", e.Category); err != nil {
		return e, err
	}
	var err error
	if e.Note, err = trimmedText("note", e.Note, maxShortText); err != nil {
		return e, err
	}
	if e.Planned == nil && e.Actual == nil {
		return e, NewValidationError("planned_amount", "required", "an expense needs an amount")
	}
	if e.SpentOn != nil {
		date := dateOnly(*e.SpentOn)
		e.SpentOn = &date
	}
	return e, nil
}

// DayExpenses - picks the expenses of one day, or the ones tied to no day when
// dayID is nil, keeping the order they came in.
//
// Arguments:
//   - expenses: the document's expenses.
//   - dayID: the day, or nil for the expenses of the whole trip.
//
// Returns:
//   - the matching expenses.
func DayExpenses(expenses []Expense, dayID *uuid.UUID) []Expense {
	picked := make([]Expense, 0, len(expenses))
	for _, expense := range expenses {
		if sameDay(expense.DayID, dayID) {
			picked = append(picked, expense)
		}
	}
	return picked
}

// PlannedTotal - sums the planned amounts of a set of expenses.
//
// Arguments:
//   - expenses: the expenses to add up.
//
// Returns:
//   - the total; zero when none of them has a planned amount.
func PlannedTotal(expenses []Expense) Money {
	var total Money
	for _, expense := range expenses {
		total += moneyOrZero(expense.Planned)
	}
	return total
}

// ActualTotal - sums the actual amounts of a set of expenses.
//
// Arguments:
//   - expenses: the expenses to add up.
//
// Returns:
//   - the total; zero when none of them has an actual amount.
func ActualTotal(expenses []Expense) Money {
	var total Money
	for _, expense := range expenses {
		total += moneyOrZero(expense.Actual)
	}
	return total
}
