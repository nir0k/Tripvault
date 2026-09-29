package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A packing list is printed to be ticked with a pen before the road: a dark
// band with the trip and the application's mark, then the categories as cards
// in two columns, each in its own colour and with its icon, and an empty box
// for every item. Every box is empty, because a list is printed to pack from;
// what is ticked already is read in the interface. It is written apart from
// the plan's document, because it is used before the road rather than on it.

// logoPNG is the application's mark, printed in the band of the first page.
//
//go:embed images/logo.png
var logoPNG []byte

// quantityMark reads how many of one thing an item counts.
const quantityMark = "×"

// The geometry of the list, in millimetres.
const (
	packingColumnGap = 7.0
	packingColumn    = (contentWidth - packingColumnGap) / 2
	// bandHeight is the dark band at the top of the first page.
	bandHeight = 56.0
	// logoRadius is the disc the mark sits on, at the right of the band.
	logoRadius = 16.0

	cardPadding   = 4.5
	cardRadius    = 3.0
	cardGap       = 5.0
	badgeSize     = 9.0
	badgeRadius   = 2.0
	badgeIcon     = 5.0
	cardHeaderGap = 3.5

	packingBox       = 3.8
	packingBoxRadius = 0.8
	packingBoxGap    = 2.6
	quantityWidth    = 9.0
	// packingInner is the width of a card inside its padding.
	packingInner = packingColumn - 2*cardPadding
	packingText  = packingInner - packingBox - packingBoxGap - quantityWidth
	packingLine  = 4.8
	// packingMetaLine is a line of an item's note.
	packingMetaLine = 3.8
	pillHeight      = 4.4
	pillGap         = 1.0
	sizePill        = 8.0
	packingRowGap   = 2.8

	// packingChrome is what a card takes besides its rows: its padding, its
	// heading, less the gap the last row does not need.
	packingChrome = 2*cardPadding + badgeSize + cardHeaderGap - packingRowGap
)

// packingBottom is the lowest a card may reach.
const packingBottom = pageHeight - marginBottom - 2

// The inks of the list. The page is a warm paper rather than white, the band
// a deep green, and a category's own colour tints only its card and badge.
var (
	packingPaper    = [3]int{246, 242, 234}
	packingBand     = [3]int{57, 74, 68}
	packingBandText = [3]int{214, 222, 217}
	packingInk      = [3]int{38, 38, 36}
	packingCard     = [3]int{254, 253, 250}
	packingBorder   = [3]int{222, 212, 199}
	packingBoxInk   = [3]int{150, 142, 132}
	packingPill     = [3]int{238, 231, 221}
	packingWhite    = [3]int{255, 255, 255}
	packingBlack    = [3]int{0, 0, 0}
)

// tagInks are the colours of the tags' palette, as the interface draws them
// (frontend/src/style.css).
var tagInks = map[domain.TagColor][3]int{
	"red": {0xdc, 0x26, 0x26}, "orange": {0xea, 0x58, 0x0c}, "amber": {0xd9, 0x77, 0x06},
	"green": {0x16, 0xa3, 0x4a}, "teal": {0x0d, 0x94, 0x88}, "sky": {0x02, 0x84, 0xc7},
	"blue": {0x25, 0x63, 0xeb}, "violet": {0x7c, 0x3a, 0xed}, "pink": {0xdb, 0x27, 0x77},
	"gray": {0x6b, 0x72, 0x80}, "yellow": {0xca, 0x8a, 0x04}, "emerald": {0x05, 0x96, 0x69},
	"indigo": {0x4f, 0x46, 0xe5}, "fuchsia": {0xc0, 0x26, 0xd3}, "lime": {0x65, 0xa3, 0x0d},
	"cyan": {0x08, 0x91, 0xb2}, "purple": {0x93, 0x33, 0xea}, "rose": {0xe1, 0x1d, 0x48},
	"brown": {0x92, 0x40, 0x0e},
}

