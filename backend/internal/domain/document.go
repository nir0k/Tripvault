package domain

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Input bounds for document content.
const (
	maxNameLength     = 200
	maxMarkdownLength = 20000
	maxShortText      = 500
	maxURLLength      = 2000
	// MaxVisitMinutes bounds the time spent at one place: a whole day.
	MaxVisitMinutes = 1440
)

// ClockTime is a local time of day, counted in minutes after midnight.
type ClockTime int

// minutesPerDay is the length of a day in minutes.
const minutesPerDay = 24 * 60

// DefaultDayStart is when a day's schedule starts unless the day says otherwise.
const DefaultDayStart ClockTime = 9 * 60

// ParseClockTime - reads a time of day in HH:MM form.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - value: the time string.
//
// Returns:
//   - the time of day.
//   - a *ValidationError when the string is not a time of day.
func ParseClockTime(field, value string) (ClockTime, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, NewValidationError(field, "invalid_time", "must be a time in HH:MM form")
	}
	return ClockTime(parsed.Hour()*60 + parsed.Minute()), nil
}

// String - formats the time of day as HH:MM.
//
// Returns:
//   - the time, such as "09:00".
func (c ClockTime) String() string {
	return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60)
}

// PlaceCategory classifies a place. The category decides the default time spent
// there and the default expense category of its cost.
type PlaceCategory string

// Place categories.
const (
	CategorySight     PlaceCategory = "sight"
	CategoryNature    PlaceCategory = "nature"
	CategoryMuseum    PlaceCategory = "museum"
	CategoryFood      PlaceCategory = "food"
	CategoryShopping  PlaceCategory = "shopping"
	CategoryActivity  PlaceCategory = "activity"
	CategoryTransport PlaceCategory = "transport"
	CategoryOther     PlaceCategory = "other"
)

// placeCategoryDefaults holds, per category, the minutes usually spent at such
// a place and the budget category its cost goes to. Adding a category means
// adding a row here and to the database check.
var placeCategoryDefaults = map[PlaceCategory]struct {
	minutes int
	cost    CostCategory
}{
	CategorySight:     {60, CostActivities},
	CategoryNature:    {90, CostActivities},
	CategoryMuseum:    {120, CostActivities},
	CategoryFood:      {60, CostFood},
	CategoryShopping:  {45, CostShopping},
	CategoryActivity:  {120, CostActivities},
	CategoryTransport: {15, CostTransport},
	CategoryOther:     {30, CostOther},
}

// DefaultVisitMinutes - reports how long a visit to a place of a category
// usually lasts.
//
// Returns:
//   - the minutes, or the "other" default for an unknown category.
func (c PlaceCategory) DefaultVisitMinutes() int {
	if defaults, ok := placeCategoryDefaults[c]; ok {
		return defaults.minutes
	}
	return placeCategoryDefaults[CategoryOther].minutes
}

// DefaultCostCategory - reports the budget category a place's cost goes to
// unless the place says otherwise.
//
// Returns:
//   - the expense category.
func (c PlaceCategory) DefaultCostCategory() CostCategory {
	if defaults, ok := placeCategoryDefaults[c]; ok {
		return defaults.cost
	}
	return CostOther
}

// CostCategory is a budget category.
type CostCategory string

// Budget categories.
const (
	CostAccommodation CostCategory = "accommodation"
	CostTransport     CostCategory = "transport"
	CostFood          CostCategory = "food"
	CostActivities    CostCategory = "activities"
	CostShopping      CostCategory = "shopping"
	CostOther         CostCategory = "other"
)

// StayKind classifies a place to sleep.
type StayKind string

// Stay kinds.
const (
	StayHotel     StayKind = "hotel"
	StayApartment StayKind = "apartment"
	StayHostel    StayKind = "hostel"
	StayCamping   StayKind = "camping"
	StayFriends   StayKind = "friends"
	StayOther     StayKind = "other"
)

// ItemKind distinguishes places and activities from stay marks.
type ItemKind string

// Item kinds.
const (
	ItemPlace ItemKind = "place"
	// ItemActivity is something done rather than somewhere seen: a hike, a
	// walk, a descent of a canyon. It lives in a day like a place, with the
	// same costs, statuses and pictures, and its position is where it starts.
	ItemActivity   ItemKind = "activity"
	ItemStayAnchor ItemKind = "stay_anchor"
)

