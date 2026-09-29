package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPackingItemNormalize checks an item's name is required, its quantity
// defaults to one and stays within bounds.
func TestPackingItemNormalize(t *testing.T) {
	item, err := PackingItem{Name: "  Passport ", Note: " in the bag "}.Normalize()
	if err != nil || item.Name != "Passport" || item.Note != "in the bag" || item.Quantity != 1 {
		t.Fatalf("item %+v, %v", item, err)
	}
	for _, bad := range []PackingItem{
		{Name: "   "},
		{Name: "Socks", Quantity: -1},
		{Name: "Socks", Quantity: MaxPackingQuantity + 1},
		{Name: strings.Repeat("a", maxPackingItemName+1)},
	} {
		var validation *ValidationError
		if _, err := bad.Normalize(); !errors.As(err, &validation) {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if _, err := (PackingCategory{Name: ""}).Normalize(); err == nil {
		t.Error("a category without a name was accepted")
	}
}

// TestPackingCategoryNormalize checks a category takes the "other" icon when
// none is named, and refuses a colour or an icon outside their sets.
func TestPackingCategoryNormalize(t *testing.T) {
	category, err := PackingCategory{Name: " Documents "}.Normalize()
	if err != nil || category.Name != "Documents" || category.Icon != PackingIconOther || category.Color != "" {
		t.Fatalf("category %+v, %v", category, err)
	}
	if _, err := (PackingCategory{Name: "Kit", Color: "teal", Icon: "first_aid"}).Normalize(); err != nil {
		t.Errorf("a colour and an icon of the sets were refused: %v", err)
	}
	if code := validationCode(t, first(PackingCategory{Name: "Kit", Color: "#ff0000"}.Normalize())); code != "color:unsupported" {
		t.Errorf("a colour outside the palette: %s", code)
	}
	if code := validationCode(t, first(PackingCategory{Name: "Kit", Icon: "rocket"}.Normalize())); code != "icon:unsupported" {
		t.Errorf("an icon outside the set: %s", code)
	}
	if DefaultPackingColor(0) != TagColors[0] || DefaultPackingColor(len(TagColors)) != TagColors[0] {
		t.Error("the default colours do not go round the palette")
	}
}

// first drops the value of a result, keeping its error.
func first(_ PackingCategory, err error) error {
	return err
}

// TestPackingListInCategory checks the items are picked by category, the ones
// without a category apart, and the packed ones counted.
func TestPackingListInCategory(t *testing.T) {
	gear := uuid.New()
	list := PackingList{Items: []PackingItem{
		{Name: "Tent", CategoryID: &gear, Packed: true},
		{Name: "Passport"},
		{Name: "Stove", CategoryID: &gear},
	}}
	if got := list.InCategory(&gear); len(got) != 2 || got[1].Name != "Stove" {
		t.Errorf("gear %+v", got)
	}
	if got := list.InCategory(nil); len(got) != 1 || got[0].Name != "Passport" {
		t.Errorf("without a category %+v", got)
	}
	if list.PackedCount() != 1 {
		t.Errorf("packed %d", list.PackedCount())
	}
}
