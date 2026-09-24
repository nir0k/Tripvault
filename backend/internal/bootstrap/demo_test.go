package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakeUsers holds the accounts seeding created, starting with an administrator.
type fakeUsers struct {
	accounts []domain.User
}

// Count reports how many accounts exist.
func (f *fakeUsers) Count(context.Context) (int64, error) { return int64(len(f.accounts)), nil }

// Create stores an account.
func (f *fakeUsers) Create(_ context.Context, user domain.User) (domain.User, error) {
	f.accounts = append(f.accounts, user)
	return user, nil
}

// List returns the stored accounts.
func (f *fakeUsers) List(context.Context) ([]domain.User, error) { return f.accounts, nil }

// fakeTrips records the trips, members and links the examples wrote.
type fakeTrips struct {
	trips   []domain.Trip
	members int
	links   int
	// documents is where a report trip's document was written, which Get
	// reads back as the repository would.
	documents *fakeDocuments
}

// Create records a trip and answers with a plan for it.
func (f *fakeTrips) Create(_ context.Context, trip domain.Trip) (domain.TripSummary, error) {
	f.trips = append(f.trips, trip)
	planID := uuid.New()
	return domain.TripSummary{Trip: trip, PlanID: &planID, Role: domain.RoleOwner}, nil
}

// Get answers with a report trip and the document CreateReport wrote for it.
func (f *fakeTrips) Get(_ context.Context, tripID, _ uuid.UUID) (domain.TripSummary, error) {
	documentID, ok := f.documents.reports[tripID]
	if !ok {
		return domain.TripSummary{}, domain.ErrNotFound
	}
	return domain.TripSummary{Trip: domain.Trip{ID: tripID, Kind: domain.DocumentReport}, ReportID: &documentID,
		Role: domain.RoleOwner}, nil
}

// AddMember counts the people given access.
func (f *fakeTrips) AddMember(_ context.Context, _, userID uuid.UUID,
	role domain.TripRole) (domain.TripMember, error) {
	f.members++
	return domain.TripMember{User: domain.TripUser{ID: userID}, Role: role}, nil
}

// CreateShareLink counts the links handed out.
func (f *fakeTrips) CreateShareLink(context.Context, domain.ShareLink, []byte) error {
	f.links++
	return nil
}

// AdminList reports the trips written so far, which is what tells seeding that
// the instance already has examples.
func (f *fakeTrips) AdminList(context.Context, domain.AdminTripFilter) (domain.TripPage, error) {
	items := make([]domain.TripSummary, 0, len(f.trips))
	for _, trip := range f.trips {
		items = append(items, domain.TripSummary{Trip: trip})
	}
	return domain.TripPage{Items: items}, nil
}

// fakeDoc is one document of the fake store: what was written into it, plus the
// two things the service derives rather than stores - a stay mark on each day,
// and a leg leaving every element of it. The examples name both, because a leg is
// found by the name of the element it starts at, which may be a stay.
type fakeDoc struct {
	kind     domain.DocumentKind
	days     []domain.Day
	places   []domain.Item
	anchors  []domain.Item
	stays    []domain.Stay
	expenses []domain.Expense
	legs     []domain.Leg
	intro    string
	derived  bool
	// translations are the fields written in the report's second language.
	translations []domain.Translation
}

// derive places the marks and draws the legs, once. The identifiers have to
// survive later calls, because a leg is read in one and written back in the next.
func (d *fakeDoc) derive(documentID uuid.UUID) {
	if d.derived || len(d.places) == 0 {
		return
	}
	d.derived = true
	for _, day := range d.days {
		dayID := day.ID
		items := make([]domain.Item, 0, len(d.places)+len(d.stays))
		for _, stay := range d.stays {
			stayID := stay.ID
			anchor := domain.Item{ID: uuid.New(), DocumentID: documentID, DayID: &dayID,
				Kind: domain.ItemStayAnchor, Anchor: domain.AnchorEvening, StayID: &stayID}
			d.anchors = append(d.anchors, anchor)
			items = append(items, anchor)
		}
		for _, place := range d.places {
			if place.DayID != nil && *place.DayID == dayID {
				items = append(items, place)
			}
		}
		for _, item := range items {
			d.legs = append(d.legs, domain.Leg{ID: uuid.New(), DocumentID: documentID, DayID: dayID,
				FromItemID: item.ID, ToItemID: uuid.New(), Mode: domain.ModeCar})
		}
	}
}

// fakeDocuments keeps a document per identifier, the way the real store does, so
// the second example does not write into the first one's days.
type fakeDocuments struct {
	docs map[uuid.UUID]*fakeDoc
	// reports maps each report trip to its document.
	reports map[uuid.UUID]uuid.UUID
}

// doc finds a document, giving a new one the days a fresh plan has.
func (f *fakeDocuments) doc(id uuid.UUID) *fakeDoc {
	if f.docs == nil {
		f.docs = map[uuid.UUID]*fakeDoc{}
	}
	if found, ok := f.docs[id]; ok {
		return found
	}
	// A plan is created with one day per date of the trip; four covers the
	// longest example.
	created := &fakeDoc{kind: domain.DocumentPlan}
	for position := range 4 {
		created.days = append(created.days, domain.Day{ID: uuid.New(), DocumentID: id, Position: position})
	}
	f.docs[id] = created
	return created
}