// IsVisit - reports whether an element is something the trip goes to - a place
// or an activity - rather than a stay mark the stays put there.
//
// Returns:
//   - true for places and activities.
func (k ItemKind) IsVisit() bool {
	return k == ItemPlace || k == ItemActivity
}

// ActivityType says what kind of activity an activity is.
type ActivityType string

// Activity types.
const (
	ActivityHike       ActivityType = "hike"
	ActivityWalk       ActivityType = "walk"
	ActivityBike       ActivityType = "bike"
	ActivityRun        ActivityType = "run"
	ActivityCanyoning  ActivityType = "canyoning"
	ActivityClimbing   ActivityType = "climbing"
	ActivityViaFerrata ActivityType = "via_ferrata"
	ActivityKayak      ActivityType = "kayak"
	ActivitySwim       ActivityType = "swim"
	ActivitySki        ActivityType = "ski"
	ActivityTour       ActivityType = "tour"
	ActivityOther      ActivityType = "other"
)

// ActivityTypes is the order activity types are listed in.
var ActivityTypes = []ActivityType{ActivityHike, ActivityWalk, ActivityBike, ActivityRun, ActivityCanyoning,
	ActivityClimbing, ActivityViaFerrata, ActivityKayak, ActivitySwim, ActivitySki, ActivityTour, ActivityOther}

// activityMinutes holds, per activity type, how long such an activity usually
// takes. Adding a type means adding a row here and to the database check.
var activityMinutes = map[ActivityType]int{
	ActivityHike:       300,
	ActivityWalk:       90,
	ActivityBike:       180,
	ActivityRun:        60,
	ActivityCanyoning:  240,
	ActivityClimbing:   180,
	ActivityViaFerrata: 240,
	ActivityKayak:      180,
	ActivitySwim:       60,
	ActivitySki:        300,
	ActivityTour:       120,
	ActivityOther:      120,
}

// DefaultVisitMinutes - reports how long an activity of a type usually lasts.
//
// Returns:
//   - the minutes, or the "other" default for an unknown type.
func (a ActivityType) DefaultVisitMinutes() int {
	if minutes, ok := activityMinutes[a]; ok {
		return minutes
	}
	return activityMinutes[ActivityOther]
}

// ItemStatus is how a place of a report turned out. A plan keeps every place
// at StatusPlanned and fills none of the other report fields. A report counts
// every place it holds as visited unless it says otherwise: a report is written
// about what happened, and the few places that were missed are the exception.
type ItemStatus string

// Item statuses.
const (
	// StatusPlanned is the status of every place of a plan. A report does not
	// keep it: a place of a report is visited until marked skipped.
	StatusPlanned ItemStatus = "planned"
	StatusVisited ItemStatus = "visited"
	StatusSkipped ItemStatus = "skipped"
	// StatusUnplanned is a place added straight into the report.
	StatusUnplanned ItemStatus = "unplanned"
)

// ItemStatuses is the order statuses are listed in.
var ItemStatuses = []ItemStatus{StatusPlanned, StatusVisited, StatusSkipped, StatusUnplanned}

// ValidateItemStatus - checks that a value names a status.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - status: the value to check.
//
// Returns:
//   - a *ValidationError when the value is not a known status.
func ValidateItemStatus(field string, status ItemStatus) error {
	switch status {
	case StatusPlanned, StatusVisited, StatusSkipped, StatusUnplanned:
		return nil
	default:
		return NewValidationError(field, "unsupported", "is not a known status")
	}
}

// AnchorSlot says whether a stay mark opens or closes a day.
type AnchorSlot string

// Anchor slots.
const (
	// AnchorMorning is where the previous night was spent.
	AnchorMorning AnchorSlot = "morning"
	// AnchorEvening is where the coming night is spent.
	AnchorEvening AnchorSlot = "evening"
)

