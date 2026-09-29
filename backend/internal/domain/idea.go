package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// An idea is somewhere a person would like to go one day: where it is, when it
// is best, how long it takes and roughly what it costs. It is the person's own,
// like a tag - nobody else sees it - and it is not a trip: it has no days, no
// members and no pictures. When the time comes a plan is made from it, and the
// idea stays, since one idea may be travelled more than once.

// Limits of an idea.
const (
	maxIdeaCountries  = 30
	maxIdeaPlaces     = 20
	maxIdeaTransports = 10
	// maxMixedModes bounds the ways of travelling one way of getting there mixes.
	maxMixedModes = 5
	// MaxIdeaDays bounds how long an idea's trip takes: an idea is a trip of a
	// few days or weeks, not a stay of months.
	MaxIdeaDays = 30
	// maxTransportMinutes bounds how long getting there takes: a week.
	maxTransportMinutes = 7 * 24 * 60
)

// countryPattern is an ISO 3166-1 alpha-2 code.
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

// VisaRequirement says whether the reader needs a visa to go.
type VisaRequirement string

// Visa requirements. VisaNotNeeded is where an idea starts, as it is for most
// of the places a person thinks of going.
const (
	VisaUnknown   VisaRequirement = "unknown"
	VisaNotNeeded VisaRequirement = "not_needed"
	VisaNeeded    VisaRequirement = "needed"
	// VisaOnArrival is a visa got online or at the border, without an embassy.
	VisaOnArrival VisaRequirement = "on_arrival"
)

// IdeaCosts are the rough costs of an idea's trip, for the whole of it, by
// what they pay for, besides getting there, which each way of getting there
// prices itself. Each is nil while unknown.
type IdeaCosts struct {
	Stay  *Money
	Food  *Money
	Other *Money
}

// IdeaPlace is one place an idea goes to: a name or an address, and a point on
// a map when one was found.
type IdeaPlace struct {
	Name string
	Lat  *float64
	Lng  *float64
}

// IdeaTransport is one way of getting there, an alternative to the others:
// one way of travelling, or several mixed along one route - a flight and a
// car. Its cost and time are the whole way there and back.
type IdeaTransport struct {
	Modes   []TravelMode
	Cost    *Money
	Minutes *int
}

// MaxIdeaPhotos bounds the pictures one idea keeps: enough to remember a
// place by, not a gallery.
const MaxIdeaPhotos = 10

// IdeaPhoto is one picture of an idea. What is kept is never what was sent:
// the server renders the upload into a picture to show and a preview, both
// without the camera's metadata, like an avatar.
type IdeaPhoto struct {
	ID     uuid.UUID
	IdeaID uuid.UUID
	// Key and ThumbKey are where the picture and its preview lie in the media store.
	Key       string
	ThumbKey  string
	Width     int
	Height    int
	CreatedAt time.Time
}

// Idea is somewhere a person would like to go.
type Idea struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Title   string
	// Countries are ISO 3166-1 alpha-2 codes: a tour of several countries
	// names them all.
	Countries []string
	// Places are the places the idea goes to, in their order.
	Places []IdeaPlace
	// Months are the months of the year, 1 to 12, best for going; none means
	// it was not said, all twelve that any time will do.
	Months []int
	// DaysMin and DaysMax are how many days the trip takes, DaysIdeal how many
	// would be best; each may be nil.
	DaysMin       *int
	DaysMax       *int
	DaysIdeal     *int
	DescriptionMD string
	Currency      string
	Costs         IdeaCosts
	Visa          VisaRequirement
	// Transports are the ways of getting there, each an alternative.
	Transports []IdeaTransport
	// Photos are the idea's pictures, in their order.
	Photos []IdeaPhoto
	// Tags are the owner's own tags on the idea.
	Tags      []TripTag
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TransportRange - finds the cheapest and the dearest way of getting there
// among the ones priced.
//
// Returns:
//   - the least and the most, both nil when no way is priced.
func (i Idea) TransportRange() (*Money, *Money) {
	var least, most *Money
	for _, transport := range i.Transports {
		if transport.Cost == nil {
			continue
		}
		cost := *transport.Cost
		if least == nil || cost < *least {
			least = &cost
		}
		if most == nil || cost > *most {
			most = &cost
		}
	}
	return least, most
}

// TotalRange - sums the known costs with the cheapest and with the dearest way
// of getting there: the whole trip costs somewhere between the two.
//
// Returns:
//   - the least and the most the trip costs, both nil when nothing is priced.
func (i Idea) TotalRange() (*Money, *Money) {
	var base *Money
	for _, part := range []*Money{i.Costs.Stay, i.Costs.Food, i.Costs.Other} {
		if part != nil {
			sum := *part
			if base != nil {
				sum += *base
			}
			base = &sum
		}
	}
	least, most := i.TransportRange()
	add := func(transport *Money) *Money {
		switch {
		case transport == nil:
			return base
		case base == nil:
			return transport
		}
		sum := *base + *transport
		return &sum
	}
	return add(least), add(most)
}