// Content answers with the document as it now stands.
func (f *fakeDocuments) Content(_ context.Context, id uuid.UUID) (domain.DocumentContent, error) {
	doc := f.doc(id)
	doc.derive(id)
	return domain.DocumentContent{
		Document: domain.Document{ID: id, Kind: doc.kind},
		Days:     doc.days, Items: append(doc.places, doc.anchors...), Stays: doc.stays,
		Legs: doc.legs, Expenses: doc.expenses,
	}, nil
}

// CreateReport copies a plan into a new report trip, as the repository does.
func (f *fakeDocuments) CreateReport(_ context.Context, trip domain.Trip, planID uuid.UUID) error {
	reportID := uuid.New()
	report := &fakeDoc{kind: domain.DocumentReport}
	{
		plan := f.doc(planID)
		for _, day := range plan.days {
			report.days = append(report.days, domain.Day{ID: uuid.New(), DocumentID: reportID,
				Position: day.Position, Title: day.Title, NotesMD: day.NotesMD})
		}
		for index, place := range plan.places {
			copied := place
			copied.ID, copied.DocumentID = uuid.New(), reportID
			copied.Status = domain.StatusVisited
			if place.DayID != nil {
				dayID := report.days[index%len(report.days)].ID
				copied.DayID = &dayID
			}
			report.places = append(report.places, copied)
		}
		for _, stay := range plan.stays {
			copied := stay
			copied.ID, copied.DocumentID = uuid.New(), reportID
			report.stays = append(report.stays, copied)
		}
		for _, expense := range plan.expenses {
			copied := expense
			copied.ID, copied.DocumentID = uuid.New(), reportID
			report.expenses = append(report.expenses, copied)
		}
	}
	if f.docs == nil {
		f.docs = map[uuid.UUID]*fakeDoc{}
	}
	f.docs[reportID] = report
	if f.reports == nil {
		f.reports = map[uuid.UUID]uuid.UUID{}
	}
	f.reports[trip.ID] = reportID
	return nil
}

// UpdateDocument stores the words around the days.
func (f *fakeDocuments) UpdateDocument(_ context.Context, id uuid.UUID, intro, _ string) error {
	f.doc(id).intro = intro
	return nil
}

// UpdateDay stores a day as it now stands.
func (f *fakeDocuments) UpdateDay(_ context.Context, day domain.Day) error {
	doc := f.doc(day.DocumentID)
	for index := range doc.days {
		if doc.days[index].ID == day.ID {
			doc.days[index] = day
		}
	}
	return nil
}

// CreatePlace stores a place.
func (f *fakeDocuments) CreatePlace(_ context.Context, place domain.Item, _ *int) error {
	doc := f.doc(place.DocumentID)
	doc.places = append(doc.places, place)
	return nil
}

// UpdatePlace stores a place as it now stands.
func (f *fakeDocuments) UpdatePlace(_ context.Context, place domain.Item) error {
	doc := f.doc(place.DocumentID)
	for index := range doc.places {
		if doc.places[index].ID == place.ID {
			doc.places[index] = place
		}
	}
	return nil
}

// CreateStay stores a place to sleep.
func (f *fakeDocuments) CreateStay(_ context.Context, stay domain.Stay) error {
	doc := f.doc(stay.DocumentID)
	doc.stays = append(doc.stays, stay)
	return nil
}

// CreateExpense stores a separate cost.
func (f *fakeDocuments) CreateExpense(_ context.Context, expense domain.Expense) error {
	doc := f.doc(expense.DocumentID)
	doc.expenses = append(doc.expenses, expense)
	return nil
}

// UpdateStay stores a place to sleep as it now stands.
func (f *fakeDocuments) UpdateStay(_ context.Context, stay domain.Stay) error {
	doc := f.doc(stay.DocumentID)
	for index := range doc.stays {
		if doc.stays[index].ID == stay.ID {
			doc.stays[index] = stay
		}
	}
	return nil
}

// UpdateExpense stores a separate cost as it now stands.
func (f *fakeDocuments) UpdateExpense(_ context.Context, expense domain.Expense) error {
	doc := f.doc(expense.DocumentID)
	for index := range doc.expenses {
		if doc.expenses[index].ID == expense.ID {
			doc.expenses[index] = expense
		}
	}
	return nil
}

// UpdateLeg stores a leg as it now stands.
func (f *fakeDocuments) UpdateLeg(_ context.Context, leg domain.Leg) error {
	doc := f.doc(leg.DocumentID)
	for index := range doc.legs {
		if doc.legs[index].ID == leg.ID {
			doc.legs[index] = leg
		}
	}
	return nil
}

// SaveTranslations keeps the translated fields of a report.
func (f *fakeDocuments) SaveTranslations(_ context.Context, id uuid.UUID, _ string,
	translations []domain.Translation) error {
	doc := f.doc(id)
	doc.translations = append(doc.translations, translations...)
	return nil
}

