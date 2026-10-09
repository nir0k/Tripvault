package domain

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// A stop is somewhere along the line of an activity: a café halfway up a hike,
// a rest by a lake, a spring to fill the bottles at. It is part of the
// activity, not a place of the day, so the day's legs and schedule never run
// through it; it hangs on the activity's track and goes with it. Its point lies
// on the line, and how far along the line it is says where on the way it comes.
//
// A stop costs like a place - a lunch at the café - and is counted in the
// activity's day: in the day's total, in the budget and in what the members owe
// each other. In a report it also keeps when it was reached.

// StopKind says what a stop is for.
type StopKind string

// Stop kinds.
const (
	// StopFood is a café or a restaurant.
	StopFood      StopKind = "food"
	StopShop      StopKind = "shop"
	StopRest      StopKind = "rest"
	StopViewpoint StopKind = "viewpoint"
	StopWater     StopKind = "water"
	StopShelter   StopKind = "shelter"
	// StopHut is a mountain hut, staffed or not.
	StopHut    StopKind = "hut"
	StopSummit StopKind = "summit"
	StopCave   StopKind = "cave"
	// StopSwim is somewhere to bathe: a lake, a river, a hot spring.
	StopSwim  StopKind = "swim"
	StopOther StopKind = "other"
)

// StopKinds lists every kind of stop, in the order a form offers them.
var StopKinds = []StopKind{StopFood, StopShop, StopRest, StopViewpoint, StopWater, StopShelter, StopHut, StopSummit,
	StopCave, StopSwim, StopOther}

// MaxActivityStops bounds the stops of one activity; a long day on a trail has
// a handful.
const MaxActivityStops = 50

// Stop is one stop along the line of an activity.
type Stop struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	// ItemID is the activity whose line the stop lies on.
	ItemID uuid.UUID
	Kind   StopKind
	// Name names the stop, such as the café; empty when its kind says enough.
	Name   string
	NoteMD string
	// Point is where the stop lies, on the line.
	Point Point
	// DistanceM is how far along the line from its start the stop lies.
	DistanceM int
	// GradesTo is how many metres of the line up to the stop run at each slope,
	// as Track.Grades are for the whole line; nil when the file records no
	// heights.
	GradesTo []int
	// ActualTime is when the stop was reached; a report's only.
	ActualTime *ClockTime
	// The cost of the stop, as a place's: what it was planned at, in a report
	// what was really spent, and who pays it and how it is shared.
	PlannedCost   *Money
	ActualCost    *Money
	CostPerPerson bool
	CostCategory  CostCategory
	CostNote      string
	PaidBy        *uuid.UUID
	CostSplit     CostSplit
	CostShares    []CostShare
	CreatedAt     time.Time
}

// Normalize - trims a stop and checks it can be stored in a document of a kind.
//
// Arguments:
//   - kind: whether the stop belongs to a plan or a report; a plan's stop has
//     no time it was reached and no actual cost.
//
// Returns:
//   - the normalised stop, its cost category food unless chosen.
//   - the first *ValidationError found.
func (s Stop) Normalize(kind DocumentKind) (Stop, error) {
	if s.Kind == "" {
		s.Kind = StopOther
	}
	if !slices.Contains(StopKinds, s.Kind) {
		return s, NewValidationError("kind", "unsupported", "is not a known kind of stop")
	}
	var err error
	if s.Name, err = trimmedText("name", s.Name, maxNameLength); err != nil {
		return s, err
	}
	if err := checkLength("note_md", s.NoteMD, maxMarkdownLength); err != nil {
		return s, err
	}
	lat, lng := s.Point.Lat, s.Point.Lng
	if err := validateCoordinates(&lat, &lng); err != nil {
		return s, err
	}
	if s.CostCategory == "" {
		s.CostCategory = CostFood
	}
	if err := ValidateCostCategory("cost_category", s.CostCategory); err != nil {
		return s, err
	}
	if s.CostNote, err = trimmedText("cost_note", s.CostNote, maxShortText); err != nil {
		return s, err
	}
	if kind != DocumentReport {
		switch {
		case s.ActualTime != nil:
			return s, reportOnly("actual_time")
		case s.ActualCost != nil:
			return s, reportOnly("actual_cost_amount")
		}
	}
	if s.ActualTime != nil && (*s.ActualTime < 0 || *s.ActualTime >= minutesPerDay) {
		return s, NewValidationError("actual_time", "invalid_time", "must be a time in HH:MM form")
	}
	split, err := s.CostItem(Item{}).normalizeSplit()
	if err != nil {
		return s, err
	}
	s.CostSplit, s.CostShares = split.CostSplit, split.CostShares
	return s, nil
}

