package pdf

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The characters the document decorates itself with. They are named here
// because the embedded typeface has to have all of them: one it does not draws
// as an empty box, which is how the star this once used was found. The test
// beside this file reads the font and checks each of them.
const (
	// bulletMark opens an item of a list.
	bulletMark = "•"
	// arrowMark opens the journey from one place to the next.
	arrowMark = "→"
	// separator stands between the facts on one line.
	separator = "   ·   "
	// dot stands between the facts of one tag.
	dot = " · "
)

// Photo is one picture of the report, already rendered to the size the document
// uses. Decoding and scaling belong to the media package; by the time a picture
// reaches here it is a JPEG of a known width.
type Photo struct {
	// ID is the file the picture was rendered from, which is how a picture
	// that opens a day is kept out of the place it also hangs on.
	ID   uuid.UUID
	JPEG []byte
}

// Report is everything the document is built from.
//
// The pictures arrive resolved rather than as a callback, so that nothing in
// here reads from a database or a disk: what the document looks like can then be
// tested without either.
type Report struct {
	Trip    domain.Trip
	Content domain.DocumentContent
	Totals  domain.ReportTotals
	// Cover is the trip's own picture, whole, empty when it has none; the
	// cover's frame (Trip.CoverCrop) says which part of it matters.
	Cover []byte
	// Photos holds the pictures of the days and places, by the identifier of
	// the thing each hangs on, in the order they are shown.
	Photos map[uuid.UUID][]Photo
	// DayHeroes holds the large picture each day opens with, by the day: its
	// cover, or else one of the pictures of its places.
	DayHeroes map[uuid.UUID]Photo
	// Language is the reader's language; anything but "ru" is written in English.
	Language string
	// Units is what the reader counts distances in; kilometres when unset.
	Units domain.Units
	// Maps are the maps ReportMapRequests asked for, by the same keys: uuid.Nil
	// for the whole trip, a day's identifier for that day. A map not here is
	// not drawn.
	Maps map[uuid.UUID]Map
	// MapAttribution is the credit the tile provider asks for, in plain text.
	MapAttribution string
}

// The sizes of the journal's pictures, in millimetres.
const (
	coverHeight    = 165.0
	dayHeroHeight  = 80.0
	placeHeroLimit = 88.0
	singleLimit    = 95.0
	rowLimit       = 46.0
	photoGap       = 3.0
)

// Render - writes a report as a PDF, laid out as a travel journal.
//
// Arguments:
//   - w: where the document goes.
//   - report: the trip, its report and the pictures to show.
//
// Returns:
//   - an error if the document could not be written, which includes a
//     photograph that could not be read.
func Render(w io.Writer, report Report) error {
	text := wording(report.Language, report.Units)
	doc := newDocument(report.Trip.Title, report.Trip.Title)
	doc.useDisplay()

	if err := writeCover(doc, text, report); err != nil {
		return err
	}
	// Each page names the part of the journal it belongs to. The footer of a
	// page is written as the next page begins, so the name is changed only
	// once the new part's first page has started.
	doc.page()
	doc.footer = report.Trip.Title + " / " + text.overviewPage
	if err := writeOverview(doc, text, report); err != nil {
		return err
	}
	for index, day := range report.Content.Days {
		doc.page()
		doc.footer = report.Trip.Title + " / " + fmt.Sprintf(text.dayNumber, index+1)
		if err := writeDay(doc, text, report, day, index); err != nil {
			return err
		}
	}
	writeClosing(doc, text, report)
	return doc.render(w)
}

// writeCover draws the title page: the trip's picture across the top of the
// page, and under it what the journal is, the trip's name and words, and when
// it happened and who went. A trip without a picture opens on a band of the
// journal's green instead.
func writeCover(doc *document, text labels, report Report) error {
	doc.page()
	pdf := doc.pdf
	top := coverHeight
	if len(report.Cover) > 0 {
		name, info, err := doc.register(report.Cover)
		if err != nil {
			return err
		}
		focusX, focusY := 0.5, 0.5
		if crop := report.Trip.CoverCrop; crop != nil {
			focusX, focusY = crop.X+crop.W/2, crop.Y+crop.H/2
		}
		doc.framedPicture(name, info, 0, 0, pageWidth, coverHeight, focusX, focusY, 0)
	} else {
		top = coverHeight * 0.6
		doc.fill(greenColor)
		pdf.Rect(0, 0, pageWidth, top, "F")
	}
	doc.fill(accentColor)
	pdf.Rect(marginLeft, top-1.2, 24, 1.2, "F")

	doc.label(text.journal, marginLeft, top+14, 8, accentColor)
	pdf.SetY(top + 22)
	doc.display(report.Trip.Title, marginLeft, contentWidth, 30, inkColor)
	if report.Trip.Summary != "" {
		pdf.Ln(3)
		pdf.SetFont(fontFamily, "", 12)
		doc.ink(mutedColor)
		pdf.SetX(marginLeft)
		pdf.MultiCell(contentWidth, 6, report.Trip.Summary, "", "L", false)
		doc.ink(inkColor)
	}

	// The facts sit at the foot of the page, under a hairline, however long
	// the title and the words above them ran.
	line := max(pdf.GetY()+10, pageHeight-marginBottom-22)
	doc.stroke(borderColor)
	pdf.SetLineWidth(0.3)
	pdf.Line(marginLeft, line, pageWidth-marginRight, line)
	if span := text.dateRange(report.Trip.StartDate, report.Trip.EndDate); span != "" {
		doc.label(span, marginLeft, line+6, 8.5, inkColor)
	}
	// How many went is left out: a journal is written by its travellers, and a
	// count of them is a figure of the budget, not of the story.
	if report.Totals.Days > 0 {
		days := text.count(report.Totals.Days, text.dayWord)
		doc.label(days, pageWidth-marginRight-doc.labelWidth(days, 8.5), line+6, 8.5, inkColor)
	}
	return nil
}

