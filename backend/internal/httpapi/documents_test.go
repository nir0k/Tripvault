package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/googlelink"
	"github.com/nir0k/tripvault/backend/internal/routing"
)

// fakeDocuments serves one document of the fake trip, with one day, one place
// and one stay mark, and accepts every change.
type fakeDocuments struct {
	document domain.Document
	day      domain.Day
	place    domain.Item
	anchor   domain.Item
	stay     domain.Stay
	leg      domain.Leg
	expense  *domain.Expense
	transfer *domain.Transfer
	track    *domain.Track
	// trackFile is the file the stored track was imported from.
	trackFile []byte
	changed   int
	// copied is the report the last CreateReport was asked to write.
	copied *domain.Trip
	// trips is the fake trip this document belongs to.
	trips *fakeTrips
	// translations are what SaveTranslations was given.
	translations []domain.Translation
}

// Document returns the one document, or not found.
func (f *fakeDocuments) Document(_ context.Context, id uuid.UUID) (domain.Document, error) {
	if id != f.document.ID {
		return domain.Document{}, domain.ErrNotFound
	}
	return f.document, nil
}

// Day returns the one day.
func (f *fakeDocuments) Day(context.Context, uuid.UUID) (domain.Day, error) { return f.day, nil }

// Stay returns the one stay.
func (f *fakeDocuments) Stay(context.Context, uuid.UUID) (domain.Stay, error) { return f.stay, nil }

// Item returns the stay mark when asked for it, the place otherwise.
func (f *fakeDocuments) Item(_ context.Context, id uuid.UUID) (domain.Item, error) {
	if id == f.anchor.ID {
		return f.anchor, nil
	}
	return f.place, nil
}

// Content returns the whole fake document.
func (f *fakeDocuments) Content(context.Context, uuid.UUID) (domain.DocumentContent, error) {
	return domain.DocumentContent{Document: f.document, Days: []domain.Day{f.day},
		Items: []domain.Item{f.place, f.anchor}, Stays: []domain.Stay{f.stay}, Legs: []domain.Leg{f.leg},
		Transfers: f.transfers(), Expenses: f.expenses(), Tracks: f.tracks(), Translations: f.translations}, nil
}

// tracks lists the stored track, if there is one.
func (f *fakeDocuments) tracks() []domain.Track {
	if f.track == nil {
		return nil
	}
	return []domain.Track{*f.track}
}

// CreateReport records the report and lets the fake trips read it back as a
// trip of its own.
func (f *fakeDocuments) CreateReport(_ context.Context, report domain.Trip, planID uuid.UUID) error {
	if planID != f.document.ID {
		return domain.ErrNotFound
	}
	f.changed++
	f.copied = &report
	reportID := uuid.New()
	f.trips.report = &domain.TripSummary{Trip: report, ReportID: &reportID, Role: domain.RoleOwner,
		SourceVisible: true}
	return nil
}

// UpdateDocument records the stored words.
func (f *fakeDocuments) UpdateDocument(_ context.Context, _ uuid.UUID, intro, summary string) error {
	f.document.IntroMD, f.document.SummaryMD = intro, summary
	f.changed++
	return nil
}

// AddDay records a change.
func (f *fakeDocuments) AddDay(context.Context, domain.Day, *int) error { f.changed++; return nil }

// DuplicateDay records a change.
func (f *fakeDocuments) DuplicateDay(context.Context, uuid.UUID) (uuid.UUID, error) {
	f.changed++
	return uuid.New(), nil
}

// UpdateDay records a change.
func (f *fakeDocuments) UpdateDay(context.Context, domain.Day) error { f.changed++; return nil }

// DeleteDay asks for confirmation unless it was given.
func (f *fakeDocuments) DeleteDay(_ context.Context, _ uuid.UUID, confirm bool) error {
	if !confirm {
		return &domain.DaysWouldBeRemovedError{Days: []domain.RemovedDay{{DocumentKind: domain.DocumentReport, Position: 3}}}
	}
	f.changed++
	return nil
}

