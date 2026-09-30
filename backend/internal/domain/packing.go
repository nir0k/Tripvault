package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// What to take is a list kept on a plan's trip: things, gear, documents. It is
// packed together, so one tick serves the whole group, and an item may name
// the member who brings it. A report has no list: by then everything was taken.

// Limits of a packing list.
const (
	maxPackingCategoryName = 100
	maxPackingItemName     = 200
	// MaxPackingQuantity bounds how many of one thing an item counts.
	MaxPackingQuantity = 999
	// MaxPackingSections bounds how many categories one addition may bring,
	// and MaxPackingSectionItems how many things each of them may hold.
	MaxPackingSections     = 20
	MaxPackingSectionItems = 100
)

// PackingIcon is the picture a category is shown with: a key of a fixed set
// drawn alike by the interface and the PDF, since a category's name is free text
// and could not choose one of its own.
type PackingIcon string

// PackingIconOther is the icon of a category nobody chose one for.
const PackingIconOther PackingIcon = "other"

// PackingIcons is the set of icons a category may take.
var PackingIcons = []PackingIcon{
	"documents", "tickets", "cards", "money", "coins", "keys", "work", "clothes", "bags", "luggage", "glasses",
	"rain", "first_aid", "medicine", "thermometer", "hygiene", "health", "cosmetics", "electronics", "phone",
	"chargers", "power_bank", "headphones", "camera", "flash_drive", "watch", "games", "food", "snacks", "drinks",
	"groceries", "books", "music", "drawing", "toys", "gifts", "kids", "sport", "swimming", "beach", "snow",
	"camping", "lamp", "compass", "map", "binoculars", "hiking", "sleep", "nature", "gear", "flight", "train",
	"bus", "car", "fuel", "home", "tools", "stationery", "shopping", PackingIconOther,
}

// ValidatePackingIcon - checks that an icon is one of the set.
//
// Arguments:
//   - icon: the icon to check.
//
// Returns:
//   - a *ValidationError on icon when it is not in the set.
func ValidatePackingIcon(icon PackingIcon) error {
	for _, known := range PackingIcons {
		if icon == known {
			return nil
		}
	}
	return NewValidationError("icon", "unsupported", "must be an icon of the set")
}

// DefaultPackingColor - picks the colour of a new category that names none, so
// neighbouring categories come out in different colours.
//
// Arguments:
//   - count: how many categories the list holds already.
//
// Returns:
//   - a colour of the palette tags use.
func DefaultPackingColor(count int) TagColor {
	return TagColors[count%len(TagColors)]
}

// PackingCategory is one heading of a trip's packing list, named by the trip's
// people rather than taken from a fixed set. Its colour and icon only tell it
// apart on the page; the colour is a key of the tags' palette.
type PackingCategory struct {
	ID       uuid.UUID
	TripID   uuid.UUID
	Name     string
	Color    TagColor
	Icon     PackingIcon
	Position int
}

// Normalize - trims a category's name, fills its icon and checks it. An empty
// colour is left for the repository, which picks one by the category's place.
//
// Returns:
//   - the normalised category.
//   - the first *ValidationError found.
func (c PackingCategory) Normalize() (PackingCategory, error) {
	var err error
	if c.Name, err = requiredPackingText("name", c.Name, maxPackingCategoryName); err != nil {
		return c, err
	}
	if c.Color != "" {
		if err := ValidateTagColor(c.Color); err != nil {
			return c, err
		}
	}
	if c.Icon == "" {
		c.Icon = PackingIconOther
	}
	return c, ValidatePackingIcon(c.Icon)
}

