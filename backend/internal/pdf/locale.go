package pdf

import (
	"fmt"
	"math"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The document is rendered on the server, so the words around the trip's own
// text have to be here rather than in the interface's dictionaries. They are a
// struct rather than a map so that a language missing a line does not compile;
// the alternative, a key that is silently absent, would show up as a blank label
// in somebody's printed report.
//
// Nothing here needs a plural form: every count is written as a label beside its
// number ("Days: 5"), which reads the same in both languages and spares the
// document a grammar it would otherwise have to know.

// labels is the wording of one language, and the units it writes distances in.
//
// The units sit here rather than in every call because they are a choice about
// how the document reads, exactly like the language: resolved once, and then not
// thought about again.
type labels struct {
	code  string
	units domain.Units

	travelers string
	currency  string
	summary   string
	planFact  string
	tripMap   string
	days      string
	nights    string
	visited   string
	skipped   string
	unplanned string
	planned   string
	distance  string
	spent     string
	rating    string
	ratingOf  string
	rated     string

	// difficulty names the level of an activity, and difficulties the five
	// levels from very easy to extreme.
	difficulty   string
	difficulties [domain.MaxDifficulty]string

	day          string
	track        string
	stay         string
	checkIn      string
	checkOut     string
	expenses     string
	byMode       string
	plannedCost  string
	actualCost   string
	difference   string
	statuses     map[string]string
	modes        map[string]string
	stayKinds    map[string]string
	categories   map[string]string
	activities   map[string]string
	months       [12]string
	dateOrder    func(day int, month string, year int) string
	hourMinute   string
	kilometres   string
	miles        string
	hoursMinutes func(hours, minutes int) string

	// metres and feet read a height, which is counted in feet where distances
	// are counted in miles.
	metres string
	feet   string
	// climb reads a recording's height gained and lost.
	climb string
	// legFrom names where the journey that opens a day started.
	legFrom string
}

// english is the wording every instance has.
var english = labels{
	code:        "en",
	travelers:   "Travellers",
	currency:    "Currency",
	summary:     "Summary",
	planFact:    "Plan and fact",
	tripMap:     "Route",
	days:        "Days",
	nights:      "Nights",
	visited:     "Visited",
	skipped:     "Skipped",
	unplanned:   "Unplanned",
	planned:     "Planned",
	distance:    "Distance",
	spent:       "Spent",
	rating:      "Average rating",
	ratingOf:    "Rating",
	rated:       "%d rated",
	day:         "Day %d",
	track:       "Recorded track: %s",
	stay:        "Stay",
	checkIn:     "from",
	checkOut:    "to",
	expenses:    "Other costs",
	byMode:      "By means of travel",
	plannedCost: "Planned",
	actualCost:  "Spent",
	difference:  "Difference",
	statuses: map[string]string{
		"planned": "Planned", "visited": "Visited",
		"skipped": "Skipped", "unplanned": "Unplanned",
	},
	modes: map[string]string{
		"walk": "On foot", "car": "By car", "bike": "By bicycle",
		"transit": "By public transport", "flight": "By air", "other": "Other",
	},
	activities: map[string]string{
		"hike": "Hike", "walk": "Walk", "bike": "Bike ride", "run": "Run",
		"canyoning": "Canyoning", "climbing": "Climbing", "via_ferrata": "Via ferrata",
		"kayak": "Kayaking", "swim": "Swim", "ski": "Skiing", "tour": "Guided tour", "other": "Activity",
	},
	stayKinds: map[string]string{
		"hotel": "Hotel", "apartment": "Apartment", "hostel": "Hostel",
		"camping": "Camping", "friends": "With friends", "other": "Other",
	},
	categories: map[string]string{
		"accommodation": "Accommodation", "transport": "Transport", "food": "Food",
		"activities": "Activities", "shopping": "Shopping", "other": "Other",
	},
	months: [12]string{"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"},
	dateOrder:  func(day int, month string, year int) string { return fmt.Sprintf("%d %s %d", day, month, year) },
	hourMinute: "%02d:%02d",
	kilometres: "%s km",
	miles:      "%s mi",
	metres:     "%d m",
	feet:       "%d ft",
	climb:      "up %s, down %s",
	legFrom:    "from %s",
	hoursMinutes: func(hours, minutes int) string {
		if hours == 0 {
			return fmt.Sprintf("%d min", minutes)
		}
		return fmt.Sprintf("%d h %02d min", hours, minutes)
	},

	difficulty:   "Difficulty: %s",
	difficulties: [domain.MaxDifficulty]string{"very easy", "easy", "moderate", "hard", "extreme"},
}

// russian is the wording of the Russian interface, the one place in the backend
// where text other than English belongs: the document is rendered here, and a
// reader whose interface is Russian should not be handed an English page.
var russian = labels{
	code:        "ru",
	travelers:   "Участников",
	currency:    "Валюта",
	summary:     "Итоги",
	planFact:    "План и факт",
	tripMap:     "Маршрут",
	days:        "Дней",
	nights:      "Ночей",
	visited:     "Посещено",
	skipped:     "Пропущено",
	unplanned:   "Вне плана",
	planned:     "По плану",
	distance:    "Расстояние",
	spent:       "Потрачено",
	rating:      "Средняя оценка",
	ratingOf:    "Оценка",
	rated:       "с оценкой: %d",
	day:         "День %d",
	track:       "Записанный трек: %s",
	stay:        "Проживание",
	checkIn:     "с",
	checkOut:    "по",
	expenses:    "Прочие расходы",
	byMode:      "По способам передвижения",
	plannedCost: "План",
	actualCost:  "Факт",
	difference:  "Разница",
	statuses: map[string]string{
		"planned": "В плане", "visited": "Посещено",
		"skipped": "Пропущено", "unplanned": "Вне плана",
	},
	modes: map[string]string{
		"walk": "Пешком", "car": "На машине", "bike": "На велосипеде",
		"transit": "Общественным транспортом", "flight": "Самолётом", "other": "Иначе",
	},
	activities: map[string]string{
		"hike": "Поход", "walk": "Прогулка", "bike": "Велопрогулка", "run": "Пробежка",
		"canyoning": "Каньонинг", "climbing": "Скалолазание", "via_ferrata": "Виа феррата",
		"kayak": "Каякинг", "swim": "Плавание", "ski": "Лыжи", "tour": "Экскурсия", "other": "Активность",
	},
	stayKinds: map[string]string{
		"hotel": "Отель", "apartment": "Квартира", "hostel": "Хостел",
		"camping": "Кемпинг", "friends": "У друзей", "other": "Другое",
	},
	categories: map[string]string{
		"accommodation": "Проживание", "transport": "Транспорт", "food": "Еда",
		"activities": "Активности", "shopping": "Покупки", "other": "Прочее",
	},
	months: [12]string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"},
	dateOrder:  func(day int, month string, year int) string { return fmt.Sprintf("%d %s %d", day, month, year) },
	hourMinute: "%02d:%02d",
	kilometres: "%s км",
	miles:      "%s миль",
	metres:     "%d м",
	feet:       "%d футов",
	climb:      "набор %s, сброс %s",
	legFrom:    "от %s",
	hoursMinutes: func(hours, minutes int) string {
		if hours == 0 {
			return fmt.Sprintf("%d мин", minutes)
		}
		return fmt.Sprintf("%d ч %02d мин", hours, minutes)
	},

	difficulty:   "Сложность: %s",
	difficulties: [domain.MaxDifficulty]string{"очень легко", "легко", "средне", "сложно", "экстрим"},
}

// wording - picks the language a document is written in and the units it counts in.
//
// Arguments:
//   - language: the reader's language, as the profile or the request spells it.
//   - units: what the reader counts distances in; kilometres when unset.
//
// Returns:
//   - the Russian wording for "ru", and English for anything else, so an
//     unknown language produces a readable document rather than none.
func wording(language string, units domain.Units) labels {
	text := english
	if len(language) >= 2 && language[:2] == "ru" {
		text = russian
	}
	text.units = units.OrDefault()
	return text
}

// date - writes a date the way the language does.
//
// Arguments:
//   - at: the date to write.
//
// Returns:
//   - the date in words, such as "12 June 2026".
func (l labels) date(at time.Time) string {
	return l.dateOrder(at.Day(), l.months[int(at.Month())-1], at.Year())
}

// dateTime - writes a date with the time of day, as a photograph's caption.
//
// Arguments:
//   - at: the instant, already in the zone it should be read in.
//
// Returns:
//   - the date and the time, such as "12 June 2026, 15:20".
func (l labels) dateTime(at time.Time) string {
	return fmt.Sprintf("%s, %s", l.date(at), l.clock(at.Hour()*60+at.Minute()))
}

// dateRange - writes the span a trip covers.
//
// Arguments:
//   - from, to: the first and last day; either may be nil on a trip with no
//     dates, in which case the other alone is written.
//
// Returns:
//   - the span, or an empty string when neither date is known.
func (l labels) dateRange(from, to *time.Time) string {
	switch {
	case from == nil && to == nil:
		return ""
	case from == nil:
		return l.date(*to)
	case to == nil:
		return l.date(*from)
	case from.Equal(*to):
		return l.date(*from)
	case from.Year() == to.Year() && from.Month() == to.Month():
		// One month: the month and the year are written once.
		return fmt.Sprintf("%d–%s", from.Day(), l.date(*to))
	default:
		return fmt.Sprintf("%s – %s", l.date(*from), l.date(*to))
	}
}

// clock - writes a time of day counted in minutes from midnight.
//
// Arguments:
//   - minutes: the time, 0 to 1439.
//
// Returns:
//   - the time in HH:MM.
func (l labels) clock(minutes int) string {
	return fmt.Sprintf(l.hourMinute, minutes/60, minutes%60)
}

// clockPeriod - writes when a place was reached and, when it is known, when it
// was left.
//
// Arguments:
//   - from: the time it was reached.
//   - to: the time it was left, or nil.
//
// Returns:
//   - "HH:MM" or "HH:MM–HH:MM".
func (l labels) clockPeriod(from domain.ClockTime, to *domain.ClockTime) string {
	if to == nil {
		return l.clock(int(from))
	}
	return l.clock(int(from)) + "–" + l.clock(int(*to))
}

// duration - writes a length of time given in seconds.
//
// Arguments:
//   - seconds: how long it took.
//
// Returns:
//   - the length in hours and minutes.
func (l labels) duration(seconds int) string {
	minutes := (seconds + 30) / 60
	return l.hoursMinutes(minutes/60, minutes%60)
}

// metresPerMile is the exact length of a statute mile.
const metresPerMile = 1609.344

// metresPerFoot is the exact length of an international foot.
const metresPerFoot = 0.3048

// distanceOf - writes a distance given in metres, in the reader's units.
//
// Arguments:
//   - metres: the distance.
//
// Returns:
//   - the distance, with one decimal below ten and none above it: a report is
//     read, not navigated by.
func (l labels) distanceOf(metres int) string {
	format, value := l.kilometres, float64(metres)/1000
	if l.units == domain.UnitsMiles {
		format, value = l.miles, float64(metres)/metresPerMile
	}
	if value < 10 {
		return fmt.Sprintf(format, fmt.Sprintf("%.1f", value))
	}
	return fmt.Sprintf(format, fmt.Sprintf("%.0f", value))
}

// heightOf - formats a height gained or lost in the reader's units: metres, or
// feet for somebody who counts distances in miles.
//
// Arguments:
//   - metres: the height in metres.
//
// Returns:
//   - the height, rounded to a whole unit.
func (l labels) heightOf(metres int) string {
	if l.units == domain.UnitsMiles {
		return fmt.Sprintf(l.feet, int(math.Round(float64(metres)/metresPerFoot)))
	}
	return fmt.Sprintf(l.metres, metres)
}
