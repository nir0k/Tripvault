package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AnchorKey names one stay mark: a day and its morning or evening.
type AnchorKey struct {
	DayID uuid.UUID
	Slot  AnchorSlot
}

// sortedStays returns the stays by check-in date, then by identifier, which is
// the order that decides between overlapping stays.
func sortedStays(stays []Stay) []Stay {
	sorted := append([]Stay(nil), stays...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if !sorted[a].CheckInDate.Equal(sorted[b].CheckInDate) {
			return sorted[a].CheckInDate.Before(sorted[b].CheckInDate)
		}
		return sorted[a].ID.String() < sorted[b].ID.String()
	})
	return sorted
}

// stayForNight returns the stay where the night starting on a date is spent.
// When stays overlap, the one checked into first wins; the overlap itself is
// reported separately by Nights.
func stayForNight(stays []Stay, night time.Time) *uuid.UUID {
	for _, stay := range stays {
		if stay.Covers(night) {
			id := stay.ID
			return &id
		}
	}
	return nil
}

// AnchorAssignments - decides which stay marks each day carries.
//
// The morning mark is the stay of the previous night, the evening mark the stay
// of the coming night. A mark is left out when the day switched it off, when the
// day has no date, when no stay covers that night, and - for the evening - when
// the coming night is marked as spent without a stay.
//
// Arguments:
//   - days: the document's days.
//   - stays: the document's stays.
//
// Returns:
//   - the stay of every mark that should exist.
func AnchorAssignments(days []Day, stays []Stay) map[AnchorKey]uuid.UUID {
	ordered := sortedStays(stays)
	assignments := make(map[AnchorKey]uuid.UUID)
	for _, day := range days {
		if day.Date == nil {
			continue
		}
		if day.MorningAnchor {
			if stay := stayForNight(ordered, day.Date.AddDate(0, 0, -1)); stay != nil {
				assignments[AnchorKey{DayID: day.ID, Slot: AnchorMorning}] = *stay
			}
		}
		if day.EveningAnchor && !day.NoOvernight {
			if stay := stayForNight(ordered, *day.Date); stay != nil {
				assignments[AnchorKey{DayID: day.ID, Slot: AnchorEvening}] = *stay
			}
		}
	}
	return assignments
}

// Night is one night of a trip and where it is spent.
type Night struct {
	Date time.Time
	// StayIDs lists every stay covering the night; more than one is an overlap.
	StayIDs []uuid.UUID
	// NoOvernight marks a night deliberately spent without a stay.
	NoOvernight bool
}

// Missing - reports whether the night has no stay and was not marked as spent
// without one.
//
// Returns:
//   - true when the plan should warn about the night.
func (n Night) Missing() bool {
	return len(n.StayIDs) == 0 && !n.NoOvernight
}

// Nights - lists the nights of a trip with the stays covering each.
//
// A trip from the 20th to the 27th has the nights starting on the 20th to the
// 26th: the last day ends the trip.
//
// Arguments:
//   - start, end: the trip dates, nil for a trip without dates.
//   - days: the document's days, which carry the "no overnight" marks.
//   - stays: the document's stays.
//
// Returns:
//   - the nights in date order, empty for a trip without dates.
func Nights(start, end *time.Time, days []Day, stays []Stay) []Night {
	if start == nil || end == nil {
		return nil
	}
	withoutStay := make(map[time.Time]bool)
	for _, day := range days {
		if day.Date != nil && day.NoOvernight {
			withoutStay[dateOnly(*day.Date)] = true
		}
	}
	ordered := sortedStays(stays)
	var nights []Night
	for date := dateOnly(*start); date.Before(dateOnly(*end)); date = date.AddDate(0, 0, 1) {
		night := Night{Date: date, StayIDs: []uuid.UUID{}, NoOvernight: withoutStay[date]}
		for _, stay := range ordered {
			if stay.Covers(date) {
				night.StayIDs = append(night.StayIDs, stay.ID)
			}
		}
		nights = append(nights, night)
	}
	return nights
}

// StaySummary totals a document's stays.
type StaySummary struct {
	Nights int
	Cost   Money
	// AveragePerNight is nil when no night has a price.
	AveragePerNight *Money
}

// SummarizeStays - totals the nights and the cost of the stays.
//
// The average divides the cost only over the nights of stays that have a cost,
// so a free night at friends does not pull the hotel average down.
//
// Arguments:
//   - stays: the document's stays.
//
// Returns:
//   - the totals.
func SummarizeStays(stays []Stay) StaySummary {
	var summary StaySummary
	pricedNights := 0
	for _, stay := range stays {
		summary.Nights += stay.Nights()
		if stay.PlannedCost != nil {
			summary.Cost += *stay.PlannedCost
			pricedNights += stay.Nights()
		}
	}
	if pricedNights > 0 {
		average := Money((int64(summary.Cost) + int64(pricedNights)/2) / int64(pricedNights))
		summary.AveragePerNight = &average
	}
	return summary
}

