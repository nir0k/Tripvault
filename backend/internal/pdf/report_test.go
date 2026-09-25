package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// samplePhoto encodes a small JPEG, which is what the media package hands the
// renderer for every picture whatever it was uploaded as.
func samplePhoto(t *testing.T) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for x := range 32 {
		for y := range 24 {
			picture.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, nil); err != nil {
		t.Fatalf("encode the sample photograph: %v", err)
	}
	return out.Bytes()
}

// sampleReport builds a trip of two days with everything a report can carry.
func sampleReport(t *testing.T) Report {
	t.Helper()

	start := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	second := start.AddDate(0, 0, 1)
	end := start.AddDate(0, 0, 2)

	documentID := uuid.New()
	firstDay := domain.Day{ID: uuid.New(), DocumentID: documentID, Position: 0, Date: &start,
		Title: "Reykjavík", NotesMD: "We landed at dawn.\n\n- coffee\n- a walk"}
	secondDay := domain.Day{ID: uuid.New(), DocumentID: documentID, Position: 1, Date: &second,
		Title: "The south coast"}

	rating := 5
	visitTime := domain.ClockTime(9 * 60)
	planned := domain.Money(4500)
	actual := domain.Money(5200)

	harbour := domain.Item{
		ID: uuid.New(), DocumentID: documentID, DayID: &firstDay.ID, Position: 0,
		Kind: domain.ItemPlace, Name: "The old harbour", Status: domain.StatusVisited,
		StoryMD: "Cold, bright and **completely empty**.", Rating: &rating,
		ActualTime: &visitTime, ActualCost: &actual, PlannedCost: &planned,
		Address: "Reykjavík", CostCategory: domain.CostActivities,
	}
	museum := domain.Item{
		ID: uuid.New(), DocumentID: documentID, DayID: &firstDay.ID, Position: 1,
		Kind: domain.ItemPlace, Name: "The settlement museum", Status: domain.StatusSkipped,
		PlannedCost: &planned, CostCategory: domain.CostActivities,
	}
	// A stay mark must not be written as a place: the stay itself says more.
	mark := domain.Item{
		ID: uuid.New(), DocumentID: documentID, DayID: &firstDay.ID, Position: 2,
		Kind: domain.ItemStayAnchor, Name: "Hotel mark",
	}

	distance := 43_000
	duration := 2700
	leg := domain.Leg{
		ID: uuid.New(), DocumentID: documentID, DayID: firstDay.ID,
		FromItemID: harbour.ID, ToItemID: museum.ID, Mode: domain.ModeCar,
		DistanceM: &distance, DurationS: &duration, ActualCost: &actual,
	}

	stay := domain.Stay{
		ID: uuid.New(), DocumentID: documentID, Name: "Guesthouse Sunna",
		Kind: domain.StayHotel, CheckInDate: start, CheckOutDate: end,
		ActualCost: &actual, Address: "Þórsgata 26", NotesMD: "Breakfast until ten.",
	}

	departure, arrival := domain.ClockTime(7*60+40), domain.ClockTime(12*60+55)
	flight := domain.Transfer{
		ID: uuid.New(), DocumentID: documentID, Kind: domain.TransferFlight, Name: "FI 204",
		FromName: "Keflavík", ToName: "Copenhagen", DepartureDate: start,
		DepartureTime: &departure, ArrivalTime: &arrival, PlannedCost: &planned, CostPerPerson: true,
	}

	spentOn := start
	expense := domain.Expense{
		ID: uuid.New(), DocumentID: documentID, Category: domain.CostFood,
		Actual: &actual, SpentOn: &spentOn, Note: "Dinner on the first night",
	}

	ascent, descent := 840, 810
	track := domain.Track{
		ID: uuid.New(), DocumentID: documentID, ItemID: harbour.ID,
		OriginalName: "harbour-walk.gpx", DistanceM: 61_400, PointCount: 1200, AscentM: &ascent, DescentM: &descent,
	}

	trip := domain.Trip{
		ID: uuid.New(), Title: "Iceland: the south coast", Summary: "Nine days by car.",
		StartDate: &start, EndDate: &end, Currency: "EUR", Travelers: 2,
	}
	content := domain.DocumentContent{
		Document: domain.Document{ID: documentID, TripID: trip.ID, Kind: domain.DocumentReport,
			IntroMD:   "## How it went\n\nBetter than the forecast said.",
			SummaryMD: "We would go again in winter."},
		Days:      []domain.Day{firstDay, secondDay},
		Items:     []domain.Item{museum, harbour, mark},
		Stays:     []domain.Stay{stay},
		Transfers: []domain.Transfer{flight},
		Legs:      []domain.Leg{leg},
		Expenses:  []domain.Expense{expense},
		Tracks:    []domain.Track{track},
	}

	photo := samplePhoto(t)
	return Report{
		Trip:    trip,
		Content: content,
		Totals:  domain.BuildReportTotals(trip, content),
		Cover:   photo,
		Photos: map[uuid.UUID][]Photo{
			firstDay.ID: {{JPEG: photo, TakenAt: &start}},
			harbour.ID:  {{JPEG: photo}},
		},
	}
}

