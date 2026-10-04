package pdf

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/boombuler/barcode/qr"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A plan's document is read on the way rather than afterwards: in a phone at a
// bus stop, or printed and folded in a pocket. It is a reference, so it is laid
// out to be looked things up in - a page a day, the stays and the journeys up
// front - and every place can be found from it: its address opens it on a map
// on a screen, and a QR code beside it does the same from paper. A place's own
// website is given as a QR code too, never as its address, which would take
// lines to print and minutes to type; the two codes stand at opposite corners
// of the place's block, each named, so a phone aimed at one does not read the
// other.

// qrSize is the side of a place's QR code: large enough for a phone to read
// from a printed page at arm's length, small enough to sit beside the text.
const qrSize = 22.0

// qrGap is the room between the text of a place and its QR code.
const qrGap = 4.0

// qrCaption is the height of the line naming a QR code.
const qrCaption = 4.0

// Plan is everything a plan's document is built from.
type Plan struct {
	Trip    domain.Trip
	Content domain.DocumentContent
	// Language is the reader's language; anything but "ru" is written in English.
	Language string
	// Units is what the reader counts distances in; kilometres when unset.
	Units domain.Units
	// Maps are the maps MapRequests asked for, as a report's are.
	Maps map[uuid.UUID]Map
	// MapAttribution is the credit the tile provider asks for, in plain text.
	MapAttribution string
}

// RenderPlan - writes a plan as a PDF to take on the way.
//
// Arguments:
//   - w: where the document goes.
//   - plan: the trip and its plan.
//
// Returns:
//   - an error if the document could not be written.
func RenderPlan(w io.Writer, plan Plan) error {
	text := wording(plan.Language, plan.Units)
	doc := newDocument(plan.Trip.Title, plan.Trip.Title)
	// The pieces written for a report take the trip and its content from one.
	report := Report{Trip: plan.Trip, Content: plan.Content, Language: plan.Language, Units: plan.Units}

	links := make([]int, len(plan.Content.Days))
	for index := range links {
		links[index] = doc.pdf.AddLink()
	}
	writePlanCover(doc, text, plan, links)

	doc.page()
	writePlanOverview(doc, text, plan, report)
	if m, ok := plan.Maps[uuid.Nil]; ok {
		doc.keepTogether(sizeHeading*0.5 + headingSpace + mapHeight(m) + lineHeight)
		doc.heading(text.tripMap)
		if err := doc.drawMap(tripLayer(plan.Content), m, plan.MapAttribution); err != nil {
			return err
		}
	}

	for index, day := range plan.Content.Days {
		doc.page()
		doc.pdf.SetLink(links[index], 0, -1)
		if err := writePlanDay(doc, text, plan, report, day, index); err != nil {
			return err
		}
	}

	writeIdeas(doc, text, plan)
	return doc.render(w)
}

// writePlanCover writes the title page: the trip, its dates and who goes, and
// the days, each a link to its page.
func writePlanCover(doc *document, text labels, plan Plan, links []int) {
	doc.page()
	doc.title(plan.Trip.Title)
	if span := text.dateRange(plan.Trip.StartDate, plan.Trip.EndDate); span != "" {
		doc.note(span)
	}
	doc.note(strings.Join([]string{
		fmt.Sprintf("%s: %d", text.travelers, max(plan.Trip.Travelers, 1)),
		fmt.Sprintf("%s: %s", text.currency, plan.Trip.Currency),
	}, separator))
	if plan.Trip.Summary != "" {
		doc.space(4)
		doc.body(plan.Trip.Summary, 0, "I")
	}

	doc.heading(text.contents)
	doc.pdf.SetFont(fontFamily, "", sizeBody)
	for index, day := range plan.Content.Days {
		doc.pdf.CellFormat(contentWidth, lineHeight*1.2, dayTitle(text, day, index), "", 1, "L", false,
			links[index], "")
	}
	doc.space(4)
	doc.note(text.qrHint)
}

