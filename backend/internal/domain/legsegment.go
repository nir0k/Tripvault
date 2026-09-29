package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A leg with changes on the way - a walk to the station, a train, a bus - is
// made of segments. A leg without changes has none and keeps everything in its
// own row, exactly as before segments existed; a leg gets segments only once it
// has at least one change, and loses them when it is back to one.
//
// A composite leg is folded when it is read: its distance, time, line, cost and
// state are worked out from its segments and put where a plain leg keeps them,
// so the schedule, the budget and the report read one leg either way. The
// segments stay beside it for what needs them one by one: the totals by mode,
// the map and the interface.

// Bounds of a composite leg.
const (
	// MaxLegSegments bounds the parts of one journey between two places.
	MaxLegSegments = 10
	// MaxLegTickets bounds the tickets bought for one journey.
	MaxLegTickets = 10
	// maxStopNameLength bounds the name of a change, such as a station.
	maxStopNameLength = 200
	// maxTicketNameLength bounds a ticket's name, such as "Swiss Travel Pass".
	maxTicketNameLength = 200
	// maxWaitMinutes bounds the wait at a change to a day.
	maxWaitMinutes = 24 * 60
)

// LegSegment is one part of a composite leg: travelled one way, from where the
// previous part ended - or the leg's start - to its stop - or the leg's end.
type LegSegment struct {
	ID       uuid.UUID
	LegID    uuid.UUID
	Position int
	Mode     TravelMode
	// DistanceM, DurationS and Geometry are the calculated values, as on a leg.
	DistanceM    *int
	DurationS    *int
	Geometry     string
	Source       LegSource
	Error        string
	Input        string
	CalculatedAt *time.Time
	// ManualDistanceM and ManualDurationS override the calculated values.
	ManualDistanceM *int
	ManualDurationS *int
	// TicketID is the ticket this part travels on; nil for a part nobody paid
	// for, such as a walk.
	TicketID *uuid.UUID
	// StopName, StopLat and StopLng are the change where this part ends and the
	// next begins: a station, a stop, a pier. The last part ends at the leg's
	// end and has no stop. A change without a position cannot be routed to.
	StopName string
	StopLat  *float64
	StopLng  *float64
	// WaitMinutes is the time spent at the change before the next part leaves.
	WaitMinutes int
}

// LegTicket is what was paid for one or more parts of a composite leg: a
// ticket for one train, or one for the whole journey. Which parts it covers is
// said by the parts.
type LegTicket struct {
	ID       uuid.UUID
	LegID    uuid.UUID
	Position int
	Name     string
	// PlannedCost is what the ticket costs in the plan; ActualCost what it
	// really cost, in a report only.
	PlannedCost *Money
	ActualCost  *Money
}

// Distance - reports the distance of a part: the typed one, else the calculated one.
//
// Returns:
//   - metres, or nil when unknown.
func (s LegSegment) Distance() *int {
	if s.ManualDistanceM != nil {
		return s.ManualDistanceM
	}
	return s.DistanceM
}

// Duration - reports the time of a part: the typed one, else the calculated one.
//
// Returns:
//   - seconds, or nil when unknown.
func (s LegSegment) Duration() *int {
	if s.ManualDurationS != nil {
		return s.ManualDurationS
	}
	return s.DurationS
}

// Stop - reports where a part ends when that is a change with a position.
//
// Returns:
//   - the change's position, or nil for the last part or a change without one.
func (s LegSegment) Stop() *Point {
	if s.StopLat == nil || s.StopLng == nil {
		return nil
	}
	return &Point{Lat: *s.StopLat, Lng: *s.StopLng}
}

// Composite - reports whether a leg is made of segments.
//
// Returns:
//   - true for a leg with at least one change.
func (l Leg) Composite() bool {
	return len(l.Segments) > 1
}

