package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// packingList builds a list long enough to fill both columns of a page and go
// on to the next, with a bringer and items without a category.
func packingList() Packing {
	trip := domain.Trip{ID: uuid.New(), Title: "Iceland"}
	bringer := uuid.New()
	list := domain.PackingList{}
	for c := range 6 {
		category := domain.PackingCategory{ID: uuid.New(), TripID: trip.ID, Name: fmt.Sprintf("Category %d", c),
			Color: domain.DefaultPackingColor(c), Icon: domain.PackingIcons[c]}
		list.Categories = append(list.Categories, category)
		for i := range 14 {
			item := domain.PackingItem{ID: uuid.New(), TripID: trip.ID, CategoryID: &category.ID,
				Name: fmt.Sprintf("Thing %d.%d", c, i), Quantity: 1 + i%3, Packed: i%2 == 0}
			if i == 0 {
				item.BringerID = &bringer
				item.Note = "a long note that wraps under the name of the thing it belongs to"
			}
			list.Items = append(list.Items, item)
		}
	}
	list.Items = append(list.Items, domain.PackingItem{ID: uuid.New(), TripID: trip.ID, Name: "Passport", Quantity: 1})
	return Packing{Trip: trip, List: list, Bringers: map[uuid.UUID]string{bringer: "Ada"}}
}

// TestRenderPackingWritesAChecklist checks a long list flows onto a second
// page, and a Russian one and an empty one render too.
func TestRenderPackingWritesAChecklist(t *testing.T) {
	packing := packingList()
	var out bytes.Buffer
	if err := RenderPacking(&out, packing); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.HasPrefix(out.String(), "%PDF") {
		t.Fatal("not a PDF")
	}
	if pages := bytes.Count(out.Bytes(), []byte("/Type /Page\n")); pages < 2 {
		t.Errorf("%d pages, want the list to go on", pages)
	}

	packing.Language = "ru"
	packing.Trip.Title = strings.Repeat("A trip with a title far too long for one line ", 3)
	out.Reset()
	if err := RenderPacking(&out, packing); err != nil {
		t.Fatalf("render in Russian: %v", err)
	}

	out.Reset()
	if err := RenderPacking(&out, Packing{Trip: packing.Trip}); err != nil {
		t.Fatalf("render an empty list: %v", err)
	}
}

// TestPackingBlocks checks empty categories are left out and the items
// without one come last.
func TestPackingBlocks(t *testing.T) {
	gear := domain.PackingCategory{ID: uuid.New(), Name: "Gear"}
	empty := domain.PackingCategory{ID: uuid.New(), Name: "Empty"}
	list := domain.PackingList{
		Categories: []domain.PackingCategory{gear, empty},
		Items: []domain.PackingItem{
			{Name: "Passport"},
			{Name: "Tent", CategoryID: &gear.ID},
		},
	}
	blocks := packingBlocks(wording("en", ""), list)
	if len(blocks) != 2 || blocks[0].title != "Gear" || blocks[1].title != "Other things" {
		t.Errorf("blocks %+v", blocks)
	}
}

// TestPackingIconsAreDrawable checks every icon a category may take has a path
// the renderer can read, and no path is kept for an icon the set left out.
func TestPackingIconsAreDrawable(t *testing.T) {
	for _, icon := range domain.PackingIcons {
		paths, ok := packingIconPaths[string(icon)]
		if !ok || len(paths) == 0 {
			t.Errorf("%s has no path", icon)
		}
		for _, path := range paths {
			if _, err := parseIconPath(path); err != nil {
				t.Errorf("%s: %v", icon, err)
			}
		}
	}
	if len(packingIconPaths) != len(domain.PackingIcons) {
		t.Errorf("%d paths for %d icons", len(packingIconPaths), len(domain.PackingIcons))
	}
	if _, err := parseIconPath("M1 2A1 1 0 0 1 2 2Z"); err == nil {
		t.Error("an arc was read")
	}
}
