package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"sort"
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

// The geometry of the list at its full size, in millimetres. A layout scales
// all of it at once (packingStyle), so a compact list is the same list smaller.
const (
	packingColumnGap = 7.0
	// bandHeight is the dark band at the top of the first page, and
	// compactBandHeight the thin one of a compact list.
	bandHeight        = 56.0
	compactBandHeight = 24.0
	// logoRadius is the disc the mark sits on, at the right of the band.
	logoRadius        = 16.0
	compactLogoRadius = 8.0

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
	packingLine      = 4.8
	// packingMetaLine is a line of an item's note.
	packingMetaLine = 3.8
	pillHeight      = 4.4
	pillGap         = 1.0
	sizePill        = 8.0
	packingRowGap   = 2.8
)

// packingBottom is the lowest a card may reach.
const packingBottom = pageHeight - marginBottom - 2

// packingStyle is one way of laying a list out: how many columns, at what
// scale of the full size, and whether a category may be split between
// columns when it would fit whole.
type packingStyle struct {
	columns int
	scale   float64
	compact bool
	// split lets a category that fits a column be broken across two to fill
	// the page; one taller than a whole column is always broken.
	split bool

	gap, column, padding, radius, cardGap         float64
	badge, badgeRadius, badgeIcon, headerGap      float64
	box, boxRadius, boxGap, quantity, inner, text float64
	line, metaLine, pillHeight, pillGap, rowGap   float64
	sizeBody, sizeSmall, sizePill, sizeSubhead    float64
}

// newPackingStyle works out every measure of a layout from its base.
//
// Arguments:
//   - columns: how many columns the page is divided into.
//   - scale: the share of the full size everything is drawn at.
//   - compact: whether the first page carries the thin band.
//   - split: whether a category may be split while it would fit whole.
//
// Returns:
//   - the style.
func newPackingStyle(columns int, scale float64, compact, split bool) packingStyle {
	s := packingStyle{columns: columns, scale: scale, compact: compact, split: split}
	s.gap = packingColumnGap * scale
	s.column = (contentWidth - float64(columns-1)*s.gap) / float64(columns)
	s.padding, s.radius, s.cardGap = cardPadding*scale, cardRadius*scale, cardGap*scale
	s.badge, s.badgeRadius, s.badgeIcon = badgeSize*scale, badgeRadius*scale, badgeIcon*scale
	s.headerGap = cardHeaderGap * scale
	s.box, s.boxRadius, s.boxGap = packingBox*scale, packingBoxRadius*scale, packingBoxGap*scale
	s.quantity = quantityWidth * scale
	s.inner = s.column - 2*s.padding
	s.text = s.inner - s.box - s.boxGap - s.quantity
	s.line, s.metaLine = packingLine*scale, packingMetaLine*scale
	s.pillHeight, s.pillGap, s.rowGap = pillHeight*scale, pillGap*scale, packingRowGap*scale
	s.sizeBody, s.sizeSmall = sizeBody*scale, sizeSmall*scale
	s.sizePill, s.sizeSubhead = sizePill*scale, sizeSubhead*scale
	return s
}

// chrome is what a card takes besides its rows: its padding and its heading,
// less the gap the last row does not need.
func (s packingStyle) chrome() float64 {
	return 2*s.padding + s.badge + s.headerGap - s.rowGap
}

