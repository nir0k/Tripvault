package pdf

import (
	"math"
	"strconv"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
)

// The maps of the document: one of the whole trip after the summary and one at
// the head of every day, drawn the way the interface draws its map. Each day
// keeps its colour, a route that followed the roads is a solid line and one
// that was only estimated is dashed, a recorded track is a trail - a dark
// casing, the day's colour and a dotted white core - with its start and finish
// marked, and the places of a day carry the numbers their headings carry below.
//
// Only the background is a picture - tiles glued together by the caller, who
// may reach the tile server - and everything drawn over it is drawn here, as
// lines and text of the document, so it prints sharp at any size.

// The size of a map's background, in pixels. Its width spans the text column,
// which puts a tile's lettering at about the size of the document's own notes.
const (
	mapWidthPx       = 800
	overviewHeightPx = 500
	dayMapHeightPx   = 320
)

// dayColors tell the days apart, in the order the interface's map uses them
// (frontend/src/utils/plan.ts), so the paper and the screen agree.
var dayColors = [][3]int{
	{0xc2, 0x41, 0x0c}, {0x03, 0x69, 0xa1}, {0x15, 0x80, 0x3d}, {0x9f, 0x12, 0x39},
	{0x7c, 0x3a, 0xed}, {0xb4, 0x53, 0x09}, {0x0f, 0x76, 0x6e}, {0xbe, 0x18, 0x5d},
}

// stayColor marks where a night was spent, apart from every day's colour.
var stayColor = [3]int{0x33, 0x41, 0x55}

// mapPaper fills a map whose background could not be had.
var mapPaper = [3]int{244, 242, 236}

// MapRequest is one map of a report and the size of its background, for the
// caller to fetch the tiles of.
type MapRequest struct {
	// Key is the day the map heads, or uuid.Nil for the map of the whole trip.
	Key    uuid.UUID
	Points []domain.Point
	Width  int
	Height int
}

// Map is one map ready to draw: the part of the world it shows and the
// background under it, empty when the tiles could not be had.
type Map struct {
	Frame      staticmap.Frame
	Background []byte
}

// mapLine is one line of a map.
type mapLine struct {
	points []domain.Point
	color  [3]int
	dashed bool
	// track marks a recording imported from a file rather than a journey.
	track bool
}

// trackEnd is where a recorded track starts or, with finish set, ends.
type trackEnd struct {
	point  domain.Point
	color  [3]int
	finish bool
}

// mapMarker is one point of a map; a label turns the dot into a numbered pin.
type mapMarker struct {
	point domain.Point
	color [3]int
	label string
}

// mapLayer is everything drawn over a map's background.
type mapLayer struct {
	lines []mapLine
	// ends are drawn under the markers, so a place at the start of its own
	// track keeps its pin on top. They lie on their tracks' lines, so the
	// frame needs nothing from them.
	ends    []trackEnd
	markers []mapMarker
}

// points lists every position of the layer, which is what a frame is fitted to.
func (l mapLayer) points() []domain.Point {
	var points []domain.Point
	for _, line := range l.lines {
		points = append(points, line.points...)
	}
	for _, marker := range l.markers {
		points = append(points, marker.point)
	}
	return points
}

// The size of the background of a day's map in the report's journal, which
// sits in half the page beside the day's story.
const (
	journalDayWidthPx  = 480
	journalDayHeightPx = 400
)

// journalMapInner is how wide a day's map is drawn inside its card: half the
// page beside the story, or the whole of it when nothing sits beside it.
func journalMapInner(wide bool) float64 {
	if wide {
		return contentWidth - 2*journalPadding
	}
	return (contentWidth-journalGap)/2 - 2*journalPadding
}

// journalWideDayWidthPx is the width of the background of a day's map drawn
// across the page. Its height is the half-page map's, so the wide map stands
// as tall on the page and shows more of the land to either side, at the same
// scale, rather than growing twice as tall.
func journalWideDayWidthPx() int {
	return int(math.Round(journalDayWidthPx * journalMapInner(true) / journalMapInner(false)))
}

