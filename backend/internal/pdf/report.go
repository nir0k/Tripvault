package pdf

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The characters the document decorates itself with. They are named here
// because the embedded typeface has to have all of them: one it does not draws
// as an empty box, which is how the star this once used was found. The test
// beside this file reads the font and checks each of them.
const (
	// bulletMark opens an item of a list.
	bulletMark = "\u2022"
	// arrowMark opens the journey from one place to the next.
	arrowMark = "\u2192"
	// separator stands between the facts on one line.
	separator = "   \u00b7   "
)

// Photo is one picture of the report, already rendered to the size the document
// uses. Decoding and scaling belong to the media package; by the time a picture
// reaches here it is a JPEG of a known width.
type Photo struct {
	JPEG []byte
	// TakenAt is when the camera took it, read from the file at upload and nil
	// when it carried none. It is the only caption a picture has: the name of
	// the file it arrived as says nothing a reader wants.
	TakenAt *time.Time
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
	// Cover is the trip's own picture, empty when it has none.
	Cover []byte
	// Photos holds the pictures of the trip, its days and its places, by the
	// identifier of the thing each hangs on.
	Photos map[uuid.UUID][]Photo
	// Language is the reader's language; anything but "ru" is written in English.
	Language string
	// Units is what the reader counts distances in; kilometres when unset.
	Units domain.Units
	// Maps are the maps MapRequests asked for, by the same keys: uuid.Nil for
	// the whole trip, a day's identifier for that day. A map not here is not
	// drawn.
	Maps map[uuid.UUID]Map
	// MapAttribution is the credit the tile provider asks for, in plain text.
	MapAttribution string
}

// Render - writes a report as a PDF.
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

	if err := writeCover(doc, text, report); err != nil {
		return err
	}

	doc.page()
	writeIntro(doc, report)
	writeSummary(doc, text, report)
	if err := writeTripMap(doc, text, report); err != nil {
		return err
	}

	for index, day := range report.Content.Days {
		if err := writeDay(doc, text, report, day, index); err != nil {
			return err
		}
	}

	writeExpenses(doc, text, report)
	writeClosing(doc, text, report)
	return doc.render(w)
}

// writeCover draws the title page: the picture the trip is shown by, its name,
// when it happened and who went.
func writeCover(doc *document, text labels, report Report) error {
	doc.page()

	if len(report.Cover) > 0 {
		if err := doc.picture(report.Cover, contentWidth, ""); err != nil {
			return err
		}
		doc.space(4)
	}

	doc.title(report.Trip.Title)
	if span := text.dateRange(report.Trip.StartDate, report.Trip.EndDate); span != "" {
		doc.note(span)
	}

	facts := []string{
		fmt.Sprintf("%s: %d", text.travelers, max(report.Trip.Travelers, 1)),
		fmt.Sprintf("%s: %s", text.currency, report.Trip.Currency),
	}
	doc.note(strings.Join(facts, separator))

	if report.Trip.Summary != "" {
		doc.space(4)
		doc.body(report.Trip.Summary, 0, "I")
	}
	return nil
}

// writeIntro writes the words the report opens with, if it has any.
func writeIntro(doc *document, report Report) {
	if report.Content.Document.IntroMD == "" {
		return
	}
	writeMarkdown(doc, report.Content.Document.IntroMD)
	doc.space(2)
}

// writeSummary writes the figures the reading mode of a report opens with.
func writeSummary(doc *document, text labels, report Report) {
	totals := report.Totals

	doc.heading(text.summary)
	figures := [][2]string{
		{text.days, fmt.Sprint(totals.Days)},
		{text.nights, fmt.Sprint(totals.Nights)},
		{text.visited, fmt.Sprintf("%d / %d", totals.Places.Visited, totals.Places.Total())},
		{text.distance, text.distanceOf(totals.DistanceM)},
		{text.spent, money(totals.ActualCost, report.Trip.Currency)},
		{text.planned, money(totals.PlannedCost, report.Trip.Currency)},
	}
	if totals.Places.Skipped > 0 {
		figures = append(figures, [2]string{text.skipped, fmt.Sprint(totals.Places.Skipped)})
	}
	if totals.Places.Unplanned > 0 {
		figures = append(figures, [2]string{text.unplanned, fmt.Sprint(totals.Places.Unplanned)})
	}
	if totals.AverageRating != nil {
		figures = append(figures, [2]string{
			text.rating,
			fmt.Sprintf("%.1f (%s)", *totals.AverageRating, fmt.Sprintf(text.rated, totals.Rated)),
		})
	}
	doc.figures(figures)

	if len(totals.ByMode) > 0 {
		doc.space(2)
		doc.note(text.byMode)
		for _, mode := range travelModeOrder {
			total, ok := totals.ByMode[mode]
			if !ok {
				continue
			}
			line := fmt.Sprintf("%s — %s", text.modes[string(mode)], text.distanceOf(total.DistanceM))
			if total.DurationS > 0 {
				line += ", " + text.duration(total.DurationS)
			}
			doc.body(line, 4, "")
		}
	}
}