// dayTitle names a day as its page heads it: its number, date and title.
func dayTitle(text labels, day domain.Day, index int) string {
	title := fmt.Sprintf(text.day, index+1)
	if day.Date != nil {
		title += " · " + text.date(*day.Date)
	}
	if day.Title != "" {
		title += " · " + day.Title
	}
	return title
}

// writePlanOverview writes what is looked up most on the way: where each night
// is spent, and the flights and trains booked between places.
func writePlanOverview(doc *document, text labels, plan Plan, report Report) {
	doc.heading(text.overview)
	if len(plan.Content.Stays) > 0 {
		doc.subheading(text.stays)
		doc.space(1)
		for _, stay := range plan.Content.Stays {
			writePlanStay(doc, text, plan, stay)
		}
	}
	if len(plan.Content.Transfers) > 0 {
		doc.space(2)
		doc.subheading(text.transfers)
		for _, transfer := range plan.Content.Transfers {
			writeTransfer(doc, text, report, transfer)
			if transfer.BookingRef != "" {
				doc.note(fmt.Sprintf(text.booking, transfer.BookingRef))
			}
		}
	}
}

// writePlanStay writes one stay: its dates and times, how to find it and what
// to show at the desk.
func writePlanStay(doc *document, text labels, plan Plan, stay domain.Stay) {
	facts := []string{}
	if kind, ok := text.stayKinds[string(stay.Kind)]; ok {
		facts = append(facts, kind)
	}
	facts = append(facts, stayPeriod(text, stay))
	if nights := stay.Nights(); nights > 0 {
		facts = append(facts, fmt.Sprintf("%s: %d", text.nights, nights))
	}
	if stay.PlannedCost != nil {
		facts = append(facts, money(*stay.PlannedCost, plan.Trip.Currency))
	}
	lines := []infoLine{{text: strings.Join(facts, separator), grey: true}}
	lines = append(lines, whereLines(stay.Address, stay.Lat, stay.Lng)...)
	if stay.BookingRef != "" {
		lines = append(lines, infoLine{text: fmt.Sprintf(text.booking, stay.BookingRef)})
	}
	if stay.Contacts != "" {
		lines = append(lines, infoLine{text: stay.Contacts})
	}
	writeInfoBlock(doc, text, stay.Name, lines, mapLink(stay.Lat, stay.Lng), stay.URL)
	if stay.NotesMD != "" {
		writeMarkdown(doc, stay.NotesMD)
	}
	doc.space(2)
}

// stayPeriod reads when a stay begins and ends, with the times of the desk.
func stayPeriod(text labels, stay domain.Stay) string {
	end := func(date string, clock *domain.ClockTime) string {
		if clock == nil {
			return date
		}
		return date + " " + text.clock(int(*clock))
	}
	return end(text.date(stay.CheckInDate), stay.CheckInTime) + " " + arrowMark + " " +
		end(text.date(stay.CheckOutDate), stay.CheckOutTime)
}