// storyBeside reports whether a day's story is short enough to sit beside its
// half-page map; a longer one is written under it across the page.
func storyBeside(doc *document, story string) bool {
	inner := journalMapInner(false)
	return story != "" && markdownHeight(doc, story, inner) < journalDayHeightPx*inner/journalDayWidthPx+40
}

// wideDayMap reports whether a day's map takes the whole width of the page:
// it does when nothing would sit beside it - no story short enough to, and no
// moment of the day, which otherwise fills the place a long story left.
func wideDayMap(doc *document, day domain.Day) bool {
	return day.Highlight == "" && !storyBeside(doc, day.NotesMD)
}

// ReportMapRequests - lists the maps a report's journal is drawn with: the
// whole trip across the page, and each day in half of it, beside its story,
// or across the page when nothing sits beside it.
//
// The words are measured here, before the tiles are fetched, so content must
// already be in the language the document is written in.
//
// Arguments:
//   - content: the report, translated into the language it is read in.
//
// Returns:
//   - the maps, keyed as MapRequests keys them.
func ReportMapRequests(content domain.DocumentContent) []MapRequest {
	requests := MapRequests(content)
	days := make(map[uuid.UUID]domain.Day, len(content.Days))
	for _, day := range content.Days {
		days[day.ID] = day
	}
	var measure *document
	for index := range requests {
		key := requests[index].Key
		if key == uuid.Nil {
			continue
		}
		requests[index].Width, requests[index].Height = journalDayWidthPx, journalDayHeightPx
		if measure == nil {
			measure = newDocument("", "")
		}
		if wideDayMap(measure, days[key]) {
			requests[index].Width = journalWideDayWidthPx()
		}
	}
	return requests
}

// MapRequests - lists the maps a report is drawn with.
//
// Arguments:
//   - content: the report.
//
// Returns:
//   - the map of the whole trip, keyed uuid.Nil, and one for every day, keyed
//     by the day; a map with nothing on it is not asked for.
func MapRequests(content domain.DocumentContent) []MapRequest {
	var requests []MapRequest
	if points := tripLayer(content).points(); len(points) > 0 {
		requests = append(requests, MapRequest{Key: uuid.Nil, Points: points, Width: mapWidthPx, Height: overviewHeightPx})
	}
	for index, day := range content.Days {
		if points := dayLayer(content, day, index, true).points(); len(points) > 0 {
			requests = append(requests, MapRequest{Key: day.ID, Points: points, Width: mapWidthPx, Height: dayMapHeightPx})
		}
	}
	return requests
}

// tripLayer draws every day of the trip in its colour, with a dot for each
// place: the numbers belong to the maps of the days, where they are read.
func tripLayer(content domain.DocumentContent) mapLayer {
	var layer mapLayer
	for index, day := range content.Days {
		each := dayLayer(content, day, index, false)
		layer.lines = append(layer.lines, each.lines...)
		layer.ends = append(layer.ends, each.ends...)
		layer.markers = append(layer.markers, each.markers...)
	}
	return layer
}