// stat is one figure of the overview: its value and what it counts.
type stat struct {
	value string
	label string
}

// writeOverview writes the page that sums the trip up: a green band with its
// dates, the figures that have something to say, the words the report opens
// with, the map of the whole trip and the distance covered by each means of
// travel. A figure with nothing in it - no rating, nothing spent - is left out
// rather than written as a zero.
func writeOverview(doc *document, text labels, report Report) error {
	pdf := doc.pdf
	totals := report.Totals
	const band = 50.0
	doc.fill(greenColor)
	pdf.Rect(0, 0, pageWidth, band, "F")
	doc.label(text.glance, marginLeft, 15, 7.5, onGreenSoft)
	pdf.SetY(21)
	doc.display(text.journey, marginLeft, contentWidth, 26, onGreenColor)
	if span := text.dateRange(report.Trip.StartDate, report.Trip.EndDate); span != "" {
		pdf.SetFont(fontFamily, "", 10)
		doc.ink(onGreenSoft)
		pdf.SetX(marginLeft)
		pdf.CellFormat(contentWidth, 5, span, "", 1, "L", false, 0, "")
		doc.ink(inkColor)
	}
	pdf.SetY(band + 8)
	writeStats(doc, overviewStats(text, report))

	if intro := report.Content.Document.IntroMD; intro != "" {
		writeMarkdown(doc, intro)
		pdf.Ln(journalGap)
	}

	if m, ok := report.Maps[uuid.Nil]; ok {
		inner := contentWidth - 2*journalPadding
		height := journalPadding + 7 + mapHeightAt(m, inner) + 5 + journalPadding
		doc.keepTogether(height)
		top := pdf.GetY()
		doc.box(marginLeft, top, contentWidth, height)
		doc.label(text.route, marginLeft+journalPadding, top+journalPadding, 7.5, accentColor)
		if _, err := doc.drawMapIn(tripLayer(report.Content), m, report.MapAttribution,
			marginLeft+journalPadding, top+journalPadding+7, inner, 2.5); err != nil {
			return err
		}
		pdf.SetXY(marginLeft, top+height+journalGap)
	}

	writeModes(doc, text, totals)
	return nil
}

// overviewStats are the figures of the overview that have something to say,
// in the order they are shown: a rating only when something was rated, an
// amount only when something was spent.
func overviewStats(text labels, report Report) []stat {
	totals := report.Totals
	var stats []stat
	if totals.Days > 0 {
		stats = append(stats, stat{fmt.Sprint(totals.Days), text.days})
	}
	if total := totals.Places.Total(); total > 0 {
		stats = append(stats, stat{fmt.Sprintf("%d / %d", totals.Places.Visited, total), text.visited})
	}
	if totals.DistanceM > 0 {
		stats = append(stats, stat{text.distanceOf(totals.DistanceM), text.distance})
	}
	if totals.AverageRating != nil {
		stats = append(stats, stat{fmt.Sprintf("%.1f", *totals.AverageRating), text.averageShort})
	}
	if totals.ActualCost != 0 {
		stats = append(stats, stat{money(totals.ActualCost, report.Trip.Currency), text.spent})
	}
	return stats
}

// box draws a card of a known size: its paper and its border. A card whose
// height is known only once it is written is begun and ended instead.
func (d *document) box(x, y, w, h float64) {
	d.fill(cardColor)
	d.stroke(borderColor)
	d.pdf.SetLineWidth(0.25)
	d.pdf.RoundedRect(x, y, w, h, journalRadius, "1234", "FD")
}

// writeStats writes the figures of the overview as a row of small cards, as
// many across as there are figures.
func writeStats(doc *document, stats []stat) {
	if len(stats) == 0 {
		return
	}
	const height = 24.0
	width := (contentWidth - journalGap*float64(len(stats)-1)) / float64(len(stats))
	doc.keepTogether(height)
	top := doc.pdf.GetY()
	for index, figure := range stats {
		x := marginLeft + float64(index)*(width+journalGap)
		doc.box(x, top, width, height)
		size := 17.0
		// A long figure - an amount with its currency - steps down until it
		// fits its card.
		for doc.pdf.SetFont(displayFamily, "", size); size > 9 && doc.pdf.GetStringWidth(figure.value) > width-8; {
			size--
			doc.pdf.SetFont(displayFamily, "", size)
		}
		doc.ink(inkColor)
		doc.pdf.SetXY(x+4, top+5)
		doc.pdf.CellFormat(width-8, size*0.42, figure.value, "", 0, "L", false, 0, "")
		// A label that needs two lines starts higher, so both stay in the card.
		labelSize, lines := doc.fitLabel(figure.label, width-8, 6.5)
		labelTop := top + 16 - float64(len(lines)-1)*labelLineHeight(labelSize)/2
		doc.fittedLabel(figure.label, x+4, labelTop, width-8, 6.5, accentColor)
	}
	doc.pdf.SetXY(marginLeft, top+height+journalGap)
}

