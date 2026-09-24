package domain

import (
	"testing"

	"github.com/google/uuid"
)

// TestReconcileLegs checks legs follow neighbours: kept with their data,
// created with an inherited mode, reset when a point moved, and deleted.
func TestReconcileLegs(t *testing.T) {
	dayID := uuid.New()
	walk := ModeWalk
	lat, lng := 64.1, -21.9
	moved := 63.4
	a := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &dayID, Position: 0, Lat: &lat, Lng: &lng}
	b := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &dayID, Position: 1, Lat: &lat, Lng: &lng}
	c := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &dayID, Position: 2}
	content := DocumentContent{
		Days:  []Day{{ID: dayID, DefaultMode: &walk}},
		Items: []Item{a, b, c},
	}

	ids := 0
	newID := func() uuid.UUID { ids++; return uuid.New() }
	plan := ReconcileLegs(content, nil, newID)
	if len(plan.Create) != 2 || plan.Create[0].Mode != ModeWalk || plan.Create[1].Input != "walk|64.10000,-21.90000|?" {
		t.Fatalf("first plan: %+v", plan.Create)
	}

	// a moves: a→b goes back to pending, b→c is untouched.
	existing := []Leg{plan.Create[0], plan.Create[1]}
	existing[0].Mode = ModeBike
	existing[0].Input = LegInput(ModeBike, &Point{lat, lng}, &Point{lat, lng})
	content.Items[0].Lat = &moved
	plan = ReconcileLegs(content, existing, newID)
	if len(plan.Reset) != 1 || plan.Reset[0].ID != existing[0].ID || plan.Reset[0].Input != "bike|63.40000,-21.90000|64.10000,-21.90000" ||
		len(plan.Create) != 0 || len(plan.Delete) != 0 {
		t.Errorf("moved point: %+v", plan)
	}

	// c moves before b: both old legs go; a→c inherits the mode of the old
	// leg that started at a, c→b takes the day's mode.
	existing[0].Input = plan.Reset[0].Input
	content.Items[1].Position = 2
	content.Items[2].Position = 1
	plan = ReconcileLegs(content, existing, newID)
	if len(plan.Delete) != 2 || len(plan.Create) != 2 || len(plan.Reset) != 0 {
		t.Fatalf("reorder: %+v", plan)
	}
	if plan.Create[0].FromItemID != a.ID || plan.Create[0].Mode != ModeBike || plan.Create[1].Mode != ModeWalk {
		t.Errorf("reorder modes: %+v", plan.Create)
	}
}

// TestLegValues checks typed values win over calculated ones.
func TestLegValues(t *testing.T) {
	auto, typed := 1000, 1500
	leg := Leg{DistanceM: &auto, DurationS: &auto, ManualDistanceM: &typed}
	if *leg.Distance() != 1500 || *leg.Duration() != 1000 {
		t.Errorf("values: %v %v", *leg.Distance(), *leg.Duration())
	}
	if _, err := (Leg{Mode: "boat"}).Normalize(DocumentPlan); validationCode(t, err) != "mode:unsupported" {
		t.Errorf("bad mode accepted: %v", err)
	}
}

// TestItemPoint checks a stay mark takes its stay's position.
func TestItemPoint(t *testing.T) {
	lat, lng := 63.4, -19.0
	stay := Stay{ID: uuid.New(), Lat: &lat, Lng: &lng}
	anchor := Item{Kind: ItemStayAnchor, StayID: &stay.ID}
	point := ItemPoint(anchor, map[uuid.UUID]Stay{stay.ID: stay})
	if point == nil || point.Lat != lat {
		t.Errorf("anchor point: %v", point)
	}
	if ItemPoint(Item{Kind: ItemPlace}, nil) != nil {
		t.Error("a place without coordinates has a point")
	}
}

// TestLegEndsFollowTracks checks a journey leaves a recorded place where its
// recording ended and reaches one where it began, which is what keeps two hikes
// that meet from being joined by a road retracing the first; and that adding or
// removing a recording sends the journey back to be calculated.
func TestLegEndsFollowTracks(t *testing.T) {
	dayID := uuid.New()
	pinLat, pinLng := 64.0, -19.0
	first := Item{ID: uuid.New(), Kind: ItemActivity, DayID: &dayID, Position: 0, Lat: &pinLat, Lng: &pinLng}
	second := Item{ID: uuid.New(), Kind: ItemActivity, DayID: &dayID, Position: 1, Lat: &pinLat, Lng: &pinLng}
	tracks := []Track{
		{ItemID: first.ID, Geometry: EncodePolyline([]Point{{63.99, -19.06}, {63.92, -19.15}, {63.8577, -19.22738}})},
		{ItemID: second.ID, Geometry: EncodePolyline([]Point{{63.85705, -19.22705}, {63.7662, -19.37383}})},
	}

	start, end := LegEnds(first, second, nil, tracks)
	if start == nil || *start != (Point{63.8577, -19.22738}) || end == nil || *end != (Point{63.85705, -19.22705}) {
		t.Errorf("ends with tracks: %v %v", start, end)
	}
	start, end = LegEnds(first, second, nil, nil)
	if start == nil || *start != (Point{pinLat, pinLng}) || end == nil || *end != (Point{pinLat, pinLng}) {
		t.Errorf("ends without tracks: %v %v", start, end)
	}

	content := DocumentContent{Days: []Day{{ID: dayID}}, Items: []Item{first, second}}
	plan := ReconcileLegs(content, nil, uuid.New)
	if len(plan.Create) != 1 || plan.Create[0].Input != "car|64.00000,-19.00000|64.00000,-19.00000" {
		t.Fatalf("without tracks: %+v", plan.Create)
	}
	content.Tracks = tracks
	plan = ReconcileLegs(content, plan.Create, uuid.New)
	if len(plan.Reset) != 1 || plan.Reset[0].Input != "car|63.85770,-19.22738|63.85705,-19.22705" {
		t.Errorf("with tracks: %+v", plan)
	}
}

