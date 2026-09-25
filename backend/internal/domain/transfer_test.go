package domain

import (
	"testing"

	"github.com/google/uuid"
)

// transfer builds a transfer between two towns departing on a date.
func transfer(t *testing.T, departure string) Transfer {
	t.Helper()
	return Transfer{ID: uuid.New(), Kind: TransferFlight, FromName: "Keflavík", ToName: "Copenhagen",
		DepartureDate: *date(t, departure)}
}

// TestTransferNormalize checks the ends, the dates and the report-only amount.
func TestTransferNormalize(t *testing.T) {
	valid := transfer(t, "2026-06-20")
	valid.Kind = ""
	valid.FromName = "  Keflavík  "
	normalized, err := valid.Normalize(DocumentPlan)
	if err != nil {
		t.Fatalf("a valid transfer: %v", err)
	}
	if normalized.Kind != TransferFlight || normalized.FromName != "Keflavík" {
		t.Errorf("normalised to %+v", normalized)
	}

	lat := 55.6
	cases := map[string]func(*Transfer){
		"to_name:required":               func(tr *Transfer) { tr.ToName = " " },
		"kind:unsupported":               func(tr *Transfer) { tr.Kind = "rocket" },
		"to_lng:coordinates_incomplete":  func(tr *Transfer) { tr.ToLat = &lat },
		"arrival_date:end_before_start":  func(tr *Transfer) { tr.ArrivalDate = date(t, "2026-06-18") },
		"actual_cost_amount:report_only": func(tr *Transfer) { tr.ActualCost = money(10) },
		"url:invalid_url":                func(tr *Transfer) { tr.URL = "ftp://example.com" },
	}
	for want, change := range cases {
		broken := transfer(t, "2026-06-20")
		change(&broken)
		if _, err := broken.Normalize(DocumentPlan); validationCode(t, err) != want {
			t.Errorf("%s: got %q", want, validationCode(t, err))
		}
	}

	// A flight west over the date line lands the calendar day before it left.
	west := transfer(t, "2026-06-20")
	west.ArrivalDate = date(t, "2026-06-19")
	if _, err := west.Normalize(DocumentPlan); err != nil {
		t.Errorf("over the date line: %v", err)
	}
}

// TestTransfersOnDate checks a transfer shows on the day it departs and the day
// it arrives, in the order of departure.
func TestTransfersOnDate(t *testing.T) {
	early, late, night := transfer(t, "2026-06-20"), transfer(t, "2026-06-20"), transfer(t, "2026-06-20")
	seven, noon := ClockTime(7*60), ClockTime(12*60)
	early.DepartureTime, late.DepartureTime = &seven, &noon
	night.ArrivalDate = date(t, "2026-06-21")

	day := TransfersOnDate([]Transfer{late, night, early}, *date(t, "2026-06-20"))
	if len(day) != 3 || day[0].ID != early.ID || day[1].ID != late.ID || day[2].ID != night.ID {
		t.Errorf("the departure day holds %+v", day)
	}
	next := TransfersOnDate([]Transfer{late, night, early}, *date(t, "2026-06-21"))
	if len(next) != 1 || next[0].ID != night.ID {
		t.Errorf("the arrival day holds %+v", next)
	}
}

// TestBudgetCountsTransfers checks a per-person ticket reaches the budget as
// the group's, apart from the days, in the transport category.
func TestBudgetCountsTransfers(t *testing.T) {
	trip, content := budgetPlan(t)
	flight := transfer(t, "2026-06-20")
	flight.PlannedCost = money(100)
	flight.CostPerPerson = true
	content.Transfers = []Transfer{flight}

	before := BuildBudget(trip, DocumentContent{Days: content.Days, Items: content.Items, Stays: content.Stays,
		Legs: content.Legs, Expenses: content.Expenses})
	budget := BuildBudget(trip, content)
	want := Money(100*100) * Money(trip.Travelers)
	if budget.Transfers != want || budget.Planned-before.Planned != want {
		t.Errorf("transfers %s, planned grew by %s, want %s", budget.Transfers, budget.Planned-before.Planned, want)
	}
	found := false
	for _, entry := range budget.Entries {
		if entry.Kind == BudgetTransfer {
			found = entry.Category == CostTransport && entry.DayID == nil && entry.PerPerson &&
				entry.Label == "Keflavík → Copenhagen"
		}
	}
	if !found {
		t.Errorf("no transfer entry among %+v", budget.Entries)
	}
}