// tagInk reads a colour of the palette, grey for one it does not know.
func tagInk(color domain.TagColor) [3]int {
	if ink, ok := tagInks[color]; ok {
		return ink
	}
	return tagInks["gray"]
}

// mixInk blends share of a colour into a base, as color-mix does in the browser.
func mixInk(color, base [3]int, share float64) [3]int {
	var mixed [3]int
	for index := range mixed {
		mixed[index] = int(float64(color[index])*share + float64(base[index])*(1-share) + 0.5)
	}
	return mixed
}

// Packing is everything a packing list's document is built from.
type Packing struct {
	Trip domain.Trip
	List domain.PackingList
	// Bringers name the members who bring something, by their identifier; an
	// item whose bringer is missing here is printed without one.
	Bringers map[uuid.UUID]string
	// Language is the reader's language; anything but "ru" is written in English.
	Language string
}

// packingBlock is one category as the page lays it out.
type packingBlock struct {
	title string
	color domain.TagColor
	icon  domain.PackingIcon
	items []domain.PackingItem
}

// RenderPacking - writes a trip's packing list as a checklist to print.
//
// Arguments:
//   - w: where the document goes.
//   - packing: the trip, its list and who brings what.
//
// Returns:
//   - an error if the document could not be written.
func RenderPacking(w io.Writer, packing Packing) error {
	text := wording(packing.Language, "")
	doc := newDocument(text.packingTitle+" · "+packing.Trip.Title, packing.Trip.Title)
	pdf := doc.pdf
	// Every page is laid on the same paper; the first carries the band.
	pdf.SetHeaderFunc(func() {
		setFill(pdf, packingPaper)
		pdf.Rect(0, 0, pageWidth, pageHeight, "F")
		if pdf.PageNo() == 1 {
			drawPackingBand(doc, text, packing.Trip)
		}
	})
	// Every page of a checklist is as much the list as the first, so each
	// carries the trip and its number.
	pdf.SetFooterFunc(func() {
		pdf.SetY(-marginBottom + 6)
		pdf.SetFont(fontFamily, "", sizeSmall)
		setText(pdf, grey)
		pdf.CellFormat(contentWidth/2, 5, packing.Trip.Title, "", 0, "L", false, 0, "")
		pdf.CellFormat(contentWidth/2, 5, fmt.Sprint(pdf.PageNo()), "", 0, "R", false, 0, "")
		setText(pdf, packingBlack)
	})
	// The columns are laid out by hand, so fpdf must not break a page under them.
	pdf.SetAutoPageBreak(false, marginBottom)
	doc.page()

	top := bandHeight + 8
	blocks := packingBlocks(text, packing.List)
	if len(blocks) == 0 {
		pdf.SetXY(marginLeft, top)
		doc.body(text.packingEmpty, 0, "I")
		return doc.render(w)
	}
	layout := &packingLayout{doc: doc, packing: packing, top: top, y: top}
	heights := make([]float64, len(blocks))
	for index, block := range blocks {
		heights[index] = layout.blockHeight(block)
	}
	layout.balance(heights)
	for index, block := range blocks {
		// A category that fits in a column of its own is never split: it goes
		// to the next column when the rest of this one is too short for it, or
		// when it would reach past the middle of what is left on the last page.
		height := heights[index]
		overflows := layout.y+height > packingBottom
		pastHalf := layout.column == 0 && layout.half > 0 && layout.y+height/2 > layout.half
		if layout.y > layout.top && height <= packingBottom-marginTop && (overflows || pastHalf) {
			if layout.next() {
				layout.balance(heights[index:])
			}
		}
		if layout.block(block) {
			layout.balance(heights[index+1:])
		}
	}
	return doc.render(w)
}

