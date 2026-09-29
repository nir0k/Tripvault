// Package track reads the line a day, a place or an activity was really
// travelled from the files a watch or a phone exports: GPX and KML. What comes
// out is what a report shows - how far it went, how much it climbed and
// descended, and a line a map can draw. The file itself is kept by the caller,
// so it can be downloaded again as it was recorded.
package track

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/routing"
)

// Errors a file can be rejected with.
var (
	// ErrUnsupported reports a file that is neither GPX nor KML.
	ErrUnsupported = errors.New("the file is neither a GPX nor a KML track")
	// ErrEmpty reports a track without usable points.
	ErrEmpty = errors.New("the track holds no points")
	// ErrTooManyPoints reports a file too large to read at all.
	ErrTooManyPoints = errors.New("the track holds more points than this service reads")
)

// MaxPoints bounds what is read from a file. A day of recording at one point a
// second is under a hundred thousand; beyond that the file is something else.
const MaxPoints = 500_000

// DrawnPoints is how many points the stored line keeps. A map draws a few
// thousand points without noticing and none of the rest.
const DrawnPoints = 2000

// How the climb of a line is measured. The heights of a recording are first
// smoothed with a moving average over ClimbSmoothing either side of each
// point, and a change then counts once it reaches ClimbThreshold from the last
// height counted.
//
// The smoothing is what keeps the noise of a GPS out: a height that wanders by
// a few metres from one second to the next averages out over ten seconds,
// while a real slope survives it. The small threshold after it keeps the short
// real climbs a barometric watch records in whole metres, which a coarse
// threshold alone threw away - and which a coarse threshold did not even keep
// the noise out of. A line whose points carry no time is a route drawn ahead,
// its heights read from a map of the terrain: it has no noise to smooth, and
// its points may lie far apart, so it is measured as it is.
const (
	ClimbThreshold = 1.0
	ClimbSmoothing = 5 * time.Second
)

// ClimbVersion names the way the climb and the slopes are measured. Raising it
// when the measurement changes makes the service measure every stored track
// again from its file when it starts.
const ClimbVersion = 3

// How the slopes of a line are measured, for the time it takes to walk. The
// line is cut into stretches of at least GradeStep metres, measured over the
// heights smoothed as for the climb, and each stretch is counted in the bucket
// of its slope in whole percent, from -MaxGrade to +MaxGrade; a steeper one
// counts in the last bucket. A stretch that long keeps the metre or two a
// height wanders by from passing for a slope of its own.
const (
	GradeStep = 20.0
	MaxGrade  = 50
)

// Formats a file can be read as.
const (
	FormatGPX = "gpx"
	FormatKML = "kml"
)

// Track is a recorded line, ready to be stored.
type Track struct {
	// Format is the kind of file it was read from: FormatGPX or FormatKML.
	Format string
	// Geometry is the thinned line in the encoded polyline format legs use.
	Geometry string
	// DistanceM is the length measured over every point of the file.
	DistanceM int
	// PointCount is how many points the file held.
	PointCount int
	// AscentM and DescentM are the height gained and lost, nil when the file
	// records no heights.
	AscentM  *int
	DescentM *int
	// Grades is how many metres of the line run at each slope, from -MaxGrade
	// to +MaxGrade percent; nil when the file records no heights.
	Grades []int
	// StartedAt and EndedAt are the earliest and latest moments the file
	// records, nil when its points carry no time.
	StartedAt *time.Time
	EndedAt   *time.Time
	// Start is the first point of the line, where the recording began.
	Start domain.Point
}

// recording is what readPoints takes out of a file.
type recording struct {
	points []domain.Point
	// heights holds the height of each point, NaN where the file gives none.
	heights []float64
	// moments holds the time of each point, zero where the file gives none.
	moments []time.Time
	format  string
	// first and last are the earliest and latest timestamps, zero without any.
	first, last time.Time
}

// see widens the recorded period to include one moment.
func (r *recording) see(moment time.Time) {
	if r.first.IsZero() || moment.Before(r.first) {
		r.first = moment
	}
	if r.last.IsZero() || moment.After(r.last) {
		r.last = moment
	}
}

