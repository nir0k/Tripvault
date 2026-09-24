package domain

import (
	"time"

	"github.com/google/uuid"
)

// A track is the line a place or an activity was really travelled - a hike, a
// walk around a lake - imported from a watch or a phone. It belongs to a
// report: a plan has the route it intends to take, and what a recording says is
// what actually happened. Each place or activity holds at most one. A day has
// none of its own: the journeys between its places are its legs, which are
// drawn and counted whether or not a part of the day was recorded.
//
// The line is stored already thinned to what a map can draw, while the length
// and the climb are measured over every point the file held. The file itself
// is kept beside it, compressed, so it can be downloaded as it was recorded;
// it is read only by the download and never with the document.

// Track is a recorded line of a place or an activity.
type Track struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	// ItemID is the place or activity the line was recorded for.
	ItemID uuid.UUID
	// OriginalName is the name of the file it was imported from.
	OriginalName string
	// Format is the kind of file it was: "gpx" or "kml".
	Format string
	// Geometry is the line in the encoded polyline format legs use.
	Geometry string
	// DistanceM is how far the day went, over every point of the file.
	DistanceM int
	// PointCount is how many points that file held.
	PointCount int
	// AscentM and DescentM are the height gained and lost, nil when the file
	// records no heights.
	AscentM  *int
	DescentM *int
	// StartedAt and EndedAt are the first and last moment the file records,
	// nil when its points carry no time.
	StartedAt *time.Time
	EndedAt   *time.Time
	CreatedAt time.Time
}

// ClockPeriod - reads when a recording started and ended as times of day in the
// trip's own time zone, which is how a report's places keep their times.
//
// Arguments:
//   - location: the trip's time zone.
//
// Returns:
//   - the start and the end, both nil when the file records no time.
func (t Track) ClockPeriod(location *time.Location) (*ClockTime, *ClockTime) {
	if t.StartedAt == nil || t.EndedAt == nil {
		return nil, nil
	}
	clock := func(moment time.Time) *ClockTime {
		local := moment.In(location)
		value := ClockTime(local.Hour()*60 + local.Minute())
		return &value
	}
	return clock(*t.StartedAt), clock(*t.EndedAt)
}

// TrackFile is the file a track was imported from, as it will be downloaded.
type TrackFile struct {
	// Name is the name it was uploaded under, which the download offers again.
	Name   string
	Format string
	// Data is the file's bytes, uncompressed.
	Data []byte
}

// TrackOfItem - finds the track of a place or an activity among a document's
// tracks.
//
// Arguments:
//   - tracks: the document's tracks.
//   - itemID: the place or activity in question.
//
// Returns:
//   - its track, or nil when it has none.
func TrackOfItem(tracks []Track, itemID uuid.UUID) *Track {
	for index := range tracks {
		if tracks[index].ItemID == itemID {
			return &tracks[index]
		}
	}
	return nil
}