// dayLayer draws one day: its journeys, the recordings of its places, and the
// places themselves, numbered in their order in the day when numbered is set.
// The journey in from the day before is part of it, as it is on the page.
func dayLayer(content domain.DocumentContent, day domain.Day, index int, numbered bool) mapLayer {
	color := dayColors[index%len(dayColors)]
	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	everything := make(map[uuid.UUID]domain.Item, len(content.Items))
	for _, item := range content.Items {
		everything[item.ID] = item
	}

	var layer mapLayer
	items := domain.DayItems(content.Items, &day.ID)
	legs := domain.DayLegs(content.Legs, items)
	if opening := domain.OpeningLeg(content.Legs, items); opening != nil {
		legs = append([]*domain.Leg{opening}, legs...)
	}
	for _, leg := range legs {
		// Two neighbours without a journey between them yet have a gap here.
		if leg == nil {
			continue
		}
		// A journey with changes is drawn part by part, each dashed unless it
		// follows roads.
		if leg.Composite() {
			for _, segment := range leg.Segments {
				if points := domain.DecodePolyline(segment.Geometry, 5); len(points) >= 2 {
					layer.lines = append(layer.lines, mapLine{points: points, color: color,
						dashed: segment.Source != domain.LegProvider})
				}
			}
			continue
		}
		if points := legPoints(*leg, everything, stays, content.Tracks); len(points) >= 2 {
			layer.lines = append(layer.lines, mapLine{points: points, color: color, dashed: leg.Source != domain.LegProvider})
		}
	}

	number := 0
	for _, item := range items {
		if track := domain.TrackOfItem(content.Tracks, item.ID); track != nil {
			if points := domain.DecodePolyline(track.Geometry, 5); len(points) >= 2 {
				layer.lines = append(layer.lines, mapLine{points: points, color: color, track: true})
				layer.ends = append(layer.ends, trackEnd{point: points[0], color: color},
					trackEnd{point: points[len(points)-1], color: color, finish: true})
			}
		}
		if item.Kind.IsVisit() {
			number++
		}
		point := domain.ItemPoint(item, stays)
		if point == nil {
			continue
		}
		marker := mapMarker{point: *point, color: color}
		if item.Kind == domain.ItemStayAnchor {
			marker.color = stayColor
		} else if numbered {
			marker.label = strconv.Itoa(number)
		}
		layer.markers = append(layer.markers, marker)
	}
	return layer
}

// legPoints finds a journey's line: the route it was calculated along, or the
// straight line between its ends, which are where a recording ended or began
// rather than a pin when there is one.
func legPoints(leg domain.Leg, items map[uuid.UUID]domain.Item, stays map[uuid.UUID]domain.Stay,
	tracks []domain.Track) []domain.Point {
	if leg.Geometry != "" {
		return domain.DecodePolyline(leg.Geometry, 5)
	}
	from, fromKnown := items[leg.FromItemID]
	to, toKnown := items[leg.ToItemID]
	if !fromKnown || !toKnown {
		return nil
	}
	start, end := domain.LegEnds(from, to, stays, tracks)
	if start == nil || end == nil {
		return nil
	}
	return []domain.Point{*start, *end}
}

// mapHeight is how tall a map is drawn, in millimetres.
func mapHeight(m Map) float64 {
	return float64(m.Frame.Height) * contentWidth / float64(m.Frame.Width)
}

// drawMap draws one map across the text column: its background, or plain
// paper when there is none, the layer over it, and the credit the tile
// provider asks for under it.
//
// Arguments:
//   - layer: what is drawn over the background.
//   - m: the frame and the background.
//   - attribution: the credit line; left out when the map has no background,
//     since then nothing of the provider's is shown.
//
// Returns:
//   - an error when the background cannot be read.
func (d *document) drawMap(layer mapLayer, m Map, attribution string) error {
	d.keepTogether(mapHeight(m) + lineHeight)
	bottom, err := d.drawMapIn(layer, m, attribution, marginLeft, d.pdf.GetY(), contentWidth, 0)
	d.pdf.SetXY(marginLeft, bottom)
	return err
}

