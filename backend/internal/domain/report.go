package domain

import (
	"math"

	"github.com/google/uuid"
)

// The reading mode of a report opens with a handful of figures about the trip as
// it actually went: how long it lasted, how far it went by which means, how many
// of the planned places were reached and what it all cost. They are derived from
// the report on every read, like the budget, so nothing has to be kept in step.

// ReportCounts counts the places of a report by status.
type ReportCounts struct {
	Planned   int
	Visited   int
	Skipped   int
	Unplanned int
}

// Total - counts every place of the report.
//
// Returns:
//   - the number of places, whatever their status.
func (c ReportCounts) Total() int {
	return c.Planned + c.Visited + c.Skipped + c.Unplanned
}

// ReportTotals is what the reading mode of a report shows above its days.
type ReportTotals struct {
	Days int
	// Nights counts the nights covered by a stay.
	Nights int
	Places ReportCounts
	// DistanceM and ByMode are the distance travelled, in total and per means.
	DistanceM int
	ByMode    map[TravelMode]ModeTotal
	// PlannedCost is the snapshot the report carries; ActualCost is what was
	// really spent, and Difference is the second minus the first.
	PlannedCost Money
	ActualCost  Money
	Difference  Money
	// Rated counts the places with a rating; AverageRating is their mean, nil
	// when nothing was rated.
	Rated         int
	AverageRating *float64
}

// BuildReportTotals - sums up a report for its reading mode.
//
// Unassigned places are not counted: a report has none, and a plan passed here
// would be summed as if its backlog had been travelled.
//
// Arguments:
//   - trip: the trip the report belongs to, for its number of travellers.
//   - content: the report with its days, places, stays, legs and expenses.
//
// Returns:
//   - the totals.
func BuildReportTotals(trip Trip, content DocumentContent) ReportTotals {
	travelers := max(trip.Travelers, 1)
	totals := ReportTotals{
		Days:   len(content.Days),
		ByMode: map[TravelMode]ModeTotal{},
	}

	ratings := 0
	for _, item := range content.Items {
		if !item.Kind.IsVisit() || item.DayID == nil {
			continue
		}
		switch item.Status {
		case StatusVisited:
			totals.Places.Visited++
		case StatusSkipped:
			totals.Places.Skipped++
		case StatusUnplanned:
			totals.Places.Unplanned++
		default:
			totals.Places.Planned++
		}
		if item.Rating != nil {
			totals.Rated++
			ratings += *item.Rating
		}
		totals.PlannedCost += item.PlannedCostTotal(travelers)
		totals.ActualCost += item.ActualCostTotal(travelers)
	}
	if totals.Rated > 0 {
		average := math.Round(float64(ratings)/float64(totals.Rated)*10) / 10
		totals.AverageRating = &average
	}

	// A recording of a place or an activity is distance covered inside it, so
	// it is added to the legs between the places. A report has no unassigned
	// places, but a plan passed here would not count its backlog.
	scheduled := make(map[uuid.UUID]bool, len(content.Items))
	for _, item := range content.Items {
		scheduled[item.ID] = item.DayID != nil
	}
	for _, track := range content.Tracks {
		if scheduled[track.ItemID] {
			totals.DistanceM += track.DistanceM
		}
	}

	for _, leg := range content.Legs {
		total := totals.ByMode[leg.Mode]
		if distance := leg.Distance(); distance != nil {
			totals.DistanceM += *distance
			total.DistanceM += *distance
		}
		if duration := leg.Duration(); duration != nil {
			total.DurationS += *duration
		}
		totals.ByMode[leg.Mode] = total
		totals.PlannedCost += moneyOrZero(leg.PlannedCost)
		totals.ActualCost += moneyOrZero(leg.ActualCost)
	}

	for _, stay := range content.Stays {
		totals.Nights += stay.Nights()
		totals.PlannedCost += moneyOrZero(stay.PlannedCost)
		totals.ActualCost += moneyOrZero(stay.ActualCost)
	}
	for _, expense := range content.Expenses {
		totals.PlannedCost += moneyOrZero(expense.Planned)
		totals.ActualCost += moneyOrZero(expense.Actual)
	}

	totals.Difference = totals.ActualCost - totals.PlannedCost
	return totals
}

// moneyOrZero reads an optional amount as a number.
func moneyOrZero(amount *Money) Money {
	if amount == nil {
		return 0
	}
	return *amount
}