// TestRenderWritesAPDF checks the renderer produces a real PDF of more than one
// page, with the pictures in it.
func TestRenderWritesAPDF(t *testing.T) {
	var out bytes.Buffer
	if err := Render(&out, sampleReport(t)); err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}

	body := out.Bytes()
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("what was written does not begin like a PDF: %q", body[:min(8, len(body))])
	}
	if !bytes.Contains(body, []byte("%%EOF")) {
		t.Error("the document was not closed")
	}
	if pages := bytes.Count(body, []byte("/Type /Page\n")); pages < 2 {
		t.Errorf("the document has %d pages, want the cover and at least one more", pages)
	}
	if !bytes.Contains(body, []byte("/Subtype /Image")) {
		t.Error("the photographs did not reach the document")
	}
	// Roughly the size of three small JPEGs and two embedded fonts; far below it
	// would mean something was quietly dropped.
	if len(body) < 20_000 {
		t.Errorf("the document is %d bytes, which is too small to hold what it should", len(body))
	}
}

// TestPhotosShareRows checks the photographs of a place are laid out in rows
// rather than one to a page: seven of them add at most one page to the report.
func TestPhotosShareRows(t *testing.T) {
	pages := func(report Report) int {
		t.Helper()
		var out bytes.Buffer
		if err := Render(&out, report); err != nil {
			t.Fatalf("Render() returned an unexpected error: %v", err)
		}
		return bytes.Count(out.Bytes(), []byte("/Type /Page\n"))
	}

	report := sampleReport(t)
	report.Photos = nil
	without := pages(report)

	report = sampleReport(t)
	var place uuid.UUID
	for id := range report.Photos {
		if id != report.Content.Days[0].ID {
			place = id
		}
	}
	taken := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)
	seven := make([]Photo, 7)
	for i := range seven {
		seven[i] = Photo{JPEG: samplePhoto(t), TakenAt: &taken}
	}
	report.Photos = map[uuid.UUID][]Photo{place: seven}
	if with := pages(report); with > without+1 {
		t.Errorf("seven photographs took the report from %d pages to %d", without, with)
	}
}

// TestRenderInEitherLanguage checks a document is written in the reader's
// language, and that an unknown language still produces one.
func TestRenderInEitherLanguage(t *testing.T) {
	for _, language := range []string{"en", "ru", "", "fr", "ru-RU"} {
		report := sampleReport(t)
		report.Language = language

		var out bytes.Buffer
		if err := Render(&out, report); err != nil {
			t.Errorf("Render(%q) returned an unexpected error: %v", language, err)
		}
		if out.Len() == 0 {
			t.Errorf("Render(%q) wrote nothing", language)
		}
	}
}

