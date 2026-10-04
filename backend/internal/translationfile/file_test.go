package translationfile

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// report builds a small report: one day with a place, a journey, a stay and
// the story of the night spent there.
func report() (domain.TripSummary, domain.DocumentContent) {
	dayID, first, second, stayID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	trip := domain.TripSummary{Trip: domain.Trip{ID: uuid.New(), Title: "Iceland", Summary: "Four days",
		Languages: []string{"en", "ru"}}}
	trip.Translations = domain.TripTranslations{"ru": {"title": "Исландия"}}
	content := domain.DocumentContent{
		Document: domain.Document{ID: uuid.New(), IntroMD: "It rained.\n\nThen it did not."},
		Days:     []domain.Day{{ID: dayID, Title: "Reykjavík"}},
		Items: []domain.Item{
			{ID: first, DayID: &dayID, Position: 0, Kind: domain.ItemPlace, Name: "Harbour",
				StoryMD: "Boats.\n\n- one\n- two"},
			{ID: second, DayID: &dayID, Position: 1, Kind: domain.ItemPlace, Name: "Museum"},
			{ID: uuid.New(), DayID: &dayID, Position: 2, Kind: domain.ItemStayAnchor, Anchor: domain.AnchorEvening,
				StayID: &stayID, StoryMD: "Aurora"},
		},
		Legs: []domain.Leg{{ID: uuid.New(), DayID: dayID, FromItemID: first, ToItemID: second, Note: "Bus 12"}},
		Tracks: []domain.Track{{ID: uuid.New(), ItemID: first,
			Stops: []domain.Stop{{ID: uuid.New(), ItemID: first, Kind: domain.StopFood, Name: "Kiosk"}}}},
		Stays: []domain.Stay{{ID: stayID, Name: "Guesthouse"}},
		Translations: []domain.Translation{
			{Target: domain.TranslateItem, TargetID: first, Field: "name", Lang: "ru", Value: "Гавань"},
		},
	}
	return trip, content
}

// TestBuildWritesTheWordsOfAReport checks the file names its languages, holds
// every text beside its translation so far, and keeps Markdown as a block.
func TestBuildWritesTheWordsOfAReport(t *testing.T) {
	trip, content := report()
	data, err := Build(trip, content, "ru")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	file := string(data)
	for _, want := range []string{
		"format: " + Format, "from: en", "to: ru",
		"original: Iceland", "translation: Исландия",
		"original: Harbour", "translation: Гавань",
		"between: Harbour → Museum", "original: Bus 12",
		"original: Guesthouse",
		"night:\n", "original: Aurora",
		"stops:\n", "original: Kiosk",
		"original: |-\n", "- one\n",
	} {
		if !strings.Contains(file, want) {
			t.Errorf("the file lacks %q:\n%s", want, file)
		}
	}
	if strings.Contains(file, "description:") {
		t.Error("a text that does not exist was written")
	}
}

// TestReadBringsTheTranslationsBack checks a filled file becomes translations,
// an emptied one removes a translation, and an element deleted since the file
// was written is skipped rather than failing the rest.
func TestReadBringsTheTranslationsBack(t *testing.T) {
	trip, content := report()
	data, err := Build(trip, content, "ru")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	filled := strings.Replace(string(data), "translation: Гавань", "translation: Порт", 1)
	filled = strings.Replace(filled, "original: Museum\n          translation: \"\"",
		"original: Museum\n          translation: Музей", 1)
	filled = strings.Replace(filled, "original: Kiosk\n              translation: \"\"",
		"original: Kiosk\n              translation: Ларёк", 1)
	filled = strings.Replace(filled, "original: Aurora\n        translation: \"\"",
		"original: Aurora\n        translation: Сияние", 1)

	// The museum is deleted after the file was written.
	museum, night := content.Items[1].ID, content.Items[2].ID
	content.Items = []domain.Item{content.Items[0], content.Items[2]}
	result, err := Read([]byte(filled), trip, content, "ru")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("skipped %d, want 1", result.Skipped)
	}
	values := map[string]string{}
	nightRead := false
	for _, translation := range result.Translations {
		if translation.TargetID == museum {
			t.Error("a deleted element was translated")
		}
		if translation.TargetID == night {
			if translation.Field != "story_md" || translation.Value != "Сияние" {
				t.Errorf("night: %+v", translation)
			}
			nightRead = true
			continue
		}
		values[string(translation.Target)+"."+translation.Field] = translation.Value
	}
	if values["item.name"] != "Порт" || values["trip.title"] != "Исландия" || values["leg.note"] != "" {
		t.Errorf("translations: %v", values)
	}
	if values["stop.name"] != "Ларёк" {
		t.Errorf("the stop's name was not read back: %v", values)
	}
	if !nightRead {
		t.Error("the story of the night was not read back")
	}
	if _, ok := values["document.intro_md"]; !ok {
		t.Error("the introduction was not read back")
	}

	if _, err := Read(data, trip, content, "de"); !errors.Is(err, ErrLanguage) {
		t.Errorf("another language: %v", err)
	}
	if _, err := Read([]byte("hello: world"), trip, content, "ru"); !errors.Is(err, ErrFormat) {
		t.Errorf("not a translation file: %v", err)
	}
}
