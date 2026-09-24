package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LegSource says where a leg's calculated values came from.
type LegSource string

// Leg sources.
const (
	// LegPending waits for a calculation after its mode or points changed.
	LegPending LegSource = "pending"
	// LegProvider follows roads, as the routing provider returned.
	LegProvider LegSource = "provider"
	// LegStraightLine is the great-circle line flights and "other" legs use.
	LegStraightLine LegSource = "straight_line"
	// LegEstimate is a straight line scaled up, used when the provider could
	// not answer; the leg's error says why.
	LegEstimate LegSource = "estimate"
	// LegMissingCoordinates cannot be calculated: a point has no position.
	LegMissingCoordinates LegSource = "missing_coordinates"
)

// Reasons a leg holds an estimate instead of a road route.
const (
	LegErrorProviderDisabled = "provider_disabled"
	LegErrorRateLimited      = "rate_limited"
	LegErrorDailyLimit       = "daily_limit"
	LegErrorNoRoute          = "no_route"
	LegErrorProvider         = "provider_error"
)

// retryableEstimates are the reasons an estimate may become a real route on a
// later attempt: the provider was off, its limits were reached, or it failed.
// A leg with no road route between its points is not among them - a road that
// does not exist will not appear because somebody configured a key - and
// neither is a straight line, which is what a flight is meant to be.
var retryableEstimates = map[string]bool{
	LegErrorProviderDisabled: true,
	LegErrorRateLimited:      true,
	LegErrorDailyLimit:       true,
	LegErrorProvider:         true,
}

// RetryableEstimate - reports whether this leg is worth asking the provider
// about again.
//
// It is what turns a plan full of straight lines back into roads once the
// provider that was missing is configured, without asking for every leg of the
// trip again.
//
// Returns:
//   - true for an estimate whose reason may have passed.
func (l Leg) RetryableEstimate() bool {
	return l.Source == LegEstimate && retryableEstimates[l.Error]
}

// maxLegNoteLength bounds a leg's note, such as "bus Strætó 51".
const maxLegNoteLength = 500

// Leg is the journey between two neighbouring elements of a day.
type Leg struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	DayID      uuid.UUID
	FromItemID uuid.UUID
	ToItemID   uuid.UUID
	Mode       TravelMode
	// DistanceM, DurationS and Geometry are the calculated values; nil when
	// not known.
	DistanceM *int
	DurationS *int
	Geometry  string
	Source    LegSource
	Error     string
	// Input is the mode and points the calculated values belong to.
	Input        string
	CalculatedAt *time.Time
	// ManualDistanceM and ManualDurationS override the calculated values.
	ManualDistanceM *int
	ManualDurationS *int
	PlannedCost     *Money
	// ActualCost belongs to a report; in a plan it stays empty.
	ActualCost *Money
	Note       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Distance - reports the distance the plan uses: the typed one, else the
// calculated one.
//
// Returns:
//   - metres, or nil when unknown.
func (l Leg) Distance() *int {
	if l.ManualDistanceM != nil {
		return l.ManualDistanceM
	}
	return l.DistanceM
}

// Duration - reports the travel time the plan uses: the typed one, else the
// calculated one.
//
// Returns:
//   - seconds, or nil when unknown, as for an "other" leg nobody timed.
func (l Leg) Duration() *int {
	if l.ManualDurationS != nil {
		return l.ManualDurationS
	}
	return l.DurationS
}

// Normalize - trims a leg's note and checks its editable fields.
//
// Arguments:
//   - kind: the document the leg belongs to; a plan may not carry an actual cost.
//
// Returns:
//   - the normalised leg.
//   - the first *ValidationError found.
func (l Leg) Normalize(kind DocumentKind) (Leg, error) {
	if kind == DocumentPlan && l.ActualCost != nil {
		return l, reportOnly("actual_cost_amount")
	}
	if err := ValidateTravelMode("mode", l.Mode); err != nil {
		return l, err
	}
	if l.ManualDistanceM != nil && (*l.ManualDistanceM < 0 || *l.ManualDistanceM > 50_000_000) {
		return l, NewValidationError("distance_m", "out_of_range", "must be between 0 and 50 000 km")
	}
	if l.ManualDurationS != nil && (*l.ManualDurationS < 0 || *l.ManualDurationS > 7*24*3600) {
		return l, NewValidationError("duration_s", "out_of_range", "must be between 0 and 7 days")
	}
	l.Note = strings.TrimSpace(l.Note)
	return l, checkLength("note", l.Note, maxLegNoteLength)
}

// LegCalculation is a calculated leg, ready to be stored.
type LegCalculation struct {
	DistanceM *int
	DurationS *int
	Geometry  string
	Source    LegSource
	Error     string
}

// Point is a position on Earth.
type Point struct {
	Lat float64
	Lng float64
}

// ItemPoint - finds where an element of a day is: a place's own coordinates,
// or its stay's for a stay mark.
//
// Arguments:
//   - item: the element.
//   - stays: the document's stays by identifier.
//
// Returns:
//   - the position, or nil when it has none.
func ItemPoint(item Item, stays map[uuid.UUID]Stay) *Point {
	lat, lng := item.Lat, item.Lng
	if item.Kind == ItemStayAnchor && item.StayID != nil {
		stay := stays[*item.StayID]
		lat, lng = stay.Lat, stay.Lng
	}
	if lat == nil || lng == nil {
		return nil
	}
	return &Point{Lat: *lat, Lng: *lng}
}

