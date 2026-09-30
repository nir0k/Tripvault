package pdf

import (
	_ "embed"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The report is laid out as a travel journal: a cover with a large photograph,
// a page that sums the trip up, and a chapter a day - a heading, a photograph,
// the day's map beside its story, and each place in a card of its own with its
// photographs arranged around one large picture rather than in a grid.
//
// The page stays white, so the document prints on any printer without a border
// of unprinted paper around a tinted page; the warmth of the design is in the
// cards, the accents and the headings.

// The palette of the journal, as the colours of the design it follows.
type rgb [3]int

var (
	inkColor        = rgb{0x25, 0x27, 0x23}
	mutedColor      = rgb{0x77, 0x76, 0x6F}
	greenColor      = rgb{0x35, 0x52, 0x48}
	accentColor     = rgb{0xC8, 0x6E, 0x45}
	cardColor       = rgb{0xFB, 0xF8, 0xF1}
	borderColor     = rgb{0xDC, 0xCF, 0xBD}
	softColor       = rgb{0xEF, 0xE4, 0xD6}
	onGreenColor    = rgb{0xFF, 0xFD, 0xF8}
	onGreenSoft     = rgb{0xC9, 0xD3, 0xCC}
	emptyStarColor  = rgb{0xE3, 0xD8, 0xC8}
	skippedPillText = rgb{0x8A, 0x5A, 0x44}
)

// The geometry of the journal, in millimetres.
const (
	journalRadius      = 3.5
	journalPadding     = 5.0
	journalGap         = 5.0
	journalPhotoRadius = 3.0
	journalPillHeight  = 6.0
)

// The display face of the journal's headings: Manrope, cut to one static
// weight from the variable font the project publishes, under the same licence
// as the body face (fonts/LICENSE-Manrope).
//
//go:embed fonts/Manrope-ExtraBold.ttf
var fontDisplay []byte

// displayFamily is the name the display face is registered under.
const displayFamily = "Display"

// useDisplay registers the display face with the document; only the report
// uses it, so the plan's and the packing list's documents do not carry it.
func (d *document) useDisplay() {
	d.pdf.AddUTF8FontFromBytes(displayFamily, "", fontDisplay)
}

// fill, stroke and ink set the colours the next shapes and text are drawn in.
func (d *document) fill(c rgb)   { d.pdf.SetFillColor(c[0], c[1], c[2]) }
func (d *document) stroke(c rgb) { d.pdf.SetDrawColor(c[0], c[1], c[2]) }
func (d *document) ink(c rgb)    { d.pdf.SetTextColor(c[0], c[1], c[2]) }

// labelWidth measures a label as label draws it: upper case, at size, with its
// letters spaced.
func (d *document) labelWidth(text string, size float64) float64 {
	text = strings.ToUpper(text)
	d.pdf.SetFont(displayFamily, "", size)
	spacing := size * 0.08 / d.pdf.GetConversionRatio()
	return d.pdf.GetStringWidth(text) + spacing*float64(utf8.RuneCountInString(text))
}

// label writes a small heading in capitals with its letters spaced out, the way
// the journal names its sections, starting at x on the line whose top is y.
// fpdf has no letter spacing of its own, so the spacing is set in the page's
// text state around the one cell.
//
// Arguments:
//   - text: the words; they are set in capitals.
//   - x, y: where the label starts.
//   - size: the type size, in points.
//   - color: the ink.
//
// Returns:
//   - the label's width, in millimetres.
func (d *document) label(text string, x, y, size float64, color rgb) float64 {
	width := d.labelWidth(text, size)
	d.ink(color)
	d.pdf.SetXY(x, y)
	d.pdf.RawWriteStr(fmt.Sprintf("%.3f Tc\n", size*0.08))
	d.pdf.CellFormat(width+1, size*0.45, strings.ToUpper(text), "", 0, "L", false, 0, "")
	d.pdf.RawWriteStr("0 Tc\n")
	d.ink(inkColor)
	return width
}

// minLabelSize is the smallest a label is set to fit its room, in points.
const minLabelSize = 5.5

// fitLabel sets a label to fit width: at size when it fits, at a smaller size
// down to minLabelSize when that is enough, and otherwise at minLabelSize over
// two lines, broken between words. A single word too long even for that is
// left on its own line, the one case a label can overrun.
//
// Arguments:
//   - text: the label's words.
//   - width: the room it has, in millimetres.
//   - size: the size it is set at when it fits, in points.
//
// Returns:
//   - the size it is set at.
//   - its lines, one or two.
func (d *document) fitLabel(text string, width, size float64) (float64, []string) {
	for ; size > minLabelSize; size -= 0.5 {
		if d.labelWidth(text, size) <= width {
			return size, []string{text}
		}
	}
	size = minLabelSize
	if d.labelWidth(text, size) <= width {
		return size, []string{text}
	}
	words := strings.Fields(text)
	for split := len(words) - 1; split > 0; split-- {
		first := strings.Join(words[:split], " ")
		if d.labelWidth(first, size) <= width {
			return size, []string{first, strings.Join(words[split:], " ")}
		}
	}
	if len(words) > 1 {
		return size, []string{words[0], strings.Join(words[1:], " ")}
	}
	return size, []string{text}
}

// labelLineHeight is the room one line of a label of size takes, in
// millimetres.
func labelLineHeight(size float64) float64 {
	return size*0.45 + 0.8
}

// fittedLabel writes a label fitted to width by fitLabel, its lines one under
// the other from y, and returns how tall it came out.
func (d *document) fittedLabel(text string, x, y, width, size float64, color rgb) float64 {
	size, lines := d.fitLabel(text, width, size)
	for index, line := range lines {
		d.label(line, x, y+float64(index)*labelLineHeight(size), size, color)
	}
	return float64(len(lines)) * labelLineHeight(size)
}

// display writes a heading in the display face across width, wrapping as it
// must, and leaves the cursor under it.
func (d *document) display(text string, x, width, size float64, color rgb) {
	d.pdf.SetFont(displayFamily, "", size)
	d.ink(color)
	d.pdf.SetX(x)
	d.pdf.MultiCell(width, size*0.42, text, "", "L", false)
	d.ink(inkColor)
}

// pill is one small rounded tag: an optional icon and a few words.
type pill struct {
	text string
	icon string
	// muted draws the tag in the terracotta of an exception - a place skipped
	// or found on the way - rather than the plain tone of a fact.
	muted bool
}

// pillWidth measures one tag as pills draws it.
func (d *document) pillWidth(p pill) float64 {
	d.pdf.SetFont(fontFamily, "B", 7.5)
	width := d.pdf.GetStringWidth(p.text) + 6
	if p.icon != "" {
		width += 4.2
	}
	return width
}

// pillsHeight is how tall pills draws tags across width, the room under the
// last row included; zero for no tags.
func (d *document) pillsHeight(tags []pill, width float64) float64 {
	if len(tags) == 0 {
		return 0
	}
	rows, left := 1, 0.0
	for _, tag := range tags {
		w := d.pillWidth(tag)
		if left > 0 && left+w > width {
			rows++
			left = 0
		}
		left += w + 1.8
	}
	return float64(rows)*(journalPillHeight+1.8) + 0.7
}

// pills lays tags out in rows across width, starting at x on the current line,
// and leaves the cursor under the last row.
func (d *document) pills(tags []pill, x, width float64) {
	if len(tags) == 0 {
		return
	}
	top := d.pdf.GetY()
	left := x
	for _, tag := range tags {
		w := d.pillWidth(tag)
		if left > x && left+w > x+width {
			left = x
			top += journalPillHeight + 1.8
		}
		if tag.muted {
			d.fill(rgb{0xF6, 0xE3, 0xD9})
		} else {
			d.fill(softColor)
		}
		d.pdf.RoundedRect(left, top, w, journalPillHeight, journalPillHeight/2, "1234", "F")
		textLeft := left + 3
		if tag.icon != "" {
			d.fill(accentColor)
			drawPackingIcon(d.pdf, domain.PackingIcon(tag.icon), left+2.6, top+1.4, 3.2)
			textLeft += 4.2
		}
		d.pdf.SetFont(fontFamily, "B", 7.5)
		if tag.muted {
			d.ink(skippedPillText)
		} else {
			d.ink(inkColor)
		}
		d.pdf.SetXY(textLeft, top)
		d.pdf.CellFormat(w, journalPillHeight, tag.text, "", 0, "L", false, 0, "")
		left += w + 1.8
	}
	d.ink(inkColor)
	d.pdf.SetXY(x, top+journalPillHeight+2.5)
}

// stars draws a rating as five stars, as many filled as the rating, the first
// with its left edge at x on the line whose top is y. The stars are drawn as
// shapes: the body face has no star, and one it does not have draws as a box.
func (d *document) stars(rating int, x, y, size float64) {
	for index := range maxStars {
		if index < rating {
			d.fill(accentColor)
		} else {
			d.fill(emptyStarColor)
		}
		cx, cy := x+size/2+float64(index)*(size+0.8), y+size/2
		points := make([]fpdf.PointType, 0, 10)
		for corner := range 10 {
			radius := size / 2
			if corner%2 == 1 {
				radius *= 0.45
			}
			angle := -math.Pi/2 + float64(corner)*math.Pi/5
			points = append(points, fpdf.PointType{X: cx + radius*math.Cos(angle), Y: cy + radius*math.Sin(angle)})
		}
		d.pdf.Polygon(points, "F")
	}
}

// starsWidth is how wide stars draws five stars of size.
func starsWidth(size float64) float64 {
	return float64(maxStars)*(size+0.8) - 0.8
}

// maxStars is the rating a place can be given at most.
const maxStars = 5

// modeIcon is the icon a way of travelling is shown with, out of the packing
// list's set; a way with no icon of its own is shown by its words alone.
func modeIcon(mode domain.TravelMode) string {
	switch mode {
	case domain.ModeWalk:
		return "hiking"
	case domain.ModeCar:
		return "car"
	case domain.ModeBus, domain.ModeTransit:
		return "bus"
	case domain.ModeTrain, domain.ModeTram:
		return "train"
	case domain.ModeFlight:
		return "flight"
	}
	return ""
}

// transferIcon is the icon a booked journey is shown with.
func transferIcon(kind domain.TransferKind) string {
	switch kind {
	case domain.TransferFlight:
		return "flight"
	case domain.TransferTrain:
		return "train"
	case domain.TransferBus:
		return "bus"
	}
	return ""
}

// framedPicture draws a registered picture filling a box, cut to the box the
// way a photograph is cut to its frame: scaled until it covers the box, then
// placed so that the point at focus - fractions of the picture across and down
// - sits as near the middle as the edges allow. Nothing is stretched.
func (d *document) framedPicture(name string, info *fpdf.ImageInfoType, x, y, w, h, focusX, focusY, radius float64) {
	scale := math.Max(w/info.Width(), h/info.Height())
	dw, dh := info.Width()*scale, info.Height()*scale
	left := math.Min(x, math.Max(x+w-dw, x+w/2-focusX*dw))
	top := math.Min(y, math.Max(y+h-dh, y+h/2-focusY*dh))
	if radius > 0 {
		d.pdf.ClipRoundedRect(x, y, w, h, radius, false)
	} else {
		d.pdf.ClipRect(x, y, w, h, false)
	}
	d.pdf.ImageOptions(name, left, top, dw, dh, false, fpdf.ImageOptions{ImageType: "JPG"}, 0, "")
	d.pdf.ClipEnd()
}

// deferredPicture is a picture of a card, drawn once the card's paper is: the
// paper is laid under the text after the text is written, and a photograph laid
// before it would be tinted by it.
type deferredPicture struct {
	page                  int
	name                  string
	info                  *fpdf.ImageInfoType
	x, y, w, h            float64
	focusX, focusY, round float64
}

// openCard is a card being written: where it started, and the pictures that
// wait for its paper.
type openCard struct {
	page     int
	top      float64
	x, width float64
	pictures []deferredPicture
}

// beginCard starts a card across width at x, on the current line, and moves
// the cursor inside its padding. The card's paper and border are drawn by
// endCard, when its height is known.
func (d *document) beginCard(x, width float64) {
	d.card = &openCard{page: d.pdf.PageNo(), top: d.pdf.GetY(), x: x, width: width}
	d.pdf.SetY(d.pdf.GetY() + journalPadding)
}

// picture places a registered picture framed in a box, deferred to the card's
// paper when a card is being written.
func (d *document) framed(name string, info *fpdf.ImageInfoType, x, y, w, h, focusX, focusY, radius float64) {
	if d.card != nil {
		d.card.pictures = append(d.card.pictures, deferredPicture{
			page: d.pdf.PageNo(), name: name, info: info, x: x, y: y, w: w, h: h,
			focusX: focusX, focusY: focusY, round: radius,
		})
		return
	}
	d.framedPicture(name, info, x, y, w, h, focusX, focusY, radius)
}

// endCard closes the card being written: its paper goes under everything
// written on it, page by page when it ran over a page break, then its pictures
// and its border. The paper is laid in the multiply blend, which leaves the
// ink written on it as it was and turns only the white of the page to the
// card's colour.
func (d *document) endCard() {
	card := d.card
	if card == nil {
		return
	}
	d.card = nil
	lastPage := d.pdf.PageNo()
	bottom := d.pdf.GetY() + journalPadding
	if bottom > pageHeight-marginBottom {
		bottom = pageHeight - marginBottom
	}

	for page := card.page; page <= lastPage; page++ {
		d.pdf.SetPage(page)
		top, end := marginTop, pageHeight-marginBottom
		topRadius, bottomRadius := 0.0, 0.0
		if page == card.page {
			top, topRadius = card.top, journalRadius
		}
		if page == lastPage {
			end, bottomRadius = bottom, journalRadius
		}
		d.pdf.SetAlpha(1, "Multiply")
		d.fill(cardColor)
		d.pdf.RoundedRectExt(card.x, top, card.width, end-top, topRadius, topRadius, bottomRadius, bottomRadius, "F")
		d.pdf.SetAlpha(1, "Normal")
		for _, picture := range card.pictures {
			if picture.page == page {
				d.framedPicture(picture.name, picture.info, picture.x, picture.y, picture.w, picture.h,
					picture.focusX, picture.focusY, picture.round)
			}
		}
		d.stroke(borderColor)
		d.pdf.SetLineWidth(0.25)
		d.pdf.RoundedRectExt(card.x, top, card.width, end-top, topRadius, topRadius, bottomRadius, bottomRadius, "D")
	}
	d.pdf.SetPage(lastPage)
	d.pdf.SetXY(marginLeft, bottom+journalGap)
}

// callout writes a short passage set apart - the moment a day is remembered
// by - on the soft tone of the journal, under a label, across width at x.
func (d *document) callout(title, text string, x, width float64) {
	d.pdf.SetFont(fontFamily, "B", 10)
	lines := d.pdf.SplitText(text, width-8)
	height := 4 + sizeSmall*0.45 + 2 + float64(len(lines))*5 + 3
	d.keepTogether(height)
	top := d.pdf.GetY()
	d.fill(softColor)
	d.pdf.RoundedRect(x, top, width, height, 2.5, "1234", "F")
	d.label(title, x+4, top+4, 7.5, accentColor)
	d.pdf.SetFont(fontFamily, "B", 10)
	d.ink(inkColor)
	d.pdf.SetXY(x+4, top+4+sizeSmall*0.45+2)
	d.pdf.MultiCell(width-8, 5, text, "", "L", false)
	d.pdf.SetXY(marginLeft, top+height+journalGap)
}
