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

// lowTatras is a real list that used to spill onto a second page with half of
// each page empty: five categories of short Russian lines and a few notes.
func lowTatras() Packing {
	trip := domain.Trip{ID: uuid.New(), Title: "Low Tatras"}
	list := domain.PackingList{}
	add := func(name string, icon int, items ...string) {
		category := domain.PackingCategory{ID: uuid.New(), TripID: trip.ID, Name: name,
			Color: domain.DefaultPackingColor(icon), Icon: domain.PackingIcons[icon]}
		list.Categories = append(list.Categories, category)
		for _, line := range items {
			name, note, _ := strings.Cut(line, "|")
			list.Items = append(list.Items, domain.PackingItem{ID: uuid.New(), TripID: trip.ID,
				CategoryID: &category.ID, Name: name, Note: note, Quantity: 1})
		}
	}
	add("Documents", 0, "ID / паспорт", "водительские права", "документы на машину", "банковская карта",
		"немного EUR наличными",
		"медицинская / туристическая страховка|Проверить, что покрывает хайкинг и горно-спасательные работы.")
	add("First AID", 1, "эластичный бинт", "пластыри + Compeed для мозолей", "обезболивающее",
		"индивидуальные лекарства", "антисептические салфетки")
	add("Clothes", 2, "треккинговые кросовки/ботинки с хорошим протектором", "треккинговые носки",
		"флис / тёплый mid-layer", "мембранная куртка|для защиты от дождя и ветра",
		"лёгкая пуховка / утеплённая куртка", "шапка", "перчатки", "бафф", "солнцезащитные очки",
		"треккинговые палки")
	add("Gear", 3, "рюкзак", "rain cover или гермомешок для вещей", "1.5–2 л воды", "перекус / сэндвич",
		"налобный фонарь|Даже если планируете вернуться засветло — обязательный запас на случай задержки.",
		"небольшой пакет для мусора", "солнцезащитный крем", "гигиеническая помада",
		"свисток|Для подачи сигнала, если голосом докричаться сложно.",
		"спасательное термоодеяло|Практически ничего не весит, пригодится при травме или вынужденной остановке.")
	add("Electronics", 4, "телефон", "power bank", "GPX-трек загружен в телефон",
		"офлайн-карта района|Проверить именно загрузку карты, а не только наличие приложения.",
		"зарядка телефона", "камера")
	return Packing{Trip: trip, List: list, Language: "ru"}
}

// TestCompactPackingFitsOnePage checks a compact list that fits on one page is
// drawn on one, at the same size every time, and that the full-size list
// fills its columns before it begins a page.
func TestCompactPackingFitsOnePage(t *testing.T) {
	packing := lowTatras()
	packing.Compact = true
	var first, second bytes.Buffer
	if err := RenderPacking(&first, packing); err != nil {
		t.Fatalf("render: %v", err)
	}
	if pages := bytes.Count(first.Bytes(), []byte("/Type /Page\n")); pages != 1 {
		t.Errorf("a compact list takes %d pages, want 1", pages)
	}
	doc := newDocument("", "")
	blocks := packingBlocks(wording("ru", ""), packing.List)
	style, _ := choosePackingLayout(doc, packing, blocks)
	again, _ := choosePackingLayout(doc, packing, blocks)
	if style != again {
		t.Errorf("the same list chose %+v, then %+v", style, again)
	}
	if err := RenderPacking(&second, packing); err != nil {
		t.Fatalf("render again: %v", err)
	}

	// Every page but the last of a full-size list leaves no category that
	// would have fitted in one of its columns.
	packing.Compact = false
	full := newPackingStyle(2, 1, false, false)
	pages := placePacking(doc, packing, full, blocks)
	for index, page := range pages[:len(pages)-1] {
		for _, later := range pages[index+1:] {
			for _, cards := range later {
				for _, card := range cards {
					for column, placed := range page {
						used := full.top(index)
						for _, each := range placed {
							used += each.height + full.cardGap
						}
						if card.from == 0 && used+card.height <= packingBottom {
							t.Errorf("category %d went to a later page, but fits under column %d of page %d",
								card.block, column, index+1)
						}
					}
				}
			}
		}
	}
}