// LegInput - describes what a leg's calculation depends on: the mode and both
// points, rounded as the route cache rounds them.
//
// Arguments:
//   - mode: the travel mode.
//   - from, to: the points, nil when unknown.
//
// Returns:
//   - a string that changes exactly when a recalculation is needed.
func LegInput(mode TravelMode, from, to *Point) string {
	point := func(p *Point) string {
		if p == nil {
			return "?"
		}
		return fmt.Sprintf("%.5f,%.5f", p.Lat, p.Lng)
	}
	return string(mode) + "|" + point(from) + "|" + point(to)
}

// LegPlan is what bringing a document's legs in line with its days requires.
type LegPlan struct {
	// Create holds the legs for neighbours that have none yet.
	Create []Leg
	// Reset holds existing legs whose mode or points changed, with the new
	// input; their calculated values are discarded.
	Reset []Leg
	// Delete lists legs between elements that are no longer neighbours.
	Delete []uuid.UUID
}

// ReconcileLegs - works out the legs a document's days need.
//
// Every pair of neighbouring elements in a day has exactly one leg. A day that
// does not open with a stay mark also has an opening leg, from the last element
// of the nearest earlier day that has any: without a stay nothing else says how
// the trip got from one day to the next, and a campsite entered as a place, or
// a night on a bus, is not a stay. A day that opens with a stay mark needs no
// such leg, because the evening before ended at the same stay. The opening leg
// belongs to the later day, whose morning it takes.
//
// A pair that already had a leg keeps it, with its mode, typed values, cost and
// note. A new leg takes the mode of the old leg that started at the same
// element, else the day's default mode, else ModeCar.
//
// Arguments:
//   - content: the document.
//   - existing: its current legs.
//   - newID: generates identifiers for new legs.
//
// Returns:
//   - the changes to make.
func ReconcileLegs(content DocumentContent, existing []Leg, newID func() uuid.UUID) LegPlan {
	stays := make(map[uuid.UUID]Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	type pair struct{ from, to uuid.UUID }
	byPair := make(map[pair]Leg, len(existing))
	modeFrom := make(map[uuid.UUID]TravelMode, len(existing))
	for _, leg := range existing {
		byPair[pair{leg.FromItemID, leg.ToItemID}] = leg
		modeFrom[leg.FromItemID] = leg.Mode
	}

	var plan LegPlan
	kept := make(map[uuid.UUID]bool, len(existing))
	// need keeps or creates the leg between two elements, owned by a day.
	need := func(day Day, from, to Item) {
		points := func(mode TravelMode) string {
			return LegInput(mode, ItemPoint(from, stays), ItemPoint(to, stays))
		}
		if leg, ok := byPair[pair{from.ID, to.ID}]; ok && leg.DayID == day.ID {
			kept[leg.ID] = true
			if input := points(leg.Mode); input != leg.Input {
				leg.Input = input
				plan.Reset = append(plan.Reset, leg)
			}
			return
		}
		mode, ok := modeFrom[from.ID]
		if !ok {
			mode = ModeCar
			if day.DefaultMode != nil {
				mode = *day.DefaultMode
			}
		}
		plan.Create = append(plan.Create, Leg{
			ID: newID(), DocumentID: content.Document.ID, DayID: day.ID,
			FromItemID: from.ID, ToItemID: to.ID, Mode: mode,
			Source: LegPending, Input: points(mode),
		})
	}

	// previous is the last element of the nearest earlier day that has any.
	var previous *Item
	for _, day := range content.Days {
		items := DayItems(content.Items, &day.ID)
		if len(items) == 0 {
			continue
		}
		if previous != nil && items[0].Anchor != AnchorMorning {
			need(day, *previous, items[0])
		}
		for index := 1; index < len(items); index++ {
			need(day, items[index-1], items[index])
		}
		last := items[len(items)-1]
		previous = &last
	}
	for _, leg := range existing {
		if !kept[leg.ID] {
			plan.Delete = append(plan.Delete, leg.ID)
		}
	}
	return plan
}

// OpeningLeg - finds the leg that brings a day in from the day before.
//
// Arguments:
//   - legs: the document's legs.
//   - items: the day's elements in schedule order.
//
// Returns:
//   - the leg that ends at the day's first element, or nil when the day opens
//     with a stay mark, has no elements or waits for its leg to be created.
func OpeningLeg(legs []Leg, items []Item) *Leg {
	if len(items) == 0 {
		return nil
	}
	for index := range legs {
		if legs[index].ToItemID == items[0].ID {
			return &legs[index]
		}
	}
	return nil
}

// DayLegs - lines a day's legs up with its elements.
//
// Arguments:
//   - legs: the document's legs.
//   - items: the day's elements in schedule order.
//
// Returns:
//   - len(items)-1 entries, the leg from each element to the next, nil where
//     none exists yet.
func DayLegs(legs []Leg, items []Item) []*Leg {
	if len(items) < 2 {
		return nil
	}
	byFrom := make(map[[2]uuid.UUID]*Leg, len(legs))
	for index := range legs {
		byFrom[[2]uuid.UUID{legs[index].FromItemID, legs[index].ToItemID}] = &legs[index]
	}
	aligned := make([]*Leg, len(items)-1)
	for index := 1; index < len(items); index++ {
		aligned[index-1] = byFrom[[2]uuid.UUID{items[index-1].ID, items[index].ID}]
	}
	return aligned
}
