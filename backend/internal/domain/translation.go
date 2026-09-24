package domain

import (
	"slices"
	"strings"

	"github.com/google/uuid"
)

// A report may be written in several languages. Its own columns hold the
// original, in the first of its languages; every other language keeps, field
// by field, only what somebody translated. A reader is shown their own language
// when the report has it, and each field nobody translated falls back to the
// original, so a report half translated still reads as a whole.
//
// Only the words are translated. Times, ratings, amounts and pictures are the
// same in every language, and a translation never changes them.

// TranslationTarget is the kind of element a translation belongs to.
type TranslationTarget string

// Translation targets.
const (
	TranslateTrip     TranslationTarget = "trip"
	TranslateDocument TranslationTarget = "document"
	TranslateDay      TranslationTarget = "day"
	TranslateStay     TranslationTarget = "stay"
	TranslateItem     TranslationTarget = "item"
	TranslateLeg      TranslationTarget = "leg"
)

// translatableFields lists, for each kind of element, the fields a report
// translates and how long each may be. They are the words a reader of the
// report sees; everything else is shared by every language.
var translatableFields = map[TranslationTarget]map[string]int{
	TranslateTrip:     {"title": maxTripTitleLength, "summary": maxTripSummaryLength},
	TranslateDocument: {"intro_md": maxMarkdownLength, "summary_md": maxMarkdownLength},
	TranslateDay:      {"title": maxNameLength, "notes_md": maxMarkdownLength},
	TranslateStay:     {"name": maxNameLength, "notes_md": maxMarkdownLength},
	TranslateItem:     {"name": maxNameLength, "description_md": maxMarkdownLength, "story_md": maxMarkdownLength},
	TranslateLeg:      {"note": maxLegNoteLength},
}

// Translation is one field of one element of a report in one further language.
type Translation struct {
	Target TranslationTarget
	// TargetID is the element translated; for the trip itself it is the trip.
	TargetID uuid.UUID
	Field    string
	Lang     string
	// Value is the translated text; an empty value removes the translation.
	Value string
}

// Normalize - trims a translation and checks that it names a field a report
// translates.
//
// Returns:
//   - the normalised translation; an empty Value asks for its removal.
//   - a *ValidationError for an unknown target or field, or a value too long.
func (t Translation) Normalize() (Translation, error) {
	fields, ok := translatableFields[t.Target]
	if !ok {
		return t, NewValidationError("target_type", "unsupported", "must be trip, document, day, stay, item or leg")
	}
	limit, ok := fields[t.Field]
	if !ok {
		return t, NewValidationError("field", "unsupported", "is not a field this element translates")
	}
	t.Value = strings.TrimSpace(t.Value)
	return t, checkLength(t.Field, t.Value, limit)
}

// TripTranslations are a trip's own translated fields: field by language.
type TripTranslations map[string]map[string]string

// NormalizeLanguages - checks the languages a trip is written in.
//
// A report names at least one, the language of its original, followed by the
// ones it is translated into; an empty list becomes fallback alone. A plan
// names none.
//
// Arguments:
//   - kind: what the trip holds.
//   - languages: the requested languages, the original first.
//   - fallback: the original to assume when a report names none.
//
// Returns:
//   - the normalised list.
//   - a *ValidationError for a plan with languages, an unsupported language or
//     one named twice.
func NormalizeLanguages(kind DocumentKind, languages []string, fallback string) ([]string, error) {
	if kind != DocumentReport {
		if len(languages) > 0 {
			return nil, NewValidationError("languages", "report_only", "only a report is written in several languages")
		}
		return nil, nil
	}
	if len(languages) == 0 {
		return []string{ContentLanguage(fallback)}, nil
	}
	normalized := make([]string, 0, len(languages))
	for _, language := range languages {
		language = strings.TrimSpace(language)
		if !slices.Contains(SupportedLocales, language) {
			return nil, NewValidationError("languages", "unsupported", "is not a supported language")
		}
		if slices.Contains(normalized, language) {
			return nil, NewValidationError("languages", "duplicate_language", "names a language twice")
		}
		normalized = append(normalized, language)
	}
	return normalized, nil
}

// ContentLanguage - names the language a new report is written in: the
// author's own, or English when they follow the browser.
//
// Arguments:
//   - locale: the author's interface language, possibly empty.
//
// Returns:
//   - a supported language.
func ContentLanguage(locale string) string {
	if slices.Contains(SupportedLocales, locale) {
		return locale
	}
	return "en"
}

// ReadingLanguage - picks the language a report is shown in to a reader.
//
// Arguments:
//   - languages: the report's languages, the original first.
//   - requested: the reader's language.
//
// Returns:
//   - the requested language when the report has it, otherwise the original;
//     "" for a trip without languages.
func ReadingLanguage(languages []string, requested string) string {
	if slices.Contains(languages, requested) {
		return requested
	}
	if len(languages) > 0 {
		return languages[0]
	}
	return ""
}

// Translated - returns the trip with its title and summary in a language.
//
// Arguments:
//   - translations: the trip's own translated fields.
//   - language: the language to show; the original, or one the trip lacks,
//     changes nothing.
//
// Returns:
//   - the trip, each translated field replaced.
func (t Trip) Translated(translations TripTranslations, language string) Trip {
	fields := translations[language]
	if value := fields["title"]; value != "" {
		t.Title = value
	}
	if value := fields["summary"]; value != "" {
		t.Summary = value
	}
	return t
}

// Translated - returns a copy of the document with its words in a language.
//
// The copy owns its slices, so the content it was made from is left as it was.
//
// Arguments:
//   - language: the language to show; the original, or one nobody translated
//     into, changes nothing.
//
// Returns:
//   - the content, each translated field replaced.
func (c DocumentContent) Translated(language string) DocumentContent {
	texts := make(map[uuid.UUID]map[string]string)
	for _, translation := range c.Translations {
		if translation.Lang != language {
			continue
		}
		if texts[translation.TargetID] == nil {
			texts[translation.TargetID] = make(map[string]string)
		}
		texts[translation.TargetID][translation.Field] = translation.Value
	}
	if len(texts) == 0 {
		return c
	}
	apply := func(id uuid.UUID, field string, target *string) {
		if value := texts[id][field]; value != "" {
			*target = value
		}
	}

	apply(c.Document.ID, "intro_md", &c.Document.IntroMD)
	apply(c.Document.ID, "summary_md", &c.Document.SummaryMD)
	c.Days = slices.Clone(c.Days)
	for index := range c.Days {
		day := &c.Days[index]
		apply(day.ID, "title", &day.Title)
		apply(day.ID, "notes_md", &day.NotesMD)
	}
	c.Stays = slices.Clone(c.Stays)
	for index := range c.Stays {
		stay := &c.Stays[index]
		apply(stay.ID, "name", &stay.Name)
		apply(stay.ID, "notes_md", &stay.NotesMD)
	}
	c.Items = slices.Clone(c.Items)
	for index := range c.Items {
		item := &c.Items[index]
		apply(item.ID, "name", &item.Name)
		apply(item.ID, "description_md", &item.DescriptionMD)
		apply(item.ID, "story_md", &item.StoryMD)
	}
	c.Legs = slices.Clone(c.Legs)
	for index := range c.Legs {
		apply(c.Legs[index].ID, "note", &c.Legs[index].Note)
	}
	return c
}