// drawPackingBand writes the top of the first page: what the document is, the
// trip and its dates on the left, the application's mark on the right.
func drawPackingBand(doc *document, text labels, trip domain.Trip) {
	pdf := doc.pdf
	setFill(pdf, packingBand)
	pdf.Rect(0, 0, pageWidth, bandHeight, "F")

	centreX, centreY := pageWidth-marginRight-logoRadius, bandHeight/2
	setFill(pdf, mixInk(packingPaper, packingBand, 0.25))
	pdf.Circle(centreX, centreY, logoRadius+1.6, "F")
	setFill(pdf, packingPaper)
	pdf.Circle(centreX, centreY, logoRadius, "F")
	logo := logoRadius * 1.5
	options := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
	pdf.RegisterImageOptionsReader("tripvault-logo", options, bytes.NewReader(logoPNG))
	pdf.ImageOptions("tripvault-logo", centreX-logo/2, centreY-logo/2, logo, logo, false, options, 0, "")

	width := pageWidth - marginLeft - marginRight - 2*logoRadius - 10
	pdf.SetFont(fontFamily, "B", sizeSmall)
	setText(pdf, packingBandText)
	pdf.SetXY(marginLeft, 14)
	pdf.CellFormat(width, 5, strings.ToUpper(text.packingTitle), "", 0, "L", false, 0, "")

	lines, size := bandTitle(doc, trip.Title, width)
	setText(pdf, packingWhite)
	pdf.SetFont(fontFamily, "B", size)
	y := 21.0
	for _, line := range lines {
		pdf.SetXY(marginLeft, y)
		pdf.CellFormat(width, size*0.42, line, "", 0, "L", false, 0, "")
		y += size * 0.42
	}
	if span := text.dateRange(trip.StartDate, trip.EndDate); span != "" {
		pdf.SetFont(fontFamily, "", sizeSubhead)
		setText(pdf, packingBandText)
		pdf.SetXY(marginLeft, y+2)
		pdf.CellFormat(width, 6, span, "", 0, "L", false, 0, "")
	}
	setText(pdf, packingBlack)
}

// bandTitle fits the trip's title into the band: as large as one line allows,
// down to a size at which it wraps onto a second line, shortened beyond that.
//
// Returns:
//   - the lines to write, one or two.
//   - the type size they are written in.
func bandTitle(doc *document, title string, width float64) ([]string, float64) {
	const largest, smallest = sizeTitle, 18.0
	pdf := doc.pdf
	for size := largest; size >= smallest; size -= 2 {
		pdf.SetFont(fontFamily, "B", size)
		if pdf.GetStringWidth(title) <= width {
			return []string{title}, size
		}
	}
	pdf.SetFont(fontFamily, "B", smallest)
	lines := pdf.SplitText(title, width)
	if len(lines) > 2 {
		lines = []string{lines[0], fitText(doc, strings.Join(lines[1:], " "), width)}
	}
	return lines, smallest
}

// packingBlocks orders the list into what is printed: each category with
// something in it, then the items without a category.
func packingBlocks(text labels, list domain.PackingList) []packingBlock {
	blocks := make([]packingBlock, 0, len(list.Categories)+1)
	for _, category := range list.Categories {
		id := category.ID
		if items := list.InCategory(&id); len(items) > 0 {
			blocks = append(blocks, packingBlock{
				title: category.Name, color: category.Color, icon: category.Icon, items: items,
			})
		}
	}
	if items := list.InCategory(nil); len(items) > 0 {
		blocks = append(blocks, packingBlock{
			title: text.packingOther, color: "gray", icon: domain.PackingIconOther, items: items,
		})
	}
	return blocks
}

// packingLayout is where the next card of the list goes: a column of the
// current page and the height reached in it.
type packingLayout struct {
	doc     *document
	packing Packing
	// top is where the columns start on the current page: under the band on
	// the first, at the margin on the others.
	top    float64
	column int
	y      float64
	// half is where the first column of the last page ends, so the two
	// columns of what is left come out about as tall; zero while what is left
	// needs more than one page.
	half float64
}