// TestRetryableEstimate checks which legs are worth asking the provider about a
// second time: the ones whose reason can pass, and no others.
func TestRetryableEstimate(t *testing.T) {
	cases := map[string]struct {
		leg  Leg
		want bool
	}{
		"the provider was off": {Leg{Source: LegEstimate, Error: LegErrorProviderDisabled}, true},
		"the minute limit":     {Leg{Source: LegEstimate, Error: LegErrorRateLimited}, true},
		"the daily limit":      {Leg{Source: LegEstimate, Error: LegErrorDailyLimit}, true},
		"the provider failed":  {Leg{Source: LegEstimate, Error: LegErrorProvider}, true},

		// A road that does not exist will not appear because somebody entered a
		// key, and a flight is a straight line on purpose.
		"no road between them":                {Leg{Source: LegEstimate, Error: LegErrorNoRoute}, false},
		"a flight":                            {Leg{Source: LegStraightLine}, false},
		"already routed":                      {Leg{Source: LegProvider}, false},
		"waiting anyway":                      {Leg{Source: LegPending}, false},
		"no position":                         {Leg{Source: LegMissingCoordinates}, false},
		"an estimate with no reason recorded": {Leg{Source: LegEstimate}, false},
	}
	for name, test := range cases {
		if got := test.leg.RetryableEstimate(); got != test.want {
			t.Errorf("%s: retryable = %v, want %v", name, got, test.want)
		}
	}
}

// TestReconcileLegsBetweenDays checks a day that does not open with a stay mark
// is brought in from the last element of the nearest earlier day with any,
// that an empty day in between is stepped over, and that a day opening with a
// stay mark needs no such leg.
func TestReconcileLegsBetweenDays(t *testing.T) {
	first, empty, third, fourth := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	stayID := uuid.New()
	camp := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &first, Position: 0}
	hut := Item{ID: uuid.New(), Kind: ItemActivity, DayID: &first, Position: 1}
	canyon := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &third, Position: 0}
	evening := Item{ID: uuid.New(), Kind: ItemStayAnchor, DayID: &third, Anchor: AnchorEvening, StayID: &stayID}
	morning := Item{ID: uuid.New(), Kind: ItemStayAnchor, DayID: &fourth, Anchor: AnchorMorning, StayID: &stayID}
	beach := Item{ID: uuid.New(), Kind: ItemPlace, DayID: &fourth, Position: 0}
	content := DocumentContent{
		Days:  []Day{{ID: first}, {ID: empty, Position: 1}, {ID: third, Position: 2}, {ID: fourth, Position: 3}},
		Items: []Item{camp, hut, canyon, evening, morning, beach},
		Stays: []Stay{{ID: stayID}},
	}

	plan := ReconcileLegs(content, nil, uuid.New)
	pairs := map[[2]uuid.UUID]uuid.UUID{}
	for _, leg := range plan.Create {
		pairs[[2]uuid.UUID{leg.FromItemID, leg.ToItemID}] = leg.DayID
	}
	if day, ok := pairs[[2]uuid.UUID{hut.ID, canyon.ID}]; !ok || day != third {
		t.Errorf("no leg in from the day before the empty one, owned by the later day: %v", pairs)
	}
	if _, ok := pairs[[2]uuid.UUID{evening.ID, morning.ID}]; ok {
		t.Error("a day that opens with a stay mark was brought in from the evening before")
	}
	if len(plan.Create) != 4 {
		t.Errorf("want camp→hut, hut→canyon, canyon→evening, morning→beach; got %d legs", len(plan.Create))
	}

	// Once stored, the opening leg is kept like any other.
	again := ReconcileLegs(content, plan.Create, uuid.New)
	if len(again.Create) != 0 || len(again.Delete) != 0 {
		t.Errorf("a second pass changed the legs: %+v", again)
	}
	items := DayItems(content.Items, &third)
	if leg := OpeningLeg(plan.Create, items); leg == nil || leg.FromItemID != hut.ID {
		t.Errorf("the opening leg of the third day is %+v", leg)
	}
	if leg := OpeningLeg(plan.Create, DayItems(content.Items, &fourth)); leg != nil {
		t.Errorf("a day opening with a stay mark has an opening leg: %+v", leg)
	}
}