// writeModes writes the distance and time covered by each means of travel, as
// cards of three to a row, each with the icon of its means where there is one.
func writeModes(doc *document, text labels, totals domain.ReportTotals) {
	var modes []domain.TravelMode
	for _, mode := range travelModeOrder {
		if total, ok := totals.ByMode[mode]; ok && (total.DistanceM > 0 || total.DurationS > 0) {
			modes = append(modes, mode)
		}
	}
	if len(modes) == 0 {
		return
	}
	pdf := doc.pdf
	const baseHeight, perRow = 22.0, 3
	width := (contentWidth - journalGap*(perRow-1)) / perRow
	// textLeftOf is where a card's words start: after its icon, when its
	// means has one.
	textLeftOf := func(mode domain.TravelMode) float64 {
		if modeIcon(mode) != "" {
			return 18
		}
		return 4
	}
	// A row is as tall as its tallest label needs: a name of the means that
	// takes two lines - "общественным транспортом" - pushes the figures of the
	// whole row down, and the cards of a row stay alike.
	rowHeight := func(row []domain.TravelMode) (float64, float64) {
		extra := 0.0
		for _, mode := range row {
			size, lines := doc.fitLabel(text.modes[string(mode)], width-textLeftOf(mode)-3, 6.5)
			extra = max(extra, float64(len(lines)-1)*labelLineHeight(size))
		}
		return baseHeight + extra, extra
	}

	doc.keepTogether(8 + baseHeight)
	doc.label(text.byMode, marginLeft, pdf.GetY(), 7.5, mutedColor)
	top := pdf.GetY() + 6
	height := 0.0
	for start := 0; start < len(modes); start += perRow {
		row := modes[start:min(start+perRow, len(modes))]
		if start > 0 {
			top += height + journalGap
		}
		var extra float64
		height, extra = rowHeight(row)
		if top+height > pageHeight-marginBottom {
			doc.page()
			top = pdf.GetY()
		}
		for column, mode := range row {
			x := marginLeft + float64(column)*(width+journalGap)
			doc.box(x, top, width, height)
			textLeft := x + textLeftOf(mode)
			if icon := modeIcon(mode); icon != "" {
				doc.fill(softColor)
				pdf.RoundedRect(x+4, top+4, 10, 10, 2, "1234", "F")
				doc.fill(accentColor)
				drawPackingIcon(pdf, domain.PackingIcon(icon), x+6, top+6, 6)
			}
			total := totals.ByMode[mode]
			room := width - (textLeft - x) - 3
			doc.fittedLabel(text.modes[string(mode)], textLeft, top+4, room, 6.5, accentColor)
			pdf.SetFont(displayFamily, "", 13)
			pdf.SetXY(textLeft, top+8.5+extra)
			pdf.CellFormat(room, 6, text.distanceOf(total.DistanceM), "", 0, "L", false, 0, "")
			if total.DurationS > 0 {
				pdf.SetFont(fontFamily, "", 8.5)
				doc.ink(mutedColor)
				pdf.SetXY(textLeft, top+15+extra)
				pdf.CellFormat(room, 4, text.duration(total.DurationS), "", 0, "L", false, 0, "")
				doc.ink(inkColor)
			}
		}
	}
	pdf.SetXY(marginLeft, top+height+journalGap)
}

// writeDay writes one day as a chapter: its number and date, its title, the
// picture it opens with, its map beside its story and the moment it is
// remembered by, then the journeys booked for it, every place in a card of its
// own, where the night was spent and the pictures of the day itself.
func writeDay(doc *document, text labels, report Report, day domain.Day, index int) error {
	pdf := doc.pdf
	top := pdf.GetY()
	number := fmt.Sprintf(text.dayNumber, index+1)
	doc.label(number, marginLeft, top, 8, accentColor)
	if day.Date != nil {
		date := text.date(*day.Date)
		doc.label(date, pageWidth-marginRight-doc.labelWidth(date, 8), top, 8, mutedColor)
	}
	pdf.SetY(top + 7)
	if day.Title != "" {
		doc.display(day.Title, marginLeft, contentWidth, 24, inkColor)
	}
	pdf.Ln(3)

	hero, hasHero := report.DayHeroes[day.ID]
	if hasHero {
		name, info, err := doc.register(hero.JPEG)
		if err != nil {
			return err
		}
		doc.keepTogether(dayHeroHeight)
		y := pdf.GetY()
		doc.framedPicture(name, info, marginLeft, y, contentWidth, dayHeroHeight, 0.5, 0.5, journalPhotoRadius)
		pdf.SetXY(marginLeft, y+dayHeroHeight+journalGap)
	}

	items := domain.DayItems(report.Content.Items, &day.ID)
	opening := domain.OpeningLeg(report.Content.Legs, items)
	legs := domain.DayLegs(report.Content.Legs, items)
	_, summary := domain.ScheduleDay(day, items, opening, legs, report.Trip.Travelers, report.Content.Tracks)
	places := placesOf(report.Content.Items, day.ID)

	if err := writeDayStory(doc, text, report, day, summary, len(places), index); err != nil {
		return err
	}

	if day.Date != nil {
		for _, transfer := range domain.TransfersOnDate(report.Content.Transfers, *day.Date) {
			writeJournalTransfer(doc, text, report, transfer)
		}
	}

	// Each place is reached by the leg that ends at it; the first may be
	// reached from the day before, and the stay by the leg that ends the day.
	arriving := make(map[uuid.UUID]*domain.Leg)
	for _, leg := range append([]*domain.Leg{opening}, legs...) {
		if leg != nil {
			arriving[leg.ToItemID] = leg
		}
	}
	for number, place := range places {
		origin := ""
		if leg := arriving[place.ID]; leg != nil && leg == opening {
			origin = originOf(report, leg.FromItemID)
		}
		if err := writePlace(doc, text, report, place, number+1, arriving[place.ID], origin, hero.ID); err != nil {
			return err
		}
	}

	for _, stay := range staysOf(report, day) {
		var last *domain.Leg
		for _, item := range items {
			if item.Kind == domain.ItemStayAnchor && item.Anchor == domain.AnchorEvening {
				last = arriving[item.ID]
			}
		}
		writeStay(doc, text, report, stay, last)
	}

	var more []Photo
	for _, photo := range report.Photos[day.ID] {
		if photo.ID != hero.ID || !hasHero {
			more = append(more, photo)
		}
	}
	if len(more) > 0 {
		doc.keepTogether(8 + rowLimit)
		doc.label(text.moreFromDay, marginLeft, pdf.GetY(), 7.5, mutedColor)
		pdf.SetY(pdf.GetY() + 6)
		if err := doc.photoRows(more, marginLeft, contentWidth, rowLimit*1.2); err != nil {
			return err
		}
	}
	return nil
}