// ReorderDays records a change.
func (f *fakeDocuments) ReorderDays(context.Context, uuid.UUID, []uuid.UUID) error {
	f.changed++
	return nil
}

// CreatePlace records a change.
func (f *fakeDocuments) CreatePlace(context.Context, domain.Item, *int) error {
	f.changed++
	return nil
}

// UpdatePlace records a change.
func (f *fakeDocuments) UpdatePlace(context.Context, domain.Item) error { f.changed++; return nil }

// DeletePlace records a change.
func (f *fakeDocuments) DeletePlace(context.Context, domain.Item) error { f.changed++; return nil }

// MovePlace records a change.
func (f *fakeDocuments) MovePlace(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, int) error {
	f.changed++
	return nil
}

// CopyPlace records a change.
func (f *fakeDocuments) CopyPlace(context.Context, domain.Item, uuid.UUID, *uuid.UUID, *int) error {
	f.changed++
	return nil
}

// CreateStay records a change.
func (f *fakeDocuments) CreateStay(context.Context, domain.Stay) error { f.changed++; return nil }

// UpdateStay records a change.
func (f *fakeDocuments) UpdateStay(context.Context, domain.Stay) error { f.changed++; return nil }

// DeleteStay records a change.
func (f *fakeDocuments) DeleteStay(context.Context, domain.Stay) error { f.changed++; return nil }

// transfers lists the stored transfer, so the document carries it.
func (f *fakeDocuments) transfers() []domain.Transfer {
	if f.transfer == nil {
		return nil
	}
	return []domain.Transfer{*f.transfer}
}

// Transfer returns the one transfer, or not found before it exists.
func (f *fakeDocuments) Transfer(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
	if f.transfer == nil || id != f.transfer.ID {
		return domain.Transfer{}, domain.ErrNotFound
	}
	return *f.transfer, nil
}

// CreateTransfer stores the transfer.
func (f *fakeDocuments) CreateTransfer(_ context.Context, transfer domain.Transfer) error {
	f.transfer = &transfer
	f.changed++
	return nil
}

// UpdateTransfer stores the changed transfer.
func (f *fakeDocuments) UpdateTransfer(_ context.Context, transfer domain.Transfer) error {
	f.transfer = &transfer
	f.changed++
	return nil
}

// DeleteTransfer forgets the transfer.
func (f *fakeDocuments) DeleteTransfer(context.Context, uuid.UUID) error {
	f.transfer = nil
	f.changed++
	return nil
}

// expenses lists the stored expense, so the document carries it.
func (f *fakeDocuments) expenses() []domain.Expense {
	if f.expense == nil {
		return nil
	}
	return []domain.Expense{*f.expense}
}

// Expense returns the one expense, or not found before it exists.
func (f *fakeDocuments) Expense(_ context.Context, id uuid.UUID) (domain.Expense, error) {
	if f.expense == nil || id != f.expense.ID {
		return domain.Expense{}, domain.ErrNotFound
	}
	return *f.expense, nil
}

// CreateExpense stores the expense.
func (f *fakeDocuments) CreateExpense(_ context.Context, expense domain.Expense) error {
	f.expense = &expense
	f.changed++
	return nil
}

// UpdateExpense stores the changed expense.
func (f *fakeDocuments) UpdateExpense(_ context.Context, expense domain.Expense) error {
	f.expense = &expense
	f.changed++
	return nil
}

// DeleteExpense forgets the expense.
func (f *fakeDocuments) DeleteExpense(context.Context, uuid.UUID) error {
	f.expense = nil
	f.changed++
	return nil
}

// Leg returns the one leg.
func (f *fakeDocuments) Leg(context.Context, uuid.UUID) (domain.Leg, error) { return f.leg, nil }

// UpdateLeg records the stored leg.
func (f *fakeDocuments) UpdateLeg(_ context.Context, leg domain.Leg) error {
	f.leg = leg
	f.changed++
	return nil
}