// writeTripMap draws the whole trip on one map, after the figures that sum it
// up.
func writeTripMap(doc *document, text labels, report Report) error {
	m, ok := report.Maps[uuid.Nil]
	if !ok {
		return nil
	}
	doc.keepTogether(sizeHeading*0.5 + headingSpace + mapHeight(m) + lineHeight)
	doc.heading(text.tripMap)
	return doc.drawMap(tripLayer(report.Content), m, report.MapAttribution)
}

// travelModeOrder is the order the means of travel are listed in, so two reports
// of the same trip do not shuffle their rows between renders.
var travelModeOrder = []domain.TravelMode{
	domain.ModeWalk, domain.ModeCar, domain.ModeBike,
	domain.ModeTransit, domain.ModeFlight, domain.ModeCableCar, domain.ModeOther,
}

// writeDay writes one day: what it was called, what was recorded, where it went
// and what it looked like.
func writeDay(doc *document, text labels, report Report, day domain.Day, index int) error {
	doc.rule()

	heading := fmt.Sprintf(text.day, index+1)
	if day.Date != nil {
		heading += " · " + text.date(*day.Date)
	}
	if day.Title != "" {
		heading += " · " + day.Title
	}
	dayMap, mapped := report.Maps[day.ID]
	if mapped {
		doc.keepTogether(sizeHeading*0.5 + headingSpace + mapHeight(dayMap) + lineHeight)
	}
	doc.heading(heading)
	if mapped {
		if err := doc.drawMap(dayLayer(report.Content, day, index, true), dayMap, report.MapAttribution); err != nil {
			return err
		}
	}

	if day.NotesMD != "" {
		writeMarkdown(doc, day.NotesMD)
		doc.space(1)
	}

	places := placesOf(report.Content.Items, day.ID)
	legs := legsOf(report.Content.Legs, day.ID)
	stays := staysOf(report, day)

	// A flight or a train that leaves or lands this day frames it, so it is
	// written before the places.
	if day.Date != nil {
		for _, transfer := range domain.TransfersOnDate(report.Content.Transfers, *day.Date) {
			writeTransfer(doc, text, report, transfer)
		}
	}

	// A day without a stay to start from opens with the journey in from the
	// day before, which says where it started: that place is pages back.
	if leg := domain.OpeningLeg(report.Content.Legs, domain.DayItems(report.Content.Items, &day.ID)); leg != nil {
		doc.space(1)
		writeLeg(doc, text, report, *leg, originOf(report, leg.FromItemID))
	}

	for number, place := range places {
		// A day with a map numbers its places, as the pins on it are numbered.
		name := place.Name
		if mapped {
			name = strconv.Itoa(number+1) + ". " + name
		}
		writePlace(doc, text, report, place, name)
		if leg, ok := legs[place.ID]; ok {
			writeLeg(doc, text, report, leg, "")
		}
		if err := writePhotos(doc, text, report, place.ID); err != nil {
			return err
		}
	}

	for _, stay := range stays {
		writeStay(doc, text, report, stay)
	}

	return writePhotos(doc, text, report, day.ID)
}

// writePlace writes one place of a day, under the name given: what it is, how
// it went and what it cost.
func writePlace(doc *document, text labels, report Report, place domain.Item, name string) {
	doc.space(1)
	doc.subheading(name)

	// A visited place is what a report is made of, so only the exceptions are
	// named, as the page does.
	var facts []string
	if place.Kind == domain.ItemActivity {
		facts = append(facts, text.activities[string(place.ActivityType)])
	}
	if status, ok := text.statuses[string(place.Status)]; ok && place.Status != "" &&
		place.Status != domain.StatusVisited {
		facts = append(facts, status)
	}
	if place.ActualTime != nil {
		facts = append(facts, text.clockPeriod(*place.ActualTime, place.ActualEndTime))
	} else if place.DesiredTime != nil {
		facts = append(facts, text.clock(int(*place.DesiredTime)))
	}
	if place.Difficulty != nil && *place.Difficulty >= domain.MinDifficulty && *place.Difficulty <= domain.MaxDifficulty {
		facts = append(facts, fmt.Sprintf(text.difficulty, text.difficulties[*place.Difficulty-1]))
	}
	if place.Rating != nil {
		facts = append(facts, fmt.Sprintf("%s %d/5", text.ratingOf, *place.Rating))
	}
	if cost := placeCost(place, report.Trip); cost != "" {
		facts = append(facts, cost)
	}
	if place.Address != "" {
		facts = append(facts, place.Address)
	}
	doc.note(strings.Join(facts, separator))
	if track := domain.TrackOfItem(report.Content.Tracks, place.ID); track != nil {
		doc.note(trackNote(text, track))
	}

	if place.StoryMD != "" {
		writeMarkdown(doc, place.StoryMD)
	} else if place.DescriptionMD != "" {
		writeMarkdown(doc, place.DescriptionMD)
	}
}