// writeDayStory writes the day's map in a card beside the card of its story,
// and under them the moment the day is remembered by. A story too long to sit
// beside the map goes under it across the page; a day without a map gives its
// story the whole width.
func writeDayStory(doc *document, text labels, report Report, day domain.Day, summary domain.DaySummary,
	places, index int) error {
	pdf := doc.pdf
	m, mapped := report.Maps[day.ID]
	column := (contentWidth - journalGap) / 2
	inner := column - 2*journalPadding
	facts := []string{}
	if places > 0 {
		facts = append(facts, text.count(places, text.placeWord))
	}
	if summary.DistanceM > 0 {
		facts = append(facts, text.distanceOf(summary.DistanceM))
	}

	var mapBottom float64
	top := pdf.GetY()
	story := day.NotesMD
	beside := mapped && markdownHeight(doc, story, inner) < mapHeightAt(m, inner)+40
	if mapped {
		height := journalPadding + 7 + mapHeightAt(m, inner) + 5 + journalPadding
		if len(facts) > 0 {
			height += 6
		}
		doc.keepTogether(height)
		top = pdf.GetY()
		doc.box(marginLeft, top, column, height)
		doc.label(text.route, marginLeft+journalPadding, top+journalPadding, 7.5, accentColor)
		bottom, err := doc.drawMapIn(dayLayer(report.Content, day, index, true), m, report.MapAttribution,
			marginLeft+journalPadding, top+journalPadding+7, inner, 2.5)
		if err != nil {
			return err
		}
		if len(facts) > 0 {
			doc.label(strings.Join(facts, dot), marginLeft+journalPadding, bottom+1, 7, inkColor)
		}
		mapBottom = top + height
		if !beside {
			pdf.SetXY(marginLeft, mapBottom+journalGap)
		}
	}

	storyLeft, storyWidth := marginLeft, contentWidth
	if mapped && beside {
		storyLeft, storyWidth = marginLeft+column+journalGap, column
		pdf.SetY(top)
	}
	if story != "" {
		doc.beginCard(storyLeft, storyWidth)
		doc.label(text.dayStory, storyLeft+journalPadding, pdf.GetY(), 7.5, accentColor)
		pdf.SetY(pdf.GetY() + 6)
		doc.inColumn(storyLeft+journalPadding, storyWidth-2*journalPadding, func() {
			writeMarkdown(doc, story)
		})
		doc.endCard()
	} else if !mapped && len(facts) > 0 {
		doc.label(strings.Join(facts, dot), marginLeft, pdf.GetY(), 7.5, mutedColor)
		pdf.SetY(pdf.GetY() + 6)
	}
	if day.Highlight != "" {
		doc.callout(text.highlight, day.Highlight, storyLeft, storyWidth)
	}
	if mapped && beside && pdf.GetY() < mapBottom+journalGap {
		pdf.SetXY(marginLeft, mapBottom+journalGap)
	}
	pdf.SetX(marginLeft)
	return nil
}

// markdownHeight estimates how tall a story is written across width, so a
// long one can be moved from beside the map to under it before it is written.
func markdownHeight(doc *document, source string, width float64) float64 {
	height := 0.0
	doc.pdf.SetFont(fontFamily, "", sizeBody)
	for _, item := range parseMarkdown(source) {
		height += float64(len(doc.pdf.SplitText(item.text, width-4))) * lineHeight
	}
	return height
}

// writePlace writes one place of a day in a card: its number and name with its
// rating, the tags of how it was reached and how it went, its pictures around
// one large one, and its story. The picture the day opens with is not shown
// again here.
func writePlace(doc *document, text labels, report Report, place domain.Item, number int,
	arriving *domain.Leg, origin string, dayHero uuid.UUID) error {
	pdf := doc.pdf
	var photos []Photo
	for _, photo := range report.Photos[place.ID] {
		if photo.ID != dayHero {
			photos = append(photos, photo)
		}
	}
	x, width := marginLeft+journalPadding, contentWidth-2*journalPadding
	numeral := fmt.Sprintf("%02d.", number)
	pdf.SetFont(displayFamily, "", 13)
	numeralWidth := pdf.GetStringWidth(numeral) + 2.5
	titleWidth := width - numeralWidth
	if place.Rating != nil {
		titleWidth -= starsWidth(3.6) + 3
	}
	title := strings.ToUpper(place.Name)
	tags := placePills(text, report, place, arriving, origin)

	// A place starts where its heading, its tags and the first of its pictures
	// fit together, so a heading never stands alone at the foot of a page with
	// its pictures overleaf.
	pdf.SetFont(displayFamily, "", 16)
	first := journalPadding + float64(len(pdf.SplitText(title, titleWidth)))*16*0.42 + 2.5 +
		doc.pillsHeight(tags, width) + firstPhotoHeight(photos, width) + 2
	if place.Address != "" {
		first += lineHeight
	}
	doc.keepTogether(first)

	doc.beginCard(marginLeft, contentWidth)
	top := pdf.GetY()
	pdf.SetFont(displayFamily, "", 13)
	doc.ink(accentColor)
	pdf.SetXY(x, top+0.8)
	pdf.CellFormat(numeralWidth, 6, numeral, "", 0, "L", false, 0, "")
	if place.Rating != nil {
		doc.stars(*place.Rating, x+width-starsWidth(3.6), top+1.2, 3.6)
	}
	pdf.SetY(top)
	doc.display(title, x+numeralWidth, titleWidth, 16, inkColor)
	pdf.Ln(2.5)

	doc.pills(tags, x, width)
	if place.Address != "" {
		pdf.SetFont(fontFamily, "", sizeSmall)
		doc.ink(mutedColor)
		pdf.SetX(x)
		pdf.MultiCell(width, lineHeight*0.85, place.Address, "", "L", false)
		doc.ink(inkColor)
		pdf.Ln(1.5)
	}

	if err := doc.photoBlock(photos, x, width); err != nil {
		return err
	}

	story := place.StoryMD
	if story == "" {
		story = place.DescriptionMD
	}
	if story != "" {
		doc.inColumn(x, width, func() { writeMarkdown(doc, story) })
	}
	if arriving != nil && arriving.Note != "" {
		pdf.SetFont(fontFamily, "I", sizeSmall)
		doc.ink(mutedColor)
		pdf.SetX(x)
		pdf.MultiCell(width, lineHeight*0.85, arrowMark+"  "+arriving.Note, "", "L", false)
		doc.ink(inkColor)
	}
	doc.endCard()
	return nil
}