// Parse - reads a GPX or KML file into a day's track.
//
// The format is decided by the content, not by the name: a file that claims one
// thing and holds another would otherwise be stored as a track with no line.
//
// Arguments:
//   - data: the file's bytes.
//
// Returns:
//   - the track, with its line thinned and its length and climb measured in full.
//   - ErrUnsupported, ErrEmpty or ErrTooManyPoints, or a parsing error.
func Parse(data []byte) (Track, error) {
	read, err := readPoints(data)
	if err != nil {
		return Track{}, err
	}
	points := read.points
	if len(points) < 2 {
		return Track{}, ErrEmpty
	}

	var distance float64
	for index := 1; index < len(points); index++ {
		distance += routing.Haversine(points[index-1], points[index])
	}
	parsed := Track{
		Format:     read.format,
		Geometry:   domain.EncodePolyline(thin(points, DrawnPoints)),
		DistanceM:  int(math.Round(distance)),
		PointCount: len(points),
		Start:      points[0],
	}
	if ascent, descent, ok := climb(read.heights, read.moments); ok {
		parsed.AscentM, parsed.DescentM = &ascent, &descent
		parsed.Grades = grades(points, read.heights, read.moments)
	}
	if !read.first.IsZero() {
		parsed.StartedAt, parsed.EndedAt = &read.first, &read.last
	}
	return parsed, nil
}

// climb sums the height gained and lost along a line: the heights smoothed
// over a few seconds either side of each point, then every change that reaches
// ClimbThreshold from the last height counted.
//
// Arguments:
//   - heights: the height of every point, NaN where a point has none.
//   - moments: the time of every point, zero where a point has none.
//
// Returns:
//   - the metres gained and lost, rounded.
//   - false when fewer than two points carry a height, or when every height is
//     zero: a KML line drawn on the ground writes 0 where it knows nothing.
func climb(heights []float64, moments []time.Time) (int, int, bool) {
	var known []float64
	var times []time.Time
	flat := true
	for index, height := range heights {
		if math.IsNaN(height) {
			continue
		}
		known = append(known, height)
		times = append(times, moments[index])
		flat = flat && height == 0
	}
	if len(known) < 2 || flat {
		return 0, 0, false
	}

	var ascent, descent float64
	smoothed := smooth(known, times)
	reference := smoothed[0]
	for _, height := range smoothed[1:] {
		switch change := height - reference; {
		case change >= ClimbThreshold:
			ascent += change
			reference = height
		case change <= -ClimbThreshold:
			descent -= change
			reference = height
		}
	}
	return int(math.Round(ascent)), int(math.Round(descent)), true
}

// grades measures how much of a line runs at each slope. Only the points with
// a height count, their heights smoothed as for the climb; the way between two
// of them is measured over every point in between.
//
// Arguments:
//   - points: every point of the line.
//   - heights: the height of each point, NaN where a point has none.
//   - moments: the time of each point, zero where a point has none.
//
// Returns:
//   - the metres run at each slope, index 0 being -MaxGrade percent; nil when
//     fewer than two points carry a height.
func grades(points []domain.Point, heights []float64, moments []time.Time) []int {
	var known []int
	var values []float64
	var times []time.Time
	for index, height := range heights {
		if !math.IsNaN(height) {
			known = append(known, index)
			values = append(values, height)
			times = append(times, moments[index])
		}
	}
	if len(known) < 2 {
		return nil
	}
	smoothed := smooth(values, times)

	buckets := make([]float64, 2*MaxGrade+1)
	anchor, run := 0, 0.0
	for k := 1; k < len(known); k++ {
		for index := known[k-1] + 1; index <= known[k]; index++ {
			run += routing.Haversine(points[index-1], points[index])
		}
		// The last stretch is counted however short it is, so the whole line is.
		if run < GradeStep && k < len(known)-1 {
			continue
		}
		if run > 0 {
			grade := int(math.Round((smoothed[k] - smoothed[anchor]) / run * 100))
			buckets[min(max(grade, -MaxGrade), MaxGrade)+MaxGrade] += run
		}
		anchor, run = k, 0
	}
	metres := make([]int, len(buckets))
	for index, value := range buckets {
		metres[index] = int(math.Round(value))
	}
	return metres
}

