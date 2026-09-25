package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TravelMode is how a leg between two places is travelled.
type TravelMode string

// Supported travel modes.
const (
	ModeWalk    TravelMode = "walk"
	ModeCar     TravelMode = "car"
	ModeBike    TravelMode = "bike"
	ModeTransit TravelMode = "transit"
	ModeFlight  TravelMode = "flight"
	// ModeCableCar is a cable car, a gondola or a chairlift: it hangs on a
	// straight cable, so it is drawn as a line rather than routed on roads.
	ModeCableCar TravelMode = "cable_car"
	ModeOther    TravelMode = "other"
)

// ValidateTravelMode - checks that a travel mode is one the service knows.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - mode: the value to check.
//
// Returns:
//   - a *ValidationError when the value is unknown.
func ValidateTravelMode(field string, mode TravelMode) error {
	switch mode {
	case ModeWalk, ModeCar, ModeBike, ModeTransit, ModeFlight, ModeCableCar, ModeOther:
		return nil
	default:
		return NewValidationError(field, "unsupported", "must be walk, car, bike, transit, flight, cable_car or other")
	}
}

// TripRole is what a person may do with one trip. The owner is recorded on the
// trip itself; editors and viewers are members.
type TripRole string

// Trip roles.
const (
	RoleOwner  TripRole = "owner"
	RoleEditor TripRole = "editor"
	RoleViewer TripRole = "viewer"
)

// ValidateMemberRole - checks a role that can be given to a member.
//
// The owner role is not among them: a trip keeps the owner it was created with,
// and members are only editors and viewers.
//
// Arguments:
//   - role: the requested role.
//
// Returns:
//   - a *ValidationError when the role is not editor or viewer.
func ValidateMemberRole(role TripRole) error {
	if role == RoleEditor || role == RoleViewer {
		return nil
	}
	return NewValidationError("role", "unsupported", "must be editor or viewer")
}

// TripAction is something a trip role may or may not be allowed to do.
type TripAction int

// Trip actions, following the role table of the specification (§3.2).
const (
	// ActionView reads the plan, the report and the budget, private media included.
	ActionView TripAction = iota
	// ActionEdit changes the trip, its documents and media.
	ActionEdit
	// ActionManageMembers adds and removes members and changes their roles.
	ActionManageMembers
	// ActionManageShareLinks creates and revokes share links.
	ActionManageShareLinks
	// ActionDelete deletes the trip.
	ActionDelete
)

// Can - reports whether a role allows an action.
//
// Editing data does not grant deciding who sees it: every access decision is
// the owner's alone.
//
// Arguments:
//   - action: the action being attempted.
//
// Returns:
//   - true when the role allows it.
func (r TripRole) Can(action TripAction) bool {
	switch r {
	case RoleOwner:
		return true
	case RoleEditor:
		return action == ActionView || action == ActionEdit
	case RoleViewer:
		return action == ActionView
	default:
		return false
	}
}

// TripStatus is where a trip stands relative to today, derived from its dates.
type TripStatus string

// Trip statuses. StatusNoDates cannot be reached through the API, which requires
// both dates, and stays as the answer for a trip read without them.
const (
	StatusNoDates   TripStatus = "no_dates"
	StatusUpcoming  TripStatus = "upcoming"
	StatusOngoing   TripStatus = "ongoing"
	StatusCompleted TripStatus = "completed"
)

// DocumentKind distinguishes the two kinds of trip and the document each holds:
// a plan of a journey to come, or a report of one made.
type DocumentKind string

// Document kinds.
const (
	DocumentPlan   DocumentKind = "plan"
	DocumentReport DocumentKind = "report"
)

// ValidateDocumentKind - checks that a value names a kind of trip.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - kind: the value to check.
//
// Returns:
//   - a *ValidationError when the value is not plan or report.
func ValidateDocumentKind(field string, kind DocumentKind) error {
	if kind == DocumentPlan || kind == DocumentReport {
		return nil
	}
	return NewValidationError(field, "unsupported", "must be plan or report")
}

// DateLayout is how calendar dates travel through the API.
const DateLayout = "2006-01-02"

// Input bounds for trip fields.
const (
	maxTripTitleLength   = 200
	maxTripSummaryLength = 2000
	// MaxTripDays bounds a trip's period, which keeps a mistyped year from
	// producing thousands of days.
	MaxTripDays   = 366
	maxTravelers  = 100
	defaultTZName = "UTC"
)