// placePills are the tags under a place's name: how it was reached, how it
// went when that is not simply visited, what kind of activity it was, when,
// how hard, what it cost and what its recording measured.
func placePills(text labels, report Report, place domain.Item, arriving *domain.Leg, origin string) []pill {
	var tags []pill
	if arriving != nil {
		if tag, ok := legPill(text, report, *arriving, origin); ok {
			tags = append(tags, tag)
		}
	}
	if place.Status == domain.StatusSkipped || place.Status == domain.StatusUnplanned {
		tags = append(tags, pill{text: text.statuses[string(place.Status)], muted: true})
	}
	if place.Kind == domain.ItemActivity {
		tags = append(tags, pill{text: text.activities[string(place.ActivityType)]})
	}
	if place.ActualTime != nil {
		tags = append(tags, pill{text: text.clockPeriod(*place.ActualTime, place.ActualEndTime)})
	}
	if place.Difficulty != nil && *place.Difficulty >= domain.MinDifficulty && *place.Difficulty <= domain.MaxDifficulty {
		tags = append(tags, pill{text: fmt.Sprintf(text.difficulty, text.difficulties[*place.Difficulty-1])})
	}
	if cost := placeCost(place, report.Trip); cost != "" {
		tags = append(tags, pill{text: cost})
	}
	if track := domain.TrackOfItem(report.Content.Tracks, place.ID); track != nil {
		tags = append(tags, pill{text: trackNote(text, track), icon: "hiking"})
	}
	return tags
}

// legPill is the tag of the journey that reached a place: its means, how far
// and how long, where it started when that was the day before. A journey with
// nothing to say about it has no tag.
func legPill(text labels, report Report, leg domain.Leg, origin string) (pill, bool) {
	parts := []string{text.modes[string(leg.Mode)]}
	if leg.Composite() {
		parts = []string{legChain(text, leg)}
	}
	if origin != "" {
		parts = append(parts, fmt.Sprintf(text.legFrom, origin))
	}
	if distance := leg.Distance(); distance != nil && *distance > 0 {
		parts = append(parts, text.distanceOf(*distance))
	}
	if duration := leg.Duration(); duration != nil && *duration > 0 {
		parts = append(parts, text.duration(*duration))
	}
	if amount := amountOf(leg.ActualCost, leg.PlannedCost); amount != nil {
		parts = append(parts, money(*amount, report.Trip.Currency))
	}
	if len(parts) == 1 && origin == "" && !leg.Composite() {
		// "By car" alone says nothing a reader wants.
		return pill{}, false
	}
	return pill{text: strings.Join(parts, dot), icon: modeIcon(leg.Mode)}, true
}

// photoBlock lays out the pictures of a place across width at x: one alone as
// large as it reads well, two side by side, and more as one large picture - the
// first that is wider than tall - over rows of the rest. Rows keep every
// picture whole: a row is as tall as its pictures let it be while filling the
// width, so a portrait is narrow rather than cut.
func (d *document) photoBlock(photos []Photo, x, width float64) error {
	switch len(photos) {
	case 0:
		return nil
	case 1:
		name, info, err := d.register(photos[0].JPEG)
		if err != nil {
			return err
		}
		ratio := info.Width() / info.Height()
		h := min(width/ratio, singleLimit)
		w := min(width, h*ratio)
		d.keepTogether(h)
		y := d.pdf.GetY()
		d.framed(name, info, x, y, w, h, 0.5, 0.5, journalPhotoRadius)
		d.pdf.SetXY(x, y+h+photoGap)
		return nil
	case 2:
		return d.photoRows(photos, x, width, singleLimit*0.75)
	}

	hero := -1
	for index, photo := range photos {
		if landscape(photo.JPEG) {
			hero = index
			break
		}
	}
	if hero < 0 {
		return d.photoRows(photos, x, width, rowLimit*1.2)
	}
	name, info, err := d.register(photos[hero].JPEG)
	if err != nil {
		return err
	}
	h := min(width*info.Height()/info.Width(), placeHeroLimit)
	d.keepTogether(h)
	y := d.pdf.GetY()
	d.framed(name, info, x, y, width, h, 0.5, 0.5, journalPhotoRadius)
	d.pdf.SetXY(x, y+h+photoGap)
	rest := append(append([]Photo{}, photos[:hero]...), photos[hero+1:]...)
	return d.photoRows(rest, x, width, rowLimit)
}

