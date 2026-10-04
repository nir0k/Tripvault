package domain

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// A track is the line of an activity - a hike, a walk, a descent of a canyon.
// In a report it is what was really travelled, imported from a watch or a
// phone; in a plan it is the route meant to be taken, drawn in an outdoor app,
// and a report copied from the plan starts with it until a recording replaces
// it. Each activity holds at most one, and a place none: a place is somewhere
// seen, and an activity turned into a place loses its line. A day has
// none of its own: the journeys between its places are its legs, which are
// drawn and counted whether or not a part of the day was recorded.
//
// The line is stored already thinned to what a map can draw, while the length
// and the climb are measured over every point the file held. The file itself
// is kept beside it, compressed, so it can be downloaded as it was recorded;
// it is read only by the download and never with the document.

// Track is the line of an activity.
type Track struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	// ItemID is the place or activity the line was recorded for.
	ItemID uuid.UUID
	// OriginalName is the name of the file it was imported from.
	OriginalName string
	// Format is the kind of file it was: "gpx" or "kml".
	Format string
	// FileSize is the uploaded source size charged to the instance allowance.
	FileSize int64
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
	// Grades is how many metres of the line run at each slope, in whole
	// percent from -50 to +50, the steeper ones counted at the ends; nil when
	// the file records no heights. The time a line takes to walk is worked out
	// from it by the reader, at the speed they choose.
	Grades []int
	// SpeedKmh is the speed on the flat the time of a plan's line is worked out
	// at, in km/h; nil to take the plan's own (Trip.TrackSpeedKmh).
	SpeedKmh *float64
	// StartedAt and EndedAt are the first and last moment the file records,
	// nil when its points carry no time.
	StartedAt *time.Time
	EndedAt   *time.Time
	// ClimbVersion is the way AscentM, DescentM and Grades were measured; a
	// track measured an older way is measured again from its file.
	ClimbVersion int
	CreatedAt    time.Time
	// Stops are the stops along the line, the nearest its start first.
	Stops []Stop
}

// The speeds a track's time may be worked out at, in km/h on the flat, from a
// stroll to a run. A plan starts at a steady walk.
const (
	MinTrackSpeed     = 1.0
	MaxTrackSpeed     = 12.0
	DefaultTrackSpeed = 4.7
)

// ValidateTrackSpeed - checks a speed a track's time may be worked out at.
//
// Arguments:
//   - field: the field the speed came in, named by the error.
//   - speed: the speed in km/h.
//
// Returns:
//   - a *ValidationError when it is outside MinTrackSpeed to MaxTrackSpeed.
func ValidateTrackSpeed(field string, speed float64) error {
	if math.IsNaN(speed) || speed < MinTrackSpeed || speed > MaxTrackSpeed {
		return NewValidationError(field, "out_of_range", "must be between 1 and 12 km/h")
	}
	return nil
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

// Elapsed - reports how long a recording took, from its first point to its
// last, pauses included, as the total time of a watch.
//
// Returns:
//   - the time, or nil when the file records no time.
func (t Track) Elapsed() *time.Duration {
	if t.StartedAt == nil || t.EndedAt == nil {
		return nil
	}
	elapsed := t.EndedAt.Sub(*t.StartedAt)
	return &elapsed
}

// gradeFlat is the index of the flat in a track's grades, from -50 % at index 0.
const gradeFlat = 50

// WalkingTime - works out how long a line takes to walk at a speed on the flat,
// as the browser does (utils/trackTime.ts): each stretch at Tobler's pace for
// its slope, scaled so the flat is walked at the speed given, and the metres
// the grades do not account for walked as flat.
//
// Arguments:
//   - speedKmh: the speed on the flat, in km/h.
//
// Returns:
//   - the time, or zero for a speed that is not positive.
func (t Track) WalkingTime(speedKmh float64) time.Duration {
	if speedKmh <= 0 {
		return 0
	}
	flat := speedKmh / 3.6
	seconds, counted := 0.0, 0
	for index, metres := range t.Grades {
		slope := float64(index-gradeFlat) / 100
		seconds += float64(metres) / (flat * math.Exp(-3.5*(math.Abs(slope+0.05)-0.05)))
		counted += metres
	}
	seconds += float64(max(t.DistanceM-counted, 0)) / flat
	return time.Duration(seconds * float64(time.Second))
}

// TrackFile is the file a track was imported from, as it will be downloaded.
type TrackFile struct {
	// Name is the name it was uploaded under, which the download offers again.
	Name   string
	Format string
	// Data is the file's bytes, uncompressed.
	Data []byte
}

// TrackOfItem - finds the track of an activity among a document's tracks.
//
// Arguments:
//   - tracks: the document's tracks.
//   - itemID: the activity in question.
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