// writePlanDay writes one day on a page of its own: when it starts and ends,
// its map, and every element in the order it is reached, with the journeys
// between them.
func writePlanDay(doc *document, text labels, plan Plan, report Report, day domain.Day, index int) error {
	items := domain.DayItems(plan.Content.Items, &day.ID)
	opening := domain.OpeningLeg(plan.Content.Legs, items)
	legs := domain.DayLegs(plan.Content.Legs, items)
	schedules, summary := domain.ScheduleDay(day, items, opening, legs, plan.Trip.Travelers, plan.Content.Tracks)

	doc.heading(dayTitle(text, day, index))
	facts := []string{fmt.Sprintf(text.dayStarts, text.clock(int(day.StartTime)))}
	if len(items) > 0 {
		facts = append(facts, fmt.Sprintf(text.dayEnds, text.clock(summary.EndMinutes)))
	}
	if summary.TravelMinutes > 0 {
		facts = append(facts, fmt.Sprintf(text.onTheRoad, text.duration(summary.TravelMinutes*60)))
	}
	if summary.DistanceM > 0 {
		facts = append(facts, text.distanceOf(summary.DistanceM))
	}
	doc.note(strings.Join(facts, separator))

	if m, ok := plan.Maps[day.ID]; ok {
		doc.space(1)
		if err := doc.drawMap(dayLayer(plan.Content, day, index, true), m, plan.MapAttribution); err != nil {
			return err
		}
	}
	if day.NotesMD != "" {
		writeMarkdown(doc, day.NotesMD)
	}
	if day.Date != nil {
		for _, transfer := range domain.TransfersOnDate(plan.Content.Transfers, *day.Date) {
			writeTransfer(doc, text, report, transfer)
		}
	}

	doc.space(2)
	if opening != nil {
		writeLeg(doc, text, report, *opening, originOf(report, opening.FromItemID))
		doc.space(1)
	}
	stays := make(map[uuid.UUID]domain.Stay, len(plan.Content.Stays))
	for _, stay := range plan.Content.Stays {
		stays[stay.ID] = stay
	}
	number := 0
	for position, item := range items {
		if item.Kind.IsVisit() {
			// Numbered as the pins on the map of the day are.
			number++
			writePlanPlace(doc, text, plan, item, strconv.Itoa(number), &schedules[position])
		} else if stay, ok := stays[derefID(item.StayID)]; ok {
			writeAnchor(doc, text, item, stay, schedules[position])
		}
		if position < len(legs) && legs[position] != nil {
			writeLeg(doc, text, report, *legs[position], "")
			doc.space(1)
		}
	}
	return nil
}

// derefID reads an optional identifier, the nil identifier when it is absent.
func derefID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// writeAnchor writes a stay mark: where the day starts in the morning or where
// its night is spent, with the way there.
func writeAnchor(doc *document, text labels, item domain.Item, stay domain.Stay, schedule domain.ItemSchedule) {
	title := text.clock(schedule.ArrivalMinutes) + "  " + stay.Name
	if item.Anchor == domain.AnchorEvening {
		title = fmt.Sprintf(text.night, stay.Name)
	}
	writeInfoBlock(doc, text, title, whereLines(stay.Address, stay.Lat, stay.Lng), "", "")
	doc.space(1)
}

// writePlanPlace writes one place or activity: when it is reached, what it is,
// how long it takes, where it is and what it costs, with a QR code of it on a
// map beside it and one of its website below, and the stops along its line.
// number is its number on the day's map, or empty.
func writePlanPlace(doc *document, text labels, plan Plan, place domain.Item, number string,
	schedule *domain.ItemSchedule) {
	title := place.Name
	if schedule != nil {
		title = text.clock(schedule.ArrivalMinutes) + "  " + title
	}
	if number != "" {
		title = number + ".  " + title
	}

	facts := []string{}
	if place.Kind == domain.ItemActivity {
		facts = append(facts, text.activities[string(place.ActivityType)])
	} else if category, ok := text.placeCategories[string(place.Category)]; ok {
		facts = append(facts, category)
	}
	if place.VisitMinutes > 0 {
		facts = append(facts, fmt.Sprintf(text.onSite, text.duration(place.VisitMinutes*60)))
	}
	if place.DesiredTime != nil {
		facts = append(facts, fmt.Sprintf(text.wantedAt, text.clock(int(*place.DesiredTime))))
	}
	if schedule != nil && schedule.Late {
		facts = append(facts, text.late)
	}
	if place.IsOptional {
		facts = append(facts, text.optional)
	}
	if cost := placeCost(place, plan.Trip); cost != "" {
		facts = append(facts, cost)
	}

	lines := []infoLine{}
	if len(facts) > 0 {
		lines = append(lines, infoLine{text: strings.Join(facts, separator), grey: true})
	}
	lines = append(lines, whereLines(place.Address, place.Lat, place.Lng)...)
	if place.BookingRef != "" {
		lines = append(lines, infoLine{text: fmt.Sprintf(text.booking, place.BookingRef)})
	}
	writeInfoBlock(doc, text, title, lines, mapLink(place.Lat, place.Lng), place.URL)
	if place.DescriptionMD != "" {
		writeMarkdown(doc, place.DescriptionMD)
	}
	// The stops along an activity's line carry its number on the day's map.
	if numbered, err := strconv.Atoi(number); err == nil {
		writeStops(doc, text, plan.Trip, place, plan.Content.Tracks, numbered, false, marginLeft, contentWidth)
	}
	doc.space(2)
}

