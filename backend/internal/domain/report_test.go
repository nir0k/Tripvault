package domain

import (
	"testing"

	"github.com/google/uuid"
)

// reportContent builds a two-day report: one place visited and paid for, one
// skipped, one added on the spot, a stay, a leg and an expense.
func reportContent(t *testing.T) (Trip, DocumentContent) {
	t.Helper()
	days := planDays(t, 2)
	hotel := stay(t, "2026-06-20", "2026-06-22")
	hotel.PlannedCost, hotel.ActualCost = money(240), money(260)

	rating := 5
	museum := Item{ID: uuid.New(), DayID: &days[0].ID, Kind: ItemPlace, Name: "Museum", Status: StatusVisited,
		CostCategory: CostActivities, PlannedCost: money(15), ActualCost: money(18), CostPerPerson: true,
		Rating: &rating}
	cave := Item{ID: uuid.New(), DayID: &days[1].ID, Kind: ItemPlace, Name: "Cave", Status: StatusSkipped,
		CostCategory: CostActivities, PlannedCost: money(50)}
	lowRating := 3
	diner := Item{ID: uuid.New(), DayID: &days[1].ID, Kind: ItemPlace, Name: "Diner", Status: StatusUnplanned,
		CostCategory: CostFood, ActualCost: money(22), Rating: &lowRating}
	anchor := Item{ID: uuid.New(), DayID: &days[0].ID, Kind: ItemStayAnchor, Anchor: AnchorEvening, StayID: &hotel.ID}

	distance, duration := 25000, 1800
	drive := Leg{ID: uuid.New(), DayID: days[0].ID, FromItemID: museum.ID, ToItemID: anchor.ID, Mode: ModeCar,
		DistanceM: &distance, DurationS: &duration, PlannedCost: money(30), ActualCost: money(35)}

	fuel := Expense{ID: uuid.New(), Category: CostTransport, Planned: money(90), Actual: money(95), Note: "Fuel"}

	trip := Trip{Currency: "EUR", Travelers: 2, Budget: money(600)}
	content := DocumentContent{
		Document: Document{Kind: DocumentReport},
		Days:     days,
		Items:    []Item{museum, anchor, cave, diner},
		Stays:    []Stay{hotel},
		Legs:     []Leg{drive},
		Expenses: []Expense{fuel},
	}
	return trip, content
}

// TestBuildReportTotals checks the figures the reading mode opens with.
func TestBuildReportTotals(t *testing.T) {
	trip, content := reportContent(t)
	totals := BuildReportTotals(trip, content)

	if totals.Days != 2 || totals.Nights != 2 {
		t.Errorf("%d days and %d nights, want 2 and 2", totals.Days, totals.Nights)
	}
	want := ReportCounts{Visited: 1, Skipped: 1, Unplanned: 1}
	if totals.Places != want || totals.Places.Total() != 3 {
		t.Errorf("places %+v, want %+v", totals.Places, want)
	}
	if totals.DistanceM != 25000 || totals.ByMode[ModeCar].DurationS != 1800 {
		t.Errorf("distance %d, by car %+v", totals.DistanceM, totals.ByMode[ModeCar])
	}
	// 15 x 2 + 50 + 30 + 240 + 90 planned; 18 x 2 + 22 + 35 + 260 + 95 actual.
	if totals.PlannedCost != *money(440) || totals.ActualCost != *money(448) {
		t.Errorf("planned %s, actual %s", totals.PlannedCost, totals.ActualCost)
	}
	if totals.Difference != *money(8) {
		t.Errorf("difference %s, want 8.00", totals.Difference)
	}
	if totals.Rated != 2 || totals.AverageRating == nil || *totals.AverageRating != 4 {
		t.Errorf("%d rated, average %v", totals.Rated, totals.AverageRating)
	}
}

// TestBuildReportTotalsEmpty checks a report with nothing in it says so rather
// than inventing an average.
func TestBuildReportTotalsEmpty(t *testing.T) {
	totals := BuildReportTotals(Trip{Travelers: 2}, DocumentContent{Document: Document{Kind: DocumentReport}})
	if totals.Places.Total() != 0 || totals.AverageRating != nil || totals.ActualCost != 0 {
		t.Errorf("an empty report totals %+v", totals)
	}
}