// TestRenderWithoutPhotographs checks a report with no pictures, no cover and no
// dates still renders: a trip can be written up before any of that exists.
func TestRenderWithoutPhotographs(t *testing.T) {
	report := sampleReport(t)
	report.Cover = nil
	report.Photos = nil
	report.Trip.StartDate, report.Trip.EndDate = nil, nil
	for i := range report.Content.Days {
		report.Content.Days[i].Date = nil
	}

	var out bytes.Buffer
	if err := Render(&out, report); err != nil {
		t.Fatalf("Render() returned an unexpected error: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("/Subtype /Image")) {
		t.Error("a report with no pictures produced a document with one")
	}
}

// TestRenderReportsAPictureItCannotRead checks a photograph that is not one
// fails the document rather than leaving a hole in it.
func TestRenderReportsAPictureItCannotRead(t *testing.T) {
	report := sampleReport(t)
	report.Cover = []byte("this is not a photograph")

	var out bytes.Buffer
	if err := Render(&out, report); err == nil {
		t.Error("a document was written with a picture that could not be read")
	}
}

// TestPlacesOfOneDay checks a day's places come out in the order they were
// visited, and that a stay mark is not one of them.
func TestPlacesOfOneDay(t *testing.T) {
	report := sampleReport(t)
	day := report.Content.Days[0]

	places := placesOf(report.Content.Items, day.ID)
	if len(places) != 2 {
		t.Fatalf("got %d places, want the two that are places", len(places))
	}
	if places[0].Name != "The old harbour" || places[1].Name != "The settlement museum" {
		t.Errorf("the places came out as %s, %s", places[0].Name, places[1].Name)
	}
	if places := placesOf(report.Content.Items, uuid.New()); len(places) != 0 {
		t.Errorf("a day that is not in the report has %d places", len(places))
	}
}

// TestStaysOfADay checks a stay covers the nights it was booked for and not the
// day it was left on.
func TestStaysOfADay(t *testing.T) {
	report := sampleReport(t)

	if stays := staysOf(report, report.Content.Days[0]); len(stays) != 1 {
		t.Errorf("the first night has %d stays, want one", len(stays))
	}

	checkOut := report.Content.Stays[0].CheckOutDate
	left := domain.Day{ID: uuid.New(), Date: &checkOut}
	if stays := staysOf(report, left); len(stays) != 0 {
		t.Errorf("the day of the check-out lists %d stays", len(stays))
	}

	// A trip without dates has no day a stay could cover.
	if stays := staysOf(report, domain.Day{ID: uuid.New()}); len(stays) != 0 {
		t.Errorf("a day with no date lists %d stays", len(stays))
	}
}

// TestPhotoCaptionIsTheTimeItWasTaken checks a picture is captioned with when it
// was taken, in the trip's own time zone, and with nothing when the file carried
// no such metadata.
func TestPhotoCaptionIsTheTimeItWasTaken(t *testing.T) {
	text := wording("en", domain.UnitsKilometres)
	trip := domain.Trip{Timezone: "Atlantic/Reykjavik"}
	// 21:30 UTC is 21:30 in Reykjavík and 23:30 in Lisbon in June, which is the
	// difference a caption must get right.
	at := time.Date(2026, 6, 20, 21, 30, 0, 0, time.UTC)

	if got := caption(text, trip, Photo{TakenAt: &at}); got != "20 June 2026, 21:30" {
		t.Errorf("caption = %q", got)
	}
	if got := caption(text, domain.Trip{Timezone: "Europe/Lisbon"}, Photo{TakenAt: &at}); got != "20 June 2026, 22:30" {
		t.Errorf("a caption in another zone = %q", got)
	}
	// A zone the server does not know must not lose the caption altogether.
	if got := caption(text, domain.Trip{Timezone: "Mars/Olympus"}, Photo{TakenAt: &at}); got == "" {
		t.Error("an unknown time zone left the picture with no caption")
	}
	if got := caption(text, trip, Photo{}); got != "" {
		t.Errorf("a picture with no metadata was captioned %q", got)
	}
}

// TestMoneyAndDifference checks the amounts read the way a reader expects: with
// the currency where one is wanted, and with a sign on the difference.
func TestMoneyAndDifference(t *testing.T) {
	if got := money(domain.Money(5200), "EUR"); got != "52.00 EUR" {
		t.Errorf("money() = %q", got)
	}
	if got := money(domain.Money(5200), ""); got != "52.00" {
		t.Errorf("money() without a currency = %q", got)
	}
	if got := signed(domain.Money(700)); got != "+7.00" {
		t.Errorf("signed(over) = %q", got)
	}
	if got := signed(domain.Money(-700)); got != "-7.00" {
		t.Errorf("signed(under) = %q", got)
	}
}

// TestLabelsWriteDatesAndFigures checks the wording of both languages, including
// the shapes a span of dates can take.
func TestLabelsWriteDatesAndFigures(t *testing.T) {
	from := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	sameMonth := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)

	english, russian := wording("en", domain.UnitsKilometres), wording("ru-RU", domain.UnitsKilometres)
	if got := english.dateRange(&from, &sameMonth); got != "12–19 June 2026" {
		t.Errorf("an English span within a month = %q", got)
	}
	if got := russian.dateRange(&from, &later); got != "12 июня 2026 – 3 июля 2026" {
		t.Errorf("a Russian span across months = %q", got)
	}
	if got := english.dateRange(nil, nil); got != "" {
		t.Errorf("a trip with no dates = %q", got)
	}
	if got := english.dateRange(&from, &from); got != "12 June 2026" {
		t.Errorf("a trip of one day = %q", got)
	}

	if got := english.distanceOf(43_000); got != "43 km" {
		t.Errorf("distance = %q", got)
	}
	if got := english.distanceOf(3450); got != "3.5 km" {
		t.Errorf("a short distance = %q", got)
	}

	// The same distances for a reader who counts in miles, in both languages.
	inMiles := wording("en", domain.UnitsMiles)
	if got := inMiles.distanceOf(43_000); got != "27 mi" {
		t.Errorf("distance in miles = %q", got)
	}
	if got := inMiles.distanceOf(3450); got != "2.1 mi" {
		t.Errorf("a short distance in miles = %q", got)
	}
	if got := wording("ru", domain.UnitsMiles).distanceOf(43_000); got != "27 миль" {
		t.Errorf("a Russian distance in miles = %q", got)
	}
	// Units nobody chose are kilometres, so a caller that says nothing is safe.
	if got := wording("en", "").distanceOf(43_000); got != "43 km" {
		t.Errorf("distance with no units chosen = %q", got)
	}
	if got := english.duration(2700); got != "45 min" {
		t.Errorf("duration under an hour = %q", got)
	}
	if got := english.duration(3 * 3600); got != "3 h 00 min" {
		t.Errorf("duration = %q", got)
	}
	if got := russian.duration(600); got != "10 мин" {
		t.Errorf("a short Russian duration = %q", got)
	}
	if got := english.clock(9 * 60); got != "09:00" {
		t.Errorf("clock = %q", got)
	}
}

