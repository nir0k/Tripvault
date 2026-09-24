package pdf

import (
	"strconv"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/routing"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
)

// The maps of the document: one of the whole trip after the summary and one at
// the head of every day, drawn the way the interface draws its map. Each day
// keeps its colour, a route that followed the roads is a solid line and one
// that was only estimated is dashed, and the places of a day carry the numbers
// their headings carry below.
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
}

// mapMarker is one point of a map; a label turns the dot into a numbered pin.
type mapMarker struct {
	point domain.Point
	color [3]int
	label string
}

// mapLayer is everything drawn over a map's background.
type mapLayer struct {
	lines   []mapLine
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
		if points := legPoints(*leg, everything, stays); len(points) >= 2 {
			layer.lines = append(layer.lines, mapLine{points: points, color: color, dashed: leg.Source != domain.LegProvider})
		}
	}

	number := 0
	for _, item := range items {
		if track := domain.TrackOfItem(content.Tracks, item.ID); track != nil {
			if points := routing.DecodePolyline(track.Geometry, 5); len(points) >= 2 {
				layer.lines = append(layer.lines, mapLine{points: points, color: color})
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
// straight line between its ends.
func legPoints(leg domain.Leg, items map[uuid.UUID]domain.Item, stays map[uuid.UUID]domain.Stay) []domain.Point {
	if leg.Geometry != "" {
		return routing.DecodePolyline(leg.Geometry, 5)
	}
	from, fromKnown := items[leg.FromItemID]
	to, toKnown := items[leg.ToItemID]
	if !fromKnown || !toKnown {
		return nil
	}
	start, end := domain.ItemPoint(from, stays), domain.ItemPoint(to, stays)
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
	height := mapHeight(m)
	scale := contentWidth / float64(m.Frame.Width)
	d.keepTogether(height + lineHeight)
	left, top := marginLeft, d.pdf.GetY()

	if len(m.Background) > 0 {
		name, _, err := d.register(m.Background)
		if err != nil {
			return err
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
		// A pale casing under each line keeps it readable over a busy map.
		for _, pass := range []struct {
			width float64
			color [3]int
		}{{1.3, [3]int{255, 255, 255}}, {0.7, line.color}} {
			d.pdf.SetLineWidth(pass.width)
			d.pdf.SetDrawColor(pass.color[0], pass.color[1], pass.color[2])
			if line.dashed && pass.color == line.color {
				d.pdf.SetDashPattern([]float64{1.8, 1.4}, 0)
			} else {
				d.pdf.SetDashPattern([]float64{}, 0)
			}
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

	d.pdf.SetLineWidth(0.2)
	d.pdf.SetDrawColor(grey[0], grey[1], grey[2])
	d.pdf.Rect(left, top, contentWidth, height, "D")
	d.pdf.SetTextColor(grey[0], grey[1], grey[2])
	d.pdf.SetFont(fontFamily, "", sizeCaption)
	d.pdf.SetXY(left, top+height+0.5)
	if len(m.Background) > 0 && attribution != "" {
		d.pdf.CellFormat(contentWidth, sizeCaption*0.5, attribution, "", 0, "R", false, 0, "")
	}
	d.pdf.SetTextColor(0, 0, 0)
	d.pdf.SetXY(left, top+height+sizeCaption*0.5+2)
	return nil
}
