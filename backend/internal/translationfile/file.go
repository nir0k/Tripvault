// Package translationfile writes the words of a report into a file for one of
// its languages, and reads the translations back out of it. The file is YAML:
// a person reads it as easily as a program does, and a story of several
// paragraphs stays a block of Markdown rather than one line of escapes. Every
// text is a pair - the original, and the translation to fill in - kept beside
// the element it belongs to, so whoever translates, a person or a model,
// sees what a text is about.
package translationfile

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Format names this layout of the file; a file of another layout is refused
// rather than read wrong.
const Format = "tripvault-translation/1"

// MaxFileBytes bounds a file read back. A report of a long trip with every
// story written out is well under it.
const MaxFileBytes = 8 << 20

// MaxTranslations bounds the translations one file brings.
const MaxTranslations = 20000

// Errors a file can be refused with.
var (
	// ErrFormat reports a file that is not YAML of this layout.
	ErrFormat = errors.New("the file is not a translation file of this service")
	// ErrLanguage reports a file of another language than the one it is read for.
	ErrLanguage = errors.New("the file translates into another language")
)

// Text is one text of a report: the original, and its translation.
type Text struct {
	Original    string `yaml:"original"`
	Translation string `yaml:"translation"`
}

// File is the whole file.
type File struct {
	Format       string     `yaml:"format"`
	Report       string     `yaml:"report"`
	From         string     `yaml:"from"`
	To           string     `yaml:"to"`
	Instructions string     `yaml:"instructions"`
	Trip         Trip       `yaml:"trip"`
	Document     Document   `yaml:"document"`
	Days         []Day      `yaml:"days"`
	Stays        []Stay     `yaml:"stays,omitempty"`
	Transfers    []Transfer `yaml:"transfers,omitempty"`
}

// Trip holds the trip's own title and summary.
type Trip struct {
	Title   *Text `yaml:"title,omitempty"`
	Summary *Text `yaml:"summary,omitempty"`
}

// Document holds the words the report opens and ends with.
type Document struct {
	Introduction *Text `yaml:"introduction,omitempty"`
	Closing      *Text `yaml:"closing,omitempty"`
}

// Day is one day of the report with its places and journeys.
type Day struct {
	Day       int       `yaml:"day"`
	ID        string    `yaml:"id"`
	Title     *Text     `yaml:"title,omitempty"`
	Notes     *Text     `yaml:"notes,omitempty"`
	Highlight *Text     `yaml:"highlight,omitempty"`
	Places    []Place   `yaml:"places,omitempty"`
	Journeys  []Journey `yaml:"journeys,omitempty"`
	Night     *Night    `yaml:"night,omitempty"`
}

// Night is the story of the night the day ends with, told at its stay.
type Night struct {
	ID    string `yaml:"id"`
	Story *Text  `yaml:"story,omitempty"`
}

// Place is a place or an activity of a day, with the stops along its line.
type Place struct {
	ID          string `yaml:"id"`
	Name        *Text  `yaml:"name,omitempty"`
	Description *Text  `yaml:"description,omitempty"`
	Story       *Text  `yaml:"story,omitempty"`
	Stops       []Stop `yaml:"stops,omitempty"`
}

// Stop is a stop along the line of an activity.
type Stop struct {
	ID   string `yaml:"id"`
	Name *Text  `yaml:"name,omitempty"`
	Note *Text  `yaml:"note,omitempty"`
}

// Journey is the note of the journey between two elements of a day; Between
// names them, for context, and is not read back.
type Journey struct {
	ID      string `yaml:"id"`
	Between string `yaml:"between"`
	Note    *Text  `yaml:"note,omitempty"`
}

// Stay is a stay of the report.
type Stay struct {
	ID    string `yaml:"id"`
	Name  *Text  `yaml:"name,omitempty"`
	Notes *Text  `yaml:"notes,omitempty"`
}

// Transfer is a flight, a train or a ferry of the report.
type Transfer struct {
	ID       string `yaml:"id"`
	FromName *Text  `yaml:"from,omitempty"`
	ToName   *Text  `yaml:"to,omitempty"`
	Notes    *Text  `yaml:"notes,omitempty"`
}

// instructions tells whoever translates what to do, in English: the file goes
// to a person or a model, and both read it.
const instructions = `Translate every "original" into the language "to" names and write it into
"translation" beside it. Keep Markdown - lists, links, emphasis - as it is.
Leave a "translation" empty to have none: that text is then read in the
original. Do not change "id", "day", "between", "from" or "to", and do not add
or remove entries; the "original" texts are not read back.`

// key names one translatable field of one element.
type key struct {
	target domain.TranslationTarget
	id     uuid.UUID
	field  string
}