// firstPhotoHeight is how tall the first picture - or the first row - of a
// place is drawn by photoBlock across width, which is what a place's heading
// has to be kept with; zero for a place without pictures.
func firstPhotoHeight(photos []Photo, width float64) float64 {
	ratios := make([]float64, 0, len(photos))
	for _, photo := range photos {
		ratios = append(ratios, aspect(photo.JPEG))
	}
	rowOf := func(ratios []float64, limit float64) float64 {
		sum := 0.0
		for _, ratio := range ratios[:min(3, len(ratios))] {
			sum += ratio
		}
		return min((width-photoGap*float64(min(3, len(ratios))-1))/sum, limit)
	}
	switch len(ratios) {
	case 0:
		return 0
	case 1:
		return min(width/ratios[0], singleLimit) + photoGap
	case 2:
		return rowOf(ratios, singleLimit*0.75) + photoGap
	}
	for _, ratio := range ratios {
		if ratio > 1 {
			return min(width/ratio, placeHeroLimit) + photoGap
		}
	}
	return rowOf(ratios, rowLimit*1.2) + photoGap
}

// aspect is a picture's width over its height, from the size its JPEG
// declares; one that cannot be read counts as a landscape of 3:2.
func aspect(jpeg []byte) float64 {
	config, _, err := image.DecodeConfig(bytes.NewReader(jpeg))
	if err != nil || config.Height == 0 {
		return 1.5
	}
	return float64(config.Width) / float64(config.Height)
}

// photoRows lays pictures in rows of up to three across width at x, each row
// filling the width with its pictures whole and side by side, no taller than
// limit. The last row, when shorter, keeps the height of the rows above it.
func (d *document) photoRows(photos []Photo, x, width, limit float64) error {
	const perRow = 3
	full := limit
	for start := 0; start < len(photos); start += perRow {
		row := photos[start:min(start+perRow, len(photos))]
		names := make([]string, len(row))
		infos := make([]*fpdf.ImageInfoType, len(row))
		sum := 0.0
		for index, photo := range row {
			name, info, err := d.register(photo.JPEG)
			if err != nil {
				return err
			}
			names[index], infos[index] = name, info
			sum += info.Width() / info.Height()
		}
		height := min((width-photoGap*float64(len(row)-1))/sum, limit)
		if len(row) < perRow {
			height = min(height, full)
		} else {
			full = height
		}
		d.keepTogether(height)
		y := d.pdf.GetY()
		left := x
		for index := range row {
			w := height * infos[index].Width() / infos[index].Height()
			d.framed(names[index], infos[index], left, y, w, height, 0.5, 0.5, journalPhotoRadius)
			left += w + photoGap
		}
		d.pdf.SetXY(x, y+height+photoGap)
	}
	return nil
}

// landscape reports whether a picture is wider than tall, from the size its
// JPEG declares.
func landscape(jpeg []byte) bool {
	return aspect(jpeg) > 1
}

// writeJournalTransfer writes a booked journey - a flight, a train, a ferry - in a
// card: what it was, from where to where, when it left and landed, what it
// cost, and its notes.
func writeJournalTransfer(doc *document, text labels, report Report, transfer domain.Transfer) {
	pdf := doc.pdf
	doc.keepTogether(26)
	x, width := marginLeft+journalPadding, contentWidth-2*journalPadding
	doc.beginCard(marginLeft, contentWidth)
	title := text.transferKinds[string(transfer.Kind)]
	if transfer.Name != "" {
		title += dot + transfer.Name
	}
	left := x
	if icon := transferIcon(transfer.Kind); icon != "" {
		doc.fill(accentColor)
		drawPackingIcon(pdf, domain.PackingIcon(icon), x, pdf.GetY()-0.3, 3.8)
		left += 5.5
	}
	doc.label(title, left, pdf.GetY(), 7.5, accentColor)
	pdf.SetY(pdf.GetY() + 5.5)
	pdf.SetFont(fontFamily, "B", 12)
	pdf.SetX(x)
	pdf.MultiCell(width, 6, transfer.FromName+"  "+arrowMark+"  "+transfer.ToName, "", "L", false)
	pdf.Ln(1.5)

	var tags []pill
	// end reads one end of the journey: its time, and its date when it is
	// another day than the one the journey left on.
	end := func(format string, date time.Time, clock *domain.ClockTime, sameDay bool) {
		when := ""
		if !sameDay {
			when = text.date(date)
		}
		if clock != nil {
			when = strings.TrimSpace(when + " " + text.clock(int(*clock)))
		}
		if when != "" {
			tags = append(tags, pill{text: fmt.Sprintf(format, when)})
		}
	}
	sameDay := transfer.Arrival().Equal(transfer.DepartureDate)
	end(text.departs, transfer.DepartureDate, transfer.DepartureTime, sameDay)
	end(text.arrives, transfer.Arrival(), transfer.ArrivalTime, sameDay)
	travelers := report.Trip.Travelers
	if transfer.ActualCost != nil {
		tags = append(tags, pill{text: money(transfer.ActualCostTotal(travelers), report.Trip.Currency)})
	} else if transfer.PlannedCost != nil {
		tags = append(tags, pill{text: money(transfer.PlannedCostTotal(travelers), report.Trip.Currency)})
	}
	doc.pills(tags, x, width)
	if transfer.NotesMD != "" {
		doc.inColumn(x, width, func() { writeMarkdown(doc, transfer.NotesMD) })
	}
	doc.endCard()
}

