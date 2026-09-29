package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxTagNameLength bounds a tag's name, which is a word or two, not a note.
const maxTagNameLength = 50

// MaxTripTags bounds how many tags one person puts on one trip. A person may
// keep as many tags as they like; one trip wearing hundreds of them is a
// mistake of the client.
const MaxTripTags = 100

// TagColor is the colour a tag is shown in: a key of the interface's palette
// rather than a value, so a tag reads alike in the light and the dark theme.
type TagColor string

// TagColors is the palette, in the order new tags and packing categories take
// it: neighbours in the order are far apart in hue, so a list made without
// choosing comes out in colours that tell its entries apart.
var TagColors = []TagColor{
	"red", "orange", "amber", "green", "teal", "sky", "blue", "violet", "pink", "gray",
	"yellow", "emerald", "indigo", "fuchsia", "lime", "cyan", "purple", "rose", "brown",
}

// ValidateTagColor - checks that a colour is one of the palette's.
//
// Arguments:
//   - color: the colour to check.
//
// Returns:
//   - a *ValidationError on color when it is not in the palette.
func ValidateTagColor(color TagColor) error {
	for _, known := range TagColors {
		if color == known {
			return nil
		}
	}
	return NewValidationError("color", "unsupported", "must be a colour of the palette")
}

// Tag is a word a person marks their trips with. Tags belong to the person, not
// to a trip: two members of a trip tag it each in their own way, and neither
// sees the other's tags.
type Tag struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Name   string
	// Color is fixed when the tag is made and changed only on purpose.
	Color TagColor
	// TripCount is how many of the trips the person can still open wear the
	// tag; it is filled only where tags are listed for managing them.
	TripCount int
	// IdeaCount is how many of the person's ideas wear the tag, filled where
	// TripCount is.
	IdeaCount int
	CreatedAt time.Time
}

// TripTag is a tag as a trip wears it: enough to show and to name it.
type TripTag struct {
	ID    uuid.UUID
	Name  string
	Color TagColor
}

// NormalizeTagName - trims a tag's name and checks its length.
//
// Inner runs of spaces are folded into one, so "city  break" and "city break"
// are the same tag rather than two that look alike.
//
// Arguments:
//   - name: the name as typed.
//
// Returns:
//   - the name as it is stored.
//   - a *ValidationError on name when it is empty or too long.
func NormalizeTagName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return name, NewValidationError("name", "required", "must not be empty")
	}
	if utf8.RuneCountInString(name) > maxTagNameLength {
		return name, NewValidationError("name", "too_long", "must be at most 50 characters")
	}
	return name, nil
}
