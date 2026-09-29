package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// intOf returns a pointer to a whole number, for the tests.
func intOf(value int) *int {
	return &value
}

// TestFoldCompositeLeg checks a leg with changes reads as one leg: the parts'
// distances added up, their times and the waits added up, the tickets' costs
// added up, and the state of the part that says the least.
func TestFoldCompositeLeg(t *testing.T) {
	ticket := uuid.New()
	lat, lng := 38.71, -9.14
	train := Money(460)
	bus := Money(390)
	leg := Leg{
		Mode: ModeCar, ManualDurationS: intOf(1), PlannedCost: &train,
		Segments: []LegSegment{
			{Mode: ModeWalk, DistanceM: intOf(900), DurationS: intOf(600), Source: LegProvider,
				Geometry: EncodePolyline([]Point{{38.7, -9.1}, {38.71, -9.14}}), StopName: "Rossio", StopLat: &lat,
				StopLng: &lng, WaitMinutes: 10},
			{Mode: ModeTrain, DistanceM: intOf(25000), ManualDurationS: intOf(2400), Source: LegStraightLine,
				TicketID: &ticket, Geometry: EncodePolyline([]Point{{38.71, -9.14}, {38.8, -9.38}})},
		},
		Tickets: []LegTicket{{ID: ticket, PlannedCost: &train}, {ID: uuid.New(), PlannedCost: &bus}},
	}
	folded := leg.Fold()
	if folded.Mode != ModeWalk || *folded.Distance() != 25900 || *folded.Duration() != 600+600+2400 {
		t.Errorf("totals: %s %v %v", folded.Mode, *folded.Distance(), *folded.Duration())
	}
	if folded.Source != LegStraightLine || folded.ManualDurationS != nil || *folded.PlannedCost != 850 {
		t.Errorf("state and cost: %s %v %v", folded.Source, folded.ManualDurationS, *folded.PlannedCost)
	}
	if line := DecodePolyline(folded.Geometry, 5); len(line) != 4 {
		t.Errorf("line: %v", line)
	}

	// One part without a time makes the whole time unknown, one waiting part
	// makes the leg wait.
	leg.Segments[1].ManualDurationS = nil
	leg.Segments[1].Source = LegPending
	folded = leg.Fold()
	if folded.Duration() != nil || folded.Source != LegPending {
		t.Errorf("unknown part: %v %s", folded.Duration(), folded.Source)
	}

	// A plain leg is left alone.
	plain := Leg{Mode: ModeCar, ManualDurationS: intOf(60)}
	if plain.Fold().ManualDurationS == nil {
		t.Error("a plain leg was folded")
	}
}

// TestLegTravelByMode checks a leg with changes counts each part under its own
// mode, and a plain leg counts once.
func TestLegTravelByMode(t *testing.T) {
	leg := Leg{Segments: []LegSegment{
		{Mode: ModeWalk, ManualDurationS: intOf(300)},
		{Mode: ModeTrain, ManualDurationS: intOf(3600)},
	}}
	travel := leg.Travel()
	if len(travel) != 2 || travel[0].Mode != ModeWalk || *travel[1].DurationS != 3600 {
		t.Errorf("composite: %+v", travel)
	}
	plain := Leg{Mode: ModeCar, ManualDurationS: intOf(60)}
	if travel := plain.Travel(); len(travel) != 1 || travel[0].Mode != ModeCar {
		t.Errorf("plain: %+v", travel)
	}
}

// TestSegmentEnds checks the parts run from the leg's start through each
// change to the leg's end, a change without a position leaving both of its
// parts one end short.
func TestSegmentEnds(t *testing.T) {
	lat, lng := 38.71, -9.14
	start, end := &Point{38.7, -9.1}, &Point{38.8, -9.38}
	segments := []LegSegment{
		{StopLat: &lat, StopLng: &lng},
		{StopName: "unknown"},
		{},
	}
	starts, ends := SegmentEnds(segments, start, end)
	if starts[0] != start || *ends[0] != (Point{lat, lng}) || *starts[1] != (Point{lat, lng}) {
		t.Errorf("first change: %v %v", starts, ends)
	}
	if ends[1] != nil || starts[2] != nil || ends[2] != end {
		t.Errorf("change without a position: %v %v", starts, ends)
	}
}