// SegmentEnds - finds where each part of a composite leg starts and ends: the
// leg's start, then each change in turn, then the leg's end.
//
// Arguments:
//   - segments: the parts in order.
//   - start, end: where the whole leg starts and ends, nil when unknown.
//
// Returns:
//   - the start and the end of every part, each nil when unknown.
func SegmentEnds(segments []LegSegment, start, end *Point) ([]*Point, []*Point) {
	starts := make([]*Point, len(segments))
	ends := make([]*Point, len(segments))
	previous := start
	for index, segment := range segments {
		starts[index] = previous
		if index == len(segments)-1 {
			ends[index] = end
		} else {
			ends[index] = segment.Stop()
		}
		previous = ends[index]
	}
	return starts, ends
}

// SegmentInput - describes what a part's calculation depends on: its mode and
// both ends, as a leg's does. A part is always routed the default way.
//
// Arguments:
//   - mode: the part's mode.
//   - from, to: its ends, nil when unknown.
//
// Returns:
//   - a string that changes exactly when a recalculation is needed.
func SegmentInput(mode TravelMode, from, to *Point) string {
	return LegInput(mode, from, to, LegRoute{})
}

// NormalizeSegments - trims and checks the parts and tickets of a composite
// leg, as a person described them.
//
// Arguments:
//   - kind: the document the leg belongs to; a plan carries no actual cost.
//   - segments: the parts in order; the last one's stop is dropped.
//   - tickets: the tickets; a part's ticket must be one of them.
//
// Returns:
//   - the normalised parts and tickets.
//   - the first *ValidationError found.
func NormalizeSegments(kind DocumentKind, segments []LegSegment, tickets []LegTicket) ([]LegSegment, []LegTicket, error) {
	if len(segments) > MaxLegSegments {
		return nil, nil, NewValidationError("segments", "too_many", "a leg has at most 10 parts")
	}
	if len(tickets) > MaxLegTickets {
		return nil, nil, NewValidationError("tickets", "too_many", "a leg has at most 10 tickets")
	}
	known := make(map[uuid.UUID]bool, len(tickets))
	for index := range tickets {
		ticket := &tickets[index]
		ticket.Position = index
		ticket.Name = strings.TrimSpace(ticket.Name)
		if utf8.RuneCountInString(ticket.Name) > maxTicketNameLength {
			return nil, nil, NewValidationError("tickets.name", "too_long", "must be at most 200 characters")
		}
		if kind == DocumentPlan && ticket.ActualCost != nil {
			return nil, nil, reportOnly("tickets.actual_cost_amount")
		}
		known[ticket.ID] = true
	}
	for index := range segments {
		segment := &segments[index]
		segment.Position = index
		if err := ValidateTravelMode("segments.mode", segment.Mode); err != nil {
			return nil, nil, err
		}
		if segment.ManualDistanceM != nil && (*segment.ManualDistanceM < 0 || *segment.ManualDistanceM > 50_000_000) {
			return nil, nil, NewValidationError("segments.distance_m", "out_of_range", "must be between 0 and 50 000 km")
		}
		if segment.ManualDurationS != nil && (*segment.ManualDurationS < 0 || *segment.ManualDurationS > 7*24*3600) {
			return nil, nil, NewValidationError("segments.duration_s", "out_of_range", "must be between 0 and 7 days")
		}
		if segment.TicketID != nil && !known[*segment.TicketID] {
			return nil, nil, NewValidationError("segments.ticket", "unknown_ticket", "must be one of the leg's tickets")
		}
		if index == len(segments)-1 {
			// The last part ends where the leg does.
			segment.StopName, segment.StopLat, segment.StopLng, segment.WaitMinutes = "", nil, nil, 0
			continue
		}
		segment.StopName = strings.TrimSpace(segment.StopName)
		if segment.StopName == "" {
			return nil, nil, NewValidationError("segments.stop_name", "required", "a change must be named")
		}
		if utf8.RuneCountInString(segment.StopName) > maxStopNameLength {
			return nil, nil, NewValidationError("segments.stop_name", "too_long", "must be at most 200 characters")
		}
		if (segment.StopLat == nil) != (segment.StopLng == nil) {
			return nil, nil, NewValidationError("segments.stop", "invalid_point", "must be a latitude and a longitude")
		}
		if point := segment.Stop(); point != nil {
			if err := validatePoint("segments.stop", *point); err != nil {
				return nil, nil, err
			}
		}
		if segment.WaitMinutes < 0 || segment.WaitMinutes > maxWaitMinutes {
			return nil, nil, NewValidationError("segments.wait_minutes", "out_of_range", "must be between 0 and 1440")
		}
	}
	return segments, tickets, nil
}

