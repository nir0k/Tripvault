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
)

// fakeTrips serves one trip on which the reader holds a fixed role, and
// records what the handlers stored.
type fakeTrips struct {
	trip    domain.TripSummary
	updated *domain.Trip
	deleted bool
	// mediaKeys are the files the deletion hands back for removal.
	mediaKeys []string
	created   domain.DocumentKind
	// shares holds the live links by the hash of their token.
	shares map[string]domain.ShareLink
	// report is the report trip written from the fake plan, read back by Get.
	report *domain.TripSummary
	// visits counts the openings recorded against each link.
	visits map[uuid.UUID]int
	// listed is the filter the last list was read with.
	listed domain.TripFilter
}

// Create echoes the trip back as its owner's.
func (f *fakeTrips) Create(_ context.Context, trip domain.Trip) (domain.TripSummary, error) {
	f.created = trip.Kind
	return domain.TripSummary{Trip: trip, Role: domain.RoleOwner}, nil
}

// Get returns the trip when asked for it, and not found otherwise, as the
// repository does for a trip the reader has no access to.
func (f *fakeTrips) Get(_ context.Context, tripID, _ uuid.UUID) (domain.TripSummary, error) {
	if f.report != nil && tripID == f.report.ID {
		return *f.report, nil
	}
	if tripID != f.trip.ID || f.trip.Role == "" {
		return domain.TripSummary{}, domain.ErrNotFound
	}
	if f.updated != nil {
		return domain.TripSummary{Trip: *f.updated, Role: f.trip.Role}, nil
	}
	return f.trip, nil
}

// List records the filter and returns the one trip.
func (f *fakeTrips) List(_ context.Context, _ uuid.UUID, filter domain.TripFilter, _ time.Time) (domain.TripPage, error) {
	f.listed = filter
	return domain.TripPage{Items: []domain.TripSummary{f.trip}}, nil
}

// Years returns none.
func (f *fakeTrips) Years(context.Context, uuid.UUID, domain.DocumentKind) ([]int, error) {
	return nil, nil
}

// Update records the stored trip.
func (f *fakeTrips) Update(_ context.Context, trip domain.Trip, _ bool) error {
	f.updated = &trip
	return nil
}

// Delete records the deletion.
func (f *fakeTrips) Delete(context.Context, uuid.UUID) ([]string, error) {
	f.deleted = true
	return f.mediaKeys, nil
}

// Members returns nobody.
func (f *fakeTrips) Members(context.Context, uuid.UUID) ([]domain.TripMember, error) { return nil, nil }

// AddMember reports the person already has access.
func (f *fakeTrips) AddMember(context.Context, uuid.UUID, uuid.UUID, domain.TripRole) (domain.TripMember, error) {
	return domain.TripMember{}, domain.ErrAlreadyMember
}

// UpdateMember echoes the role.
func (f *fakeTrips) UpdateMember(_ context.Context, _, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error) {
	return domain.TripMember{User: domain.TripUser{ID: userID}, Role: role}, nil
}

// RemoveMember succeeds.
func (f *fakeTrips) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// CreateShareLink stores a link under the hash of its token.
func (f *fakeTrips) CreateShareLink(_ context.Context, link domain.ShareLink, tokenHash []byte) error {
	if f.shares == nil {
		f.shares = map[string]domain.ShareLink{}
	}
	f.shares[string(tokenHash)] = link
	return nil
}

// ShareLinks lists the stored links.
func (f *fakeTrips) ShareLinks(context.Context, uuid.UUID) ([]domain.ShareLink, error) {
	links := make([]domain.ShareLink, 0, len(f.shares))
	for _, link := range f.shares {
		links = append(links, link)
	}
	return links, nil
}

// ShareLinkTrip finds the trip of a stored link.
func (f *fakeTrips) ShareLinkTrip(_ context.Context, linkID uuid.UUID) (uuid.UUID, error) {
	for _, link := range f.shares {
		if link.ID == linkID {
			return link.TripID, nil
		}
	}
	return uuid.Nil, domain.ErrNotFound
}

// RevokeShareLink forgets a stored link, so its token stops working.
func (f *fakeTrips) RevokeShareLink(_ context.Context, tripID, linkID uuid.UUID, _ time.Time) error {
	for hash, link := range f.shares {
		if link.ID == linkID && link.TripID == tripID {
			delete(f.shares, hash)
			return nil
		}
	}
	return domain.ErrNotFound
}

// ShareAccess resolves a token hash to its link and the fake trip.
func (f *fakeTrips) ShareAccess(_ context.Context, tokenHash []byte, at time.Time) (domain.ShareAccess, error) {
	link, ok := f.shares[string(tokenHash)]
	if !ok || !link.Active(at) {
		return domain.ShareAccess{}, domain.ErrNotFound
	}
	// A link grants no role, exactly as the repository reports it.
	trip := f.trip
	trip.Role = ""
	return domain.ShareAccess{Link: link, Trip: trip}, nil
}

// RecordShareVisit counts an opening of a link.
func (f *fakeTrips) RecordShareVisit(_ context.Context, linkID uuid.UUID, _ time.Time) error {
	if f.visits == nil {
		f.visits = map[uuid.UUID]int{}
	}
	f.visits[linkID]++
	return nil
}

// newTripServer builds a server where the token "good" reads trips with role.
func newTripServer(role domain.TripRole) (*Server, *fakeTrips) {
	budget := domain.Money(300000)
	start := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	trips := &fakeTrips{trip: domain.TripSummary{
		Trip: domain.Trip{ID: uuid.New(), Kind: domain.DocumentPlan, Title: "Iceland", Timezone: "UTC",
			Currency: "EUR", Travelers: 2,
			StartDate: &start, EndDate: &end, Budget: &budget},
		// The repository always joins the owner, so the fake carries one too.
		Owner: domain.TripUser{ID: uuid.New(), DisplayName: "Ada", Email: "ada@example.com"},
		Role:  role,
	}}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:  &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "ISK"}},
		Users: fakeUsers{},
		Trips: trips,
	})
	return s, trips
}

