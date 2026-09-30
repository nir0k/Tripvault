package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// samplePlan builds a plan out of the sample report's trip and content, with
// its places given positions and one idea without a day.
func samplePlan(t *testing.T) Plan {
	t.Helper()
	report := sampleReport(t)
	content := report.Content
	lat, lng := 64.1500, -21.9400
	for index := range content.Items {
		content.Items[index].Lat, content.Items[index].Lng = &lat, &lng
		content.Items[index].Status = ""
	}
	// One place has a website, given as a code of its own.
	content.Items[0].URL = "https://www.parquesdesintra.pt/en/parks-monuments/park-and-national-palace-of-pena/tickets"
	// And one has a website but no position, so its code stands alone.
	content.Items = append(content.Items, domain.Item{
		ID: uuid.New(), DocumentID: content.Document.ID, Kind: domain.ItemPlace, Name: "Somewhere online",
		Category: domain.CategoryOther, URL: "https://example.com/booking",
	})
	content.Items = append(content.Items, domain.Item{
		ID: uuid.New(), DocumentID: content.Document.ID, Kind: domain.ItemPlace, Name: "Hot spring maybe",
		Category: domain.CategoryNature, Lat: &lat, Lng: &lng,
	})
	return Plan{Trip: report.Trip, Content: content, Language: "en"}
}

// TestRenderPlanWritesAPDF checks the plan's document is a PDF with a cover,
// the overview, a page a day and one for the ideas, its places linking to where
// they are on a map and to their websites, never to the way there.
func TestRenderPlanWritesAPDF(t *testing.T) {
	plan := samplePlan(t)
	var out bytes.Buffer
	if err := RenderPlan(&out, plan); err != nil {
		t.Fatalf("RenderPlan() returned an unexpected error: %v", err)
	}
	body := out.Bytes()
	if !bytes.HasPrefix(body, []byte("%PDF-")) || !bytes.Contains(body, []byte("%%EOF")) {
		t.Fatal("what was written is not a finished PDF")
	}
	// The cover, the overview, a page a day and one for the ideas.
	want := 2 + len(plan.Content.Days) + 1
	if pages := bytes.Count(body, []byte("/Type /Page\n")); pages < want {
		t.Errorf("the document has %d pages, want at least %d", pages, want)
	}
	if !bytes.Contains(body, []byte("https://www.google.com/maps/search/?api=1&query=64.150000%2C-21.940000")) {
		t.Error("an address does not open its place on a map")
	}
	if bytes.Contains(body, []byte("maps/dir/")) {
		t.Error("a link opens the way to a place rather than the place")
	}
	if !bytes.Contains(body, []byte("/URI (https://www.parquesdesintra.pt/en/parks-monuments/park-and-national-palace-of-pena/tickets)")) {
		t.Error("the website of a place cannot be opened from its code")
	}
	if !bytes.Contains(body, []byte("/URI (https://example.com/booking)")) {
		t.Error("the website of a place without a position cannot be opened")
	}
	// The contents link to the days' pages inside the document.
	if !bytes.Contains(body, []byte("/Dest [")) {
		t.Error("the contents do not link to the days")
	}
}

// TestRenderPlanInRussian checks the plan renders in the other language too.
func TestRenderPlanInRussian(t *testing.T) {
	plan := samplePlan(t)
	plan.Language = "ru"
	if err := RenderPlan(&bytes.Buffer{}, plan); err != nil {
		t.Fatalf("RenderPlan() in Russian: %v", err)
	}
}

// TestMapLinkAndQR checks a position opens as a point on a map, nothing is
// made for a place without one, and its QR code is drawn.
func TestMapLinkAndQR(t *testing.T) {
	lat, lng := 38.7142, -9.141
	if got := mapLink(&lat, &lng); got != "https://www.google.com/maps/search/?api=1&query=38.714200%2C-9.141000" {
		t.Errorf("mapLink: %q", got)
	}
	if mapLink(nil, &lng) != "" {
		t.Error("a place without a position has a map link")
	}
	doc := newDocument("QR", "QR")
	// Uncompressed, so the drawing can be read back.
	doc.pdf.SetCompression(false)
	doc.page()
	if !doc.qrCode(mapLink(&lat, &lng), marginLeft, marginTop, qrSize) {
		t.Fatal("the code was not drawn")
	}
	var out bytes.Buffer
	if err := doc.render(&out); err != nil {
		t.Fatal(err)
	}
	// Filled rectangles, one a run of dark modules.
	if runs := strings.Count(out.String(), " re f"); runs < 50 {
		t.Errorf("the code drew %d runs", runs)
	}
}

// TestStayPeriod checks a stay's dates carry the times of the desk when set.
func TestStayPeriod(t *testing.T) {
	report := sampleReport(t)
	stay := report.Content.Stays[0]
	checkIn, checkOut := domain.ClockTime(15*60), domain.ClockTime(10*60)
	stay.CheckInTime, stay.CheckOutTime = &checkIn, &checkOut
	got := stayPeriod(english, stay)
	if !strings.Contains(got, "15:00") || !strings.Contains(got, "10:00") || !strings.Contains(got, arrowMark) {
		t.Errorf("period: %q", got)
	}
}

// TestSiteName checks a website's code is named by its site alone, without the
// "www." nobody reads, and that an address with no site is named by nothing.
func TestSiteName(t *testing.T) {
	cases := map[string]string{
		"https://www.parquesdesintra.pt/en/tickets?lang=en": "parquesdesintra.pt",
		"https://example.com:8443/booking":                  "example.com",
		"not a link":                                        "",
	}
	for address, want := range cases {
		if got := siteName(address); got != want {
			t.Errorf("siteName(%q) = %q, want %q", address, got, want)
		}
	}
}
