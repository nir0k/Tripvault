package domain

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A transfer is a journey booked ahead that takes the trip from one town to
// another: a flight, a train, a bus, a ferry, the shuttle to the hotel. Like a
// stay it belongs to the whole document rather than to a day, because it is
// bought and read as one booking with its number, its times and its price. It
// appears in the day it departs on, and in the day it arrives on when that is
// another, but it takes no part in the day's schedule: the time of a day is
// made of its places and the legs between them.

// TransferKind says how a transfer travels.
type TransferKind string

// Transfer kinds.
const (
	TransferFlight TransferKind = "flight"
	TransferTrain  TransferKind = "train"
	TransferBus    TransferKind = "bus"
	TransferFerry  TransferKind = "ferry"
	// TransferShuttle is a car booked to take the travellers somewhere: a taxi
	// from the airport, a hotel shuttle.
	TransferShuttle TransferKind = "transfer"
	TransferOther   TransferKind = "other"
)

// TransferKinds is the order transfer kinds are listed in.
var TransferKinds = []TransferKind{TransferFlight, TransferTrain, TransferBus, TransferFerry,
	TransferShuttle, TransferOther}

// Transfer is a booked journey between two places.
type Transfer struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	Kind       TransferKind
	// Name is the carrier or the number, such as "Icelandair FI 204"; it may
	// be empty.
	Name        string
	FromName    string
	FromAddress string
	FromLat     *float64
	FromLng     *float64
	ToName      string
	ToAddress   string
	ToLat       *float64
	ToLng       *float64
	// DepartureDate and ArrivalDate are local dates at each end, with no zone
	// attached; a nil ArrivalDate is the day of departure.
	DepartureDate time.Time
	DepartureTime *ClockTime
	ArrivalDate   *time.Time
	ArrivalTime   *ClockTime
	BookingRef    string
	URL           string
	NotesMD       string
	PlannedCost   *Money
	// CostPerPerson multiplies both amounts by the travellers, as a ticket
	// usually is bought one per head.
	CostPerPerson bool
	// ActualCost belongs to a report; in a plan it stays empty.
	ActualCost *Money
	// SourceTransferID is the transfer of the plan this one was copied from.
	SourceTransferID *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Normalize - trims a transfer's text and checks its fields.
//
// Arguments:
//   - kind: the document the transfer belongs to; a plan may not carry an
//     actual cost.
//
// Returns:
//   - the normalised transfer.
//   - the first *ValidationError found.
func (t Transfer) Normalize(kind DocumentKind) (Transfer, error) {
	if kind == DocumentPlan && t.ActualCost != nil {
		return t, reportOnly("actual_cost_amount")
	}
	switch t.Kind {
	case TransferFlight, TransferTrain, TransferBus, TransferFerry, TransferShuttle, TransferOther:
	case "":
		t.Kind = TransferFlight
	default:
		return t, NewValidationError("kind", "unsupported", "is not a known kind of transfer")
	}
	var err error
	if t.Name, err = trimmedText("name", t.Name, maxNameLength); err != nil {
		return t, err
	}
	for _, end := range []struct {
		prefix   string
		name     *string
		address  *string
		lat, lng *float64
	}{
		{"from_", &t.FromName, &t.FromAddress, t.FromLat, t.FromLng},
		{"to_", &t.ToName, &t.ToAddress, t.ToLat, t.ToLng},
	} {
		if *end.name, err = trimmedText(end.prefix+"name", *end.name, maxNameLength); err != nil {
			return t, err
		}
		if *end.name == "" {
			return t, NewValidationError(end.prefix+"name", "required", "must not be empty")
		}
		if *end.address, err = trimmedText(end.prefix+"address", *end.address, maxShortText); err != nil {
			return t, err
		}
		if err := prefixField(end.prefix, validateCoordinates(end.lat, end.lng)); err != nil {
			return t, err
		}
	}
	// A flight west over the date line may land on the calendar day before it
	// left, and never earlier than that.
	if t.ArrivalDate != nil && dateOnly(*t.ArrivalDate).Before(dateOnly(t.DepartureDate).AddDate(0, 0, -1)) {
		return t, NewValidationError("arrival_date", "end_before_start", "must not be before the departure date")
	}
	if t.ArrivalDate != nil && dateOnly(*t.ArrivalDate).Sub(dateOnly(t.DepartureDate)).Hours()/24 > MaxTripDays {
		return t, NewValidationError("arrival_date", "trip_too_long", "a transfer lasts at most 366 days")
	}
	if t.BookingRef, err = trimmedText("booking_ref", t.BookingRef, maxNameLength); err != nil {
		return t, err
	}
	if t.URL, err = normalizeURL(t.URL); err != nil {
		return t, err
	}
	if err := checkLength("notes_md", t.NotesMD, maxMarkdownLength); err != nil {
		return t, err
	}
	return t, nil
}

// prefixField names a validation failure of one end of a transfer after that
// end, so "lat" becomes "from_lat".
func prefixField(prefix string, err error) error {
	var invalid *ValidationError
	if errors.As(err, &invalid) {
		return NewValidationError(prefix+invalid.Field, invalid.Code, invalid.Message)
	}
	return err
}

// Arrival - reports the date the transfer arrives on.
//
// Returns:
//   - the arrival date, or the departure date when none was given.
func (t Transfer) Arrival() time.Time {
	if t.ArrivalDate != nil {
		return *t.ArrivalDate
	}
	return t.DepartureDate
}

// PlannedCostTotal - reports what the transfer is planned to cost the whole
// group.
//
// Arguments:
//   - travelers: the trip's number of travellers.
//
// Returns:
//   - the amount, multiplied by the travellers for a per-person price.
func (t Transfer) PlannedCostTotal(travelers int) Money {
	return perGroup(t.PlannedCost, t.CostPerPerson, travelers)
}

// ActualCostTotal - reports what the transfer really cost the whole group.
//
// Arguments:
//   - travelers: the trip's number of travellers.
//
// Returns:
//   - the amount, multiplied by the travellers for a per-person price.
func (t Transfer) ActualCostTotal(travelers int) Money {
	return perGroup(t.ActualCost, t.CostPerPerson, travelers)
}

// TransfersOnDate - lists the transfers that depart or arrive on a date, in
// the order they depart.
//
// Arguments:
//   - transfers: the document's transfers.
//   - date: the day's date.
//
// Returns:
//   - the transfers touching that date.
func TransfersOnDate(transfers []Transfer, date time.Time) []Transfer {
	date = dateOnly(date)
	var touching []Transfer
	for _, transfer := range transfers {
		if dateOnly(transfer.DepartureDate).Equal(date) || dateOnly(transfer.Arrival()).Equal(date) {
			touching = append(touching, transfer)
		}
	}
	sortTransfers(touching)
	return touching
}

// sortTransfers puts transfers in the order they depart: by date, by time with
// an unknown time last, then by identifier so the order never shuffles.
func sortTransfers(transfers []Transfer) {
	sort.SliceStable(transfers, func(a, b int) bool {
		left, right := transfers[a], transfers[b]
		if !left.DepartureDate.Equal(right.DepartureDate) {
			return left.DepartureDate.Before(right.DepartureDate)
		}
		if (left.DepartureTime == nil) != (right.DepartureTime == nil) {
			return left.DepartureTime != nil
		}
		if left.DepartureTime != nil && *left.DepartureTime != *right.DepartureTime {
			return *left.DepartureTime < *right.DepartureTime
		}
		return strings.Compare(left.ID.String(), right.ID.String()) < 0
	})
}