// Trip is a trip as stored. Its kind says which document it holds; a report is
// a trip of its own, with its own people, links, files and budget, rather than a
// part of the plan it may have been copied from.
type Trip struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Kind    DocumentKind
	// SourceTripID is the plan a report was copied from; nil for a plan, for a
	// report written from scratch and once that plan is deleted.
	SourceTripID *uuid.UUID
	Title        string
	Summary      string
	StartDate    *time.Time
	EndDate      *time.Time
	Timezone     string
	Currency     string
	Travelers    int
	Budget       *Money
	// CoverMediaID is the picture the trip is shown by, out of its own media.
	CoverMediaID *uuid.UUID
	// Languages are a report's languages: the original first, then the ones it
	// is translated into. A plan has none.
	Languages []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Normalize - trims a trip's text fields, fills defaults and checks every rule.
//
// Arguments:
//   - none; the receiver is the trip as the client described it.
//
// Returns:
//   - the normalised trip.
//   - the first *ValidationError found.
func (t Trip) Normalize() (Trip, error) {
	if err := ValidateDocumentKind("kind", t.Kind); err != nil {
		return t, err
	}
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return t, NewValidationError("title", "required", "must not be empty")
	}
	if utf8.RuneCountInString(t.Title) > maxTripTitleLength {
		return t, NewValidationError("title", "too_long", "must be at most 200 characters")
	}
	t.Summary = strings.TrimSpace(t.Summary)
	if utf8.RuneCountInString(t.Summary) > maxTripSummaryLength {
		return t, NewValidationError("summary", "too_long", "must be at most 2000 characters")
	}

	// A trip is planned over a period: the days of a plan take their dates from
	// it, and a trip without them would leave every day undated.
	if t.StartDate == nil {
		return t, NewValidationError("start_date", "required", "must be set")
	}
	if t.EndDate == nil {
		return t, NewValidationError("end_date", "required", "must be set")
	}
	if t.EndDate.Before(*t.StartDate) {
		return t, NewValidationError("end_date", "end_before_start", "must not be before the start date")
	}
	if days := *t.DayCount(); days > MaxTripDays {
		return t, NewValidationError("end_date", "trip_too_long", "a trip lasts at most 366 days")
	}

	t.Timezone = strings.TrimSpace(t.Timezone)
	if t.Timezone == "" {
		t.Timezone = defaultTZName
	}
	// "Local" would mean the server's zone, which says nothing about the trip.
	if _, err := time.LoadLocation(t.Timezone); err != nil || t.Timezone == "Local" {
		return t, NewValidationError("timezone", "invalid_timezone", "must be an IANA time zone name")
	}

	t.Currency = strings.ToUpper(strings.TrimSpace(t.Currency))
	if !currencyPattern.MatchString(t.Currency) {
		return t, NewValidationError("currency", "invalid_currency", "must be an ISO 4217 code")
	}
	if t.Travelers < 1 || t.Travelers > maxTravelers {
		return t, NewValidationError("travelers", "out_of_range", "must be between 1 and 100")
	}
	var err error
	t.Languages, err = NormalizeLanguages(t.Kind, t.Languages, "")
	return t, err
}

// DayCount - reports how many calendar days the trip's period covers.
//
// Returns:
//   - the inclusive number of days, or nil for a trip without dates.
func (t Trip) DayCount() *int {
	if t.StartDate == nil || t.EndDate == nil {
		return nil
	}
	days := int(t.EndDate.Sub(*t.StartDate).Hours()/24) + 1
	return &days
}

// StatusAt - derives the trip's status from its dates.
//
// "Today" is read in the trip's own time zone: a trip in Iceland starts on
// Icelandic midnight, whatever zone the server or the reader is in.
//
// Arguments:
//   - now: the reference instant, passed in so the rule is testable.
//
// Returns:
//   - the status.
func (t Trip) StatusAt(now time.Time) TripStatus {
	if t.StartDate == nil || t.EndDate == nil {
		return StatusNoDates
	}
	location, err := time.LoadLocation(t.Timezone)
	if err != nil {
		location = time.UTC
	}
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case today.Before(dateOnly(*t.StartDate)):
		return StatusUpcoming
	case today.After(dateOnly(*t.EndDate)):
		return StatusCompleted
	default:
		return StatusOngoing
	}
}

// dateOnly drops the clock and zone from a calendar date read from storage.
func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

// ParseDate - reads a calendar date in the API's YYYY-MM-DD form.
//
// Arguments:
//   - field: the input field name, used in the validation error.
//   - value: the date string.
//
// Returns:
//   - the date at midnight UTC.
//   - a *ValidationError when the string is not a date.
func ParseDate(field, value string) (time.Time, error) {
	date, err := time.Parse(DateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, NewValidationError(field, "invalid_date", "must be a date in YYYY-MM-DD form")
	}
	return date, nil
}