// counts adds up what was written across every document.
func (f *fakeDocuments) counts() (reports, intros, stays, expenses, marked, unplanned int) {
	for _, doc := range f.docs {
		if doc.kind == domain.DocumentReport {
			reports++
		}
		if doc.intro != "" {
			intros++
		}
		stays += len(doc.stays)
		expenses += len(doc.expenses)
		for _, place := range doc.places {
			switch place.Status {
			case domain.StatusVisited, domain.StatusSkipped:
				marked++
			case domain.StatusUnplanned:
				unplanned++
			}
		}
	}
	return reports, intros, stays, expenses, marked, unplanned
}

// paid counts the stays and separate expenses of a report that say what they
// really cost.
func (f *fakeDocuments) paid() (stays, expenses int) {
	for _, doc := range f.docs {
		if doc.kind != domain.DocumentReport {
			continue
		}
		for _, stay := range doc.stays {
			if stay.ActualCost != nil {
				stays++
			}
		}
		for _, expense := range doc.expenses {
			if expense.Actual != nil {
				expenses++
			}
		}
	}
	return stays, expenses
}

// newDemoStores builds the three fakes with an administrator already in place and
// a leg leaving every element the examples name, since the real service draws
// those itself.
func newDemoStores() (*fakeUsers, *fakeTrips, *fakeDocuments) {
	users := &fakeUsers{accounts: []domain.User{{
		ID: uuid.New(), Email: "admin@example.com", DisplayName: "Administrator", IsAdmin: true, IsActive: true,
	}}}
	documents := &fakeDocuments{}
	return users, &fakeTrips{documents: documents}, documents
}

// discardLogger keeps the test output clean.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSeedDemoIsIdempotent checks the examples are created once: a restart of the
// service must not double them.
func TestSeedDemoIsIdempotent(t *testing.T) {
	users, trips, documents := newDemoStores()

	if err := SeedDemo(t.Context(), users, trips, documents, discardLogger()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(trips.trips) != 2 {
		t.Fatalf("%d example trips, want 2", len(trips.trips))
	}

	if err := SeedDemo(t.Context(), users, trips, documents, discardLogger()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(trips.trips) != 2 {
		t.Errorf("a second run brought the total to %d trips", len(trips.trips))
	}
	if len(users.accounts) != 3 {
		t.Errorf("%d accounts, want the administrator and two more", len(users.accounts))
	}
}

// TestSeedDemoFillsBothTrips checks the pair the stand is meant to show: one trip
// written up and one still only planned, both with their content.
func TestSeedDemoFillsBothTrips(t *testing.T) {
	users, trips, documents := newDemoStores()
	if err := SeedDemo(t.Context(), users, trips, documents, discardLogger()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reports, intros, stays, expenses, marked, unplanned := documents.counts()
	if reports != 1 {
		t.Errorf("%d reports, want one - the other trip is only planned", reports)
	}
	if intros != 1 {
		t.Errorf("%d documents got an introduction, want one", intros)
	}
	// An editor and a viewer on each of the two plans and on the report, and a
	// link for each of the three.
	if trips.members != 6 || trips.links != 3 {
		t.Errorf("%d members and %d links", trips.members, trips.links)
	}
	if stays == 0 || expenses == 0 {
		t.Errorf("%d stays and %d separate expenses", stays, expenses)
	}
	// Only the travelled trip's places say how they turned out; a plan may not
	// carry that at all.
	if marked == 0 {
		t.Error("no place was marked visited or skipped")
	}
	// The accommodation and the car hire are most of what a trip costs, so the
	// report has to record what they came to; otherwise its total is nonsense.
	if paidStays, paidExpenses := documents.paid(); paidStays == 0 || paidExpenses == 0 {
		t.Errorf("%d stays and %d expenses carry what was really spent", paidStays, paidExpenses)
	}
	if unplanned != 1 {
		t.Errorf("%d places added on the way, want one", unplanned)
	}
}

// TestSeedDemoTranslatesTheReport checks the example report comes in two
// languages: the trip's own title, the words around the days and some places.
func TestSeedDemoTranslatesTheReport(t *testing.T) {
	users, trips, documents := newDemoStores()
	if err := SeedDemo(t.Context(), users, trips, documents, discardLogger()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	targets := map[domain.TranslationTarget]int{}
	for _, doc := range documents.docs {
		if doc.kind != domain.DocumentReport && len(doc.translations) > 0 {
			t.Errorf("a plan was given %d translations", len(doc.translations))
		}
		for _, translation := range doc.translations {
			if translation.Lang != "ru" {
				t.Errorf("translation into %q, want ru", translation.Lang)
			}
			targets[translation.Target]++
		}
	}
	for _, target := range []domain.TranslationTarget{domain.TranslateTrip, domain.TranslateDocument,
		domain.TranslateDay, domain.TranslateItem} {
		if targets[target] == 0 {
			t.Errorf("nothing of the kind %q was translated", target)
		}
	}
}