// drawMapIn draws one map into a box of the page: across width from left and
// down from top, as tall as its frame makes it at that width, with its corners
// rounded by radius when it is not zero. It leaves the cursor alone.
//
// Arguments:
//   - layer: what is drawn over the background.
//   - m: the frame and the background.
//   - attribution: the credit line, written under the map.
//   - left, top, width: the box, in millimetres.
//   - radius: the rounding of its corners; zero draws the square frame
//     with a hairline border, as the plan's document does.
//
// Returns:
//   - where the map and its credit end on the page.
//   - an error when the background cannot be read.
func (d *document) drawMapIn(layer mapLayer, m Map, attribution string, left, top, width, radius float64) (float64, error) {
	height := float64(m.Frame.Height) * width / float64(m.Frame.Width)
	scale := width / float64(m.Frame.Width)
	contentWidth := width

	if radius > 0 {
		d.pdf.ClipRoundedRect(left, top, contentWidth, height, radius, false)
	}
	if len(m.Background) > 0 {
		name, _, err := d.register(m.Background)
		if err != nil {
			if radius > 0 {
				d.pdf.ClipEnd()
			}
			return top, err
		}
		d.pdf.ImageOptions(name, left, top, contentWidth, height, false, fpdf.ImageOptions{ImageType: "JPG"}, 0, "")
	} else {
		d.pdf.SetFillColor(mapPaper[0], mapPaper[1], mapPaper[2])
		d.pdf.Rect(left, top, contentWidth, height, "F")
	}

	place := func(point domain.Point) (float64, float64) {
		x, y := m.Frame.Project(point)
		return left + x*scale, top + y*scale
	}

	d.pdf.ClipRect(left, top, contentWidth, height, false)
	d.pdf.SetLineCapStyle("round")
	d.pdf.SetLineJoinStyle("round")
	for _, line := range layer.lines {
		for _, pass := range linePasses(line) {
			d.pdf.SetLineWidth(pass.width)
			d.pdf.SetDrawColor(pass.color[0], pass.color[1], pass.color[2])
			d.pdf.SetDashPattern(pass.dash, 0)
			for index, point := range line.points {
				x, y := place(point)
				if index == 0 {
					d.pdf.MoveTo(x, y)
				} else {
					d.pdf.LineTo(x, y)
				}
			}
			d.pdf.DrawPath("D")
		}
	}
	d.pdf.SetDashPattern([]float64{}, 0)

	// Arrows along each track show the way it was travelled: a white chevron
	// every so often, its point towards the finish.
	d.pdf.SetDrawColor(255, 255, 255)
	d.pdf.SetLineWidth(0.4)
	for _, line := range layer.lines {
		if !line.track {
			continue
		}
		path := make([]paperPoint, len(line.points))
		for index, point := range line.points {
			path[index].x, path[index].y = place(point)
		}
		for _, arrow := range arrowsAlong(path, arrowStepMM) {
			sin, cos := math.Sincos(arrow.angle)
			for _, side := range []float64{-1, 1} {
				// Each arm runs back from the tip and out to one side.
				backX, backY := -arrowArmMM*cos, -arrowArmMM*sin
				outX, outY := -side*arrowArmMM*sin, side*arrowArmMM*cos
				d.pdf.Line(arrow.x, arrow.y, arrow.x+backX+outX*0.8, arrow.y+backY+outY*0.8)
			}
		}
	}

	// A track starts at a white dot ringed in its colour and finishes at a dark
	// one: the flag the interface draws is too small to read in print.
	d.pdf.SetLineWidth(0.5)
	for _, end := range layer.ends {
		x, y := place(end.point)
		d.pdf.SetDrawColor(end.color[0], end.color[1], end.color[2])
		if end.finish {
			d.pdf.SetFillColor(trackCasing[0], trackCasing[1], trackCasing[2])
		} else {
			d.pdf.SetFillColor(255, 255, 255)
		}
		d.pdf.Circle(x, y, 1.2, "FD")
	}

	d.pdf.SetDrawColor(255, 255, 255)
	d.pdf.SetTextColor(255, 255, 255)
	d.pdf.SetFont(fontFamily, "B", 6.5)
	for _, marker := range layer.markers {
		x, y := place(marker.point)
		radius := 1.1
		if marker.label != "" {
			radius = 2.3
		}
		d.pdf.SetLineWidth(0.35)
		d.pdf.SetFillColor(marker.color[0], marker.color[1], marker.color[2])
		d.pdf.Circle(x, y, radius, "FD")
		if marker.label != "" {
			d.pdf.SetXY(x-radius, y-radius)
			d.pdf.CellFormat(2*radius, 2*radius, marker.label, "", 0, "CM", false, 0, "")
		}
	}
	d.pdf.ClipEnd()
	if radius > 0 {
		d.pdf.ClipEnd()
	} else {
		d.pdf.SetLineWidth(0.2)
		d.pdf.SetDrawColor(grey[0], grey[1], grey[2])
		d.pdf.Rect(left, top, contentWidth, height, "D")
	}
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	d.pdf.SetFont(fontFamily, "", sizeCaption)
	d.pdf.SetXY(left, top+height+0.5)
	if len(m.Background) > 0 && attribution != "" {
		d.pdf.CellFormat(contentWidth, sizeCaption*0.5, attribution, "", 0, "R", false, 0, "")
	}
	d.pdf.SetTextColor(0, 0, 0)
	return top + height + sizeCaption*0.5 + 2, nil
}