// SaveLegCalculation stores a calculation when the input still matches.
func (f *fakeDocuments) SaveLegCalculation(_ context.Context, legID uuid.UUID, input string,
	calculation domain.LegCalculation, pinned bool, _ time.Time) (bool, error) {
	if legID != f.leg.ID || input != f.leg.Input {
		return false, nil
	}
	f.leg.DistanceM, f.leg.DurationS, f.leg.Source = calculation.DistanceM, calculation.DurationS, calculation.Source
	f.leg.Geometry, f.leg.Pinned = calculation.Geometry, pinned
	return true, nil
}

// SaveTranslations keeps what was saved, so a test can see what reached the
// repository.
func (f *fakeDocuments) SaveTranslations(_ context.Context, _ uuid.UUID, _ string,
	translations []domain.Translation) error {
	f.translations = append(f.translations, translations...)
	return nil
}

// SaveTrack keeps the recorded line and its file, as the repository does.
func (f *fakeDocuments) SaveTrack(_ context.Context, track domain.Track, file []byte) (domain.Track, error) {
	f.track, f.trackFile = &track, file
	return track, nil
}

// Track returns the stored track, or not found.
func (f *fakeDocuments) Track(_ context.Context, id uuid.UUID) (domain.Track, error) {
	if f.track == nil || f.track.ID != id {
		return domain.Track{}, domain.ErrNotFound
	}
	return *f.track, nil
}

// TrackFile returns the file of the stored track, or not found.
func (f *fakeDocuments) TrackFile(_ context.Context, id uuid.UUID) (domain.TrackFile, error) {
	if f.track == nil || f.track.ID != id {
		return domain.TrackFile{}, domain.ErrNotFound
	}
	return domain.TrackFile{Name: f.track.OriginalName, Format: f.track.Format, Data: f.trackFile}, nil
}

// DeleteTrack removes the recorded line of the place named, reporting one
// that had none.
func (f *fakeDocuments) DeleteTrack(_ context.Context, itemID uuid.UUID) error {
	if f.track == nil || f.track.ItemID != itemID {
		return domain.ErrNotFound
	}
	f.track = nil
	return nil
}

// fakeRouter answers every leg with a fixed road route and counts the work.
type fakeRouter struct {
	calculated int
	forgotten  int
	// route is the routing the last calculation was asked for.
	route domain.LegRoute
	// alternativesErr fails the next request for alternatives.
	alternativesErr error
}

// Enabled reports a configured provider.
func (f *fakeRouter) Enabled() bool { return true }

// ProviderName names the fake.
func (f *fakeRouter) ProviderName() string { return "fake" }

// Capabilities says the fake can do everything.
func (f *fakeRouter) Capabilities() routing.Capabilities {
	return routing.Capabilities{Shortest: true, Alternatives: true}
}

// Calculate returns 12 km in 15 minutes.
func (f *fakeRouter) Calculate(_ context.Context, _ domain.TravelMode, _, _ *domain.Point,
	route domain.LegRoute) domain.LegCalculation {
	f.calculated++
	f.route = route
	distance, duration := 12000, 900
	return domain.LegCalculation{DistanceM: &distance, DurationS: &duration, Source: domain.LegProvider}
}

// Forget counts forgotten routes.
func (f *fakeRouter) Forget(context.Context, domain.TravelMode, domain.Point, domain.Point, domain.LegRoute) error {
	f.forgotten++
	return nil
}

// Alternatives returns two routes between the ends, a straight one and a
// longer one, or the error it was told to fail with.
func (f *fakeRouter) Alternatives(_ context.Context, mode domain.TravelMode, from, to domain.Point,
	_ domain.RoutePreference) ([]domain.LegCalculation, error) {
	if f.alternativesErr != nil {
		return nil, f.alternativesErr
	}
	if !mode.Routed() {
		return nil, routing.ErrNotRouted
	}
	line := domain.EncodePolyline([]domain.Point{from, to})
	first, firstTime, second, secondTime := 12000, 900, 15000, 1100
	return []domain.LegCalculation{
		{DistanceM: &first, DurationS: &firstTime, Geometry: line, Source: domain.LegProvider},
		{DistanceM: &second, DurationS: &secondTime, Geometry: line, Source: domain.LegProvider},
	}, nil
}