// TestNormalizeSegments checks the rules of a journey with changes: every
// change named, the last part without a stop, tickets known and actual costs
// in a report only.
func TestNormalizeSegments(t *testing.T) {
	ticket := LegTicket{ID: uuid.New(), Name: "  Swiss pass "}
	lat, lng := 46.9, 7.4
	valid := func() []LegSegment {
		return []LegSegment{
			{Mode: ModeTrain, TicketID: &ticket.ID, StopName: " Bern ", StopLat: &lat, StopLng: &lng, WaitMinutes: 12},
			{Mode: ModeBus, TicketID: &ticket.ID, StopName: "dropped", WaitMinutes: 5},
		}
	}
	segments, tickets, err := NormalizeSegments(DocumentPlan, valid(), []LegTicket{ticket})
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if segments[0].StopName != "Bern" || segments[1].StopName != "" || segments[1].WaitMinutes != 0 ||
		segments[1].Position != 1 || tickets[0].Name != "Swiss pass" {
		t.Errorf("normalised: %+v %+v", segments, tickets)
	}

	cases := map[string]func([]LegSegment, []LegTicket) ([]LegSegment, []LegTicket){
		"segments.stop_name:required": func(s []LegSegment, k []LegTicket) ([]LegSegment, []LegTicket) {
			s[0].StopName = " "
			return s, k
		},
		"segments.mode:unsupported": func(s []LegSegment, k []LegTicket) ([]LegSegment, []LegTicket) {
			s[1].Mode = "rocket"
			return s, k
		},
		"segments.ticket:unknown_ticket": func(s []LegSegment, _ []LegTicket) ([]LegSegment, []LegTicket) {
			return s, nil
		},
		"segments.wait_minutes:out_of_range": func(s []LegSegment, k []LegTicket) ([]LegSegment, []LegTicket) {
			s[0].WaitMinutes = 2000
			return s, k
		},
		"segments.stop:invalid_point": func(s []LegSegment, k []LegTicket) ([]LegSegment, []LegTicket) {
			s[0].StopLng = nil
			return s, k
		},
		"tickets.name:too_long": func(s []LegSegment, k []LegTicket) ([]LegSegment, []LegTicket) {
			k[0].Name = strings.Repeat("x", 201)
			return s, k
		},
	}
	for want, change := range cases {
		segments, tickets := change(valid(), []LegTicket{ticket})
		if _, _, err := NormalizeSegments(DocumentPlan, segments, tickets); validationCode(t, err) != want {
			t.Errorf("%s: %v", want, err)
		}
	}

	spent := Money(100)
	paid := ticket
	paid.ActualCost = &spent
	if _, _, err := NormalizeSegments(DocumentPlan, valid(), []LegTicket{paid}); err == nil {
		t.Error("a plan's ticket took an actual cost")
	}
	if _, _, err := NormalizeSegments(DocumentReport, valid(), []LegTicket{paid}); err != nil {
		t.Errorf("a report's ticket refused its actual cost: %v", err)
	}
}

// TestReconcileCompositeLeg checks a leg with changes keeps its parts when its
// places stay, and sends back to pending only the part whose end moved.
func TestReconcileCompositeLeg(t *testing.T) {
	dayID := uuid.New()
	lat, lng := 38.7, -9.1
	stopLat, stopLng := 38.71, -9.14
	endLat, endLng := 38.8, -9.38
	a := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &dayID, Position: 0, Lat: &lat, Lng: &lng}
	b := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &dayID, Position: 1, Lat: &endLat, Lng: &endLng}
	content := DocumentContent{Days: []Day{{ID: dayID}}, Items: []Item{a, b}}
	stop := &Point{stopLat, stopLng}
	leg := Leg{
		ID: uuid.New(), DayID: dayID, FromItemID: a.ID, ToItemID: b.ID, Mode: ModeWalk, Input: "stale",
		Segments: []LegSegment{
			{ID: uuid.New(), Mode: ModeWalk, StopName: "Rossio", StopLat: &stopLat, StopLng: &stopLng,
				Input: SegmentInput(ModeWalk, &Point{lat, lng}, stop)},
			{ID: uuid.New(), Mode: ModeTrain, Input: SegmentInput(ModeTrain, stop, &Point{endLat, endLng})},
		},
	}
	newID := func() uuid.UUID { return uuid.New() }
	plan := ReconcileLegs(content, []Leg{leg}, newID)
	if len(plan.Reset) != 0 || len(plan.ResetSegments) != 0 || len(plan.Create) != 0 || len(plan.Delete) != 0 {
		t.Fatalf("unchanged: %+v", plan)
	}

	moved := 38.9
	content.Items[1].Lat = &moved
	plan = ReconcileLegs(content, []Leg{leg}, newID)
	if len(plan.ResetSegments) != 1 || plan.ResetSegments[0].ID != leg.Segments[1].ID ||
		plan.ResetSegments[0].Input != SegmentInput(ModeTrain, stop, &Point{moved, endLng}) || len(plan.Reset) != 0 {
		t.Errorf("moved end: %+v", plan)
	}
}