// TestReportFieldsAreRefusedOnAPlan checks a plan cannot be given a status, a
// story, a rating or an amount actually spent: it would be copied into a report
// later and spoil the comparison it is meant to provide.
func TestReportFieldsAreRefusedOnAPlan(t *testing.T) {
	visited := ClockTime(600)
	rating := 4
	cases := map[string]Item{
		"status":  {Name: "x", Status: StatusVisited},
		"story":   {Name: "x", StoryMD: "It rained"},
		"time":    {Name: "x", ActualTime: &visited},
		"rating":  {Name: "x", Rating: &rating},
		"paid":    {Name: "x", ActualCost: money(10)},
		"skipped": {Name: "x", Status: StatusSkipped},
	}
	for name, item := range cases {
		if _, err := item.NormalizePlace(DocumentPlan); validationCode(t, err) == "" {
			t.Errorf("a plan accepted a place's %s", name)
		}
		if _, err := item.NormalizePlace(DocumentReport); err != nil {
			t.Errorf("a report refused a place's %s: %v", name, err)
		}
	}

	if _, err := (Stay{Name: "Hotel", CheckInDate: *date(t, "2026-06-20"),
		CheckOutDate: *date(t, "2026-06-21"), ActualCost: money(10)}).Normalize(DocumentPlan); validationCode(t,
		err) != "actual_cost_amount:report_only" {
		t.Errorf("a plan's stay accepted an actual cost: %v", err)
	}
	if _, err := (Leg{Mode: ModeCar, ActualCost: money(10)}).Normalize(DocumentPlan); validationCode(t,
		err) != "actual_cost_amount:report_only" {
		t.Errorf("a plan's leg accepted an actual cost: %v", err)
	}
	if _, err := (Expense{Planned: money(5), Actual: money(6)}).Normalize(DocumentPlan); validationCode(t,
		err) != "actual_amount:report_only" {
		t.Errorf("a plan's expense accepted an actual amount: %v", err)
	}
}

// TestReportFieldRules checks the values a report does accept.
func TestReportFieldRules(t *testing.T) {
	tooHigh := 6
	if _, err := (Item{Name: "x", Rating: &tooHigh}).NormalizePlace(DocumentReport); validationCode(t,
		err) != "rating:out_of_range" {
		t.Errorf("a rating of 6: %v", err)
	}
	if _, err := (Item{Name: "x", Status: "lost"}).NormalizePlace(DocumentReport); validationCode(t,
		err) != "status:unsupported" {
		t.Errorf("an unknown status: %v", err)
	}
	// A place of a report with no status set, or with the plan's, was visited.
	for _, status := range []ItemStatus{"", StatusPlanned} {
		place, err := Item{Name: "x", Status: status}.NormalizePlace(DocumentReport)
		if err != nil || place.Status != StatusVisited {
			t.Errorf("a place of a report marked %q: %+v %v", status, place.Status, err)
		}
	}
}

// TestOrderByActualTime checks the places with a time are sorted into the slots
// the timed places held, while a place without a time stays where it was.
func TestOrderByActualTime(t *testing.T) {
	at := func(minutes int) *ClockTime {
		clock := ClockTime(minutes)
		return &clock
	}
	places := []Item{
		{ID: uuid.New(), ActualTime: at(15 * 60)},
		{ID: uuid.New()},
		{ID: uuid.New(), ActualTime: at(9 * 60)},
		{ID: uuid.New(), ActualTime: at(12 * 60)},
		{ID: uuid.New(), ActualTime: at(9 * 60)},
	}
	got := OrderByActualTime(places)
	want := []uuid.UUID{places[2].ID, places[1].ID, places[4].ID, places[3].ID, places[0].ID}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("position %d holds %s, want %s", index, got[index], want[index])
		}
	}
}

// TestNormalizeDifficulty checks a difficulty is kept on an activity within its
// range, refused outside it and dropped when the element is a place.
func TestNormalizeDifficulty(t *testing.T) {
	level := func(value int) *int { return &value }
	activity := Item{Kind: ItemActivity, ActivityType: ActivityHike, Name: "Ridge", Difficulty: level(4)}
	if normalized, err := activity.NormalizePlace(DocumentReport); err != nil || *normalized.Difficulty != 4 {
		t.Errorf("an activity of difficulty 4: %+v %v", normalized.Difficulty, err)
	}
	activity.Difficulty = level(6)
	if _, err := activity.NormalizePlace(DocumentReport); err == nil {
		t.Error("a difficulty of 6 was accepted")
	}
	place := Item{Kind: ItemPlace, Name: "Museum", Difficulty: level(2)}
	if normalized, err := place.NormalizePlace(DocumentReport); err != nil || normalized.Difficulty != nil {
		t.Errorf("a place kept a difficulty: %+v %v", normalized.Difficulty, err)
	}
}