// TestTripRolesAreEnforced walks the role table through the HTTP routes.
func TestTripRolesAreEnforced(t *testing.T) {
	type call struct{ method, path, body string }
	calls := func(id string) map[string]call {
		base := "/api/v1/trips/" + id
		return map[string]call{
			"view":    {http.MethodGet, base, ""},
			"members": {http.MethodGet, base + "/members", ""},
			"edit":    {http.MethodPatch, base, `{"travelers":3}`},
			"add":     {http.MethodPost, base + "/members", `{"user_id":"` + uuid.NewString() + `","role":"viewer"}`},
			"role":    {http.MethodPatch, base + "/members/" + uuid.NewString(), `{"role":"editor"}`},
			"remove":  {http.MethodDelete, base + "/members/" + uuid.NewString(), ""},
			"delete":  {http.MethodDelete, base, ""},
		}
	}
	allowed := map[domain.TripRole][]string{
		domain.RoleOwner:  {"view", "members", "edit", "add", "role", "remove", "delete"},
		domain.RoleEditor: {"view", "members", "edit"},
		domain.RoleViewer: {"view", "members"},
		"":                {},
	}

	for role, permitted := range allowed {
		s, trips := newTripServer(role)
		for name, c := range calls(trips.trip.ID.String()) {
			recorder := send(s, c.method, c.path, "good", c.body)
			isPermitted := strings.Contains(" "+strings.Join(permitted, " ")+" ", " "+name+" ")
			switch {
			case role == "":
				if recorder.Code != http.StatusNotFound {
					t.Errorf("stranger %s: %d, want 404", name, recorder.Code)
				}
			case isPermitted:
				// The fake refuses adding a member as a duplicate: reaching the
				// store is what shows the role check passed.
				if recorder.Code >= 400 && errorCode(t, recorder) != "already_member" {
					t.Errorf("%s %s: %d %s", role, name, recorder.Code, recorder.Body.String())
				}
			default:
				if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "forbidden" {
					t.Errorf("%s %s: %d, want 403", role, name, recorder.Code)
				}
			}
		}
	}
}

// TestTripListSort checks the list is read in the order asked for, by relevance
// when none is, and that an unknown order is refused before the store is asked.
func TestTripListSort(t *testing.T) {
	s, trips := newTripServer(domain.RoleOwner)

	recorder := send(s, http.MethodGet, "/api/v1/trips?kind=report", "good", "")
	if recorder.Code != http.StatusOK || trips.listed.Sort != domain.SortRelevance {
		t.Errorf("default order: %d %q", recorder.Code, trips.listed.Sort)
	}

	recorder = send(s, http.MethodGet, "/api/v1/trips?kind=report&sort=updated&limit=12", "good", "")
	if recorder.Code != http.StatusOK || trips.listed.Sort != domain.SortUpdated || trips.listed.Limit != 12 {
		t.Errorf("by update: %d %+v", recorder.Code, trips.listed)
	}

	trips.listed = domain.TripFilter{}
	recorder = send(s, http.MethodGet, "/api/v1/trips?sort=title", "good", "")
	if recorder.Code != http.StatusUnprocessableEntity || errorCode(t, recorder) != "validation_failed" ||
		trips.listed.Sort != "" {
		t.Errorf("unknown order: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestTripCreateAndUpdate checks defaults on creation, the null-clears rule of
// the partial update and that the period cannot be given away.
func TestTripCreateAndUpdate(t *testing.T) {
	s, trips := newTripServer(domain.RoleOwner)

	recorder := send(s, http.MethodPost, "/api/v1/trips", "good",
		`{"title":"Norway","start_date":"2026-07-01","end_date":"2026-07-10","budget_amount":"2500.5","kind":"plan"}`)
	var created tripResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &created)
	if recorder.Code != http.StatusCreated || created.Currency != "ISK" || created.Travelers != 1 ||
		created.BudgetAmount == nil || *created.BudgetAmount != "2500.50" || created.DayCount == nil ||
		*created.DayCount != 10 || trips.created != domain.DocumentPlan {
		t.Errorf("create: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPost, "/api/v1/trips", "good", `{"title":"Norway","kind":"budget"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown document: %d", recorder.Code)
	}

	recorder = send(s, http.MethodPost, "/api/v1/trips", "good", `{"title":"Norway"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("trip without a period: %d %s", recorder.Code, recorder.Body.String())
	}

	path := "/api/v1/trips/" + trips.trip.ID.String()
	recorder = send(s, http.MethodPatch, path, "good", `{"budget_amount":null,"title":"Iceland 2026"}`)
	if recorder.Code != http.StatusOK || trips.updated == nil || trips.updated.Budget != nil ||
		trips.updated.Title != "Iceland 2026" || trips.updated.Travelers != 2 {
		t.Errorf("clear budget: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, path, "good", `{"start_date":null}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("cleared period: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, path, "good", `{"start_date":"2026-06-28"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("start after the end: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, path, "good", `{"timezone":null}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("cleared zone: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodGet, "/api/v1/trips?limit=ten", "good", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad limit: %d", recorder.Code)
	}
	recorder = send(s, http.MethodGet, "/api/v1/trips?scope=owned", "good", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"next_cursor":null`) {
		t.Errorf("list: %d %s", recorder.Code, recorder.Body.String())
	}
}