// top is where the columns start on a page: under the band on the first, at
// the margin on the others.
func (s packingStyle) top(page int) float64 {
	if page > 0 {
		return marginTop
	}
	if s.compact {
		return compactBandHeight + 7
	}
	return bandHeight + 8
}

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
	// Compact asks for the list on one page if it can be made to fit: a thin
	// band, the categories packed tightly and everything a little smaller.
	Compact bool
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
	blocks := packingBlocks(text, packing.List)
	style, pages := choosePackingLayout(doc, packing, blocks)

	// Every page is laid on the same paper; the first carries the band.
	pdf.SetHeaderFunc(func() {
		setFill(pdf, packingPaper)
		pdf.Rect(0, 0, pageWidth, pageHeight, "F")
		if pdf.PageNo() == 1 {
			drawPackingBand(doc, text, packing.Trip, style.compact)
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

	if len(blocks) == 0 {
		pdf.SetXY(marginLeft, style.top(0))
		doc.body(text.packingEmpty, 0, "I")
		return doc.render(w)
	}
	drawer := &packingDrawer{doc: doc, packing: packing, style: style}
	for index, page := range pages {
		if index > 0 {
			doc.page()
		}
		for column, cards := range page {
			y := style.top(index)
			for _, card := range cards {
				y = drawer.card(blocks[card.block], card, style.columnX(column), y)
			}
		}
	}
	return doc.render(w)
}

// The scales a list is tried at, largest first. A full list keeps its size;
// a compact one shrinks a step at a time until it fits on one page.
var packingScales = []float64{1, 0.95, 0.9, 0.85, 0.8, 0.75}

// choosePackingLayout picks the style a list is drawn in and places its
// categories with it.
//
// A list at its full size takes as many pages as it needs. A compact one is
// tried in two columns at each scale, then in three, first keeping every
// category whole and then letting one be split, and the first way that fits
// on one page is taken; a list that fits no way is drawn at the smallest
// scale on as few pages as it takes. The trial is deterministic, so the same
// list always comes out alike.
//
// Arguments:
//   - doc: the document, whose fonts measure the rows.
//   - packing: the list and how it is asked for.
//   - blocks: the categories in their order.
//
// Returns:
//   - the style.
//   - the pages, each its columns of cards.
func choosePackingLayout(doc *document, packing Packing, blocks []packingBlock) (packingStyle, [][][]placedCard) {
	if !packing.Compact {
		style := newPackingStyle(2, 1, false, false)
		return style, placePacking(doc, packing, style, blocks)
	}
	for _, split := range []bool{false, true} {
		for _, columns := range []int{2, 3} {
			for _, scale := range packingScales {
				style := newPackingStyle(columns, scale, true, split)
				if pages := placePacking(doc, packing, style, blocks); len(pages) <= 1 {
					return style, pages
				}
			}
		}
	}
	style := newPackingStyle(2, packingScales[len(packingScales)-1], true, true)
	return style, placePacking(doc, packing, style, blocks)
}

// columnX is the left edge of a column.
func (s packingStyle) columnX(column int) float64 {
	return marginLeft + float64(column)*(s.column+s.gap)
}

// placedCard is a card on a page: some rows of one category, from its
// first to before its last, and how tall it is drawn.
type placedCard struct {
	block, from, to int
	height          float64
}

// pendingBlock is what is left of a category to place: its rows from one on.
type pendingBlock struct {
	block, from int
}

// placePacking places the categories on pages, without drawing them.
//
// Each page is filled like a game of Tetris: the tallest category that still
// fits whole goes into the shortest column it fits in, until none fits. Then a
// category taller than a whole column - or, when the style allows it, the
// first one left - is broken into the column with the most room, and when
// nothing can go on the page any more the next one is begun. Within a column
// the cards are then put back in the order of the list, so it still reads in
// the order its owner gave it wherever the packing allows, and the columns in
// the order of their first cards.
//
// Arguments:
//   - doc: the document, whose fonts measure the rows.
//   - packing: who brings what, which adds to a row's height.
//   - style: the layout.
//   - blocks: the categories in their order.
//
// Returns:
//   - the pages, each its columns of cards in the order they are drawn.
func placePacking(doc *document, packing Packing, style packingStyle, blocks []packingBlock) [][][]placedCard {
	measure := &packingDrawer{doc: doc, packing: packing, style: style}
	rows := make([][]float64, len(blocks))
	for index, block := range blocks {
		rows[index] = make([]float64, len(block.items))
		for row, item := range block.items {
			rows[index][row] = measure.rowHeight(item)
		}
	}
	// cardHeight is how tall a card of some rows of a category is.
	cardHeight := func(block, from, to int) float64 {
		height := style.chrome()
		for _, row := range rows[block][from:to] {
			height += row
		}
		return height
	}

	pending := make([]pendingBlock, len(blocks))
	for index := range blocks {
		pending[index] = pendingBlock{block: index}
	}
	var pages [][][]placedCard
	for page := 0; len(pending) > 0; page++ {
		top := style.top(page)
		columns := make([][]placedCard, style.columns)
		heights := make([]float64, style.columns)
		for column := range heights {
			heights[column] = top
		}
		// room is how much a card may take in a column, its gap included.
		room := func(column int) float64 {
			gap := 0.0
			if len(columns[column]) > 0 {
				gap = style.cardGap
			}
			return packingBottom - heights[column] - gap
		}
		add := func(column int, card placedCard) {
			if len(columns[column]) > 0 {
				heights[column] += style.cardGap
			}
			heights[column] += card.height
			columns[column] = append(columns[column], card)
		}
		for len(pending) > 0 {
			// The tallest category that fits whole, into the shortest column
			// it fits in.
			best, bestColumn, bestHeight := -1, -1, 0.0
			for index, each := range pending {
				height := cardHeight(each.block, each.from, len(blocks[each.block].items))
				// A tie keeps the category that comes first in the list.
				if best >= 0 && height <= bestHeight {
					continue
				}
				column := -1
				for candidate := range columns {
					if height <= room(candidate) && (column < 0 || heights[candidate] < heights[column]) {
						column = candidate
					}
				}
				if column >= 0 {
					best, bestColumn, bestHeight = index, column, height
				}
			}
			if best >= 0 {
				each := pending[best]
				add(bestColumn, placedCard{block: each.block, from: each.from,
					to: len(blocks[each.block].items), height: bestHeight})
				pending = append(pending[:best], pending[best+1:]...)
				continue
			}

			// Nothing fits whole: break a category that cannot, or the first
			// one when the style lets it, into the column with the most room.
			breaking := -1
			for index, each := range pending {
				whole := cardHeight(each.block, each.from, len(blocks[each.block].items))
				if style.split || whole > packingBottom-top {
					breaking = index
					break
				}
			}
			if breaking < 0 {
				break
			}
			column := 0
			for each := range columns {
				if room(each) > room(column) {
					column = each
				}
			}
			each := pending[breaking]
			to := each.from
			for to < len(blocks[each.block].items) && cardHeight(each.block, each.from, to+1) <= room(column) {
				to++
			}
			if to == each.from {
				// Not even one row fits beside the rest. An empty column takes
				// one anyway, however tall; otherwise the page is full.
				if len(columns[column]) > 0 {
					break
				}
				to++
			}
			add(column, placedCard{block: each.block, from: each.from, to: to,
				height: cardHeight(each.block, each.from, to)})
			if to == len(blocks[each.block].items) {
				pending = append(pending[:breaking], pending[breaking+1:]...)
			} else {
				pending[breaking].from = to
			}
		}
		for _, cards := range columns {
			sort.SliceStable(cards, func(a, b int) bool {
				if cards[a].block != cards[b].block {
					return cards[a].block < cards[b].block
				}
				return cards[a].from < cards[b].from
			})
		}
		// The column holding the earliest category goes first, so the page
		// begins where the list does.
		sort.SliceStable(columns, func(a, b int) bool {
			if len(columns[b]) == 0 {
				return len(columns[a]) > 0
			}
			return len(columns[a]) > 0 && columns[a][0].block < columns[b][0].block
		})
		pages = append(pages, columns)
	}
	return pages
}

// drawPackingBand writes the top of the first page: what the document is, the
// trip and its dates on the left, the application's mark on the right.
func drawPackingBand(doc *document, text labels, trip domain.Trip, compact bool) {
	pdf := doc.pdf
	height, radius := bandHeight, logoRadius
	if compact {
		height, radius = compactBandHeight, compactLogoRadius
	}
	setFill(pdf, packingBand)
	pdf.Rect(0, 0, pageWidth, height, "F")

	centreX, centreY := pageWidth-marginRight-radius, height/2
	setFill(pdf, mixInk(packingPaper, packingBand, 0.25))
	pdf.Circle(centreX, centreY, radius*1.1, "F")
	setFill(pdf, packingPaper)
	pdf.Circle(centreX, centreY, radius, "F")
	logo := radius * 1.5
	options := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
	pdf.RegisterImageOptionsReader("tripvault-logo", options, bytes.NewReader(logoPNG))
	pdf.ImageOptions("tripvault-logo", centreX-logo/2, centreY-logo/2, logo, logo, false, options, 0, "")

	width := pageWidth - marginLeft - marginRight - 2*radius - 10
	if compact {
		drawCompactBandText(doc, text, trip, width, height)
		return
	}
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

// drawCompactBandText writes the thin band of a compact list: what it is and
// the trip's dates on one small line, the title in one line under it.
func drawCompactBandText(doc *document, text labels, trip domain.Trip, width, height float64) {
	pdf := doc.pdf
	caption := strings.ToUpper(text.packingTitle)
	if span := text.dateRange(trip.StartDate, trip.EndDate); span != "" {
		caption += " · " + span
	}
	pdf.SetFont(fontFamily, "B", sizeCaption+1)
	setText(pdf, packingBandText)
	pdf.SetXY(marginLeft, height/2-6.5)
	pdf.CellFormat(width, 4, fitText(doc, caption, width), "", 0, "L", false, 0, "")
	pdf.SetFont(fontFamily, "B", sizeHeading+3)
	setText(pdf, packingWhite)
	pdf.SetXY(marginLeft, height/2-1.5)
	pdf.CellFormat(width, 8, fitText(doc, trip.Title, width), "", 0, "L", false, 0, "")
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

// packingDrawer measures and draws the cards of a list in one style.
type packingDrawer struct {
	doc     *document
	packing Packing
	style   packingStyle
}

// card writes a card of a category with some of its rows - its tinted frame,
// the badge with the category's icon, its name and the rows under them - at a
// point of the page.
//
// Returns:
//   - where the next card in the column begins.
func (l *packingDrawer) card(block packingBlock, card placedCard, x, y float64) float64 {
	pdf := l.doc.pdf
	s := l.style
	ink := tagInk(block.color)

	setFill(pdf, mixInk(ink, packingCard, 0.04))
	setDraw(pdf, mixInk(ink, packingBorder, 0.25))
	pdf.SetLineWidth(0.3)
	pdf.RoundedRect(x, y, s.column, card.height, s.radius, "1234", "FD")

	badgeX, badgeY := x+s.padding, y+s.padding
	setFill(pdf, mixInk(ink, packingWhite, 0.16))
	pdf.RoundedRect(badgeX, badgeY, s.badge, s.badge, s.badgeRadius, "1234", "F")
	setFill(pdf, mixInk(ink, packingBlack, 0.85))
	inset := (s.badge - s.badgeIcon) / 2
	drawPackingIcon(pdf, block.icon, badgeX+inset, badgeY+inset, s.badgeIcon)

	titleX := badgeX + s.badge + 3*s.scale
	titleWidth := x + s.column - s.padding - titleX
	pdf.SetFont(fontFamily, "B", s.sizeSubhead)
	setText(pdf, packingInk)
	pdf.SetXY(titleX, badgeY)
	pdf.CellFormat(titleWidth, s.badge, fitText(l.doc, block.title, titleWidth), "", 0, "L", false, 0, "")

	row := badgeY + s.badge + s.headerGap
	for _, item := range block.items[card.from:card.to] {
		l.row(x+s.padding, row, item, ink)
		row += l.rowHeight(item)
	}
	setText(pdf, packingBlack)
	setDraw(pdf, packingBlack)
	return y + card.height + s.cardGap
}

// bringer is the name of the member who brings an item, empty when nobody
// is named or the reader is not told.
func (l *packingDrawer) bringer(item domain.PackingItem) string {
	if item.BringerID == nil {
		return ""
	}
	return l.packing.Bringers[*item.BringerID]
}

// rowHeight measures an item as row writes it, the gap after it included.
func (l *packingDrawer) rowHeight(item domain.PackingItem) float64 {
	pdf := l.doc.pdf
	s := l.style
	pdf.SetFont(fontFamily, "B", s.sizeBody)
	height := float64(len(pdf.SplitText(item.Name, s.text))) * s.line
	if item.Note != "" {
		pdf.SetFont(fontFamily, "", s.sizeSmall)
		height += float64(len(pdf.SplitText(item.Note, s.text))) * s.metaLine
	}
	if l.bringer(item) != "" {
		height += s.pillGap + s.pillHeight
	}
	return max(height, s.box) + s.rowGap
}

// row writes one item from its left edge: an empty box, its name, how many on
// the right in the category's colour, the note under the name and who brings
// it as a pill under that.
func (l *packingDrawer) row(x, y float64, item domain.PackingItem, ink [3]int) {
	pdf := l.doc.pdf
	s := l.style
	setFill(pdf, packingWhite)
	setDraw(pdf, packingBoxInk)
	pdf.SetLineWidth(0.3)
	pdf.RoundedRect(x, y+(s.line-s.box)/2, s.box, s.box, s.boxRadius, "1234", "FD")

	textX := x + s.box + s.boxGap
	pdf.SetFont(fontFamily, "B", s.sizeBody)
	setText(pdf, packingInk)
	pdf.SetXY(textX, y)
	pdf.MultiCell(s.text, s.line, item.Name, "", "L", false)
	after := pdf.GetY()
	if item.Quantity > 1 {
		setText(pdf, mixInk(ink, packingBlack, 0.85))
		pdf.SetXY(x+s.inner-s.quantity, y)
		pdf.CellFormat(s.quantity, s.line, fmt.Sprintf("%s%d", quantityMark, item.Quantity), "", 0, "R", false, 0, "")
	}
	if item.Note != "" {
		pdf.SetFont(fontFamily, "", s.sizeSmall)
		setText(pdf, grey)
		pdf.SetXY(textX, after)
		pdf.MultiCell(s.text, s.metaLine, item.Note, "", "L", false)
		after = pdf.GetY()
	}
	if name := l.bringer(item); name != "" {
		pdf.SetFont(fontFamily, "", s.sizePill)
		name = fitText(l.doc, name, s.text-4*s.scale)
		width := pdf.GetStringWidth(name) + 4*s.scale
		setFill(pdf, packingPill)
		pdf.RoundedRect(textX, after+s.pillGap, width, s.pillHeight, s.pillHeight/2, "1234", "F")
		setText(pdf, packingInk)
		pdf.SetXY(textX, after+s.pillGap)
		pdf.CellFormat(width, s.pillHeight, name, "", 0, "C", false, 0, "")
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
