package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Theme is a user's interface theme preference. ThemeAuto follows the
// operating system or browser setting rather than pinning a palette.
type Theme string

// Supported theme values.
const (
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
	ThemeAuto  Theme = "auto"
)

// SupportedLocales are the interface languages this build ships. An empty
// locale on an account means "follow the browser".
var SupportedLocales = []string{"en", "ru"}

// DefaultCurrency is the currency new accounts propose for new trips.
const DefaultCurrency = "EUR"

// User is an account that can sign in to the service.
type User struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	PasswordHash string
	IsAdmin      bool
	IsActive     bool
	// MustChangePassword is set when an administrator chose the password - on
	// creation or on a reset - and cleared when the owner picks their own.
	MustChangePassword bool
	Locale             string
	Theme              Theme
	Units              Units
	DateFormat         DateFormat
	TimeFormat         TimeFormat
	DefaultCurrency    string
	// AvatarKey is where the account's picture lives in the media store, empty
	// when it wears none; AvatarUpdatedAt is when it last changed, which is what
	// makes a browser fetch the new one.
	AvatarKey       string
	AvatarUpdatedAt *time.Time
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Session is one signed-in device: a stored, revocable refresh token.
//
// The row is the session and keeps its identifier for its whole life; a refresh
// replaces the token hash in place. Only the hash is stored, so a leaked
// database cannot be used to mint sessions.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TokenHash  []byte
	UserAgent  string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
}

// IsUsable - reports whether the session may still be refreshed.
//
// Arguments:
//   - now: the reference time, passed in so the check is testable.
//
// Returns:
//   - true when the session has neither expired nor been revoked.
func (s Session) IsUsable(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

// Input bounds for account fields.
const (
	maxEmailLength       = 320
	maxDisplayNameLength = 120
	// MaxUserAgentLength bounds the stored user agent; the header is
	// client-controlled and only shown to the account owner.
	MaxUserAgentLength = 255
)

// currencyPattern matches an ISO 4217 alphabetic code. The list of codes is
// not checked: it changes, and a wrong code only affects formatting.
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// NormalizeEmail - trims and lowercases an email address and checks its shape.
//
// The check is deliberately shallow - a local part, an "@", a domain with a dot
// and no whitespace. A stricter pattern rejects addresses that work, and nothing
// here sends mail to prove an address exists.
//
// Arguments:
//   - email: the address as typed.
//
// Returns:
//   - the normalised address.
//   - a *ValidationError when it is not an address.
func NormalizeEmail(email string) (string, error) {
	address := strings.ToLower(strings.TrimSpace(email))
	local, host, found := strings.Cut(address, "@")
	if !found || local == "" || !strings.Contains(host, ".") ||
		strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") ||
		strings.ContainsAny(address, " \t\r\n") || len(address) > maxEmailLength {
		return "", NewValidationError("email", "invalid_email", "must be an email address")
	}
	return address, nil
}

// NormalizeDisplayName - trims a display name and checks its length.
//
// Arguments:
//   - name: the name as typed.
//
// Returns:
//   - the trimmed name.
//   - a *ValidationError when it is empty or too long.
func NormalizeDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", NewValidationError("display_name", "required", "must not be empty")
	}
	if utf8.RuneCountInString(trimmed) > maxDisplayNameLength {
		return "", NewValidationError("display_name", "too_long", "must be at most 120 characters")
	}
	return trimmed, nil
}

// ValidateLocale - checks that a locale is empty or one this build ships.
//
// Arguments:
//   - locale: the language code.
//
// Returns:
//   - a *ValidationError when the language is not supported.
func ValidateLocale(locale string) error {
	if locale == "" {
		return nil
	}
	for _, supported := range SupportedLocales {
		if locale == supported {
			return nil
		}
	}
	return NewValidationError("locale", "unsupported", "is not a supported language")
}

// Units is how far a distance is shown in. Everything is stored in metres; this
// decides only what a reader sees, because two people on one trip may count in
// different units.
type Units string

// The units a distance is shown in.
const (
	// UnitsKilometres shows metres and kilometres.
	UnitsKilometres Units = "km"
	// UnitsMiles shows feet and miles.
	UnitsMiles Units = "mi"
)

// OrDefault - fills in the units of an account that names none.
//
// A row always carries them, but an account read by something that predates the
// setting - a fake in a test, a client of an older version - would otherwise be
// refused for a preference nobody expressed. Kilometres are what the column
// defaults to.
//
// Returns:
//   - the units, or kilometres when none are set.
func (u Units) OrDefault() Units {
	if u == "" {
		return UnitsKilometres
	}
	return u
}

