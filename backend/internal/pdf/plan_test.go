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
	content.Items = append(content.Items, domain.Item{
		ID: uuid.New(), DocumentID: content.Document.ID, Kind: domain.ItemPlace, Name: "Hot spring maybe",
		Category: domain.CategoryNature, Lat: &lat, Lng: &lng,
	})
	return Plan{Trip: report.Trip, Content: content, Language: "en"}
}

// TestRenderPlanWritesAPDF checks the plan's document is a PDF with a cover,
// the overview, a page a day and one for the ideas, its places linking to the
// way there.
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
	if !bytes.Contains(body, []byte("https://www.google.com/maps/dir/?api=1&destination=64.150000%2C-21.940000")) {
		t.Error("an address does not open the way to its place")
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

// TestNavigationAndQR checks the way to a position is a map address, nothing
// is made for a place without one, and its QR code is drawn.
func TestNavigationAndQR(t *testing.T) {
	lat, lng := 38.7142, -9.141
	if got := navigation(&lat, &lng); got != "https://www.google.com/maps/dir/?api=1&destination=38.714200%2C-9.141000" {
		t.Errorf("navigation: %q", got)
	}
	if navigation(nil, &lng) != "" {
		t.Error("a place without a position has a way there")
	}
	doc := newDocument("QR", "QR")
	// Uncompressed, so the drawing can be read back.
	doc.pdf.SetCompression(false)
	doc.page()
	if !doc.qrCode(navigation(&lat, &lng), marginLeft, marginTop, qrSize) {
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