// balance sets where the first column of the page ends, given the blocks not
// placed yet: halfway through them when they all fit on this page.
func (l *packingLayout) balance(heights []float64) {
	remaining := 0.0
	for _, height := range heights {
		remaining += height + cardGap
	}
	l.half = 0
	if l.column == 0 && remaining <= 2*(packingBottom-l.top) {
		l.half = l.y + remaining/2
	}
}

// blockHeight measures a whole category as one card.
func (l *packingLayout) blockHeight(block packingBlock) float64 {
	height := packingChrome
	for _, item := range block.items {
		height += l.rowHeight(item)
	}
	return height
}

// x is the left edge of the current column.
func (l *packingLayout) x() float64 {
	return marginLeft + float64(l.column)*(packingColumn+packingColumnGap)
}

// next moves to the top of the next column, or of a new page after the second.
//
// Returns:
//   - true when a new page was started.
func (l *packingLayout) next() bool {
	l.half = 0
	if l.column == 0 {
		l.column = 1
		l.y = l.top
		return false
	}
	l.doc.page()
	l.column = 0
	l.top = marginTop
	l.y = l.top
	return true
}

// fitting counts how many of the rows, taken in order, fit in one card in
// what is left of the column.
func (l *packingLayout) fitting(heights []float64) int {
	used := l.y + packingChrome
	for count, height := range heights {
		used += height
		if used > packingBottom {
			return count
		}
	}
	return len(heights)
}

// block writes one category where the layout stands. One longer than what is
// left of the column flows on as a card in each column, its heading repeated.
//
// Returns:
//   - true when the category flowed onto a new page, so the columns of what
//     is left are balanced afresh.
func (l *packingLayout) block(block packingBlock) bool {
	heights := make([]float64, len(block.items))
	for index, item := range block.items {
		heights[index] = l.rowHeight(item)
	}
	newPage := false
	for start := 0; start < len(block.items); {
		count := l.fitting(heights[start:])
		if count == 0 && l.y > l.top {
			newPage = l.next() || newPage
			continue
		}
		// A single item taller than a whole column is printed anyway.
		count = max(count, 1)
		l.card(block, block.items[start:start+count], heights[start:start+count])
		start += count
		if start < len(block.items) {
			newPage = l.next() || newPage
		}
	}
	return newPage
}

// card writes a card of a category with some of its rows: its tinted frame,
// the badge with the category's icon, its name and the rows under them.
func (l *packingLayout) card(block packingBlock, items []domain.PackingItem, heights []float64) {
	pdf := l.doc.pdf
	ink := tagInk(block.color)
	x := l.x()
	height := packingChrome
	for _, row := range heights {
		height += row
	}

	setFill(pdf, mixInk(ink, packingCard, 0.04))
	setDraw(pdf, mixInk(ink, packingBorder, 0.25))
	pdf.SetLineWidth(0.3)
	pdf.RoundedRect(x, l.y, packingColumn, height, cardRadius, "1234", "FD")

	badgeX, badgeY := x+cardPadding, l.y+cardPadding
	setFill(pdf, mixInk(ink, packingWhite, 0.16))
	pdf.RoundedRect(badgeX, badgeY, badgeSize, badgeSize, badgeRadius, "1234", "F")
	setFill(pdf, mixInk(ink, packingBlack, 0.85))
	inset := (badgeSize - badgeIcon) / 2
	drawPackingIcon(pdf, block.icon, badgeX+inset, badgeY+inset, badgeIcon)

	titleX := badgeX + badgeSize + 3
	titleWidth := x + packingColumn - cardPadding - titleX
	pdf.SetFont(fontFamily, "B", sizeSubhead)
	setText(pdf, packingInk)
	pdf.SetXY(titleX, badgeY)
	pdf.CellFormat(titleWidth, badgeSize, fitText(l.doc, block.title, titleWidth), "", 0, "L", false, 0, "")

	y := badgeY + badgeSize + cardHeaderGap
	for index, item := range items {
		l.row(x+cardPadding, y, item, ink)
		y += heights[index]
	}
	setText(pdf, packingBlack)
	setDraw(pdf, packingBlack)
	l.y += height + cardGap
}