// newDocumentServer builds a server whose reader holds role on the trip that
// owns the fake document.
func newDocumentServer(role domain.TripRole) (*Server, *fakeDocuments) {
	s, docs, _ := newRoutingServer(role)
	return s, docs
}

// newRoutingServer is newDocumentServer that also returns the fake router.
func newRoutingServer(role domain.TripRole) (*Server, *fakeDocuments, *fakeRouter) {
	_, trips := newTripServer(role)
	stayID := uuid.New()
	dayID := uuid.New()
	docs := &fakeDocuments{
		document: domain.Document{ID: uuid.New(), TripID: trips.trip.ID, Kind: domain.DocumentPlan},
		day:      domain.Day{ID: dayID, StartTime: domain.DefaultDayStart, MorningAnchor: true, EveningAnchor: true},
		stay:     domain.Stay{ID: stayID, Name: "Hotel Vík"},
	}
	docs.day.DocumentID = docs.document.ID
	docs.stay.DocumentID = docs.document.ID
	// The trip knows its plan, so the budget reads the same document.
	trips.trip.PlanID = &docs.document.ID
	docs.trips = trips
	docs.place = domain.Item{ID: uuid.New(), DocumentID: docs.document.ID, DayID: &dayID, Kind: domain.ItemPlace,
		Name: "Seljalandsfoss", Category: domain.CategoryNature, VisitMinutes: 60}
	docs.anchor = domain.Item{ID: uuid.New(), DocumentID: docs.document.ID, DayID: &dayID,
		Kind: domain.ItemStayAnchor, Anchor: domain.AnchorEvening, StayID: &stayID}
	docs.leg = domain.Leg{ID: uuid.New(), DocumentID: docs.document.ID, DayID: dayID, FromItemID: docs.place.ID,
		ToItemID: docs.anchor.ID, Mode: domain.ModeCar, Source: domain.LegPending, Input: "car|?|?"}
	router := &fakeRouter{}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:      &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true}},
		Users:     fakeUsers{},
		Trips:     trips,
		Documents: docs,
		Routing:   router,
	})
	return s, docs, router
}

// TestDocumentRoles checks viewers read documents and only editors change them.
func TestDocumentRoles(t *testing.T) {
	for _, role := range []domain.TripRole{domain.RoleViewer, domain.RoleEditor} {
		s, docs := newDocumentServer(role)
		base := "/api/v1/documents/" + docs.document.ID.String()
		if recorder := send(s, http.MethodGet, base, "good", ""); recorder.Code != http.StatusOK {
			t.Errorf("%s reads: %d %s", role, recorder.Code, recorder.Body.String())
		}
		changes := []struct{ method, path, body string }{
			{http.MethodPost, base + "/days", `{"title":"Arrival"}`},
			{http.MethodPatch, "/api/v1/days/" + docs.day.ID.String(), `{"start_time":"08:00"}`},
			{http.MethodPost, "/api/v1/days/" + docs.day.ID.String() + ":duplicate", ""},
			{http.MethodPost, base + "/items", `{"name":"Gljúfrabúi"}`},
			{http.MethodPatch, "/api/v1/items/" + docs.place.ID.String(), `{"visit_minutes":30}`},
			{http.MethodPost, base + "/items:move", `{"item_id":"` + docs.place.ID.String() + `","to_day_id":null}`},
			{http.MethodPost, base + "/stays", `{"name":"Hotel","check_in_date":"2026-06-20","check_out_date":"2026-06-21"}`},
		}
		for _, change := range changes {
			recorder := send(s, change.method, change.path, "good", change.body)
			want := http.StatusForbidden
			if role == domain.RoleEditor {
				want = http.StatusOK
				if change.method == http.MethodPost && recorder.Code == http.StatusCreated {
					want = http.StatusCreated
				}
			}
			if recorder.Code != want {
				t.Errorf("%s %s %s: %d %s", role, change.method, change.path, recorder.Code, recorder.Body.String())
			}
		}
		if role == domain.RoleViewer && docs.changed != 0 {
			t.Errorf("a viewer changed the document %d times", docs.changed)
		}
	}
}

