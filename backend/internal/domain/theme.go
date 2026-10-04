package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ThemeFileFormat is the version of the theme file an administrator uploads.
// A file of another version is refused rather than guessed at.
const ThemeFileFormat = 1

// maxThemeNameLength bounds a theme's name, which is a word or two.
const maxThemeNameLength = 50

// maxThemeColorLength bounds one colour value; the longest legitimate one,
// an oklch() with an alpha, is well under it.
const maxThemeColorLength = 64

// ThemeColorNames are the colours a palette sets, the interface's own
// variables without their "--color-" prefix. A palette names every one of
// them, so a theme never leaves a colour of the built-in theme showing
// through.
var ThemeColorNames = []string{
	"base-100", "base-200", "base-300", "base-content",
	"primary", "primary-content", "secondary", "secondary-content",
	"accent", "accent-content", "neutral", "neutral-content",
	"info", "info-content", "success", "success-content",
	"warning", "warning-content", "error", "error-content",
}

// hexColorPattern matches #rgb, #rgba, #rrggbb and #rrggbbaa.
var hexColorPattern = regexp.MustCompile(`^#([0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$`)

// functionColorPattern matches a CSS colour function whose arguments are
// numbers, units, separators and keywords such as "none". It admits no
// parenthesis, quote, semicolon or brace inside, so a value is written into
// a style sheet as it is and can never close the declaration it stands in.
var functionColorPattern = regexp.MustCompile(`^(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\([0-9a-z.%+\-,/ ]+\)$`)

// ThemePalette maps every name of ThemeColorNames to a colour value.
type ThemePalette map[string]string

// Theme variants a palette is shown for.
const (
	ThemeVariantLight = "light"
	ThemeVariantDark  = "dark"
)

// InstanceTheme is a colour theme an administrator uploaded for everybody on
// the instance. It carries a light palette, a dark one or both; with one, the
// interface is shown in it whatever the person's light or dark preference.
type InstanceTheme struct {
	ID        uuid.UUID
	Name      string
	Light     ThemePalette
	Dark      ThemePalette
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ThemeFile is the theme file an administrator uploads and downloads.
type ThemeFile struct {
	Format int          `json:"format"`
	Name   string       `json:"name"`
	Light  ThemePalette `json:"light,omitempty"`
	Dark   ThemePalette `json:"dark,omitempty"`
}

// ValidateThemeColor - checks that a value is a colour a style sheet may
// carry as it is.
//
// Hexadecimal colours and the CSS colour functions are accepted; names,
// variables and anything else are not, because a value goes into the page's
// style sheet unchanged.
//
// Arguments:
//   - value: the colour as written in the file.
//
// Returns:
//   - true when the value is accepted.
func ValidateThemeColor(value string) bool {
	if len(value) > maxThemeColorLength {
		return false
	}
	value = strings.ToLower(value)
	return hexColorPattern.MatchString(value) || functionColorPattern.MatchString(value)
}

// normalizeThemePalette checks one palette of a theme file and returns it
// with its values trimmed and lowercased. field names the palette in the
// error, "light" or "dark".
func normalizeThemePalette(field string, palette ThemePalette) (ThemePalette, error) {
	known := make(map[string]bool, len(ThemeColorNames))
	for _, name := range ThemeColorNames {
		known[name] = true
	}
	for name := range palette {
		if !known[name] {
			return nil, NewValidationError(field+"."+name, "unsupported", "is not a colour of the interface")
		}
	}
	normalized := make(ThemePalette, len(ThemeColorNames))
	for _, name := range ThemeColorNames {
		value, ok := palette[name]
		if !ok {
			return nil, NewValidationError(field+"."+name, "required", "must be set")
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if !ValidateThemeColor(value) {
			return nil, NewValidationError(field+"."+name, "invalid",
				"must be a hexadecimal colour or a CSS colour function")
		}
		normalized[name] = value
	}
	return normalized, nil
}

// ParseThemeFile - checks an uploaded theme file and turns it into a theme.
//
// Arguments:
//   - file: the decoded file.
//
// Returns:
//   - the theme with a trimmed name and normalised palettes, without its
//     identifier and times.
//   - a *ValidationError naming the first thing wrong with the file.
func ParseThemeFile(file ThemeFile) (InstanceTheme, error) {
	if file.Format != ThemeFileFormat {
		return InstanceTheme{}, NewValidationError("format", "unsupported", "must be 1")
	}
	name := strings.Join(strings.Fields(file.Name), " ")
	if name == "" {
		return InstanceTheme{}, NewValidationError("name", "required", "must not be empty")
	}
	if utf8.RuneCountInString(name) > maxThemeNameLength {
		return InstanceTheme{}, NewValidationError("name", "too_long", "must be at most 50 characters")
	}
	if file.Light == nil && file.Dark == nil {
		return InstanceTheme{}, NewValidationError("light", "required", "a light or a dark palette must be set")
	}
	theme := InstanceTheme{Name: name}
	var err error
	if file.Light != nil {
		if theme.Light, err = normalizeThemePalette(ThemeVariantLight, file.Light); err != nil {
			return InstanceTheme{}, err
		}
	}
	if file.Dark != nil {
		if theme.Dark, err = normalizeThemePalette(ThemeVariantDark, file.Dark); err != nil {
			return InstanceTheme{}, err
		}
	}
	return theme, nil
}