// writeStay writes where the night was spent as the end of the day's chapter:
// the stay's name, the journey that reached it, its kind, dates and cost, and
// its notes.
func writeStay(doc *document, text labels, report Report, stay domain.Stay, arriving *domain.Leg) {
	pdf := doc.pdf
	doc.keepTogether(28)
	x, width := marginLeft+journalPadding, contentWidth-2*journalPadding
	doc.beginCard(marginLeft, contentWidth)
	doc.label(text.endOfDay, x, pdf.GetY(), 7.5, accentColor)
	pdf.SetY(pdf.GetY() + 5.5)
	doc.display(stay.Name, x, width, 14, inkColor)
	pdf.Ln(2)

	var tags []pill
	if arriving != nil {
		if tag, ok := legPill(text, report, *arriving, ""); ok {
			tags = append(tags, tag)
		}
	}
	if kind, ok := text.stayKinds[string(stay.Kind)]; ok {
		tags = append(tags, pill{text: kind})
	}
	tags = append(tags, pill{text: fmt.Sprintf("%s %s %s %s",
		text.checkIn, text.date(stay.CheckInDate), text.checkOut, text.date(stay.CheckOutDate))})
	if amount := amountOf(stay.ActualCost, stay.PlannedCost); amount != nil {
		tags = append(tags, pill{text: money(*amount, report.Trip.Currency)})
	}
	doc.pills(tags, x, width)
	if stay.Address != "" {
		pdf.SetFont(fontFamily, "", sizeSmall)
		doc.ink(mutedColor)
		pdf.SetX(x)
		pdf.MultiCell(width, lineHeight*0.85, stay.Address, "", "L", false)
		doc.ink(inkColor)
		pdf.Ln(1.5)
	}
	if stay.NotesMD != "" {
		doc.inColumn(x, width, func() { writeMarkdown(doc, stay.NotesMD) })
	}
	doc.endCard()
}

// writeClosing writes the pages the journal ends with: the words the report
// closes on, the costs that hang on no place, and what was spent against what
// was planned. A report with none of them ends with its last day.
func writeClosing(doc *document, text labels, report Report) {
	totals := report.Totals
	summary := report.Content.Document.SummaryMD
	hasCosts := totals.PlannedCost != 0 || totals.ActualCost != 0
	if summary == "" && len(report.Content.Expenses) == 0 && !hasCosts {
		return
	}
	pdf := doc.pdf
	doc.page()
	doc.footer = report.Trip.Title + " / " + text.closingPage
	doc.label(text.journal, marginLeft, pdf.GetY(), 8, accentColor)
	pdf.SetY(pdf.GetY() + 7)
	doc.display(text.closingPage, marginLeft, contentWidth, 24, inkColor)
	pdf.Ln(4)
	if summary != "" {
		writeMarkdown(doc, summary)
		pdf.Ln(journalGap)
	}

	if hasCosts {
		writeStats(doc, []stat{
			{money(totals.PlannedCost, report.Trip.Currency), text.plannedCost},
			{money(totals.ActualCost, report.Trip.Currency), text.actualCost},
			{signed(totals.Difference) + " " + report.Trip.Currency, text.difference},
		})
	}

	var lines [][2]string
	for _, expense := range report.Content.Expenses {
		amount := amountOf(expense.Actual, expense.Planned)
		if amount == nil {
			continue
		}
		name := expense.Note
		if name == "" {
			name = text.categories[string(expense.Category)]
		}
		lines = append(lines, [2]string{name, money(*amount, report.Trip.Currency)})
	}
	if len(lines) == 0 {
		return
	}
	x, width := marginLeft+journalPadding, contentWidth-2*journalPadding
	doc.keepTogether(14 + lineHeight*float64(min(len(lines), 4)))
	doc.beginCard(marginLeft, contentWidth)
	doc.label(text.expenses, x, pdf.GetY(), 7.5, accentColor)
	pdf.SetY(pdf.GetY() + 6)
	for _, line := range lines {
		pdf.SetFont(fontFamily, "", sizeBody)
		pdf.SetX(x)
		pdf.CellFormat(width*0.7, lineHeight, line[0], "", 0, "L", false, 0, "")
		pdf.SetFont(fontFamily, "B", sizeBody)
		pdf.CellFormat(width*0.3, lineHeight, line[1], "", 1, "R", false, 0, "")
	}
	doc.endCard()
}

// trackNote says how far a recording went, how long it took when its points
// carry time, and, when the file had heights, how much it climbed and descended.
func trackNote(text labels, track *domain.Track) string {
	note := fmt.Sprintf(text.track, text.trackDistanceOf(track.DistanceM))
	if elapsed := track.Elapsed(); elapsed != nil {
		note += separator + fmt.Sprintf(text.trackTime, elapsedClock(*elapsed))
	}
	if track.AscentM != nil && track.DescentM != nil {
		note += separator + fmt.Sprintf(text.climb, text.heightOf(*track.AscentM), text.heightOf(*track.DescentM))
	}
	return note
}