// writeIdeas lists the places of the plan that have no day yet: somewhere to
// go if the day allows it.
func writeIdeas(doc *document, text labels, plan Plan) {
	ideas := domain.DayItems(plan.Content.Items, nil)
	if len(ideas) == 0 {
		return
	}
	doc.page()
	doc.heading(text.ideas)
	for _, idea := range ideas {
		if idea.Kind.IsVisit() {
			writePlanPlace(doc, text, plan, idea, "", nil)
		}
	}
}

// infoLine is one line of an element's block: plain, grey for its facts, or a
// link.
type infoLine struct {
	text string
	link string
	grey bool
}

// whereLines are the lines that say where something is: its address, or its
// coordinates when it has none, either opening it on a map.
func whereLines(address string, lat, lng *float64) []infoLine {
	target := mapLink(lat, lng)
	switch {
	case address != "":
		return []infoLine{{text: address, link: target}}
	case target != "":
		return []infoLine{{text: fmt.Sprintf("%.5f, %.5f", *lat, *lng), link: target}}
	}
	return nil
}

// mapLink is the address that shows a position on a map, on a phone or a
// computer, or empty for something without one. It opens the place rather than
// the way to it: the reader sees where it is, and one tap there asks for the
// way. A Google Maps address opens the map application on either kind of
// phone, and a browser everywhere else.
func mapLink(lat, lng *float64) string {
	if lat == nil || lng == nil {
		return ""
	}
	return "https://www.google.com/maps/search/?api=1&query=" +
		url.QueryEscape(fmt.Sprintf("%.6f,%.6f", *lat, *lng))
}

// siteName is the part of a website's address that says whose it is, such as
// "parquesdesintra.pt", or empty when the address says nothing readable.
func siteName(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Hostname(), "www.")
}