// trackNote says how far a recording went and, when the file had heights, how
// much it climbed and descended.
func trackNote(text labels, track *domain.Track) string {
	note := fmt.Sprintf(text.track, text.distanceOf(track.DistanceM))
	if track.AscentM != nil && track.DescentM != nil {
		note += separator + fmt.Sprintf(text.climb, text.heightOf(*track.AscentM), text.heightOf(*track.DescentM))
	}
	return note
}

// writeLeg writes the journey from one place to the next, naming where it
// started when origin is not empty.
func writeLeg(doc *document, text labels, report Report, leg domain.Leg, origin string) {
	parts := []string{text.modes[string(leg.Mode)]}
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

// writeStay writes where the night was spent.
func writeStay(doc *document, text labels, report Report, stay domain.Stay) {
	doc.space(1)
	doc.subheading(text.stay + ": " + stay.Name)

	parts := []string{}
	if kind, ok := text.stayKinds[string(stay.Kind)]; ok {
		parts = append(parts, kind)
	}
	parts = append(parts, fmt.Sprintf("%s %s %s %s",
		text.checkIn, text.date(stay.CheckInDate), text.checkOut, text.date(stay.CheckOutDate)))
	if amount := amountOf(stay.ActualCost, stay.PlannedCost); amount != nil {
		parts = append(parts, money(*amount, report.Trip.Currency))
	}
	if stay.Address != "" {
		parts = append(parts, stay.Address)
	}
	doc.note(strings.Join(parts, separator))

	if stay.NotesMD != "" {
		writeMarkdown(doc, stay.NotesMD)
	}
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

// writePhotos draws the pictures hanging on one day or place, in rows of a few,
// so that a place with its seven favourites takes half a page rather than
// seven. A short last row is drawn as tall as the full row above it, not
// stretched to the width.
func writePhotos(doc *document, text labels, report Report, target uuid.UUID) error {
	photos := report.Photos[target]
	if len(photos) == 0 {
		return nil
	}

	doc.space(1)
	// fullRow is how tall the last full row came out; a short row after it
	// matches it rather than growing to the limit.
	fullRow := maxRowHeight
	for start := 0; start < len(photos); start += photosPerRow {
		row := make([]rowPhoto, 0, photosPerRow)
		for _, photo := range photos[start:min(start+photosPerRow, len(photos))] {
			row = append(row, rowPhoto{jpeg: photo.JPEG, caption: caption(text, report.Trip, photo)})
		}
		limit := maxRowHeight
		if len(row) < photosPerRow {
			limit = fullRow
		}
		height, err := doc.pictureRow(row, limit)
		if err != nil {
			return err
		}
		if len(row) == photosPerRow {
			fullRow = height
		}
	}
	return nil
}

// caption writes when a photograph was taken, in the trip's own time zone: a
// time in UTC under a picture of an evening would say the wrong hour.
func caption(text labels, trip domain.Trip, photo Photo) string {
	if photo.TakenAt == nil {
		return ""
	}
	at := *photo.TakenAt
	if zone, err := time.LoadLocation(trip.Timezone); err == nil {
		at = at.In(zone)
	}
	return text.dateTime(at)
}

// writeExpenses writes the costs that hang on no place, stay or leg.
func writeExpenses(doc *document, text labels, report Report) {
	if len(report.Content.Expenses) == 0 {
		return
	}

	doc.rule()
	doc.heading(text.expenses)
	for _, expense := range report.Content.Expenses {
		amount := amountOf(expense.Actual, expense.Planned)
		if amount == nil {
			continue
		}
		name := expense.Note
		if name == "" {
			name = text.categories[string(expense.Category)]
		}
		doc.body(fmt.Sprintf("%s — %s", name, money(*amount, report.Trip.Currency)), 4, "")
	}
}

// writeClosing writes the words the report ends with and the comparison of what
// was planned against what was spent.
func writeClosing(doc *document, text labels, report Report) {
	if summary := report.Content.Document.SummaryMD; summary != "" {
		doc.rule()
		writeMarkdown(doc, summary)
	}

	doc.rule()
	doc.heading(text.planFact)
	doc.table(
		[]string{"", report.Trip.Currency},
		[][]string{
			{text.plannedCost, money(report.Totals.PlannedCost, "")},
			{text.actualCost, money(report.Totals.ActualCost, "")},
			{text.difference, signed(report.Totals.Difference)},
		},
		[]float64{0.7, 0.3},
	)
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

// legsOf maps each leg of a day onto the place it leaves from, which is where
// the document writes it: after the place, before the next one.
func legsOf(legs []domain.Leg, dayID uuid.UUID) map[uuid.UUID]domain.Leg {
	byOrigin := make(map[uuid.UUID]domain.Leg)
	for _, leg := range legs {
		if leg.DayID == dayID {
			byOrigin[leg.FromItemID] = leg
		}
	}
	return byOrigin
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