// smooth averages every height of a recording with the heights within
// ClimbSmoothing of it. A line without a time on every point, or whose times
// do not run forward, is returned as it is.
//
// Arguments:
//   - heights: the known heights in order.
//   - times: the time of each, zero where unknown.
//
// Returns:
//   - the smoothed heights, one per height.
func smooth(heights []float64, times []time.Time) []float64 {
	for index, moment := range times {
		if moment.IsZero() || (index > 0 && moment.Before(times[index-1])) {
			return heights
		}
	}
	// within reports whether the point at other belongs to the window of the
	// point at centre.
	within := func(centre, other int) bool {
		gap := times[other].Sub(times[centre])
		return gap >= -ClimbSmoothing && gap <= ClimbSmoothing
	}

	smoothed := make([]float64, len(heights))
	low, high, sum := 0, 0, 0.0
	for centre := range heights {
		// The window slides forward: add what came into it, drop what left it.
		for high < len(heights) && within(centre, high) {
			sum += heights[high]
			high++
		}
		for low < centre && !within(centre, low) {
			sum -= heights[low]
			low++
		}
		smoothed[centre] = sum / float64(high-low)
	}
	return smoothed
}

// readPoints pulls the coordinates, heights and timestamps out of whichever of
// the two formats the file turns out to be. A GPX point dates itself with a
// <time> child; the <time> of the file's metadata is when it was written, not
// when anybody walked, so only a point's own counts. A KML file dates its line
// with <when> or a <TimeSpan>'s <begin> and <end>.
//
// Returns:
//   - the points in file order, their heights, the format and the period.
//   - ErrUnsupported, ErrTooManyPoints or a parsing error.
func readPoints(data []byte) (recording, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	// A GPX or KML file is plain XML; anything with a document type or an
	// external entity in it is not one this service needs to read.
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	var read recording
	var inCoordinates, inHeight, inTime bool
	var coordinates, height, stamp strings.Builder
	// current is the GPX point being read, whose <ele> child gives its height.
	current := -1

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return recording{}, fmt.Errorf("read track: %w", err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(element.Name.Local)
			switch name {
			case "gpx":
				read.format = FormatGPX
			case "kml":
				read.format = FormatKML
			// A route is a track for this purpose: both describe where the day went.
			case "trkpt", "rtept", "wpt":
				if name == "wpt" && read.format != "" {
					// Waypoints are marks beside a track, not the track itself;
					// they are read only when a file holds nothing else.
					continue
				}
				point, ok := pointFrom(element)
				if ok {
					read.points = append(read.points, point)
					read.heights = append(read.heights, math.NaN())
					read.moments = append(read.moments, time.Time{})
					current = len(read.points) - 1
				}
			case "ele":
				if current >= 0 {
					inHeight = true
					height.Reset()
				}
			case "time":
				if current >= 0 {
					inTime = true
					stamp.Reset()
				}
			case "when", "begin", "end":
				if read.format == FormatKML {
					inTime = true
					stamp.Reset()
				}
			case "coordinates":
				inCoordinates = true
				coordinates.Reset()
			}
		case xml.CharData:
			switch {
			case inCoordinates:
				coordinates.Write(element)
			case inHeight:
				height.Write(element)
			case inTime:
				stamp.Write(element)
			}
		case xml.EndElement:
			switch strings.ToLower(element.Name.Local) {
			case "coordinates":
				inCoordinates = false
				points, heights := parseCoordinates(coordinates.String())
				read.points = append(read.points, points...)
				read.heights = append(read.heights, heights...)
				read.moments = append(read.moments, make([]time.Time, len(points))...)
			case "ele":
				if inHeight {
					inHeight = false
					if value, err := strconv.ParseFloat(strings.TrimSpace(height.String()), 64); err == nil {
						read.heights[current] = value
					}
				}
			case "time", "when", "begin", "end":
				if inTime {
					inTime = false
					if moment, err := time.Parse(time.RFC3339, strings.TrimSpace(stamp.String())); err == nil {
						read.see(moment.UTC())
						if current >= 0 {
							read.moments[current] = moment.UTC()
						}
					}
				}
			case "trkpt", "rtept", "wpt":
				current = -1
			}
		}
		if len(read.points) > MaxPoints {
			return recording{}, ErrTooManyPoints
		}
	}

	if read.format == "" {
		return recording{}, ErrUnsupported
	}
	return read, nil
}