// CostItem - describes the cost of a stop as a place of the activity's day, so
// the budget, the day's total and the members' debts count it with the
// arithmetic of places.
//
// Arguments:
//   - activity: the activity the stop belongs to, whose day the cost is
//     counted in and which marks it optional when the activity is.
//
// Returns:
//   - an item carrying the stop's identity, name and cost.
func (s Stop) CostItem(activity Item) Item {
	return Item{
		ID:            s.ID,
		DocumentID:    s.DocumentID,
		DayID:         activity.DayID,
		Kind:          ItemPlace,
		Name:          s.Label(activity.Name),
		IsOptional:    activity.IsOptional,
		PlannedCost:   s.PlannedCost,
		ActualCost:    s.ActualCost,
		CostPerPerson: s.CostPerPerson,
		CostCategory:  s.CostCategory,
		CostNote:      s.CostNote,
		PaidBy:        s.PaidBy,
		CostSplit:     s.CostSplit,
		CostShares:    s.CostShares,
	}
}

// Label - names a stop where it is listed apart from its activity, as in the
// budget: the activity and the stop's own name.
//
// Arguments:
//   - activity: the activity's name.
//
// Returns:
//   - "Activity · Stop", or the activity's name alone for a stop without one.
func (s Stop) Label(activity string) string {
	if s.Name == "" {
		return activity
	}
	if activity == "" {
		return s.Name
	}
	return activity + " · " + s.Name
}

// ForReport - makes the copy of a plan's stop a report starts with: the stop
// and what it was planned to cost, without who pays or shares it, since the
// report's members are its own.
//
// Arguments:
//   - id: the copy's identifier.
//   - documentID: the report.
//   - itemID: the report's activity.
//
// Returns:
//   - the copy.
func (s Stop) ForReport(id, documentID, itemID uuid.UUID) Stop {
	s.ID, s.DocumentID, s.ItemID = id, documentID, itemID
	s.ActualTime, s.ActualCost = nil, nil
	s.PaidBy, s.CostSplit, s.CostShares = nil, SplitNone, nil
	return s
}

// StopsOf - lists the stops of an activity, in the order they come along its
// line.
//
// Arguments:
//   - tracks: the document's tracks, which carry the stops.
//   - itemID: the activity.
//
// Returns:
//   - the stops, nil for an activity without a line.
func StopsOf(tracks []Track, itemID uuid.UUID) []Stop {
	if track := TrackOfItem(tracks, itemID); track != nil {
		return track.Stops
	}
	return nil
}

// StopCostItems - describes the costs of every stop of a document as places of
// their activities' days, for the budget's debts.
//
// Arguments:
//   - content: the document.
//
// Returns:
//   - one item per stop whose activity is in the document.
func StopCostItems(content DocumentContent) []Item {
	activities := make(map[uuid.UUID]Item, len(content.Items))
	for _, item := range content.Items {
		activities[item.ID] = item
	}
	var items []Item
	for _, track := range content.Tracks {
		activity, ok := activities[track.ItemID]
		if !ok {
			continue
		}
		for _, stop := range track.Stops {
			items = append(items, stop.CostItem(activity))
		}
	}
	return items
}
