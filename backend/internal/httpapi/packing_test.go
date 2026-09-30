package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakePacking keeps one list in memory, as far as the handlers need it.
type fakePacking struct {
	list domain.PackingList
	// moved is where the last move put an item.
	moved *uuid.UUID
	reset bool
}

// List returns the whole list.
func (f *fakePacking) List(context.Context, uuid.UUID) (domain.PackingList, error) {
	return f.list, nil
}

// Category finds a category by its identifier.
func (f *fakePacking) Category(_ context.Context, id uuid.UUID) (domain.PackingCategory, error) {
	for _, category := range f.list.Categories {
		if category.ID == id {
			return category, nil
		}
	}
	return domain.PackingCategory{}, domain.ErrNotFound
}

// Item finds an item by its identifier.
func (f *fakePacking) Item(_ context.Context, id uuid.UUID) (domain.PackingItem, error) {
	for _, item := range f.list.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.PackingItem{}, domain.ErrNotFound
}

// CreateCategory appends a category, in the colour of its place when it names none.
func (f *fakePacking) CreateCategory(_ context.Context, category domain.PackingCategory) error {
	if category.Color == "" {
		category.Color = domain.DefaultPackingColor(len(f.list.Categories))
	}
	f.list.Categories = append(f.list.Categories, category)
	return nil
}

// AddSections merges the sections into the list as the repository does.
func (f *fakePacking) AddSections(_ context.Context, _ uuid.UUID, sections []domain.PackingSection) error {
	categories, items := f.list.Merge(sections)
	f.list.Categories = append(f.list.Categories, categories...)
	f.list.Items = append(f.list.Items, items...)
	return nil
}

// UpdateCategory replaces a category.
func (f *fakePacking) UpdateCategory(_ context.Context, category domain.PackingCategory) error {
	for index := range f.list.Categories {
		if f.list.Categories[index].ID == category.ID {
			f.list.Categories[index] = category
		}
	}
	return nil
}

// DeleteCategory is not needed by these tests.
func (f *fakePacking) DeleteCategory(context.Context, domain.PackingCategory) error { return nil }

// ReorderCategories is not needed by these tests.
func (f *fakePacking) ReorderCategories(context.Context, uuid.UUID, []uuid.UUID) error { return nil }

// CreateItem appends an item.
func (f *fakePacking) CreateItem(_ context.Context, item domain.PackingItem) error {
	f.list.Items = append(f.list.Items, item)
	return nil
}

// UpdateItem replaces an item.
func (f *fakePacking) UpdateItem(_ context.Context, item domain.PackingItem) error {
	for index := range f.list.Items {
		if f.list.Items[index].ID == item.ID {
			f.list.Items[index] = item
		}
	}
	return nil
}

// MoveItem records the category an item was moved to.
func (f *fakePacking) MoveItem(_ context.Context, _ domain.PackingItem, categoryID *uuid.UUID, _ int) error {
	f.moved = categoryID
	return nil
}

// DeleteItem is not needed by these tests.
func (f *fakePacking) DeleteItem(context.Context, uuid.UUID) error { return nil }

// ResetPacked records the reset.
func (f *fakePacking) ResetPacked(context.Context, uuid.UUID) error {
	f.reset = true
	return nil
}

// newPackingServer builds a server over a plan with an empty list.
func newPackingServer(role domain.TripRole) (*Server, *fakeTrips, *fakePacking) {
	_, trips := newTripServer(role)
	packing := &fakePacking{}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:    &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true}},
		Users:   fakeUsers{},
		Trips:   trips,
		Packing: packing,
	})
	return s, trips, packing
}

// decodePacking reads a packing list response, failing the test on anything else.
func decodePacking(t *testing.T, body []byte) packingListResponse {
	t.Helper()
	var list packingListResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("response %q is not a packing list: %v", body, err)
	}
	return list
}