// Normalize - trims an idea's texts, sorts its sets and checks every field.
//
// Returns:
//   - the normalised idea.
//   - the first *ValidationError found.
func (i Idea) Normalize() (Idea, error) {
	var err error
	i.Title = strings.TrimSpace(i.Title)
	if i.Title == "" {
		return i, NewValidationError("title", "required", "must not be empty")
	}
	if err := checkLength("title", i.Title, maxNameLength); err != nil {
		return i, err
	}
	i.DescriptionMD = strings.TrimSpace(i.DescriptionMD)
	if err := checkLength("description_md", i.DescriptionMD, maxMarkdownLength); err != nil {
		return i, err
	}
	if i.Places, err = normalizeIdeaPlaces(i.Places); err != nil {
		return i, err
	}
	if i.Countries, err = normalizeCountries(i.Countries); err != nil {
		return i, err
	}
	if i.Months, err = normalizeMonths(i.Months); err != nil {
		return i, err
	}
	if err := validateIdeaDays(i.DaysMin, i.DaysMax, i.DaysIdeal); err != nil {
		return i, err
	}
	i.Currency = strings.ToUpper(strings.TrimSpace(i.Currency))
	if !currencyPattern.MatchString(i.Currency) {
		return i, NewValidationError("currency", "invalid_currency", "must be an ISO 4217 code")
	}
	if i.Visa == "" {
		i.Visa = VisaNotNeeded
	}
	switch i.Visa {
	case VisaUnknown, VisaNotNeeded, VisaNeeded, VisaOnArrival:
	default:
		return i, NewValidationError("visa", "unsupported", "must be unknown, not_needed, needed or on_arrival")
	}
	if i.Transports, err = normalizeIdeaTransports(i.Transports); err != nil {
		return i, err
	}
	return i, nil
}

// normalizeIdeaPlaces trims the places' names and drops the ones that say
// nothing: neither a name nor a point.
func normalizeIdeaPlaces(places []IdeaPlace) ([]IdeaPlace, error) {
	normalized := make([]IdeaPlace, 0, len(places))
	for _, place := range places {
		var err error
		if place.Name, err = trimmedText("places", place.Name, maxShortText); err != nil {
			return nil, err
		}
		if err := validateCoordinates(place.Lat, place.Lng); err != nil {
			return nil, err
		}
		if place.Name != "" || place.Lat != nil {
			normalized = append(normalized, place)
		}
	}
	if len(normalized) > maxIdeaPlaces {
		return nil, NewValidationError("places", "too_many", "must name at most 20 places")
	}
	return normalized, nil
}

// normalizeCountries upper-cases the codes, drops repeats and keeps the order
// they were named in, the main country first.
func normalizeCountries(countries []string) ([]string, error) {
	normalized := make([]string, 0, len(countries))
	for _, country := range countries {
		country = strings.ToUpper(strings.TrimSpace(country))
		if !countryPattern.MatchString(country) {
			return nil, NewValidationError("countries", "invalid_country", "must be ISO 3166-1 alpha-2 codes")
		}
		if !slices.Contains(normalized, country) {
			normalized = append(normalized, country)
		}
	}
	if len(normalized) > maxIdeaCountries {
		return nil, NewValidationError("countries", "too_many", "must name at most 30 countries")
	}
	return normalized, nil
}

// normalizeMonths sorts the months and drops repeats.
func normalizeMonths(months []int) ([]int, error) {
	normalized := make([]int, 0, len(months))
	for _, month := range months {
		if month < 1 || month > 12 {
			return nil, NewValidationError("months", "out_of_range", "must be months from 1 to 12")
		}
		if !slices.Contains(normalized, month) {
			normalized = append(normalized, month)
		}
	}
	slices.Sort(normalized)
	return normalized, nil
}

// validateIdeaDays checks the days an idea takes lie between one and
// MaxIdeaDays, the shortest is not longer than the longest, and the ideal lies
// between them.
func validateIdeaDays(least, most, ideal *int) error {
	for _, days := range []struct {
		field string
		value *int
	}{{"days_min", least}, {"days_max", most}, {"days_ideal", ideal}} {
		if days.value != nil && (*days.value < 1 || *days.value > MaxIdeaDays) {
			return NewValidationError(days.field, "out_of_range", "must be between 1 and 30")
		}
	}
	if least != nil && most != nil && *least > *most {
		return NewValidationError("days_max", "max_before_min", "must not be shorter than days_min")
	}
	if ideal != nil && ((least != nil && *ideal < *least) || (most != nil && *ideal > *most)) {
		return NewValidationError("days_ideal", "outside_range", "must lie between days_min and days_max")
	}
	return nil
}

// normalizeIdeaTransports checks every way of getting there: one way of
// travelling, or several mixed, each named once and in the order the
// interface offers them, with a time of at most a week.
func normalizeIdeaTransports(transports []IdeaTransport) ([]IdeaTransport, error) {
	if len(transports) > maxIdeaTransports {
		return nil, NewValidationError("transports", "too_many", "must offer at most 10 ways of getting there")
	}
	normalized := make([]IdeaTransport, 0, len(transports))
	for _, transport := range transports {
		for _, mode := range transport.Modes {
			if err := ValidateTravelMode("transports", mode); err != nil {
				return nil, err
			}
		}
		modes := make([]TravelMode, 0, len(transport.Modes))
		for _, known := range TravelModes {
			if slices.Contains(transport.Modes, known) {
				modes = append(modes, known)
			}
		}
		if len(modes) == 0 || len(modes) > maxMixedModes {
			return nil, NewValidationError("transports", "modes_out_of_range", "must name one to five ways of travelling")
		}
		if transport.Minutes != nil && (*transport.Minutes < 1 || *transport.Minutes > maxTransportMinutes) {
			return nil, NewValidationError("transports", "out_of_range", "must take a minute to a week")
		}
		transport.Modes = modes
		normalized = append(normalized, transport)
	}
	return normalized, nil
}