// writeInfoBlock - writes an element's title and lines, the title in bold,
// with the element's QR codes: the one of its position at the top right, named
// under it, and the one of its website at the bottom left, below the text and
// named beside it. Standing at opposite corners, a phone held close enough to
// read one does not see the other. The block is kept on one page, and whatever
// follows it starts below the text and both codes.
//
// Arguments:
//   - doc: the document being written.
//   - text: the labels of its language.
//   - title: the element's title.
//   - lines: the lines under the title.
//   - place: the address of the element on a map, or empty.
//   - site: the element's website, or empty.
func writeInfoBlock(doc *document, text labels, title string, lines []infoLine, place, site string) {
	width := contentWidth
	if place != "" {
		width -= qrSize + qrGap
	}
	textHeight := sizeSubhead*0.5 + lineHeight*float64(len(lines))
	height := textHeight
	if place != "" {
		height = max(height, qrSize+qrCaption)
	}
	if site != "" {
		height = max(height, textHeight+qrGap+qrSize)
	}
	doc.keepTogether(height + 1)

	pdf := doc.pdf
	top := pdf.GetY()
	pdf.SetFont(fontFamily, "B", sizeSubhead)
	pdf.MultiCell(width, sizeSubhead*0.5, title, "", "L", false)
	for _, line := range lines {
		pdf.SetFont(fontFamily, "", sizeSmall)
		if line.grey {
			pdf.SetTextColor(grey[0], grey[1], grey[2])
		}
		if line.link != "" {
			pdf.SetTextColor(20, 80, 160)
			// A link is written line by line, so every line of a long address is
			// a link, within the column the QR code leaves free.
			for _, piece := range pdf.SplitText(line.text, width) {
				pdf.CellFormat(width, lineHeight*0.85, piece, "", 2, "L", false, 0, line.link)
			}
		} else {
			pdf.MultiCell(width, lineHeight*0.85, line.text, "", "L", false)
		}
		pdf.SetTextColor(0, 0, 0)
	}
	bottom := pdf.GetY()
	if place != "" {
		left := pageWidth - marginRight - qrSize
		if doc.qrCode(place, left, top, qrSize) {
			pdf.LinkString(left, top, qrSize, qrSize, place)
			pdf.SetFont(fontFamily, "", sizeSmall)
			pdf.SetTextColor(grey[0], grey[1], grey[2])
			pdf.SetXY(left-qrGap, top+qrSize)
			pdf.CellFormat(qrSize+qrGap, qrCaption, text.qrPlace, "", 0, "C", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			bottom = max(bottom, top+qrSize+qrCaption)
		}
	}
	if site != "" {
		siteTop := pdf.GetY() + qrGap/2
		if doc.qrCode(site, marginLeft, siteTop, qrSize) {
			pdf.LinkString(marginLeft, siteTop, qrSize, qrSize, site)
			captionLeft := marginLeft + qrSize + qrGap/2
			captionWidth := width - qrSize - qrGap/2
			pdf.SetXY(captionLeft, siteTop+qrSize/2-lineHeight)
			pdf.SetFont(fontFamily, "B", sizeSmall)
			pdf.CellFormat(captionWidth, lineHeight*0.85, text.qrSite, "", 2, "L", false, 0, site)
			if name := siteName(site); name != "" {
				pdf.SetX(captionLeft)
				pdf.SetFont(fontFamily, "", sizeSmall)
				pdf.SetTextColor(grey[0], grey[1], grey[2])
				pdf.CellFormat(captionWidth, lineHeight*0.85, name, "", 2, "L", false, 0, site)
				pdf.SetTextColor(0, 0, 0)
			}
			bottom = max(bottom, siteTop+qrSize)
		}
	}
	pdf.SetXY(marginLeft, bottom)
}

// qrCode draws a QR code of a text as filled squares, one a module: drawn
// rather than pasted in as a picture, it is sharp at any size it is printed or
// zoomed to. The code keeps the quiet margin a reader needs around it.
//
// Arguments:
//   - content: what the code holds.
//   - x, y: its top left corner on the page.
//   - size: the side of the square it fills, margin included.
//
// Returns:
//   - whether a code was drawn; one that cannot be encoded is left out.
func (d *document) qrCode(content string, x, y, size float64) bool {
	code, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return false
	}
	// A reader wants four modules of blank all round.
	const quiet = 4
	bounds := code.Bounds()
	modules := bounds.Dx()
	cell := size / float64(modules+2*quiet)
	dark := func(column, row int) bool {
		r, _, _, _ := code.At(bounds.Min.X+column, bounds.Min.Y+row).RGBA()
		return r == 0
	}
	d.pdf.SetFillColor(0, 0, 0)
	// A run of dark modules in a row is one rectangle: squares side by side
	// show hairline seams in some viewers.
	for row := 0; row < modules; row++ {
		for column := 0; column < modules; {
			if !dark(column, row) {
				column++
				continue
			}
			start := column
			for column < modules && dark(column, row) {
				column++
			}
			d.pdf.Rect(x+cell*float64(quiet+start), y+cell*float64(quiet+row), cell*float64(column-start), cell, "F")
		}
	}
	return true
}
