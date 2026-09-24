package domain

import (
	"testing"

	"github.com/google/uuid"
)

// stay builds a stay covering the nights from checkIn to the night before checkOut.
func stay(t *testing.T, checkIn, checkOut string) Stay {
	t.Helper()
	return Stay{ID: uuid.New(), Name: "Hotel", CheckInDate: *date(t, checkIn), CheckOutDate: *date(t, checkOut)}
}

// planDays builds count consecutive days from 2026-06-20 with both marks enabled.
func planDays(t *testing.T, count int) []Day {
	t.Helper()
	start := date(t, "2026-06-20")
	days := make([]Day, count)
	for i := range days {
		day := start.AddDate(0, 0, i)
		days[i] = Day{ID: uuid.New(), Position: i, Date: &day, MorningAnchor: true,
			EveningAnchor: true, StartTime: DefaultDayStart}
	}
	return days
}

// TestAnchorAssignments checks marks follow the nights, and the switches.
func TestAnchorAssignments(t *testing.T) {
	days := planDays(t, 4)
	reykjavik := stay(t, "2026-06-20", "2026-06-22")
	vik := stay(t, "2026-06-22", "2026-06-23")

	got := AnchorAssignments(days, []Stay{vik, reykjavik})
	want := map[AnchorKey]uuid.UUID{
		{days[0].ID, AnchorEvening}: reykjavik.ID,
		{days[1].ID, AnchorMorning}: reykjavik.ID,
		{days[1].ID, AnchorEvening}: reykjavik.ID,
		{days[2].ID, AnchorMorning}: reykjavik.ID,
		{days[2].ID, AnchorEvening}: vik.ID,
		{days[3].ID, AnchorMorning}: vik.ID,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d marks, want %d: %v", len(got), len(want), got)
	}
	for key, id := range want {
		if got[key] != id {
			t.Errorf("%v: got %v, want %v", key, got[key], id)
		}
	}

	days[1].MorningAnchor = false
	days[2].NoOvernight = true
	got = AnchorAssignments(days, []Stay{vik, reykjavik})
	if _, ok := got[AnchorKey{days[1].ID, AnchorMorning}]; ok {
		t.Error("a switched-off morning mark is placed")
	}
	if _, ok := got[AnchorKey{days[2].ID, AnchorEvening}]; ok {
		t.Error("an evening mark is placed before a night without a stay")
	}

	undated := []Day{{ID: uuid.New(), MorningAnchor: true, EveningAnchor: true}}
	if len(AnchorAssignments(undated, []Stay{reykjavik})) != 0 {
		t.Error("a day without a date got a mark")
	}
}

// TestNights checks coverage, overlaps, gaps and nights without a stay.
func TestNights(t *testing.T) {
	days := planDays(t, 5)
	days[3].NoOvernight = true
	first := stay(t, "2026-06-20", "2026-06-22")
	overlapping := stay(t, "2026-06-21", "2026-06-22")

	nights := Nights(days[0].Date, days[4].Date, days, []Stay{first, overlapping})
	if len(nights) != 4 {
		t.Fatalf("got %d nights, want 4", len(nights))
	}
	if len(nights[0].StayIDs) != 1 || len(nights[1].StayIDs) != 2 {
		t.Errorf("coverage: %+v", nights[:2])
	}
	if !nights[2].Missing() || nights[3].Missing() || !nights[3].NoOvernight {
		t.Errorf("gaps: %+v", nights[2:])
	}
	if Nights(nil, nil, days, nil) != nil {
		t.Error("a trip without dates has nights")
	}
}

// TestSummarizeStays checks the totals and the price per night.
func TestSummarizeStays(t *testing.T) {
	paid := stay(t, "2026-06-20", "2026-06-23")
	cost := Money(30000)
	paid.PlannedCost = &cost
	free := stay(t, "2026-06-23", "2026-06-25")

	summary := SummarizeStays([]Stay{paid, free})
	if summary.Nights != 5 || summary.Cost != 30000 || summary.AveragePerNight == nil || *summary.AveragePerNight != 10000 {
		t.Errorf("summary: %+v", summary)
	}
	if price := paid.PricePerNight(); price == nil || *price != 10000 {
		t.Errorf("price per night: %v", price)
	}
	if SummarizeStays([]Stay{free}).AveragePerNight != nil {
		t.Error("an average without prices")
	}
}