// TripUser is the part of an account shown inside a trip: enough to recognise a
// person, nothing about their account settings.
type TripUser struct {
	ID          uuid.UUID
	DisplayName string
	Email       string
}

// TripSummary is a trip as one person sees it in lists and on its own page.
type TripSummary struct {
	Trip
	Owner TripUser
	// Role is the reader's role; empty in the list the service reads on nobody's
	// behalf.
	Role TripRole
	// PlanID and ReportID name the trip's document: exactly one is set, the one
	// of the trip's kind.
	PlanID   *uuid.UUID
	ReportID *uuid.UUID
	// SourceVisible says whether the reader may open SourceTripID. The link back
	// to a plan is shown only to somebody who can follow it, so a report handed
	// to other people does not tell them which plans exist.
	SourceVisible bool
	// Translations are the report's title and summary in its further languages.
	Translations TripTranslations
}

// DocumentID - names the trip's one document.
//
// Returns:
//   - the plan of a plan, the report of a report; nil when neither was read.
func (s TripSummary) DocumentID() *uuid.UUID {
	if s.Kind == DocumentReport {
		return s.ReportID
	}
	return s.PlanID
}

// TripMember is a person with access to a trip, the owner included.
type TripMember struct {
	User      TripUser
	Role      TripRole
	CreatedAt time.Time
}

// TripScope narrows a trip list by the reader's relation to the trips.
type TripScope string

// Trip list scopes.
const (
	ScopeAll    TripScope = "all"
	ScopeOwned  TripScope = "owned"
	ScopeShared TripScope = "shared"
)

// TripSort orders a trip list.
type TripSort string

// Trip list orders.
const (
	// SortRelevance puts what matters now first: ongoing trips, then upcoming
	// ones, then completed ones from the most recent.
	SortRelevance TripSort = "relevance"
	// SortUpdated puts the trip whose content changed last first, whoever
	// changed it.
	SortUpdated TripSort = "updated"
)

// Page size bounds for lists.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// TripFilter selects and pages a trip list.
type TripFilter struct {
	Scope TripScope
	// Kind keeps the plans or the reports; empty keeps both, which is what the
	// search across the whole service asks for.
	Kind DocumentKind
	// Year keeps trips whose period touches the year; zero keeps all.
	Year int
	// Query matches the title, case-insensitively.
	Query string
	// Sort is the order of the list; a cursor is valid only in the order it
	// was returned for.
	Sort  TripSort
	Limit int
	// Cursor is the opaque position returned with the previous page.
	Cursor string
}

// Normalize - checks a trip filter and fills its defaults.
//
// Returns:
//   - the normalised filter.
//   - a *ValidationError naming the first invalid parameter.
func (f TripFilter) Normalize() (TripFilter, error) {
	switch f.Scope {
	case "":
		f.Scope = ScopeAll
	case ScopeAll, ScopeOwned, ScopeShared:
	default:
		return f, NewValidationError("scope", "unsupported", "must be all, owned or shared")
	}
	switch f.Sort {
	case "":
		f.Sort = SortRelevance
	case SortRelevance, SortUpdated:
	default:
		return f, NewValidationError("sort", "unsupported", "must be relevance or updated")
	}
	if f.Kind != "" {
		if err := ValidateDocumentKind("kind", f.Kind); err != nil {
			return f, err
		}
	}
	if f.Year != 0 && (f.Year < 1900 || f.Year > 3000) {
		return f, NewValidationError("year", "out_of_range", "must be a calendar year")
	}
	f.Query = strings.TrimSpace(f.Query)
	if utf8.RuneCountInString(f.Query) > maxTripTitleLength {
		return f, NewValidationError("q", "too_long", "must be at most 200 characters")
	}
	f.Limit = normalizePageSize(f.Limit)
	return f, nil
}

// normalizePageSize applies the default page size and clamps it to the maximum.
func normalizePageSize(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	return min(limit, MaxPageSize)
}

// AdminTripFilter pages the list of every trip on the instance, whoever owns it.
type AdminTripFilter struct {
	Query  string
	Limit  int
	Cursor string
}

// Normalize - checks the filter of every trip and fills its defaults.
//
// Returns:
//   - the normalised filter.
//   - a *ValidationError when the query is too long.
func (f AdminTripFilter) Normalize() (AdminTripFilter, error) {
	f.Query = strings.TrimSpace(f.Query)
	if utf8.RuneCountInString(f.Query) > maxTripTitleLength {
		return f, NewValidationError("q", "too_long", "must be at most 200 characters")
	}
	f.Limit = normalizePageSize(f.Limit)
	return f, nil
}

// TripPage is one page of a trip list.
type TripPage struct {
	Items []TripSummary
	// NextCursor continues the list; empty on the last page.
	NextCursor string
}
