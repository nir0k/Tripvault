package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestTranslationNormalize checks that only the words of a report translate and
// that each keeps the limit of the field it stands in for.
func TestTranslationNormalize(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name  string
		input Translation
		field string
	}{
		{"unknown target", Translation{Target: "expense", TargetID: id, Field: "note", Value: "x"}, "target_type"},
		{"field of another element", Translation{Target: TranslateDay, TargetID: id, Field: "story_md", Value: "x"}, "field"},
		{"shared field", Translation{Target: TranslateItem, TargetID: id, Field: "address", Value: "x"}, "field"},
		{"too long", Translation{Target: TranslateItem, TargetID: id, Field: "name",
			Value: strings.Repeat("a", maxNameLength+1)}, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.input.Normalize()
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Field != tc.field {
				t.Fatalf("got %v, want a validation error on %q", err, tc.field)
			}
		})
	}

	got, err := Translation{Target: TranslateItem, TargetID: id, Field: "story_md", Value: "  "}.Normalize()
	if err != nil || got.Value != "" {
		t.Errorf("blank value: got %q, %v; want an empty value asking for removal", got.Value, err)
	}
}

// TestNormalizeLanguages checks what a trip may be written in.
func TestNormalizeLanguages(t *testing.T) {
	if _, err := NormalizeLanguages(DocumentPlan, []string{"en"}, ""); err == nil {
		t.Error("a plan was given languages")
	}
	if got, err := NormalizeLanguages(DocumentPlan, nil, "ru"); err != nil || got != nil {
		t.Errorf("plan: got %v, %v; want none", got, err)
	}
	if got, _ := NormalizeLanguages(DocumentReport, nil, "ru"); len(got) != 1 || got[0] != "ru" {
		t.Errorf("report without languages: got %v, want the fallback", got)
	}
	if got, _ := NormalizeLanguages(DocumentReport, nil, "xx"); len(got) != 1 || got[0] != "en" {
		t.Errorf("unsupported fallback: got %v, want English", got)
	}
	if _, err := NormalizeLanguages(DocumentReport, []string{"en", "en"}, ""); err == nil {
		t.Error("a language named twice was accepted")
	}
	if _, err := NormalizeLanguages(DocumentReport, []string{"en", "xx"}, ""); err == nil {
		t.Error("an unsupported language was accepted")
	}
	if got, err := NormalizeLanguages(DocumentReport, []string{"ru", "en"}, ""); err != nil ||
		strings.Join(got, ",") != "ru,en" {
		t.Errorf("got %v, %v; want the order kept", got, err)
	}
}

// TestReadingLanguage checks the reader gets their language when the report has
// it, and the original otherwise.
func TestReadingLanguage(t *testing.T) {
	languages := []string{"ru", "en"}
	if got := ReadingLanguage(languages, "en"); got != "en" {
		t.Errorf("got %q, want en", got)
	}
	if got := ReadingLanguage(languages, "de"); got != "ru" {
		t.Errorf("got %q, want the original", got)
	}
	if got := ReadingLanguage(nil, "en"); got != "" {
		t.Errorf("got %q for a plan, want nothing", got)
	}
}

// TestDocumentTranslated checks translated fields replace the original, the
// rest stays as written, and the content it was made from is not touched.
func TestDocumentTranslated(t *testing.T) {
	document := Document{ID: uuid.New(), IntroMD: "intro"}
	day := Day{ID: uuid.New(), Title: "day", NotesMD: "notes"}
	item := Item{ID: uuid.New(), Name: "waterfall", StoryMD: "wet"}
	leg := Leg{ID: uuid.New(), Note: "bus"}
	content := DocumentContent{
		Document: document, Days: []Day{day}, Items: []Item{item}, Legs: []Leg{leg},
		Translations: []Translation{
			{Target: TranslateDocument, TargetID: document.ID, Field: "intro_md", Lang: "en", Value: "intro (en)"},
			{Target: TranslateDay, TargetID: day.ID, Field: "title", Lang: "en", Value: "day (en)"},
			{Target: TranslateItem, TargetID: item.ID, Field: "story_md", Lang: "en", Value: "wet (en)"},
			{Target: TranslateLeg, TargetID: leg.ID, Field: "note", Lang: "de", Value: "bus (de)"},
		},
	}

	got := content.Translated("en")
	if got.Document.IntroMD != "intro (en)" || got.Days[0].Title != "day (en)" || got.Items[0].StoryMD != "wet (en)" {
		t.Errorf("translated fields were not applied: %+v", got)
	}
	if got.Days[0].NotesMD != "notes" || got.Items[0].Name != "waterfall" || got.Legs[0].Note != "bus" {
		t.Error("an untranslated field lost its original")
	}
	if content.Days[0].Title != "day" || content.Items[0].StoryMD != "wet" {
		t.Error("translating changed the content it was made from")
	}
	if original := content.Translated("ru"); original.Document.IntroMD != "intro" {
		t.Error("the original language was translated")
	}
}

// TestTripTranslated checks the trip's own title and summary follow the reader.
func TestTripTranslated(t *testing.T) {
	trip := Trip{Title: "iceland", Summary: "short"}
	translations := TripTranslations{"en": {"title": "iceland (en)"}}
	got := trip.Translated(translations, "en")
	if got.Title != "iceland (en)" || got.Summary != "short" {
		t.Errorf("got %q / %q", got.Title, got.Summary)
	}
	if got := trip.Translated(translations, "ru"); got.Title != "iceland" {
		t.Errorf("original: got %q", got.Title)
	}
}
