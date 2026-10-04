package track

import (
	"bytes"
	"encoding/xml"
	"math"
	"strconv"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Outside - writes a track's file again without the points a reader may not
// see, for a read-only link: the file as recorded would show where the
// recording began, which is usually somebody's door.
//
// The result is always GPX, whichever format the file was, with each point's
// height and time kept. Every run of hidden points ends a segment, so the line
// is broken there rather than joined straight across the hidden part.
//
// Arguments:
//   - data: the file as it was imported.
//   - hidden: whether a point must not be shown.
//
// Returns:
//   - the new file, and true, when some point was hidden; nil and false when
//     none was and the file can be sent as it is.
//   - an error when the file cannot be read.
func Outside(data []byte, hidden func(domain.Point) bool) ([]byte, bool, error) {
	read, err := readPoints(data)
	if err != nil {
		return nil, false, err
	}
	var segments [][]int
	var current []int
	removed := false
	for index, point := range read.points {
		if hidden(point) {
			removed = true
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, index)
	}
	if !removed {
		return nil, false, nil
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}

	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.WriteString(`<gpx version="1.1" creator="Tripvault" xmlns="http://www.topografix.com/GPX/1/1">` + "\n<trk>\n")
	for _, segment := range segments {
		out.WriteString("<trkseg>\n")
		for _, index := range segment {
			point := read.points[index]
			out.WriteString(`<trkpt lat="` + strconv.FormatFloat(point.Lat, 'f', -1, 64) +
				`" lon="` + strconv.FormatFloat(point.Lng, 'f', -1, 64) + `">`)
			if height := read.heights[index]; !math.IsNaN(height) {
				out.WriteString("<ele>" + strconv.FormatFloat(height, 'f', -1, 64) + "</ele>")
			}
			if moment := read.moments[index]; !moment.IsZero() {
				out.WriteString("<time>" + moment.Format(time.RFC3339) + "</time>")
			}
			out.WriteString("</trkpt>\n")
		}
		out.WriteString("</trkseg>\n")
	}
	out.WriteString("</trk>\n</gpx>\n")
	return out.Bytes(), true, nil
}