// bringer is the name of the member who brings an item, empty when nobody
// is named or the reader is not told.
func (l *packingLayout) bringer(item domain.PackingItem) string {
	if item.BringerID == nil {
		return ""
	}
	return l.packing.Bringers[*item.BringerID]
}

// rowHeight measures an item as row writes it, the gap after it included.
func (l *packingLayout) rowHeight(item domain.PackingItem) float64 {
	pdf := l.doc.pdf
	pdf.SetFont(fontFamily, "B", sizeBody)
	height := float64(len(pdf.SplitText(item.Name, packingText))) * packingLine
	if item.Note != "" {
		pdf.SetFont(fontFamily, "", sizeSmall)
		height += float64(len(pdf.SplitText(item.Note, packingText))) * packingMetaLine
	}
	if l.bringer(item) != "" {
		height += pillGap + pillHeight
	}
	return max(height, packingBox) + packingRowGap
}

// row writes one item from its left edge: an empty box, its name, how many on
// the right in the category's colour, the note under the name and who brings
// it as a pill under that.
func (l *packingLayout) row(x, y float64, item domain.PackingItem, ink [3]int) {
	pdf := l.doc.pdf
	setFill(pdf, packingWhite)
	setDraw(pdf, packingBoxInk)
	pdf.SetLineWidth(0.3)
	pdf.RoundedRect(x, y+(packingLine-packingBox)/2, packingBox, packingBox, packingBoxRadius, "1234", "FD")

	textX := x + packingBox + packingBoxGap
	pdf.SetFont(fontFamily, "B", sizeBody)
	setText(pdf, packingInk)
	pdf.SetXY(textX, y)
	pdf.MultiCell(packingText, packingLine, item.Name, "", "L", false)
	after := pdf.GetY()
	if item.Quantity > 1 {
		setText(pdf, mixInk(ink, packingBlack, 0.85))
		pdf.SetXY(x+packingInner-quantityWidth, y)
		pdf.CellFormat(quantityWidth, packingLine, fmt.Sprintf("%s%d", quantityMark, item.Quantity), "", 0, "R", false, 0, "")
	}
	if item.Note != "" {
		pdf.SetFont(fontFamily, "", sizeSmall)
		setText(pdf, grey)
		pdf.SetXY(textX, after)
		pdf.MultiCell(packingText, packingMetaLine, item.Note, "", "L", false)
		after = pdf.GetY()
	}
	if name := l.bringer(item); name != "" {
		pdf.SetFont(fontFamily, "", sizePill)
		name = fitText(l.doc, name, packingText-4)
		width := pdf.GetStringWidth(name) + 4
		setFill(pdf, packingPill)
		pdf.RoundedRect(textX, after+pillGap, width, pillHeight, pillHeight/2, "1234", "F")
		setText(pdf, packingInk)
		pdf.SetXY(textX, after+pillGap)
		pdf.CellFormat(width, pillHeight, name, "", 0, "C", false, 0, "")
	}
}

// setFill, setDraw and setText set fpdf's inks from one colour.
func setFill(pdf *fpdf.Fpdf, color [3]int) { pdf.SetFillColor(color[0], color[1], color[2]) }
func setDraw(pdf *fpdf.Fpdf, color [3]int) { pdf.SetDrawColor(color[0], color[1], color[2]) }
func setText(pdf *fpdf.Fpdf, color [3]int) { pdf.SetTextColor(color[0], color[1], color[2]) }

// fitText shortens a single line to a width, ending it with an ellipsis.
func fitText(doc *document, text string, width float64) string {
	if doc.pdf.GetStringWidth(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && doc.pdf.GetStringWidth(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
