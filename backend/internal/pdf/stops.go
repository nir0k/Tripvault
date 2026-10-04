package pdf

import (
	"fmt"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// stopLine describes one stop along the line of an activity in a line of
// text: how far along the line it is, what it is, when it comes and, in a
// plan, what it costs. In a plan when it comes is how long the way to it takes from the
// start at the line's speed; in a report it is when it was reached.
//
// Arguments:
//   - text: the labels of the document's language.
//   - trip: the trip, for its speed, travellers and currency.
//   - activity: the activity the stop belongs to.
//   - line: the activity's line, for its own speed.
//   - stop: the stop.
//   - report: whether the document is a report.
//
// Returns:
//   - the line, without the stop's label.
func stopLine(text labels, trip domain.Trip, activity domain.Item, line domain.Track, stop domain.Stop,
	report bool) string {
	kind := text.stopKinds[string(stop.Kind)]
	parts := []string{text.trackDistanceOf(stop.DistanceM)}
	if stop.Name != "" {
		parts = append(parts, stop.Name+" ("+strings.ToLower(kind)+")")
	} else {
		parts = append(parts, kind)
	}
	switch {
	case report && stop.ActualTime != nil:
		parts = append(parts, text.clock(int(*stop.ActualTime)))
	case !report:
		speed := trip.TrackSpeedKmh
		if line.SpeedKmh != nil {
			speed = *line.SpeedKmh
		}
		// A stop at the start is reached at once, which needs no saying.
		way := domain.Track{DistanceM: stop.DistanceM, Grades: stop.GradesTo}.WalkingTime(speed)
		if way >= 30*time.Second {
			parts = append(parts, fmt.Sprintf(text.stopFromStart, text.duration(int(way.Seconds()))))
		}
	}
	// A report's journal leaves the money to the budget.
	if cost := placeCost(stop.CostItem(activity), trip); cost != "" && !report {
		parts = append(parts, cost)
	}
	return strings.Join(parts, separator)
}

// writeStops writes the stops along the line of an activity, each under the
// label it carries on the day's map and with its note below it.
//
// Arguments:
//   - doc: the document being written.
//   - text: the labels of its language.
//   - trip: the trip.
//   - activity: the activity.
//   - tracks: the document's tracks, which carry the stops.
//   - number: the activity's number on the day's map.
//   - report: whether the document is a report.
//   - x, width: the column to write in.
func writeStops(doc *document, text labels, trip domain.Trip, activity domain.Item, tracks []domain.Track,
	number int, report bool, x, width float64) {
	line := domain.TrackOfItem(tracks, activity.ID)
	if line == nil || len(line.Stops) == 0 {
		return
	}
	pdf := doc.pdf
	labelWidth := 7.0
	pdf.Ln(1)
	for index, stop := range line.Stops {
		pdf.SetFont(fontFamily, "B", sizeSmall)
		doc.ink(accentColor)
		pdf.SetX(x)
		pdf.CellFormat(labelWidth, lineHeight*0.85, stopLabel(number, index), "", 0, "L", false, 0, "")
		pdf.SetFont(fontFamily, "", sizeSmall)
		doc.ink(inkColor)
		pdf.MultiCell(width-labelWidth, lineHeight*0.85, stopLine(text, trip, activity, *line, stop, report),
			"", "L", false)
		if stop.NoteMD != "" {
			doc.inColumn(x+labelWidth, width-labelWidth, func() { writeMarkdown(doc, stop.NoteMD) })
		}
	}
	pdf.Ln(1)
}