// TestScheduleDay checks arrival times, waiting for a desired time, lateness
// and the day's totals.
// TestScheduleDayAddsRecordings checks the recording of an activity is added
// to the distance of the legs, while the means of travel and the times still
// come from the legs alone.
func TestScheduleDayAddsRecordings(t *testing.T) {
	day := planDays(t, 1)[0]
	items := []Item{
		{ID: uuid.New(), Kind: ItemPlace, VisitMinutes: 60},
		{ID: uuid.New(), Kind: ItemActivity, VisitMinutes: 30},
	}
	thirty, km := 30*60, 25000
	legs := []*Leg{{Mode: ModeCar, DurationS: &thirty, DistanceM: &km}}

	_, fromLegs := ScheduleDay(day, items, nil, legs, 2, nil)
	if fromLegs.DistanceM != 25000 {
		t.Fatalf("without a recording the day reports %d m, want the legs' 25000", fromLegs.DistanceM)
	}
	// A hike recorded inside an activity is distance on top of the legs.
	hike := Track{ItemID: items[1].ID, DistanceM: 9000}
	_, recorded := ScheduleDay(day, items, nil, legs, 2, []Track{hike})
	if recorded.DistanceM != 34_000 {
		t.Errorf("a day with a recorded activity reports %d m, want 34000", recorded.DistanceM)
	}

	// The leg in from the day before is travelled first thing in the morning.
	hour, far := 3600, 90_000
	opening := &Leg{Mode: ModeCar, DurationS: &hour, DistanceM: &far}
	schedules, opened := ScheduleDay(day, items, opening, legs, 2, nil)
	if opened.DistanceM != 115_000 || schedules[0].ArrivalMinutes != int(day.StartTime)+60 {
		t.Errorf("with an opening leg: %d m, first arrival %d", opened.DistanceM, schedules[0].ArrivalMinutes)
	}
	if len(recorded.ByMode) != len(fromLegs.ByMode) {
		t.Errorf("the breakdown by means changed: %v against %v", recorded.ByMode, fromLegs.ByMode)
	}
	if recorded.TravelMinutes != fromLegs.TravelMinutes {
		t.Errorf("the travel time changed: %d against %d", recorded.TravelMinutes, fromLegs.TravelMinutes)
	}
}

func TestScheduleDay(t *testing.T) {
	day := planDays(t, 1)[0]
	stayID := uuid.New()
	lunch := ClockTime(12 * 60)
	early := ClockTime(9 * 60)
	price := Money(1500)
	items := []Item{
		{Kind: ItemStayAnchor, Anchor: AnchorMorning, StayID: &stayID},
		{Kind: ItemPlace, VisitMinutes: 60, PlannedCost: &price, CostPerPerson: true},
		{Kind: ItemPlace, VisitMinutes: 45, DesiredTime: &lunch, PlannedCost: &price},
		{Kind: ItemPlace, VisitMinutes: 30, DesiredTime: &early, IsOptional: true, PlannedCost: &price},
		{Kind: ItemStayAnchor, Anchor: AnchorEvening, StayID: &stayID},
	}

	thirty, fifteen, km := 30*60, 15*60, 25000
	legCost := Money(800)
	legs := []*Leg{
		{Mode: ModeCar, DurationS: &thirty, DistanceM: &km, PlannedCost: &legCost},
		{Mode: ModeWalk, DurationS: &thirty, ManualDurationS: &fifteen},
		nil,
		{Mode: ModeOther},
	}
	schedules, summary := ScheduleDay(day, items, nil, legs, 2, nil)
	want := []ItemSchedule{
		{ArrivalMinutes: 540, DepartureMinutes: 540},
		{ArrivalMinutes: 570, DepartureMinutes: 630},
		{ArrivalMinutes: 720, DepartureMinutes: 765},
		{ArrivalMinutes: 765, DepartureMinutes: 795, Late: true},
		{ArrivalMinutes: 795, DepartureMinutes: 795},
	}
	for i := range want {
		if schedules[i] != want[i] {
			t.Errorf("item %d: got %+v, want %+v", i, schedules[i], want[i])
		}
	}
	if summary.VisitMinutes != 135 || summary.TravelMinutes != 45 || summary.EndMinutes != 795 ||
		summary.PlannedCost != 6800 || summary.DistanceM != 25000 ||
		!summary.UnknownTravel || summary.ByMode[ModeWalk].DurationS != 900 {
		t.Errorf("summary: %+v", summary)
	}
}