// pointFrom reads the lat and lon attributes of a GPX point.
func pointFrom(element xml.StartElement) (domain.Point, bool) {
	var point domain.Point
	var haveLat, haveLng bool
	for _, attribute := range element.Attr {
		value, err := strconv.ParseFloat(strings.TrimSpace(attribute.Value), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(attribute.Name.Local) {
		case "lat":
			point.Lat, haveLat = value, true
		case "lon", "lng":
			point.Lng, haveLng = value, true
		}
	}
	return point, haveLat && haveLng && valid(point)
}

// parseCoordinates reads a KML coordinate list: "lng,lat[,height]" triples
// separated by whitespace. The order is the opposite of GPX's, which is the one
// mistake this format invites. It returns the points and, beside each, its
// height or NaN.
func parseCoordinates(raw string) ([]domain.Point, []float64) {
	var points []domain.Point
	var heights []float64
	for _, triple := range strings.Fields(raw) {
		parts := strings.Split(triple, ",")
		if len(parts) < 2 {
			continue
		}
		lng, errLng := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if errLng != nil || errLat != nil {
			continue
		}
		point := domain.Point{Lat: lat, Lng: lng}
		if !valid(point) {
			continue
		}
		height := math.NaN()
		if len(parts) > 2 {
			if value, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64); err == nil {
				height = value
			}
		}
		points = append(points, point)
		heights = append(heights, height)
	}
	return points, heights
}

// valid rejects coordinates outside the world.
func valid(point domain.Point) bool {
	return point.Lat >= -90 && point.Lat <= 90 && point.Lng >= -180 && point.Lng <= 180
}

// thin reduces a line to at most limit points, keeping its shape: the points
// that carry the shape are the ones furthest from the line between their
// neighbours, which is what Ramer-Douglas-Peucker keeps. The tolerance is
// raised until the line is short enough, so a track of any length comes out at
// a size a map can draw.
func thin(points []domain.Point, limit int) []domain.Point {
	if len(points) <= limit {
		return points
	}
	// Degrees rather than metres: the comparison is between distances on the
	// same line, so the unit cancels out, and this avoids a projection.
	tolerance := 0.00001
	for range 32 {
		simplified := simplify(points, tolerance)
		if len(simplified) <= limit {
			return simplified
		}
		tolerance *= 2
	}
	// A line that resists thinning is cut instead, keeping every nth point.
	step := (len(points) + limit - 1) / limit
	kept := make([]domain.Point, 0, limit+1)
	for index := 0; index < len(points); index += step {
		kept = append(kept, points[index])
	}
	return append(kept, points[len(points)-1])
}

// simplify runs Ramer-Douglas-Peucker with the given tolerance.
func simplify(points []domain.Point, tolerance float64) []domain.Point {
	if len(points) < 3 {
		return points
	}
	first, last := points[0], points[len(points)-1]
	furthest, distance := 0, 0.0
	for index := 1; index < len(points)-1; index++ {
		if away := perpendicular(points[index], first, last); away > distance {
			furthest, distance = index, away
		}
	}
	if distance <= tolerance {
		return []domain.Point{first, last}
	}
	left := simplify(points[:furthest+1], tolerance)
	right := simplify(points[furthest:], tolerance)
	return append(left[:len(left)-1], right...)
}

// perpendicular measures how far a point lies from the line between two others,
// in degrees, with longitudes narrowed by the latitude so the shape is not
// distorted towards the poles.
func perpendicular(point, from, to domain.Point) float64 {
	scale := math.Cos(point.Lat * math.Pi / 180)
	px, py := (point.Lng-from.Lng)*scale, point.Lat-from.Lat
	lx, ly := (to.Lng-from.Lng)*scale, to.Lat-from.Lat
	length := lx*lx + ly*ly
	if length == 0 {
		return math.Hypot(px, py)
	}
	// The projection of the point onto the line, clamped to the segment.
	share := math.Max(0, math.Min(1, (px*lx+py*ly)/length))
	return math.Hypot(px-share*lx, py-share*ly)
}