// mapHeightAt is how tall a map is drawn across width, in millimetres.
func mapHeightAt(m Map, width float64) float64 {
	return float64(m.Frame.Height) * width / float64(m.Frame.Width)
}

// trackCasing is the dark edge a recorded track is drawn over, the colour the
// interface's map uses.
var trackCasing = [3]int{31, 41, 55}

// linePass is one stroke of a line; a line is drawn as several, widest first.
type linePass struct {
	width float64
	color [3]int
	dash  []float64
}

// linePasses lists the strokes a line is drawn with. A journey has a pale
// casing that keeps it readable over a busy map, then its colour, dashed while
// it was only estimated. A recorded track is a trail instead: a dark casing,
// its colour and a dotted white core, so it is never read as a road.
func linePasses(line mapLine) []linePass {
	if line.track {
		return []linePass{
			{width: 1.7, color: trackCasing, dash: []float64{}},
			{width: 1.0, color: line.color, dash: []float64{}},
			{width: 0.35, color: [3]int{255, 255, 255}, dash: []float64{0.01, 0.9}},
		}
	}
	stroke := linePass{width: 0.7, color: line.color, dash: []float64{}}
	if line.dashed {
		stroke.dash = []float64{1.8, 1.4}
	}
	return []linePass{{width: 1.3, color: [3]int{255, 255, 255}, dash: []float64{}}, stroke}
}

// The spacing of the arrows along a track and the length of an arrow's arm,
// in millimetres of the page.
const (
	arrowStepMM = 25.0
	arrowArmMM  = 0.7
)

// paperPoint is a position on the page, in millimetres.
type paperPoint struct {
	x, y float64
}

// trackArrow is one arrow along a track: where its tip is and the direction of
// travel there, in radians on the page (y grows downwards).
type trackArrow struct {
	x, y  float64
	angle float64
}

// arrowsAlong places an arrow every step millimetres along a path, the first
// half a step from its start so it keeps clear of the start's dot. A path
// shorter than half a step gets none.
//
// Arguments:
//   - path: the track on the page, from its start to its finish.
//   - step: the distance between two arrows.
//
// Returns:
//   - the arrows, in the order they are met along the path.
func arrowsAlong(path []paperPoint, step float64) []trackArrow {
	var arrows []trackArrow
	next, walked := step/2, 0.0
	for index := 1; index < len(path); index++ {
		from, to := path[index-1], path[index]
		length := math.Hypot(to.x-from.x, to.y-from.y)
		for length > 0 && walked+length >= next {
			along := (next - walked) / length
			arrows = append(arrows, trackArrow{
				x:     from.x + (to.x-from.x)*along,
				y:     from.y + (to.y-from.y)*along,
				angle: math.Atan2(to.y-from.y, to.x-from.x),
			})
			next += step
		}
		walked += length
	}
	return arrows
}