// TestPackingListOverHTTP checks a category and an item are added, the item is
// ticked and moved, and a bringer must be a member.
func TestPackingListOverHTTP(t *testing.T) {
	s, trips, packing := newPackingServer(domain.RoleEditor)
	base := "/api/v1/trips/" + trips.trip.ID.String() + "/packing"

	recorder := send(s, http.MethodPost, base+"/categories", "good", `{"name":"  Documents "}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add a category: %d %s", recorder.Code, recorder.Body.String())
	}
	list := decodePacking(t, recorder.Body.Bytes())
	if len(list.Categories) != 1 || list.Categories[0].Name != "Documents" ||
		list.Categories[0].Color != string(domain.TagColors[0]) || list.Categories[0].Icon != "other" {
		t.Fatalf("categories %+v", list.Categories)
	}
	category := list.Categories[0].ID

	// A change names only what it changes; a colour must be one of the palette.
	recorder = send(s, http.MethodPatch, "/api/v1/packing-categories/"+category, "good", `{"color":"teal","icon":"documents"}`)
	list = decodePacking(t, recorder.Body.Bytes())
	if got := list.Categories[0]; got.Name != "Documents" || got.Color != "teal" || got.Icon != "documents" {
		t.Errorf("after the change: %+v", got)
	}
	for _, bad := range []string{`{"color":""}`, `{"color":"#fff"}`, `{"icon":"rocket"}`} {
		recorder = send(s, http.MethodPatch, "/api/v1/packing-categories/"+category, "good", bad)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", bad, recorder.Code, recorder.Body.String())
		}
	}

	member := uuid.New()
	trips.members = []domain.TripMember{{User: domain.TripUser{ID: member, DisplayName: "Ada"}}}
	body := `{"category_id":"` + category + `","name":"Passport","bringer_id":"` + member.String() + `"}`
	recorder = send(s, http.MethodPost, base+"/items", "good", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add an item: %d %s", recorder.Code, recorder.Body.String())
	}
	list = decodePacking(t, recorder.Body.Bytes())
	item := list.Items[0]
	if item.Quantity != 1 || item.CategoryID == nil || *item.CategoryID != category || item.BringerID == nil {
		t.Errorf("item %+v", item)
	}

	// Somebody who is not on the trip brings nothing.
	stranger := `{"name":"Tent","bringer_id":"` + uuid.NewString() + `"}`
	if recorder := send(s, http.MethodPost, base+"/items", "good", stranger); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a stranger brings a tent: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPatch, "/api/v1/packing-items/"+item.ID, "good", `{"packed":true,"quantity":2}`)
	list = decodePacking(t, recorder.Body.Bytes())
	if list.Packed != 1 || list.Total != 1 || list.Items[0].Quantity != 2 || list.Items[0].BringerID == nil {
		t.Errorf("after the tick: %+v", list)
	}

	recorder = send(s, http.MethodPost, "/api/v1/packing-items/"+item.ID+":move", "good", `{"category_id":null,"position":0}`)
	if recorder.Code != http.StatusOK || packing.moved != nil {
		t.Errorf("move out of the category: %d %s", recorder.Code, recorder.Body.String())
	}

	if recorder := send(s, http.MethodPost, base+":reset", "good", ""); recorder.Code != http.StatusOK || !packing.reset {
		t.Errorf("reset: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestPackingAddOverHTTP checks a template's categories come in with their
// things, adding it again brings nothing new, and a template without a
// category or with a nameless thing is refused.
func TestPackingAddOverHTTP(t *testing.T) {
	s, trips, packing := newPackingServer(domain.RoleEditor)
	path := "/api/v1/trips/" + trips.trip.ID.String() + "/packing:add"
	body := `{"categories":[
		{"name":"Hiking gear","color":"green","icon":"gear","items":[{"name":"Backpack"},{"name":"Poles","quantity":2}]},
		{"name":"Hiking clothes","color":"green","icon":"clothes","items":[{"name":"Socks","quantity":3}]}]}`
	for range 2 {
		recorder := send(s, http.MethodPost, path, "good", body)
		if recorder.Code != http.StatusOK {
			t.Fatalf("add a template: %d %s", recorder.Code, recorder.Body.String())
		}
		list := decodePacking(t, recorder.Body.Bytes())
		if len(list.Categories) != 2 || list.Total != 3 || list.Categories[1].Icon != "clothes" ||
			list.Categories[1].Color != "green" || list.Items[1].Quantity != 2 || list.Items[1].Position != 1 {
			t.Fatalf("list %+v", list)
		}
	}
	for _, bad := range []string{`{"categories":[]}`, `{"categories":[{"name":"Gear","items":[{"name":" "}]}]}`} {
		if recorder := send(s, http.MethodPost, path, "good", bad); recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", bad, recorder.Code, recorder.Body.String())
		}
	}
	if len(packing.list.Items) != 3 {
		t.Errorf("a refused template changed the list: %+v", packing.list.Items)
	}
}

// TestPackingListRoles checks a viewer reads the list and changes nothing, and
// a report has no list.
func TestPackingListRoles(t *testing.T) {
	s, trips, packing := newPackingServer(domain.RoleViewer)
	base := "/api/v1/trips/" + trips.trip.ID.String() + "/packing"
	if recorder := send(s, http.MethodGet, base, "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("a viewer reads: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodPost, base+"/categories", "good", `{"name":"Gear"}`); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer adds a category: %d", recorder.Code)
	}
	if len(packing.list.Categories) != 0 {
		t.Error("a viewer changed the list")
	}

	trips.trip.Kind = domain.DocumentReport
	recorder := send(s, http.MethodGet, base, "good", "")
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "not_a_plan" {
		t.Errorf("a report's list: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestPackingPDFOverHTTP checks the checklist comes back as a PDF named after
// its trip.
func TestPackingPDFOverHTTP(t *testing.T) {
	s, trips, packing := newPackingServer(domain.RoleViewer)
	packing.list.Items = []domain.PackingItem{{ID: uuid.New(), TripID: trips.trip.ID, Name: "Passport", Quantity: 1}}
	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/packing/pdf", "good", "")
	if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Body.String(), "%PDF") {
		t.Fatalf("checklist: %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "Iceland-packing.pdf") {
		t.Errorf("file name %q", got)
	}
}