// sourceRank orders the states of parts by how much they say about the whole:
// a part still waiting makes the leg wait, a part without a position makes it
// unknown, an estimate makes it an estimate.
var sourceRank = map[LegSource]int{
	LegPending:            5,
	LegMissingCoordinates: 4,
	LegEstimate:           3,
	LegStraightLine:       2,
	LegProvider:           1,
}

// Fold - works a composite leg's totals out of its parts and puts them where a
// plain leg keeps its own, so everything that reads a leg reads this one too.
//
// The distance is what the known parts add up to. The time is the parts and
// the waits at the changes, and unknown while any part's time is. The line
// runs through every part. The cost is what the tickets add up to. The state
// is the part that says the least: one waiting part makes the leg wait. A leg
// without changes is returned as it is.
//
// Returns:
//   - the folded leg.
func (l Leg) Fold() Leg {
	if !l.Composite() {
		return l
	}
	var (
		distance, duration int
		distanceKnown      bool
		durationUnknown    bool
		line               []Point
		source             = LegProvider
		reason             string
	)
	for index, segment := range l.Segments {
		if value := segment.Distance(); value != nil {
			distance += *value
			distanceKnown = true
		}
		if value := segment.Duration(); value != nil {
			duration += *value
		} else {
			durationUnknown = true
		}
		if index < len(l.Segments)-1 {
			duration += segment.WaitMinutes * 60
		}
		if segment.Geometry != "" {
			line = append(line, DecodePolyline(segment.Geometry, 5)...)
		}
		state := segment.Source
		if segment.Duration() != nil && state == LegMissingCoordinates {
			// A part timed by hand is known even without a line.
			state = LegStraightLine
		}
		if sourceRank[state] > sourceRank[source] {
			source = state
		}
		if reason == "" && segment.Source == LegEstimate {
			reason = segment.Error
		}
	}
	l.Mode = l.Segments[0].Mode
	l.DistanceM, l.DurationS = nil, nil
	if distanceKnown {
		l.DistanceM = &distance
	}
	if !durationUnknown {
		l.DurationS = &duration
	}
	l.ManualDistanceM, l.ManualDurationS = nil, nil
	l.Geometry = ""
	if len(line) > 1 {
		l.Geometry = EncodePolyline(line)
	}
	l.Source, l.Error = source, reason
	l.PlannedCost, l.ActualCost = nil, nil
	for _, ticket := range l.Tickets {
		l.PlannedCost = addMoney(l.PlannedCost, ticket.PlannedCost)
		l.ActualCost = addMoney(l.ActualCost, ticket.ActualCost)
	}
	l.Via, l.Pinned = nil, false
	l.Preference = RouteFastest
	return l
}

// addMoney adds an optional amount to an optional total; the total stays
// unknown until some amount is given.
func addMoney(total, amount *Money) *Money {
	if amount == nil {
		return total
	}
	sum := *amount
	if total != nil {
		sum += *total
	}
	return &sum
}

// SegmentTravel is the time and the distance one mode takes within a leg.
type SegmentTravel struct {
	Mode      TravelMode
	DistanceM *int
	DurationS *int
}

// Travel - lists what each way of travelling takes within a leg: the leg as a
// whole for a plain one, each part for a composite one, so a day's totals by
// mode count five minutes on foot apart from an hour on the train.
//
// Returns:
//   - one entry per part, or one for a plain leg.
func (l Leg) Travel() []SegmentTravel {
	if !l.Composite() {
		return []SegmentTravel{{Mode: l.Mode, DistanceM: l.Distance(), DurationS: l.Duration()}}
	}
	travel := make([]SegmentTravel, len(l.Segments))
	for index, segment := range l.Segments {
		travel[index] = SegmentTravel{Mode: segment.Mode, DistanceM: segment.Distance(), DurationS: segment.Duration()}
	}
	return travel
}