// Build - writes the words of a report into a file for one of its further
// languages, each with the translation it already has.
//
// Only texts that exist are written: an element without notes has no notes to
// translate.
//
// Arguments:
//   - trip: the report's trip, with its own translations.
//   - content: the report.
//   - language: the language the file translates into.
//
// Returns:
//   - the file as YAML.
//   - an error if it cannot be written.
func Build(trip domain.TripSummary, content domain.DocumentContent, language string) ([]byte, error) {
	existing := make(map[key]string, len(content.Translations))
	for _, translation := range content.Translations {
		if translation.Lang == language {
			existing[key{translation.Target, translation.TargetID, translation.Field}] = translation.Value
		}
	}
	text := func(target domain.TranslationTarget, id uuid.UUID, field, original string) *Text {
		if original == "" {
			return nil
		}
		return &Text{Original: original, Translation: existing[key{target, id, field}]}
	}
	tripText := func(field, original string) *Text {
		if original == "" {
			return nil
		}
		return &Text{Original: original, Translation: trip.Translations[language][field]}
	}

	from := ""
	if len(trip.Languages) > 0 {
		from = trip.Languages[0]
	}
	file := File{
		Format: Format, Report: trip.Title, From: from, To: language, Instructions: instructions,
		Trip: Trip{Title: tripText("title", trip.Title), Summary: tripText("summary", trip.Summary)},
		Document: Document{
			Introduction: text(domain.TranslateDocument, content.Document.ID, "intro_md", content.Document.IntroMD),
			Closing:      text(domain.TranslateDocument, content.Document.ID, "summary_md", content.Document.SummaryMD),
		},
	}

	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	everything := make(map[uuid.UUID]domain.Item, len(content.Items))
	for _, item := range content.Items {
		everything[item.ID] = item
	}
	for index, day := range content.Days {
		entry := Day{
			Day: index + 1, ID: day.ID.String(),
			Title:     text(domain.TranslateDay, day.ID, "title", day.Title),
			Notes:     text(domain.TranslateDay, day.ID, "notes_md", day.NotesMD),
			Highlight: text(domain.TranslateDay, day.ID, "highlight", day.Highlight),
		}
		items := domain.DayItems(content.Items, &day.ID)
		for _, item := range items {
			if !item.Kind.IsVisit() {
				continue
			}
			place := Place{
				ID:          item.ID.String(),
				Name:        text(domain.TranslateItem, item.ID, "name", item.Name),
				Description: text(domain.TranslateItem, item.ID, "description_md", item.DescriptionMD),
				Story:       text(domain.TranslateItem, item.ID, "story_md", item.StoryMD),
			}
			for _, stop := range domain.StopsOf(content.Tracks, item.ID) {
				if stop.Name == "" && stop.NoteMD == "" {
					continue
				}
				place.Stops = append(place.Stops, Stop{
					ID:   stop.ID.String(),
					Name: text(domain.TranslateStop, stop.ID, "name", stop.Name),
					Note: text(domain.TranslateStop, stop.ID, "note_md", stop.NoteMD),
				})
			}
			entry.Places = append(entry.Places, place)
		}
		legs := domain.DayLegs(content.Legs, items)
		if opening := domain.OpeningLeg(content.Legs, items); opening != nil {
			legs = append([]*domain.Leg{opening}, legs...)
		}
		for _, leg := range legs {
			if leg == nil || leg.Note == "" {
				continue
			}
			entry.Journeys = append(entry.Journeys, Journey{
				ID: leg.ID.String(),
				Between: domain.ItemLabel(everything[leg.FromItemID], stays) + " → " +
					domain.ItemLabel(everything[leg.ToItemID], stays),
				Note: text(domain.TranslateLeg, leg.ID, "note", leg.Note),
			})
		}
		for _, item := range items {
			if item.IsNight() && item.StoryMD != "" {
				entry.Night = &Night{ID: item.ID.String(),
					Story: text(domain.TranslateItem, item.ID, "story_md", item.StoryMD)}
			}
		}
		file.Days = append(file.Days, entry)
	}
	for _, stay := range content.Stays {
		file.Stays = append(file.Stays, Stay{
			ID:    stay.ID.String(),
			Name:  text(domain.TranslateStay, stay.ID, "name", stay.Name),
			Notes: text(domain.TranslateStay, stay.ID, "notes_md", stay.NotesMD),
		})
	}
	for _, transfer := range content.Transfers {
		file.Transfers = append(file.Transfers, Transfer{
			ID:       transfer.ID.String(),
			FromName: text(domain.TranslateTransfer, transfer.ID, "from_name", transfer.FromName),
			ToName:   text(domain.TranslateTransfer, transfer.ID, "to_name", transfer.ToName),
			Notes:    text(domain.TranslateTransfer, transfer.ID, "notes_md", transfer.NotesMD),
		})
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(file); err != nil {
		return nil, fmt.Errorf("write translation file: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("write translation file: %w", err)
	}
	return out.Bytes(), nil
}

// Result is what a file read back brings.
type Result struct {
	// Translations are the texts of the report's elements, the empty ones
	// included: an empty translation removes one.
	Translations []domain.Translation
	// Skipped counts entries naming an element the report no longer has.
	Skipped int
}

// Read - reads the translations out of a file for one of a report's languages.
//
// Only the translations are read; the originals, and the names that give a
// journey its context, are there for whoever translated. An entry naming an
// element the report no longer has - deleted since the file was written - is
// skipped and counted rather than failing the rest.
//
// Arguments:
//   - data: the file.
//   - trip: the report's trip.
//   - content: the report as it is now.
//   - language: the language the file must translate into.
//
// Returns:
//   - the translations, not yet normalised, and how many entries were skipped.
//   - ErrFormat for a file that is not one of these, ErrLanguage for a file of
//     another language.
func Read(data []byte, trip domain.TripSummary, content domain.DocumentContent, language string) (Result, error) {
	var file File
	if err := yaml.Unmarshal(data, &file); err != nil || file.Format != Format {
		return Result{}, ErrFormat
	}
	if file.To != language {
		return Result{}, ErrLanguage
	}

	known := make(map[uuid.UUID]domain.TranslationTarget)
	// nights are the evening stay marks, whose story alone is translated.
	nights := make(map[uuid.UUID]bool)
	for _, day := range content.Days {
		known[day.ID] = domain.TranslateDay
	}
	for _, item := range content.Items {
		if item.Kind.IsVisit() {
			known[item.ID] = domain.TranslateItem
		}
		if item.IsNight() {
			nights[item.ID] = true
		}
	}
	for _, leg := range content.Legs {
		known[leg.ID] = domain.TranslateLeg
	}
	for _, stay := range content.Stays {
		known[stay.ID] = domain.TranslateStay
	}
	for _, transfer := range content.Transfers {
		known[transfer.ID] = domain.TranslateTransfer
	}
	for _, line := range content.Tracks {
		for _, stop := range line.Stops {
			known[stop.ID] = domain.TranslateStop
		}
	}

	var result Result
	add := func(target domain.TranslationTarget, id uuid.UUID, field string, text *Text) {
		if text != nil {
			result.Translations = append(result.Translations, domain.Translation{
				Target: target, TargetID: id, Field: field, Lang: language, Value: text.Translation,
			})
		}
	}
	// element resolves an entry's id, counting it as skipped when the report
	// has no such element of that kind.
	element := func(raw string, target domain.TranslationTarget) (uuid.UUID, bool) {
		id, err := uuid.Parse(raw)
		if err != nil || known[id] != target {
			result.Skipped++
			return uuid.Nil, false
		}
		return id, true
	}

	add(domain.TranslateTrip, trip.ID, "title", file.Trip.Title)
	add(domain.TranslateTrip, trip.ID, "summary", file.Trip.Summary)
	add(domain.TranslateDocument, content.Document.ID, "intro_md", file.Document.Introduction)
	add(domain.TranslateDocument, content.Document.ID, "summary_md", file.Document.Closing)
	for _, day := range file.Days {
		if id, ok := element(day.ID, domain.TranslateDay); ok {
			add(domain.TranslateDay, id, "title", day.Title)
			add(domain.TranslateDay, id, "notes_md", day.Notes)
			add(domain.TranslateDay, id, "highlight", day.Highlight)
		}
		for _, place := range day.Places {
			if id, ok := element(place.ID, domain.TranslateItem); ok {
				add(domain.TranslateItem, id, "name", place.Name)
				add(domain.TranslateItem, id, "description_md", place.Description)
				add(domain.TranslateItem, id, "story_md", place.Story)
			}
			for _, stop := range place.Stops {
				if id, ok := element(stop.ID, domain.TranslateStop); ok {
					add(domain.TranslateStop, id, "name", stop.Name)
					add(domain.TranslateStop, id, "note_md", stop.Note)
				}
			}
		}
		for _, journey := range day.Journeys {
			if id, ok := element(journey.ID, domain.TranslateLeg); ok {
				add(domain.TranslateLeg, id, "note", journey.Note)
			}
		}
		if day.Night != nil {
			if id, err := uuid.Parse(day.Night.ID); err == nil && nights[id] {
				add(domain.TranslateItem, id, "story_md", day.Night.Story)
			} else {
				result.Skipped++
			}
		}
	}
	for _, stay := range file.Stays {
		if id, ok := element(stay.ID, domain.TranslateStay); ok {
			add(domain.TranslateStay, id, "name", stay.Name)
			add(domain.TranslateStay, id, "notes_md", stay.Notes)
		}
	}
	for _, transfer := range file.Transfers {
		if id, ok := element(transfer.ID, domain.TranslateTransfer); ok {
			add(domain.TranslateTransfer, id, "from_name", transfer.FromName)
			add(domain.TranslateTransfer, id, "to_name", transfer.ToName)
			add(domain.TranslateTransfer, id, "notes_md", transfer.Notes)
		}
	}
	if len(result.Translations) > MaxTranslations {
		return Result{}, ErrFormat
	}
	return result, nil
}