// elapsedClock writes a length of time as a watch does: hours, minutes and
// seconds, "4:57:51".
func elapsedClock(elapsed time.Duration) string {
	seconds := int(elapsed.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
}

// legChain writes the parts of a journey with changes as one line: each way
// of travelling with its time, and the changes between them.
func legChain(text labels, leg domain.Leg) string {
	var chain strings.Builder
	for index, segment := range leg.Segments {
		chain.WriteString(text.modes[string(segment.Mode)])
		if duration := segment.Duration(); duration != nil {
			chain.WriteString(" " + text.duration(*duration))
		}
		if index < len(leg.Segments)-1 {
			chain.WriteString(" " + arrowMark + " " + segment.StopName + " " + arrowMark + " ")
		}
	}
	return chain.String()
}

// originOf names the element a leg starts at: a place by its name, a stay mark
// by its stay's.
func originOf(report Report, itemID uuid.UUID) string {
	for _, item := range report.Content.Items {
		if item.ID != itemID {
			continue
		}
		if item.Kind == domain.ItemStayAnchor && item.StayID != nil {
			for _, stay := range report.Content.Stays {
				if stay.ID == *item.StayID {
					return stay.Name
				}
			}
		}
		return item.Name
	}
	return ""
}

// travelModeOrder is the order the means of travel are listed in, so two reports
// of the same trip do not shuffle their rows between renders.
var travelModeOrder = []domain.TravelMode{
	domain.ModeWalk, domain.ModeCar, domain.ModeBike, domain.ModeTransit, domain.ModeBus, domain.ModeTrain,
	domain.ModeTram, domain.ModeFerry, domain.ModeFlight, domain.ModeCableCar, domain.ModeOther,
}

// placesOf returns the places of one day, in the order they were visited.
//
// Stay marks are left out: the stay itself is written under the day, and a mark
// saying "the hotel" between two places would say nothing the stay does not.
func placesOf(items []domain.Item, dayID uuid.UUID) []domain.Item {
	var places []domain.Item
	for _, item := range items {
		if item.Kind.IsVisit() && item.DayID != nil && *item.DayID == dayID {
			places = append(places, item)
		}
	}
	sort.SliceStable(places, func(a, b int) bool { return places[a].Position < places[b].Position })
	return places
}

// staysOf returns the stays covering one day, which is how a reader learns where
// the night was spent without a mark in the middle of the schedule.
func staysOf(report Report, day domain.Day) []domain.Stay {
	if day.Date == nil {
		return nil
	}
	date := *day.Date

	var covering []domain.Stay
	for _, stay := range report.Content.Stays {
		// The night of the check-out day belongs to the next stay, so the last
		// day of one is not listed under it.
		if !date.Before(stay.CheckInDate) && date.Before(stay.CheckOutDate) {
			covering = append(covering, stay)
		}
	}
	return covering
}

// amountOf prefers what was really spent over what was planned, and reports
// nothing when neither is set.
func amountOf(actual, planned *domain.Money) *domain.Money {
	if actual != nil {
		return actual
	}
	return planned
}

// placeCost writes what a place cost, marking a planned amount as such when
// nothing was recorded against it.
func placeCost(place domain.Item, trip domain.Trip) string {
	amount := amountOf(place.ActualCost, place.PlannedCost)
	if amount == nil {
		return ""
	}
	total := *amount
	if place.CostPerPerson {
		total = domain.Money(int64(total) * int64(max(trip.Travelers, 1)))
	}
	return money(total, trip.Currency)
}

// money writes an amount with its currency, or without when the column already
// names one.
func money(amount domain.Money, currency string) string {
	if currency == "" {
		return amount.String()
	}
	return amount.String() + " " + currency
}

// signed writes a difference with its sign, so a report that came in under what
// was planned says so at a glance.
func signed(amount domain.Money) string {
	if amount > 0 {
		return "+" + amount.String()
	}
	return amount.String()
}

// The plan's document writes its journeys and bookings as plain lines rather
// than in the journal's cards.

// writeLeg writes the journey from one place to the next, naming where it
// started when origin is not empty. A journey with changes names each part and
// each change in turn before its totals.
func writeLeg(doc *document, text labels, report Report, leg domain.Leg, origin string) {
	parts := []string{text.modes[string(leg.Mode)]}
	if leg.Composite() {
		parts = []string{legChain(text, leg)}
	}
	if distance := leg.Distance(); distance != nil {
		parts = append(parts, text.distanceOf(*distance))
	}
	if duration := leg.Duration(); duration != nil {
		parts = append(parts, text.duration(*duration))
	}
	if amount := amountOf(leg.ActualCost, leg.PlannedCost); amount != nil {
		parts = append(parts, money(*amount, report.Trip.Currency))
	}
	if leg.Note != "" {
		parts = append(parts, leg.Note)
	}
	if origin != "" {
		parts = append(parts, fmt.Sprintf(text.legFrom, origin))
	}
	doc.note(arrowMark + "  " + strings.Join(parts, ", "))
}

// writeTransfer writes a booked journey: how and where it went, when it left
// and arrived, and what it cost.
func writeTransfer(doc *document, text labels, report Report, transfer domain.Transfer) {
	doc.space(1)
	doc.subheading(text.transferKinds[string(transfer.Kind)] + ": " +
		transfer.FromName + " " + arrowMark + " " + transfer.ToName)

	parts := []string{}
	if transfer.Name != "" {
		parts = append(parts, transfer.Name)
	}
	// endOf reads one end of the journey: its time, and its date when it is
	// another day than the one being written.
	endOf := func(format string, date time.Time, clock *domain.ClockTime, sameDay bool) {
		when := ""
		if !sameDay {
			when = text.date(date)
		}
		if clock != nil {
			when = strings.TrimSpace(when + " " + text.clock(int(*clock)))
		}
		if when != "" {
			parts = append(parts, fmt.Sprintf(format, when))
		}
	}
	sameDay := transfer.Arrival().Equal(transfer.DepartureDate)
	endOf(text.departs, transfer.DepartureDate, transfer.DepartureTime, sameDay)
	endOf(text.arrives, transfer.Arrival(), transfer.ArrivalTime, sameDay)
	travelers := report.Trip.Travelers
	if transfer.ActualCost != nil {
		parts = append(parts, money(transfer.ActualCostTotal(travelers), report.Trip.Currency))
	} else if transfer.PlannedCost != nil {
		parts = append(parts, money(transfer.PlannedCostTotal(travelers), report.Trip.Currency))
	}
	if len(parts) > 0 {
		doc.note(strings.Join(parts, separator))
	}
	if transfer.NotesMD != "" {
		writeMarkdown(doc, transfer.NotesMD)
	}
}