// TestEveryLabelIsTranslated checks the two languages describe the same set of
// values: a status or a means of travel missing from one would print as nothing
// in somebody's report.
func TestEveryLabelIsTranslated(t *testing.T) {
	maps := []struct {
		name             string
		english, russian map[string]string
	}{
		{"statuses", english.statuses, russian.statuses},
		{"modes", english.modes, russian.modes},
		{"stay kinds", english.stayKinds, russian.stayKinds},
		{"transfer kinds", english.transferKinds, russian.transferKinds},
		{"categories", english.categories, russian.categories},
		{"activities", english.activities, russian.activities},
	}
	for _, pair := range maps {
		for key := range pair.english {
			if strings.TrimSpace(pair.russian[key]) == "" {
				t.Errorf("%s: %q has no Russian wording", pair.name, key)
			}
		}
		if len(pair.english) != len(pair.russian) {
			t.Errorf("%s: %d English against %d Russian", pair.name, len(pair.english), len(pair.russian))
		}
	}

	for index := range english.difficulties {
		if english.difficulties[index] == "" || russian.difficulties[index] == "" {
			t.Errorf("the difficulty %d has no wording", index+1)
		}
	}

	// The values the domain actually uses must all be covered.
	for _, status := range domain.ItemStatuses {
		if english.statuses[string(status)] == "" {
			t.Errorf("the status %q has no wording", status)
		}
	}
	for _, kind := range domain.TransferKinds {
		if english.transferKinds[string(kind)] == "" {
			t.Errorf("the transfer kind %q has no wording", kind)
		}
	}
	for _, activity := range domain.ActivityTypes {
		if english.activities[string(activity)] == "" {
			t.Errorf("the activity %q has no wording", activity)
		}
	}
	for _, mode := range travelModeOrder {
		if english.modes[string(mode)] == "" {
			t.Errorf("the mode %q has no wording", mode)
		}
	}
	for _, category := range domain.CostCategories {
		if english.categories[string(category)] == "" {
			t.Errorf("the category %q has no wording", category)
		}
	}
}