// DayItems - lists a day's elements in schedule order: the morning mark, the
// places by position, the evening mark.
//
// Arguments:
//   - items: every item of the document.
//   - dayID: the day, or nil for the unassigned places.
//
// Returns:
//   - the ordered items.
func DayItems(items []Item, dayID *uuid.UUID) []Item {
	var morning, evening *Item
	var places []Item
	for _, item := range items {
		if !sameDay(item.DayID, dayID) {
			continue
		}
		switch {
		case item.Kind.IsVisit():
			places = append(places, item)
		case item.Anchor == AnchorMorning:
			morning = &item
		default:
			evening = &item
		}
	}
	sort.SliceStable(places, func(a, b int) bool { return places[a].Position < places[b].Position })

	ordered := make([]Item, 0, len(places)+2)
	if morning != nil {
		ordered = append(ordered, *morning)
	}
	ordered = append(ordered, places...)
	if evening != nil {
		ordered = append(ordered, *evening)
	}
	return ordered
}

// sameDay compares two optional day identifiers.
func sameDay(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ItemSchedule is when the schedule puts a person at one element of a day.
// Minutes count from the midnight the day starts on and may pass 1440 when a
// day runs past midnight.
type ItemSchedule struct {
	ArrivalMinutes   int
	DepartureMinutes int
	// Late is set when the place has a desired arrival time the schedule
	// cannot meet.
	Late bool
}

// ModeTotal sums the legs of one travel mode.
type ModeTotal struct {
	DistanceM int
	DurationS int
}

// DaySummary totals one day.
type DaySummary struct {
	VisitMinutes  int
	TravelMinutes int
	DistanceM     int
	// ByMode sums distance and time per travel mode.
	ByMode map[TravelMode]ModeTotal
	// UnknownTravel is set when some leg has no travel time, so the end of
	// the day is earlier than it will really be.
	UnknownTravel bool
	EndMinutes    int
	// PlannedCost is everything the day plans to spend, optional places
	// included: the money has to be there if they are visited.
	PlannedCost Money
	// ActualCost is what the day really cost, filled in a report only.
	ActualCost Money
}

// ScheduleDay - works out when each element of a day is reached and what the
// day costs.
//
// The schedule starts at the day's start time. Reaching an element takes the
// travel time of the leg from the previous one, and a place then takes its
// visit time. A place with a desired arrival time is not reached early: the
// schedule waits for it. Reaching it later than desired marks it late.
//
// Arguments:
//   - day: the day.
//   - items: the day's elements in schedule order, as DayItems returns them.
//   - opening: the leg from the day before to the first element, as
//     OpeningLeg returns it, or nil. It is travelled first thing in the
//     morning, so it delays the first arrival and counts towards the day.
//   - legs: the leg from each element to the next, as DayLegs returns them;
//     a nil leg counts as no travel.
//   - travelers: the trip's number of travellers, for per-person costs.
//   - tracks: the document's recorded lines. The recordings of the day's
//     places and activities - a hike, a walk around a lake - are added to the
//     legs, because that is distance covered inside them rather than between
//     them.
//
// Returns:
//   - one schedule per element, in the same order.
//   - the day's totals.
func ScheduleDay(day Day, items []Item, opening *Leg, legs []*Leg, travelers int,
	tracks []Track) ([]ItemSchedule, DaySummary) {
	schedules := make([]ItemSchedule, len(items))
	summary := DaySummary{ByMode: map[TravelMode]ModeTotal{}}
	cursor := int(day.StartTime)

	for index, item := range items {
		var leg *Leg
		switch {
		case index == 0:
			leg = opening
		case index-1 < len(legs):
			leg = legs[index-1]
		}
		if leg != nil {
			total := summary.ByMode[leg.Mode]
			if distance := leg.Distance(); distance != nil {
				summary.DistanceM += *distance
				total.DistanceM += *distance
			}
			if duration := leg.Duration(); duration != nil {
				minutes := (*duration + 30) / 60
				cursor += minutes
				summary.TravelMinutes += minutes
				total.DurationS += *duration
			} else {
				summary.UnknownTravel = true
			}
			summary.ByMode[leg.Mode] = total
			if leg.PlannedCost != nil {
				summary.PlannedCost += *leg.PlannedCost
			}
			if leg.ActualCost != nil {
				summary.ActualCost += *leg.ActualCost
			}
		}
		schedule := ItemSchedule{ArrivalMinutes: cursor}
		if item.Kind.IsVisit() {
			if item.DesiredTime != nil {
				desired := int(*item.DesiredTime)
				schedule.Late = cursor > desired
				schedule.ArrivalMinutes = max(cursor, desired)
			}
			cursor = schedule.ArrivalMinutes + item.VisitMinutes
			summary.VisitMinutes += item.VisitMinutes
			summary.PlannedCost += item.PlannedCostTotal(travelers)
			summary.ActualCost += item.ActualCostTotal(travelers)
			if track := TrackOfItem(tracks, item.ID); track != nil {
				summary.DistanceM += track.DistanceM
			}
		}
		schedule.DepartureMinutes = cursor
		schedules[index] = schedule
	}
	summary.EndMinutes = cursor
	return schedules, summary
}

// RemovedDay describes a day with content a change would remove.
type RemovedDay struct {
	DocumentKind DocumentKind
	Position     int
	Date         *time.Time
	Title        string
}

// DaysWouldBeRemovedError reports that a change removes days holding content
// and needs an explicit confirmation.
type DaysWouldBeRemovedError struct {
	Days []RemovedDay
}

// Error - lists the days the change would remove.
//
// Returns:
//   - an English description.
func (e *DaysWouldBeRemovedError) Error() string {
	parts := make([]string, 0, len(e.Days))
	for _, day := range e.Days {
		parts = append(parts, fmt.Sprintf("%s day %d", day.DocumentKind, day.Position+1))
	}
	return "the change removes days with content: " + strings.Join(parts, ", ")
}