// TestSaveTranslations checks a translation reaches the repository only from
// somebody who may edit, and only when it names a field a report translates.
func TestSaveTranslations(t *testing.T) {
	path := func(docs *fakeDocuments) string {
		return "/api/v1/documents/" + docs.document.ID.String() + "/translations/en"
	}
	body := func(docs *fakeDocuments, field string) string {
		return `{"translations":[{"target_type":"item","target_id":"` + docs.place.ID.String() +
			`","field":"` + field + `","value":" Waterfall "}]}`
	}

	s, docs := newDocumentServer(domain.RoleViewer)
	if recorder := send(s, http.MethodPut, path(docs), "good", body(docs, "name")); recorder.Code != http.StatusForbidden {
		t.Errorf("viewer: %d %s", recorder.Code, recorder.Body.String())
	}

	s, docs = newDocumentServer(domain.RoleEditor)
	recorder := send(s, http.MethodPut, path(docs), "good", body(docs, "address"))
	if recorder.Code != http.StatusUnprocessableEntity || len(docs.translations) != 0 {
		t.Errorf("shared field: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodPut, path(docs), "good", body(docs, "name"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("save: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(docs.translations) != 1 || docs.translations[0].Value != "Waterfall" || docs.translations[0].Lang != "en" {
		t.Errorf("saved %+v", docs.translations)
	}
	want := `"translations":{"en":{"` + docs.place.ID.String() + `":{"name":"Waterfall"}}}`
	if !strings.Contains(recorder.Body.String(), want) {
		t.Errorf("document lacks %s: %s", want, recorder.Body.String())
	}
}

// TestDocumentRules checks stay marks are read-only, a stay mark shows its
// stay, and a removal that needs confirmation says so.
func TestDocumentRules(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleEditor)

	recorder := send(s, http.MethodPatch, "/api/v1/items/"+docs.anchor.ID.String(), "good", `{"name":"x"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("anchor patch: %d", recorder.Code)
	}

	recorder = send(s, http.MethodGet, "/api/v1/documents/"+docs.document.ID.String(), "good", "")
	body := recorder.Body.String()
	for _, want := range []string{`"name":"Hotel Vík"`, `"arrival_minutes":540`, `"end_minutes":600`} {
		if !strings.Contains(body, want) {
			t.Errorf("document lacks %s: %s", want, body)
		}
	}

	recorder = send(s, http.MethodDelete, "/api/v1/days/"+docs.day.ID.String(), "good", "")
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "days_would_be_removed" {
		t.Errorf("unconfirmed delete: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodDelete, "/api/v1/days/"+docs.day.ID.String()+"?confirm=true", "good", "")
	if recorder.Code != http.StatusOK {
		t.Errorf("confirmed delete: %d", recorder.Code)
	}

	recorder = send(s, http.MethodPost, "/api/v1/documents/"+docs.document.ID.String()+"/stays", "good",
		`{"name":"Hotel","check_in_date":"2026-06-20"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("stay without check-out: %d", recorder.Code)
	}
}

// TestLegs checks pending legs are calculated once, typed values win, and a
// day recalculation forgets cached routes.
func TestLegs(t *testing.T) {
	s, docs, router := newRoutingServer(domain.RoleEditor)
	base := "/api/v1/documents/" + docs.document.ID.String()

	recorder := send(s, http.MethodPost, base+"/legs:calculate", "good", "")
	if recorder.Code != http.StatusOK || router.calculated != 1 || docs.leg.Source != domain.LegProvider {
		t.Fatalf("calculate: %d %s calculated=%d", recorder.Code, recorder.Body.String(), router.calculated)
	}
	var document documentResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &document)
	day := document.Days[0]
	if day.Summary.DistanceM != 12000 || day.Summary.TravelMinutes != 15 || len(day.Legs) != 1 ||
		day.Items[1].Schedule.ArrivalMinutes != 540+60+15 {
		t.Errorf("schedule with the leg: %+v", day)
	}

	send(s, http.MethodPost, base+"/legs:calculate", "good", "")
	if router.calculated != 1 {
		t.Errorf("a calculated leg was calculated again: %d", router.calculated)
	}

	legPath := "/api/v1/legs/" + docs.leg.ID.String()
	recorder = send(s, http.MethodPatch, legPath, "good", `{"duration_s":3600,"note":"ferry","mode":"other"}`)
	if recorder.Code != http.StatusOK || docs.leg.ManualDurationS == nil || docs.leg.Mode != domain.ModeOther {
		t.Errorf("update leg: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = send(s, http.MethodPatch, legPath, "good", `{"reset_manual":true}`)
	if recorder.Code != http.StatusOK || docs.leg.ManualDurationS != nil {
		t.Errorf("reset manual: %d", recorder.Code)
	}
	if recorder = send(s, http.MethodPatch, legPath, "good", `{"distance_m":-5}`); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("negative distance: %d", recorder.Code)
	}

	// The fake points are unknown, so nothing is forgotten, but the day's
	// leg is calculated again although it is not pending.
	recorder = send(s, http.MethodPost, "/api/v1/days/"+docs.day.ID.String()+":recalculate", "good", "")
	if recorder.Code != http.StatusOK || router.calculated != 2 {
		t.Errorf("recalculate day: %d calculated=%d", recorder.Code, router.calculated)
	}

	viewer, viewerDocs, _ := newRoutingServer(domain.RoleViewer)
	viewerPath := "/api/v1/documents/" + viewerDocs.document.ID.String() + "/legs:calculate"
	if recorder = send(viewer, http.MethodPost, viewerPath, "good", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("viewer calculates: %d", recorder.Code)
	}
}

// TestRetryEstimatedLegs checks the action that turns a plan of straight lines
// back into roads once the provider that was missing is configured.
func TestRetryEstimatedLegs(t *testing.T) {
	s, docs, router := newRoutingServer(domain.RoleEditor)
	base := "/api/v1/documents/" + docs.document.ID.String()

	// A leg as it is left when the provider was off: an estimate, with the
	// reason recorded, and no longer waiting for anything.
	distance, duration := 9000, 1200
	docs.leg.Source = domain.LegEstimate
	docs.leg.Error = domain.LegErrorProviderDisabled
	docs.leg.DistanceM, docs.leg.DurationS = &distance, &duration

	// The ordinary calculation leaves it alone: it is not pending.
	recorder := send(s, http.MethodPost, base+"/legs:calculate", "good", "")
	if recorder.Code != http.StatusOK || router.calculated != 0 {
		t.Fatalf("calculate touched %d legs: %d %s", router.calculated, recorder.Code, recorder.Body.String())
	}
	document := decodeDocument(t, recorder.Body.Bytes())
	if document.EstimatedLegs != 1 || document.Days[0].Summary.EstimatedLegs != 1 {
		t.Errorf("the estimate was not counted: %d in the document, %d in the day",
			document.EstimatedLegs, document.Days[0].Summary.EstimatedLegs)
	}

	// Retrying asks about exactly that leg.
	recorder = send(s, http.MethodPost, base+"/legs:retry", "good", "")
	if recorder.Code != http.StatusOK || router.calculated != 1 {
		t.Fatalf("retry: %d %s calculated=%d", recorder.Code, recorder.Body.String(), router.calculated)
	}
	if docs.leg.Source != domain.LegProvider {
		t.Errorf("the leg is still %s", docs.leg.Source)
	}
	if document := decodeDocument(t, recorder.Body.Bytes()); document.EstimatedLegs != 0 {
		t.Errorf("%d estimates are still counted", document.EstimatedLegs)
	}

	// A leg with no road between its points is not asked about again: no key
	// makes a road appear.
	docs.leg.Source = domain.LegEstimate
	docs.leg.Error = domain.LegErrorNoRoute
	recorder = send(s, http.MethodPost, base+"/legs:retry", "good", "")
	if recorder.Code != http.StatusOK || router.calculated != 1 {
		t.Errorf("a leg with no route was retried: calculated=%d", router.calculated)
	}
	if document := decodeDocument(t, recorder.Body.Bytes()); document.EstimatedLegs != 0 {
		t.Errorf("a leg with no route is offered for retrying: %d", document.EstimatedLegs)
	}
}

// TestRetryEstimatedLegsNeedsAnEditor checks a viewer cannot spend the
// provider's requests on somebody else's trip.
func TestRetryEstimatedLegsNeedsAnEditor(t *testing.T) {
	s, docs, router := newRoutingServer(domain.RoleViewer)
	docs.leg.Source = domain.LegEstimate
	docs.leg.Error = domain.LegErrorProviderDisabled

	path := "/api/v1/documents/" + docs.document.ID.String() + "/legs:retry"
	if recorder := send(s, http.MethodPost, path, "good", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
	if router.calculated != 0 {
		t.Errorf("a viewer spent %d provider requests", router.calculated)
	}
}

// TestLegRouteChoice checks choosing how a road leg is routed: its preference
// and via points reach the router, the alternatives are listed, a chosen one is
// kept and left alone by later calculations, and a route link is read into via
// points.
func TestLegRouteChoice(t *testing.T) {
	s, docs, router := newRoutingServer(domain.RoleEditor)
	placeLat, placeLng, stayLat, stayLng := 63.6156, -19.9886, 63.4186, -19.006
	docs.place.Lat, docs.place.Lng = &placeLat, &placeLng
	docs.stay.Lat, docs.stay.Lng = &stayLat, &stayLng
	to := domain.Point{Lat: stayLat, Lng: stayLng}
	base := "/api/v1/documents/" + docs.document.ID.String()
	legPath := "/api/v1/legs/" + docs.leg.ID.String()

	recorder := send(s, http.MethodPatch, legPath, "good",
		`{"route_preference":"shortest","via":[{"lat":63.5,"lng":-19.5}]}`)
	if recorder.Code != http.StatusOK || docs.leg.Preference != domain.RouteShortest || len(docs.leg.Via) != 1 {
		t.Fatalf("set the routing: %d %s", recorder.Code, recorder.Body.String())
	}
	send(s, http.MethodPost, base+"/legs:calculate", "good", "")
	if router.route.Preference != domain.RouteShortest || len(router.route.Via) != 1 {
		t.Errorf("the routing reached the router as %+v", router.route)
	}
	for _, body := range []string{`{"route_preference":"scenic"}`, `{"via":[{"lat":91,"lng":0}]}`} {
		if recorder = send(s, http.MethodPatch, legPath, "good", body); recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", body, recorder.Code)
		}
	}

	recorder = send(s, http.MethodPost, legPath+":alternatives", "good", "")
	var routes routeListResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &routes)
	if recorder.Code != http.StatusOK || len(routes.Items) != 2 || routes.Items[1].DistanceM != 15000 {
		t.Fatalf("alternatives: %d %s", recorder.Code, recorder.Body.String())
	}
	chosen, _ := json.Marshal(routes.Items[1])

	// A leg routed through points of its own takes the route they make.
	if recorder = send(s, http.MethodPost, legPath+":route", "good", string(chosen)); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a route chosen for a leg with via points: %d", recorder.Code)
	}
	send(s, http.MethodPatch, legPath, "good", `{"via":null}`)
	recorder = send(s, http.MethodPost, legPath+":route", "good", string(chosen))
	if recorder.Code != http.StatusOK || !docs.leg.Pinned || *docs.leg.DistanceM != 15000 {
		t.Fatalf("pin the route: %d %s %+v", recorder.Code, recorder.Body.String(), docs.leg)
	}
	elsewhere, _ := json.Marshal(routeBody{DistanceM: 1, DurationS: 1,
		Geometry: domain.EncodePolyline([]domain.Point{{Lat: 64.1, Lng: -21.9}, to})})
	if recorder = send(s, http.MethodPost, legPath+":route", "good", string(elsewhere)); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a route from elsewhere: %d", recorder.Code)
	}

	// A pinned leg waiting after a change is not calculated over its choice;
	// an explicit recalculation replaces it.
	docs.leg.Source = domain.LegPending
	calculated := router.calculated
	send(s, http.MethodPost, base+"/legs:calculate", "good", "")
	if router.calculated != calculated {
		t.Errorf("a pinned leg was calculated again")
	}
	send(s, http.MethodPost, legPath+":recalculate", "good", "")
	if router.calculated != calculated+1 || docs.leg.Pinned {
		t.Errorf("a recalculation kept the choice: calculated=%d pinned=%v", router.calculated, docs.leg.Pinned)
	}

	link := "https://www.google.com/maps/dir/A/B/data=!4m19!4m18!1m10!1m1!1s0x0:0x0!2m2!1d-19.9886!2d63.6156" +
		"!3m4!1m2!1d-19.5!2d63.5!3s0x0:0x0!1m5!1m1!1s0x0:0x0!2m2!1d-19.006!2d63.4186!3e0"
	recorder = send(s, http.MethodPost, legPath+":google-link", "good", `{"url":"`+link+`"}`)
	var via viaListResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &via)
	if recorder.Code != http.StatusOK || len(via.Via) != 1 || via.Via[0].Lat != 63.5 {
		t.Errorf("read a route link: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder = send(s, http.MethodPost, legPath+":google-link", "good", `{"url":"https://example.com"}`); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a foreign link: %d", recorder.Code)
	}

	docs.leg.Mode = domain.ModeFlight
	if recorder = send(s, http.MethodPost, legPath+":alternatives", "good", ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("alternatives of a flight: %d", recorder.Code)
	}
	docs.leg.Mode = domain.ModeCar
	router.alternativesErr = routing.ErrDisabled
	if recorder = send(s, http.MethodPost, legPath+":alternatives", "good", ""); recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("alternatives without a provider: %d", recorder.Code)
	}

	viewer, viewerDocs, _ := newRoutingServer(domain.RoleViewer)
	if recorder = send(viewer, http.MethodPost, "/api/v1/legs/"+viewerDocs.leg.ID.String()+":alternatives", "good", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer lists alternatives: %d", recorder.Code)
	}
}

// TestLinkViaTrimsTheEnds checks which positions of a link become via points:
// a labelled link loses its start and end stops, one read by coordinates alone
// the positions near the leg's own ends.
func TestLinkViaTrimsTheEnds(t *testing.T) {
	from, to := domain.Point{Lat: 64.1466, Lng: -21.9426}, domain.Point{Lat: 63.4186, Lng: -19.006}
	middle := domain.Point{Lat: 63.9, Lng: -20.5}
	labelled := googlelink.Route{Labelled: true, Points: []googlelink.Point{
		{Point: domain.Point{Lat: 60, Lng: 10}, Stop: true}, {Point: middle}, {Point: to, Stop: true},
	}}
	if via := linkVia(labelled, from, to); len(via) != 1 || via[0] != middle {
		t.Errorf("labelled: %+v", via)
	}
	near := domain.Point{Lat: from.Lat + 0.005, Lng: from.Lng}
	plain := googlelink.Route{Points: []googlelink.Point{
		{Point: near, Stop: true}, {Point: middle, Stop: true}, {Point: to, Stop: true},
	}}
	if via := linkVia(plain, from, to); len(via) != 1 || via[0] != middle {
		t.Errorf("by coordinates: %+v", via)
	}
}
