package domain

import (
	"testing"

	"github.com/google/uuid"
)

// stopDocument builds a report of one day with a hike whose line has a café
// along it, paid by ann and shared with bob.
func stopDocument(ann, bob uuid.UUID) DocumentContent {
	day := Day{ID: uuid.New(), StartTime: DefaultDayStart}
	hike := Item{ID: uuid.New(), DayID: &day.ID, Kind: ItemActivity, ActivityType: ActivityHike, Name: "Reykjadalur",
		Status: StatusVisited}
	cafe := Stop{ID: uuid.New(), ItemID: hike.ID, Kind: StopFood, Name: "Café", NoteMD: "Soup",
		PlannedCost: cents(2000), ActualCost: cents(2400), CostCategory: CostFood,
		PaidBy: &ann, CostSplit: SplitEveryone, CostShares: []CostShare{{UserID: ann}, {UserID: bob}}}
	return DocumentContent{
		Document: Document{ID: uuid.New(), Kind: DocumentReport},
		Days:     []Day{day},
		Items:    []Item{hike},
		Tracks:   []Track{{ID: uuid.New(), ItemID: hike.ID, DistanceM: 7000, Stops: []Stop{cafe}}},
	}
}

// TestStopNormalize checks a stop's kind, its plan-only fields and its split.
func TestStopNormalize(t *testing.T) {
	ann := uuid.New()
	cases := []struct {
		name string
		stop Stop
		kind DocumentKind
		code string
	}{
		{"defaults", Stop{}, DocumentPlan, ""},
		{"unknown kind", Stop{Kind: "spa"}, DocumentPlan, "kind:unsupported"},
		{"plan with a time", Stop{ActualTime: new(ClockTime)}, DocumentPlan, "actual_time:report_only"},
		{"plan with a spend", Stop{ActualCost: cents(100)}, DocumentPlan, "actual_cost_amount:report_only"},
		{"report with a time", Stop{ActualTime: new(ClockTime)}, DocumentReport, ""},
		{"split without shares", Stop{PaidBy: &ann, CostSplit: SplitEveryone}, DocumentPlan, "cost_shares:required"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stop, err := test.stop.Normalize(test.kind)
			if code := validationCode(t, err); code != test.code {
				t.Fatalf("code = %q, want %q (%v)", code, test.code, err)
			}
			if err == nil && (stop.Kind == "" || stop.CostCategory == "") {
				t.Errorf("no defaults: %+v", stop)
			}
		})
	}
}

// TestStopsCostInTheirDay checks a stop's cost counts in its activity's day,
// in the budget as an entry of its own, in the members' debts and in the
// report's totals.
func TestStopsCostInTheirDay(t *testing.T) {
	ann, bob := uuid.New(), uuid.New()
	content := stopDocument(ann, bob)
	trip := Trip{Kind: DocumentReport, Currency: "EUR", Travelers: 2}

	_, summary := ScheduleDay(content.Days[0], content.Items, nil, nil, 2, content.Tracks)
	if summary.PlannedCost != 2000 || summary.ActualCost != 2400 {
		t.Errorf("day costs %v planned, %v spent", summary.PlannedCost, summary.ActualCost)
	}

	budget := BuildBudget(trip, content)
	if len(budget.Entries) != 1 || budget.Entries[0].Kind != BudgetStop ||
		budget.Entries[0].Label != "Reykjadalur · Café" || budget.Actual != 2400 {
		t.Errorf("budget %+v", budget.Entries)
	}
	if len(budget.Settlements) != 1 || budget.Settlements[0].From != bob || budget.Settlements[0].Amount != 1200 {
		t.Errorf("settlements %+v", budget.Settlements)
	}

	if totals := BuildReportTotals(trip, content); totals.ActualCost != 2400 {
		t.Errorf("report spent %v", totals.ActualCost)
	}
}

// TestStopsAreTranslatedAndHiddenFromALink checks a stop's words are read in a
// translation and its payer is left out of what a link reads.
func TestStopsAreTranslatedAndHiddenFromALink(t *testing.T) {
	content := stopDocument(uuid.New(), uuid.New())
	cafe := content.Tracks[0].Stops[0]
	content.Translations = []Translation{{Target: TranslateStop, TargetID: cafe.ID, Field: "name", Lang: "ru",
		Value: "Кафе"}}

	if name := content.Translated("ru").Tracks[0].Stops[0].Name; name != "Кафе" {
		t.Errorf("translated name %q", name)
	}
	shared := content.ForShareLink().Tracks[0].Stops[0]
	if shared.PaidBy != nil || shared.CostShares != nil || shared.CostSplit != SplitNone || shared.PlannedCost == nil {
		t.Errorf("a link reads %+v", shared)
	}
	if content.Tracks[0].Stops[0].PaidBy == nil {
		t.Error("the content itself was changed")
	}
}