// PackingItem is one thing to take. An item without a category is listed apart,
// after the categories.
type PackingItem struct {
	ID         uuid.UUID
	TripID     uuid.UUID
	CategoryID *uuid.UUID
	Name       string
	Quantity   int
	Note       string
	// Packed is the one tick of the group.
	Packed bool
	// BringerID is the member who brings the item; nil when nobody was named.
	BringerID *uuid.UUID
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Normalize - trims an item's texts, fills its quantity and checks its fields.
//
// Returns:
//   - the normalised item.
//   - the first *ValidationError found.
func (i PackingItem) Normalize() (PackingItem, error) {
	var err error
	if i.Name, err = requiredPackingText("name", i.Name, maxPackingItemName); err != nil {
		return i, err
	}
	if i.Note, err = trimmedText("note", i.Note, maxShortText); err != nil {
		return i, err
	}
	if i.Quantity == 0 {
		i.Quantity = 1
	}
	if i.Quantity < 1 || i.Quantity > MaxPackingQuantity {
		return i, NewValidationError("quantity", "out_of_range", "must be between 1 and 999")
	}
	return i, nil
}

// PackingList is a trip's whole list: the categories in their order and the
// items, each list of a category in its order.
type PackingList struct {
	Categories []PackingCategory
	Items      []PackingItem
}

// InCategory - picks the items of one category, or the ones without a category
// when categoryID is nil, in the order they came in.
//
// Arguments:
//   - categoryID: the category, or nil for the items without one.
//
// Returns:
//   - the matching items.
func (l PackingList) InCategory(categoryID *uuid.UUID) []PackingItem {
	picked := make([]PackingItem, 0)
	for _, item := range l.Items {
		if sameDay(item.CategoryID, categoryID) {
			picked = append(picked, item)
		}
	}
	return picked
}

// PackedCount - counts the items ticked as packed.
//
// Returns:
//   - the number of packed items.
func (l PackingList) PackedCount() int {
	count := 0
	for _, item := range l.Items {
		if item.Packed {
			count++
		}
	}
	return count
}

// PackingSection is a category with the things to put in it, added to a list
// in one go, as a template of the interface is.
type PackingSection struct {
	Category PackingCategory
	Items    []PackingItem
}

// ValidatePackingSections - normalises the sections of one addition and checks
// their number.
//
// Arguments:
//   - sections: the categories with their items, identifiers and trip set.
//
// Returns:
//   - the normalised sections.
//   - a *ValidationError on the first field that is wrong.
func ValidatePackingSections(sections []PackingSection) ([]PackingSection, error) {
	if len(sections) == 0 || len(sections) > MaxPackingSections {
		return nil, NewValidationError("categories", "out_of_range", "must hold between 1 and 20 categories")
	}
	normalised := make([]PackingSection, 0, len(sections))
	for _, section := range sections {
		category, err := section.Category.Normalize()
		if err != nil {
			return nil, err
		}
		if len(section.Items) > MaxPackingSectionItems {
			return nil, NewValidationError("items", "out_of_range", "must hold at most 100 items a category")
		}
		items := make([]PackingItem, 0, len(section.Items))
		for _, item := range section.Items {
			if item, err = item.Normalize(); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		normalised = append(normalised, PackingSection{Category: category, Items: items})
	}
	return normalised, nil
}

// Merge - works out what adding sections to the list brings into it. A section
// whose name the list has already, in any case, fills that category rather
// than making a second one, and a thing its category holds already under the
// same name is left out, so adding a template twice adds nothing.
//
// Arguments:
//   - sections: the validated sections, with the identifiers new rows take.
//
// Returns:
//   - the categories to create, placed at the end of the list and coloured by
//     that place when they name no colour.
//   - the items to create, each at the end of its category.
func (l PackingList) Merge(sections []PackingSection) ([]PackingCategory, []PackingItem) {
	categories := append([]PackingCategory(nil), l.Categories...)
	items := append([]PackingItem(nil), l.Items...)
	var newCategories []PackingCategory
	var newItems []PackingItem
	for _, section := range sections {
		category, found := findPackingCategory(categories, section.Category.Name)
		if !found {
			category = section.Category
			category.Position = len(categories)
			if category.Color == "" {
				category.Color = DefaultPackingColor(len(categories))
			}
			categories = append(categories, category)
			newCategories = append(newCategories, category)
		}
		for _, item := range section.Items {
			position, taken := 0, false
			for _, existing := range items {
				if !sameDay(existing.CategoryID, &category.ID) {
					continue
				}
				position = max(position, existing.Position+1)
				taken = taken || strings.EqualFold(existing.Name, item.Name)
			}
			if taken {
				continue
			}
			item.TripID = category.TripID
			item.CategoryID = &category.ID
			item.Position = position
			items = append(items, item)
			newItems = append(newItems, item)
		}
	}
	return newCategories, newItems
}

// findPackingCategory finds a category by its name, in any case.
func findPackingCategory(categories []PackingCategory, name string) (PackingCategory, bool) {
	for _, category := range categories {
		if strings.EqualFold(category.Name, name) {
			return category, true
		}
	}
	return PackingCategory{}, false
}

// requiredPackingText trims a single-line text that must not be empty.
func requiredPackingText(field, value string, limit int) (string, error) {
	value, err := trimmedText(field, value, limit)
	if err != nil {
		return value, err
	}
	if value == "" {
		return value, NewValidationError(field, "required", "must not be empty")
	}
	return value, nil
}