// TestDayItemsOrder checks marks frame the places whatever their positions.
func TestDayItemsOrder(t *testing.T) {
	dayID := uuid.New()
	other := uuid.New()
	items := []Item{
		{Name: "evening", Kind: ItemStayAnchor, Anchor: AnchorEvening, DayID: &dayID},
		{Name: "second", Kind: ItemPlace, Position: 1, DayID: &dayID},
		{Name: "unassigned", Kind: ItemPlace},
		{Name: "first", Kind: ItemPlace, Position: 0, DayID: &dayID},
		{Name: "elsewhere", Kind: ItemPlace, DayID: &other},
		{Name: "morning", Kind: ItemStayAnchor, Anchor: AnchorMorning, DayID: &dayID, Position: 9},
	}
	var names []string
	for _, item := range DayItems(items, &dayID) {
		names = append(names, item.Name)
	}
	if got := len(names); got != 4 || names[0] != "morning" || names[1] != "first" || names[3] != "evening" {
		t.Errorf("order: %v", names)
	}
	if unassigned := DayItems(items, nil); len(unassigned) != 1 || unassigned[0].Name != "unassigned" {
		t.Errorf("unassigned: %v", unassigned)
	}
}

// TestPlaceNormalize checks defaults and the rules on a place's fields.
func TestPlaceNormalize(t *testing.T) {
	lat := 63.6156
	place, err := Item{Name: " Seljalandsfoss ", Category: CategoryNature}.NormalizePlace(DocumentPlan)
	if err != nil || place.Name != "Seljalandsfoss" || place.CostCategory != CostActivities {
		t.Errorf("valid place: %+v %v", place, err)
	}
	cases := map[string]struct {
		item Item
		want string
	}{
		"no name":      {Item{Name: ""}, "name:required"},
		"half coords":  {Item{Name: "x", Lat: &lat}, "lng:coordinates_incomplete"},
		"bad url":      {Item{Name: "x", URL: "javascript:alert(1)"}, "url:invalid_url"},
		"long visit":   {Item{Name: "x", VisitMinutes: 2000}, "visit_minutes:out_of_range"},
		"bad category": {Item{Name: "x", Category: "bar"}, "category:unsupported"},
	}
	for name, tc := range cases {
		_, err := tc.item.NormalizePlace(DocumentPlan)
		if got := validationCode(t, err); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
	if CategoryMuseum.DefaultVisitMinutes() != 120 {
		t.Error("museum default visit")
	}
}

// TestStayNormalize checks a stay needs a night and a known kind.
func TestStayNormalize(t *testing.T) {
	valid := stay(t, "2026-06-20", "2026-06-21")
	if normalized, err := valid.Normalize(DocumentPlan); err != nil || normalized.Kind != StayHotel {
		t.Errorf("valid stay: %+v %v", normalized, err)
	}
	sameDay := stay(t, "2026-06-20", "2026-06-20")
	if _, err := sameDay.Normalize(DocumentPlan); validationCode(t, err) != "check_out_date:end_before_start" {
		t.Errorf("zero nights accepted: %v", err)
	}
	if clock, err := ParseClockTime("t", "23:59"); err != nil || clock.String() != "23:59" {
		t.Errorf("clock: %v %v", clock, err)
	}
	if _, err := ParseClockTime("t", "24:00"); validationCode(t, err) != "t:invalid_time" {
		t.Errorf("24:00 accepted: %v", err)
	}
}

// TestNormalizeActivity checks what an activity is given and refused.
func TestNormalizeActivity(t *testing.T) {
	activity, err := Item{Name: "Canyon", Kind: ItemActivity}.NormalizePlace(DocumentPlan)
	if err != nil || activity.ActivityType != ActivityOther || activity.Category != CategoryActivity {
		t.Errorf("an activity with no type: %+v %v", activity, err)
	}
	if _, err := (Item{Name: "x", Kind: ItemActivity, ActivityType: "flying"}).NormalizePlace(
		DocumentPlan); validationCode(t, err) != "activity_type:unsupported" {
		t.Errorf("an unknown activity type: %v", err)
	}
	if _, err := (Item{Name: "x", Kind: ItemStayAnchor}).NormalizePlace(DocumentPlan); validationCode(t,
		err) != "kind:unsupported" {
		t.Errorf("a stay mark through the place rules: %v", err)
	}
	// A place that stops being an activity forgets what kind it was.
	place, err := Item{Name: "x", Kind: ItemPlace, ActivityType: ActivityHike}.NormalizePlace(DocumentPlan)
	if err != nil || place.ActivityType != "" {
		t.Errorf("a place with an activity type: %+v %v", place, err)
	}
	for _, activity := range ActivityTypes {
		if activity.DefaultVisitMinutes() <= 0 {
			t.Errorf("%q has no default length", activity)
		}
	}
	if !ItemActivity.IsVisit() || ItemStayAnchor.IsVisit() {
		t.Error("only places and activities are visits")
	}
}