// ValidateUnits - checks that a value names units the interface knows.
//
// Arguments:
//   - units: the value.
//
// Returns:
//   - a *ValidationError when the value is unknown.
func ValidateUnits(units Units) error {
	switch units {
	case UnitsKilometres, UnitsMiles:
		return nil
	default:
		return NewValidationError("units", "unsupported", "must be km or mi")
	}
}

// DateFormat is the order a date is written in for a reader. Like the units, it
// changes nothing that is stored: two people on one trip may read the same day
// as 21/3 and as 3/21.
type DateFormat string

// The orders a date is written in.
const (
	// DateFormatDayMonth writes the day before the month.
	DateFormatDayMonth DateFormat = "dmy"
	// DateFormatMonthDay writes the month before the day.
	DateFormatMonthDay DateFormat = "mdy"
)

// TimeFormat is the clock a time of day is read on.
type TimeFormat string

// The clocks a time is read on.
const (
	// TimeFormatH24 writes 14:00.
	TimeFormatH24 TimeFormat = "h24"
	// TimeFormatH12 writes 2:00 PM.
	TimeFormatH12 TimeFormat = "h12"
)

// OrDefault - fills in the date order of an account that names none, the way
// Units.OrDefault does, so an account read by something that predates the
// setting is not refused for a preference nobody expressed.
//
// Returns:
//   - the order, or day before month when none is set.
func (f DateFormat) OrDefault() DateFormat {
	if f == "" {
		return DateFormatDayMonth
	}
	return f
}

// OrDefault - fills in the clock of an account that names none.
//
// Returns:
//   - the clock, or the 24-hour one when none is set.
func (f TimeFormat) OrDefault() TimeFormat {
	if f == "" {
		return TimeFormatH24
	}
	return f
}

// ValidateDateFormat - checks that a value names an order the interface knows.
//
// Arguments:
//   - format: the value.
//
// Returns:
//   - a *ValidationError when the value is unknown.
func ValidateDateFormat(format DateFormat) error {
	switch format {
	case DateFormatDayMonth, DateFormatMonthDay:
		return nil
	default:
		return NewValidationError("date_format", "unsupported", "must be dmy or mdy")
	}
}

// ValidateTimeFormat - checks that a value names a clock the interface knows.
//
// Arguments:
//   - format: the value.
//
// Returns:
//   - a *ValidationError when the value is unknown.
func ValidateTimeFormat(format TimeFormat) error {
	switch format {
	case TimeFormatH24, TimeFormatH12:
		return nil
	default:
		return NewValidationError("time_format", "unsupported", "must be h24 or h12")
	}
}

// ValidateTheme - checks that a theme is one the interface knows.
//
// Arguments:
//   - theme: the theme value.
//
// Returns:
//   - a *ValidationError when the value is unknown.
func ValidateTheme(theme Theme) error {
	switch theme {
	case ThemeLight, ThemeDark, ThemeAuto:
		return nil
	default:
		return NewValidationError("theme", "unsupported", "must be light, dark or auto")
	}
}

// ValidateCurrency - checks that a currency looks like an ISO 4217 code.
//
// Arguments:
//   - currency: the code, already uppercased by the caller.
//
// Returns:
//   - a *ValidationError when it is not three capital letters.
func ValidateCurrency(currency string) error {
	if !currencyPattern.MatchString(currency) {
		return NewValidationError("default_currency", "invalid_currency", "must be an ISO 4217 code")
	}
	return nil
}

// Profile is the part of an account its owner may change.
type Profile struct {
	DisplayName     string
	Locale          string
	Theme           Theme
	Units           Units
	DateFormat      DateFormat
	TimeFormat      TimeFormat
	DefaultCurrency string
}

// Validate - checks a profile against the account rules.
//
// Returns:
//   - the first *ValidationError found, or nil.
func (p Profile) Validate() error {
	if _, err := NormalizeDisplayName(p.DisplayName); err != nil {
		return err
	}
	if err := ValidateLocale(p.Locale); err != nil {
		return err
	}
	if err := ValidateTheme(p.Theme); err != nil {
		return err
	}
	if err := ValidateUnits(p.Units); err != nil {
		return err
	}
	if err := ValidateDateFormat(p.DateFormat); err != nil {
		return err
	}
	if err := ValidateTimeFormat(p.TimeFormat); err != nil {
		return err
	}
	return ValidateCurrency(p.DefaultCurrency)
}

// UserChanges is what an administrator may change on an account. A nil field is
// left as it is.
type UserChanges struct {
	DisplayName *string
	IsAdmin     *bool
	IsActive    *bool
}

// UserStats are the account counts the status screen shows.
type UserStats struct {
	Total  int64
	Active int64
	Admins int64
}
