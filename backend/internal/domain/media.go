package domain

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

// A file belongs to a trip and is shown wherever it is linked: the trip itself,
// a day of a document, or a place. The bytes live in the media store and are
// served only through the API, so the rules below - who may read a trip, and
// whether a read-only link shows private files - are the only way in.

// Errors the media rules report.
var (
	// ErrMediaTooLarge reports a file above the configured size limit.
	ErrMediaTooLarge = errors.New("the file is larger than this service accepts")
	// ErrMediaQuota reports a trip that has used up the space allowed for it.
	ErrMediaQuota = errors.New("the trip has no space left for more files")
	// ErrMediaUnsupported reports a kind of file this service does not store.
	ErrMediaUnsupported = errors.New("this kind of file is not accepted")
	// ErrMediaDuplicate reports a picture the trip already holds: the same
	// bytes, or the same original the browser shrank before sending.
	ErrMediaDuplicate = errors.New("the trip already holds this picture")
)

// MediaStatus says whether a file is ready to be served. Everything the current
// scope accepts is ready at once; the other two belong to the formats a later
// stage converts in the background.
type MediaStatus string

// Media statuses.
const (
	MediaReady      MediaStatus = "ready"
	MediaProcessing MediaStatus = "processing"
	MediaFailed     MediaStatus = "failed"
)

// MediaTarget is the kind of thing a file is shown under.
type MediaTarget string

// Media link targets.
const (
	MediaTargetTrip MediaTarget = "trip"
	MediaTargetDay  MediaTarget = "day"
	MediaTargetItem MediaTarget = "item"
)

// MediaTargets are the targets a link may point at.
var MediaTargets = []MediaTarget{MediaTargetTrip, MediaTargetDay, MediaTargetItem}

// MediaFavoriteLimit is how many pictures a day or a place shows in the report.
// A report is read, not browsed: a page of a hundred photographs is a gallery,
// and the gallery of the trip is where the rest of them live.
const MediaFavoriteLimit = 7

// Errors the favourites of a gallery report.
var (
	// ErrMediaFavoriteTarget reports favourites asked for outside a report: a
	// plan's days and places carry pictures, but show all of them.
	ErrMediaFavoriteTarget = NewValidationError("target_id", "favorite_not_report",
		"only the days and places of a report have favourite pictures")
	// ErrMediaFavoriteLimit reports a day or place given more favourites than a
	// report shows.
	ErrMediaFavoriteLimit = NewValidationError("media_ids", "favorite_limit",
		"a day or a place shows at most seven favourite pictures")
	// ErrMediaFavoriteUnlinked reports a favourite asked for a file that does
	// not hang on the day or the place at all.
	ErrMediaFavoriteUnlinked = NewValidationError("media_ids", "favorite_not_linked",
		"a picture is shown as a favourite only where it already hangs")
)

// maxOriginalNameLength bounds the name a browser sends with an upload.
const maxOriginalNameLength = 255

// Media is one stored file with what is known about it.
type Media struct {
	ID     uuid.UUID
	TripID uuid.UUID
	// StorageKey is where the bytes live inside the media store.
	StorageKey   string
	OriginalName string
	MIME         string
	Size         int64
	// Checksum is the SHA-256 of the bytes.
	Checksum []byte
	// SourceChecksum is the SHA-256 of the file the browser was given, before
	// it shrank it; nil when the browser could not say. Two uploads of one
	// photograph shrunk differently share it where their bytes differ.
	SourceChecksum []byte
	Width          int
	Height         int
	// TakenAt, Lat and Lng come from the picture's own metadata and are empty
	// when it carries none.
	TakenAt *time.Time
	Lat     *float64
	Lng     *float64
	// IsPrivate keeps the file out of read-only links that do not ask for
	// private files.
	IsPrivate bool
	Status    MediaStatus
	// UploadedBy is nil once that account has been deleted; the file stays with
	// the trip it belongs to.
	UploadedBy *uuid.UUID
	CreatedAt  time.Time
}

// MediaChanges holds the fields of a file that can be changed after it is
// stored. Everything else describes the bytes and cannot move.
type MediaChanges struct {
	IsPrivate *bool
}

// MediaLink is one place a file is shown, and where it sits among the others.
type MediaLink struct {
	MediaID  uuid.UUID
	Target   MediaTarget
	TargetID uuid.UUID
	Position int
	// IsFavorite marks one of the pictures the report shows for this day or
	// place, out of everything that hangs on it.
	IsFavorite bool
}

// Normalize - trims the name a file was uploaded under and checks the fields
// the storage layer will not check for itself.
//
// Returns:
//   - the normalised file.
//   - the first *ValidationError found.
func (m Media) Normalize() (Media, error) {
	var err error
	if m.OriginalName, err = trimmedText("original_name", m.OriginalName, maxOriginalNameLength); err != nil {
		return Media{}, err
	}
	if m.Size <= 0 {
		return Media{}, NewValidationError("file", "empty_file", "the file is empty")
	}
	if m.Status == "" {
		m.Status = MediaReady
	}
	if (m.Lat == nil) != (m.Lng == nil) {
		m.Lat, m.Lng = nil, nil
	}
	return m, nil
}

// ParseMediaTarget - reads the target of a link as the API spells it.
//
// Arguments:
//   - value: "trip", "day" or "item".
//
// Returns:
//   - the target.
//   - a *ValidationError for anything else.
func ParseMediaTarget(value string) (MediaTarget, error) {
	for _, target := range MediaTargets {
		if MediaTarget(value) == target {
			return target, nil
		}
	}
	return "", NewValidationError("target_type", "invalid_target", "must be trip, day or item")
}

// VisibleTo - says whether a read-only link may show this file.
//
// Arguments:
//   - includePrivate: whether the link was created with private files allowed.
//
// Returns:
//   - true when the file may be served through that link.
func (m Media) VisibleTo(includePrivate bool) bool {
	return !m.IsPrivate || includePrivate
}

// MediaBefore - reports whether one picture comes before another in a gallery.
//
// Every gallery - a day's, a place's, the trip's own list - reads in the order
// the pictures were taken, which is the order the trip happened in. A picture
// whose camera recorded no time follows those that did, in the order it was
// uploaded; the identifier breaks the last ties, so the order never shuffles.
//
// Arguments:
//   - a, b: the two pictures.
//
// Returns:
//   - true when a comes first.
func MediaBefore(a, b Media) bool {
	if (a.TakenAt == nil) != (b.TakenAt == nil) {
		return a.TakenAt != nil
	}
	if a.TakenAt != nil && !a.TakenAt.Equal(*b.TakenAt) {
		return a.TakenAt.Before(*b.TakenAt)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID.String() < b.ID.String()
}

// SortMedia - puts pictures in gallery order, as MediaBefore defines it.
//
// Arguments:
//   - items: the pictures, sorted in place.
func SortMedia(items []Media) {
	sort.SliceStable(items, func(a, b int) bool { return MediaBefore(items[a], items[b]) })
}