// Document is a plan or a report.
type Document struct {
	ID               uuid.UUID
	TripID           uuid.UUID
	Kind             DocumentKind
	SourceDocumentID *uuid.UUID
	IntroMD          string
	SummaryMD        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CheckDocumentText - checks the words around a document's days.
//
// Arguments:
//   - intro: the opening text, in Markdown.
//   - summary: the closing text, in Markdown.
//
// Returns:
//   - a *ValidationError when either is too long.
func CheckDocumentText(intro, summary string) error {
	if err := checkLength("intro_md", intro, maxMarkdownLength); err != nil {
		return err
	}
	return checkLength("summary_md", summary, maxMarkdownLength)
}

// Day is one day of a document.
type Day struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	Position   int
	// Date is the trip start plus Position, or nil on a trip without dates.
	Date      *time.Time
	Title     string
	NotesMD   string
	StartTime ClockTime
	// DefaultMode and Timezone are nil when the day inherits the trip's.
	DefaultMode   *TravelMode
	Timezone      *string
	MorningAnchor bool
	EveningAnchor bool
	NoOvernight   bool
	// CoverMediaID is the picture the day is shown by, out of its own media.
	CoverMediaID *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Normalize - trims a day's text and checks its fields.
//
// Returns:
//   - the normalised day.
//   - the first *ValidationError found.
func (d Day) Normalize() (Day, error) {
	d.Title = strings.TrimSpace(d.Title)
	if err := checkLength("title", d.Title, maxNameLength); err != nil {
		return d, err
	}
	if err := checkLength("notes_md", d.NotesMD, maxMarkdownLength); err != nil {
		return d, err
	}
	if d.StartTime < 0 || d.StartTime >= minutesPerDay {
		return d, NewValidationError("start_time", "invalid_time", "must be a time in HH:MM form")
	}
	if d.DefaultMode != nil {
		if err := ValidateTravelMode("default_mode", *d.DefaultMode); err != nil {
			return d, err
		}
	}
	if d.Timezone != nil {
		if _, err := time.LoadLocation(*d.Timezone); err != nil || *d.Timezone == "" || *d.Timezone == "Local" {
			return d, NewValidationError("timezone", "invalid_timezone", "must be an IANA time zone name")
		}
	}
	return d, nil
}

// HasContent - reports whether removing the day would lose something the
// person wrote: a title, notes or places.
//
// Arguments:
//   - places: how many places the day holds.
//
// Returns:
//   - true when the day is not empty.
func (d Day) HasContent(places int) bool {
	return d.Title != "" || strings.TrimSpace(d.NotesMD) != "" || places > 0
}

// Stay is a place to sleep, covering the nights from CheckInDate up to the
// night before CheckOutDate.
type Stay struct {
	ID           uuid.UUID
	DocumentID   uuid.UUID
	Name         string
	Kind         StayKind
	Address      string
	Lat          *float64
	Lng          *float64
	CheckInDate  time.Time
	CheckInTime  *ClockTime
	CheckOutDate time.Time
	CheckOutTime *ClockTime
	BookingRef   string
	URL          string
	Contacts     string
	NotesMD      string
	PlannedCost  *Money
	// ActualCost belongs to a report; in a plan it stays empty.
	ActualCost *Money
	// SourceStayID is the stay of the plan this one was copied from.
	SourceStayID *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Normalize - trims a stay's text and checks its fields.
//
// Arguments:
//   - kind: the document the stay belongs to; a plan may not carry an actual cost.
//
// Returns:
//   - the normalised stay.
//   - the first *ValidationError found.
func (s Stay) Normalize(kind DocumentKind) (Stay, error) {
	if kind == DocumentPlan && s.ActualCost != nil {
		return s, reportOnly("actual_cost_amount")
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return s, NewValidationError("name", "required", "must not be empty")
	}
	if err := checkLength("name", s.Name, maxNameLength); err != nil {
		return s, err
	}
	switch s.Kind {
	case StayHotel, StayApartment, StayHostel, StayCamping, StayFriends, StayOther:
	case "":
		s.Kind = StayHotel
	default:
		return s, NewValidationError("kind", "unsupported", "is not a known kind of stay")
	}
	if !s.CheckOutDate.After(s.CheckInDate) {
		return s, NewValidationError("check_out_date", "end_before_start", "must be after the check-in date")
	}
	if nights := s.Nights(); nights > MaxTripDays {
		return s, NewValidationError("check_out_date", "trip_too_long", "a stay lasts at most 366 nights")
	}
	var err error
	if s.Address, err = trimmedText("address", s.Address, maxShortText); err != nil {
		return s, err
	}
	if err := validateCoordinates(s.Lat, s.Lng); err != nil {
		return s, err
	}
	if s.BookingRef, err = trimmedText("booking_ref", s.BookingRef, maxNameLength); err != nil {
		return s, err
	}
	if s.URL, err = normalizeURL(s.URL); err != nil {
		return s, err
	}
	if err := checkLength("contacts", s.Contacts, maxShortText); err != nil {
		return s, err
	}
	if err := checkLength("notes_md", s.NotesMD, maxMarkdownLength); err != nil {
		return s, err
	}
	return s, nil
}

// Nights - counts the nights the stay covers.
//
// Returns:
//   - the number of nights.
func (s Stay) Nights() int {
	return int(dateOnly(s.CheckOutDate).Sub(dateOnly(s.CheckInDate)).Hours() / 24)
}

// Covers - reports whether the stay is where the night starting on a date is spent.
//
// Arguments:
//   - night: the date the night starts on.
//
// Returns:
//   - true when check-in is on or before that date and check-out after it.
func (s Stay) Covers(night time.Time) bool {
	night = dateOnly(night)
	return !dateOnly(s.CheckInDate).After(night) && dateOnly(s.CheckOutDate).After(night)
}

// PricePerNight - divides the stay's cost over its nights.
//
// Returns:
//   - the rounded price of one night, or nil when the stay has no cost.
func (s Stay) PricePerNight() *Money {
	if s.PlannedCost == nil || s.Nights() <= 0 {
		return nil
	}
	price := Money(math.Round(float64(*s.PlannedCost) / float64(s.Nights())))
	return &price
}

// Item is a place, an activity, or a stay mark inside a day.
type Item struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	// DayID is nil for an unassigned place.
	DayID    *uuid.UUID
	Position int
	Kind     ItemKind
	// Anchor and StayID are set on stay marks only.
	Anchor   AnchorSlot
	StayID   *uuid.UUID
	Name     string
	Category PlaceCategory
	// ActivityType is set on activities only.
	ActivityType  ActivityType
	Lat           *float64
	Lng           *float64
	Address       string
	OSMRef        string
	DescriptionMD string
	URL           string
	DesiredTime   *ClockTime
	VisitMinutes  int
	IsOptional    bool
	BookingRef    string
	PlannedCost   *Money
	// CostPerPerson multiplies both costs by the trip's travellers.
	CostPerPerson bool
	CostCategory  CostCategory
	// The fields below belong to a report; in a plan they stay empty.
	Status ItemStatus
	// StoryMD is what happened here.
	StoryMD string
	// ActualTime is when the place was really reached.
	ActualTime *ClockTime
	// ActualEndTime is when the place was left or the activity finished.
	ActualEndTime *ClockTime
	// Rating is 1 to 5, or nil when the place was not rated.
	Rating *int
	// ActualCost is what was really spent, kept apart from the plan's snapshot.
	ActualCost *Money
	// CoverMediaID is the picture the place is shown by, out of its own media.
	CoverMediaID *uuid.UUID
	// SourceItemID is the place of the plan this one was copied from.
	SourceItemID *uuid.UUID
	// Difficulty is how hard an activity is, MinDifficulty to MaxDifficulty,
	// or nil when nobody said. A place has none.
	Difficulty *int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NormalizePlace - trims the text of a place or an activity and checks its
// fields. An element with no kind is a place; an activity with no type is
// "other", and a place carries no type at all.
//
// Arguments:
//   - kind: the document the place belongs to. A plan carries none of the report
//     fields, so setting one on a plan is refused rather than silently kept: it
//     would be copied into a report later and corrupt its plan-against-fact
//     comparison.
//
// Returns:
//   - the normalised place.
//   - the first *ValidationError found.
func (i Item) NormalizePlace(kind DocumentKind) (Item, error) {
	switch i.Kind {
	case "":
		i.Kind = ItemPlace
	case ItemPlace, ItemActivity:
	default:
		return i, NewValidationError("kind", "unsupported", "must be a place or an activity")
	}
	if i.Kind == ItemActivity {
		if i.ActivityType == "" {
			i.ActivityType = ActivityOther
		}
		if _, ok := activityMinutes[i.ActivityType]; !ok {
			return i, NewValidationError("activity_type", "unsupported", "is not a known activity type")
		}
		if i.Category == "" {
			i.Category = CategoryActivity
		}
		if i.Difficulty != nil && (*i.Difficulty < MinDifficulty || *i.Difficulty > MaxDifficulty) {
			return i, NewValidationError("difficulty", "out_of_range", "must be between 1 and 5")
		}
	} else {
		// Turning an activity into a place leaves it no difficulty to keep.
		i.ActivityType, i.Difficulty = "", nil
	}
	i.Name = strings.TrimSpace(i.Name)
	if i.Name == "" {
		return i, NewValidationError("name", "required", "must not be empty")
	}
	if err := checkLength("name", i.Name, maxNameLength); err != nil {
		return i, err
	}
	if i.Category == "" {
		i.Category = CategoryOther
	}
	if _, ok := placeCategoryDefaults[i.Category]; !ok {
		return i, NewValidationError("category", "unsupported", "is not a known place category")
	}
	if i.CostCategory == "" {
		i.CostCategory = i.Category.DefaultCostCategory()
	}
	if err := ValidateCostCategory("cost_category", i.CostCategory); err != nil {
		return i, err
	}
	if err := validateCoordinates(i.Lat, i.Lng); err != nil {
		return i, err
	}
	if i.VisitMinutes < 0 || i.VisitMinutes > MaxVisitMinutes {
		return i, NewValidationError("visit_minutes", "out_of_range", "must be between 0 and 1440")
	}
	var err error
	if i.Address, err = trimmedText("address", i.Address, maxShortText); err != nil {
		return i, err
	}
	if i.OSMRef, err = trimmedText("osm_ref", i.OSMRef, maxNameLength); err != nil {
		return i, err
	}
	if i.BookingRef, err = trimmedText("booking_ref", i.BookingRef, maxNameLength); err != nil {
		return i, err
	}
	if i.URL, err = normalizeURL(i.URL); err != nil {
		return i, err
	}
	if err := checkLength("description_md", i.DescriptionMD, maxMarkdownLength); err != nil {
		return i, err
	}
	if i.DesiredTime != nil && (*i.DesiredTime < 0 || *i.DesiredTime >= minutesPerDay) {
		return i, NewValidationError("desired_time", "invalid_time", "must be a time in HH:MM form")
	}
	return i.normalizeReportFields(kind)
}

// normalizeReportFields checks the fields only a report carries, and empties
// them on a plan rather than trusting a caller to have left them alone.
func (i Item) normalizeReportFields(kind DocumentKind) (Item, error) {
	if kind != DocumentReport {
		switch {
		case i.Status != "" && i.Status != StatusPlanned:
			return i, reportOnly("status")
		case strings.TrimSpace(i.StoryMD) != "":
			return i, reportOnly("story_md")
		case i.ActualTime != nil:
			return i, reportOnly("actual_time")
		case i.ActualEndTime != nil:
			return i, reportOnly("actual_end_time")
		case i.Rating != nil:
			return i, reportOnly("rating")
		case i.ActualCost != nil:
			return i, reportOnly("actual_cost_amount")
		}
		i.Status = StatusPlanned
		i.StoryMD, i.ActualTime, i.ActualEndTime, i.Rating, i.ActualCost = "", nil, nil, nil, nil
		return i, nil
	}

	if i.Status == "" || i.Status == StatusPlanned {
		i.Status = StatusVisited
	}
	if err := ValidateItemStatus("status", i.Status); err != nil {
		return i, err
	}
	if err := checkLength("story_md", i.StoryMD, maxMarkdownLength); err != nil {
		return i, err
	}
	if i.ActualTime != nil && (*i.ActualTime < 0 || *i.ActualTime >= minutesPerDay) {
		return i, NewValidationError("actual_time", "invalid_time", "must be a time in HH:MM form")
	}
	if i.ActualEndTime != nil && (*i.ActualEndTime < 0 || *i.ActualEndTime >= minutesPerDay) {
		return i, NewValidationError("actual_end_time", "invalid_time", "must be a time in HH:MM form")
	}
	if i.Rating != nil && (*i.Rating < 1 || *i.Rating > 5) {
		return i, NewValidationError("rating", "out_of_range", "must be between 1 and 5")
	}
	return i, nil
}

// PlannedCostTotal - reports what the item is planned to cost the whole group.
//
// Arguments:
//   - travelers: the trip's number of travellers.
//
// Returns:
//   - the planned cost, multiplied for a per-person cost, or zero without one.
func (i Item) PlannedCostTotal(travelers int) Money {
	return perGroup(i.PlannedCost, i.CostPerPerson, travelers)
}

// ActualCostTotal - reports what the item really cost the whole group.
//
// The per-person flag applies to both amounts, so a plan and a fact entered the
// same way stay comparable.
//
// Arguments:
//   - travelers: the trip's number of travellers.
//
// Returns:
//   - the actual cost, multiplied for a per-person cost, or zero without one.
func (i Item) ActualCostTotal(travelers int) Money {
	return perGroup(i.ActualCost, i.CostPerPerson, travelers)
}

// perGroup turns an amount into what the whole group pays.
func perGroup(amount *Money, perPerson bool, travelers int) Money {
	if amount == nil {
		return 0
	}
	if perPerson {
		return *amount * Money(max(travelers, 1))
	}
	return *amount
}

// The range of an activity's difficulty: very easy, easy, moderate, hard and
// extreme.
const (
	MinDifficulty = 1
	MaxDifficulty = 5
)

// OrderByActualTime - puts the places of a report's day in the order they were
// reached. The places with a time are sorted by it and take the positions the
// timed places held between them; a place without a time keeps its position,
// since nothing says where else it belongs. Equal times keep their order.
//
// Arguments:
//   - places: the places and activities of one day, in their current order.
//
// Returns:
//   - their identifiers in the new order.
func OrderByActualTime(places []Item) []uuid.UUID {
	timed := make([]Item, 0, len(places))
	for _, place := range places {
		if place.ActualTime != nil {
			timed = append(timed, place)
		}
	}
	sort.SliceStable(timed, func(a, b int) bool { return *timed[a].ActualTime < *timed[b].ActualTime })

	order := make([]uuid.UUID, len(places))
	next := 0
	for index, place := range places {
		if place.ActualTime == nil {
			order[index] = place.ID
			continue
		}
		order[index] = timed[next].ID
		next++
	}
	return order
}

// reportOnly is the error for a field only a report may carry.
func reportOnly(field string) error {
	return NewValidationError(field, "report_only", "belongs to a report, not to a plan")
}

// DocumentContent is a whole document with everything inside it.
type DocumentContent struct {
	Document Document
	// Days are ordered by position.
	Days []Day
	// Items are every place and mark, in no particular order.
	Items []Item
	// Stays are ordered by check-in.
	Stays []Stay
	// Legs are every leg of the days, in no particular order.
	Legs []Leg
	// Expenses are the costs tied to no place, stay or leg, oldest first.
	Expenses []Expense
	// Tracks are the lines of the places and activities that have one: in a
	// report what was recorded, in a plan the route intended.
	Tracks []Track
	// Translations are the report's words in its further languages, the trip's
	// own title and summary aside; a plan has none.
	Translations []Translation
}

// checkLength refuses text longer than a limit, counted in characters.
func checkLength(field, value string, limit int) error {
	if utf8.RuneCountInString(value) > limit {
		return NewValidationError(field, "too_long", fmt.Sprintf("must be at most %d characters", limit))
	}
	return nil
}

// trimmedText trims a single-line value and checks its length.
func trimmedText(field, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	return value, checkLength(field, value, limit)
}

// validateCoordinates checks that coordinates come as a pair and lie on Earth.
func validateCoordinates(lat, lng *float64) error {
	if (lat == nil) != (lng == nil) {
		return NewValidationError("lng", "coordinates_incomplete", "latitude and longitude are set together")
	}
	if lat == nil {
		return nil
	}
	if math.IsNaN(*lat) || *lat < -90 || *lat > 90 {
		return NewValidationError("lat", "out_of_range", "must be between -90 and 90")
	}
	if math.IsNaN(*lng) || *lng < -180 || *lng > 180 {
		return NewValidationError("lng", "out_of_range", "must be between -180 and 180")
	}
	return nil
}

// normalizeURL accepts an empty value or an absolute http(s) address. Other
// schemes are refused: the interface renders these as links.
func normalizeURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	invalid := NewValidationError("url", "invalid_url", "must be an http or https address")
	if utf8.RuneCountInString(value) > maxURLLength {
		return "", invalid
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", invalid
	}
	return value, nil
}
