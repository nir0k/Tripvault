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
// Most counts are written as a label beside their number ("Days: 5"), which
// reads the same in both languages. The few the report's journal pages write
// as words - "4 days", "6 places" - go through count, which knows the three
// forms a Russian noun takes after a number.

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
	rating    string
	ratingOf  string
	rated     string

	// difficulty names the level of an activity, and difficulties the five
	// levels from very easy to extreme.
	difficulty   string
	difficulties [domain.MaxDifficulty]string

	day       string
	track     string
	trackTime string
	// trackEstimate reads how long a line without times takes to walk.
	trackEstimate string
	stay          string
	checkIn       string
	checkOut      string
	byMode        string
	statuses      map[string]string
	modes         map[string]string
	stayKinds     map[string]string
	// transferKinds names how a transfer travels; departs and arrives read
	// its two ends.
	transferKinds map[string]string
	departs       string
	arrives       string
	categories    map[string]string
	activities    map[string]string
	// stopKinds names what a stop along a line is for; stopFromStart reads how
	// long the way to it takes from the start of the line.
	stopKinds     map[string]string
	stopFromStart string
	months        [12]string
	dateOrder     func(day int, month string, year int) string
	hourMinute    string
	kilometres    string
	miles         string
	hoursMinutes  func(hours, minutes int) string

	// metres and feet read a height, which is counted in feet where distances
	// are counted in miles.
	metres string
	feet   string
	// climb reads a recording's height gained and lost.
	climb string
	// legFrom names where the journey that opens a day started.
	legFrom string

	// The words of a plan's document, which is read on the way rather than
	// afterwards: where to sleep, how to get there, what comes next.
	contents  string
	overview  string
	stays     string
	transfers string
	ideas     string
	optional  string
	onSite    string
	wantedAt  string
	late      string
	booking   string
	dayStarts string
	dayEnds   string
	onTheRoad string
	night     string
	qrHint    string
	qrPlace   string
	qrSite    string

	// The words of the report's journal pages: the cover's label, the page
	// that sums the trip up, a day's chapter and the end of it.
	journal      string
	glance       string
	journey      string
	route        string
	dayNumber    string
	dayStory     string
	highlight    string
	endOfDay     string
	moreFromDay  string
	overviewPage string
	closingPage  string
	averageShort string
	// dayWord and placeWord are the three forms of a noun
	// counted by count: one, a few, many. English uses the first two.
	dayWord         [3]string
	placeWord       [3]string
	placeCategories map[string]string

	// The words of a packing list: its title, the items without a category
	// and a list with nothing in it.
	packingTitle string
	packingOther string
	packingEmpty string
}

// english is the wording every instance has.
var english = labels{
	code:          "en",
	travelers:     "Travellers",
	currency:      "Currency",
	summary:       "Summary",
	planFact:      "Plan and fact",
	tripMap:       "Route",
	days:          "Days",
	nights:        "Nights",
	visited:       "Visited",
	skipped:       "Skipped",
	unplanned:     "Unplanned",
	planned:       "Planned",
	distance:      "Distance",
	rating:        "Average rating",
	ratingOf:      "Rating",
	rated:         "%d rated",
	day:           "Day %d",
	track:         "Recorded track: %s",
	trackTime:     "time %s",
	trackEstimate: "time about %s",
	stay:          "Stay",
	checkIn:       "from",
	checkOut:      "to",
	byMode:        "By means of travel",
	statuses: map[string]string{
		"planned": "Planned", "visited": "Visited",
		"skipped": "Skipped", "unplanned": "Unplanned",
	},
	modes: map[string]string{
		"walk": "On foot", "car": "By car", "bike": "By bicycle",
		"transit": "By public transport", "bus": "By bus", "train": "By train", "tram": "By metro or tram",
		"ferry": "By ferry", "flight": "By air", "cable_car": "By cable car", "other": "Other",
	},
	activities: map[string]string{
		"hike": "Hike", "walk": "Walk", "bike": "Bike ride", "run": "Run",
		"canyoning": "Canyoning", "climbing": "Climbing", "via_ferrata": "Via ferrata",
		"kayak": "Kayaking", "swim": "Swim", "ski": "Skiing", "tour": "Guided tour", "other": "Activity",
	},
	stopKinds: map[string]string{
		"food": "Café", "shop": "Shop", "rest": "Rest", "viewpoint": "Viewpoint", "water": "Water",
		"shelter": "Shelter", "hut": "Hut", "summit": "Summit", "cave": "Cave", "swim": "Swimming",
		"other": "Stop",
	},
	stopFromStart: "about %s from the start",
	stayKinds: map[string]string{
		"hotel": "Hotel", "apartment": "Apartment", "hostel": "Hostel",
		"camping": "Camping", "friends": "With friends", "other": "Other",
	},
	transferKinds: map[string]string{
		"flight": "Flight", "train": "Train", "bus": "Bus",
		"ferry": "Ferry", "transfer": "Transfer", "other": "Journey",
	},
	departs: "departs %s",
	arrives: "arrives %s",
	categories: map[string]string{
		"accommodation": "Lodging", "transport": "Transport", "car_rental": "Car rental",
		"fuel": "Fuel", "tolls": "Tolls and vignettes", "food": "Restaurants and bars",
		"groceries": "Groceries", "sightseeing": "Sightseeing", "activities": "Activities",
		"shopping": "Shopping", "other": "Other",
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

	contents:  "Days",
	overview:  "Stays and journeys",
	stays:     "Where to sleep",
	transfers: "Flights and transfers",
	ideas:     "Ideas without a day",
	optional:  "optional",
	onSite:    "%s on site",
	wantedAt:  "wanted at %s",
	late:      "later than wanted",
	booking:   "Booking: %s",
	dayStarts: "starts at %s",
	dayEnds:   "ends at %s",
	onTheRoad: "%s on the road",
	night:     "Night: %s",
	qrHint: "The QR code at the top right of a place shows it on a map, the one at its bottom left opens its " +
		"website; on the screen, the codes and the address can be clicked.",
	qrPlace: "On the map",
	qrSite:  "Website",

	journal:      "Travel journal",
	glance:       "Trip at a glance",
	journey:      "The journey",
	route:        "Route",
	dayNumber:    "Day %02d",
	dayStory:     "Day story",
	highlight:    "Moment of the day",
	endOfDay:     "End of the day",
	moreFromDay:  "More from the day",
	overviewPage: "Overview",
	closingPage:  "Looking back",
	averageShort: "Avg rating",
	dayWord:      [3]string{"day", "days", "days"},
	placeWord:    [3]string{"place", "places", "places"},
	packingTitle: "Packing list",
	packingOther: "Other things",
	packingEmpty: "Nothing is on the list yet.",
	placeCategories: map[string]string{
		"sight": "Sight", "nature": "Nature", "museum": "Museum", "food": "Food", "shopping": "Shopping",
		"activity": "Activity", "transport": "Transport", "parking": "Parking", "other": "Other",
	},
}

// russian is the wording of the Russian interface, the one place in the backend
// where text other than English belongs: the document is rendered here, and a
// reader whose interface is Russian should not be handed an English page.
var russian = labels{
	code:          "ru",
	travelers:     "Участников",
	currency:      "Валюта",
	summary:       "Итоги",
	planFact:      "План и факт",
	tripMap:       "Маршрут",
	days:          "Дней",
	nights:        "Ночей",
	visited:       "Посещено",
	skipped:       "Пропущено",
	unplanned:     "Вне плана",
	planned:       "По плану",
	distance:      "Расстояние",
	rating:        "Средняя оценка",
	ratingOf:      "Оценка",
	rated:         "с оценкой: %d",
	day:           "День %d",
	track:         "Записанный трек: %s",
	trackTime:     "время %s",
	trackEstimate: "время около %s",
	stay:          "Проживание",
	checkIn:       "с",
	checkOut:      "по",
	byMode:        "По способам передвижения",
	statuses: map[string]string{
		"planned": "В плане", "visited": "Посещено",
		"skipped": "Пропущено", "unplanned": "Вне плана",
	},
	modes: map[string]string{
		"walk": "Пешком", "car": "На машине", "bike": "На велосипеде",
		"transit": "Общественным транспортом", "bus": "Автобусом", "train": "Поездом", "tram": "На метро или трамвае",
		"ferry": "Паромом", "flight": "Самолётом", "cable_car": "Канатной дорогой", "other": "Иначе",
	},
	activities: map[string]string{
		"hike": "Поход", "walk": "Прогулка", "bike": "Велопрогулка", "run": "Пробежка",
		"canyoning": "Каньонинг", "climbing": "Скалолазание", "via_ferrata": "Виа феррата",
		"kayak": "Каякинг", "swim": "Плавание", "ski": "Лыжи", "tour": "Экскурсия", "other": "Активность",
	},
	stopKinds: map[string]string{
		"food": "Кафе", "shop": "Магазин", "rest": "Отдых", "viewpoint": "Смотровая", "water": "Вода",
		"shelter": "Укрытие", "hut": "Хижина", "summit": "Вершина", "cave": "Пещера", "swim": "Купание",
		"other": "Остановка",
	},
	stopFromStart: "около %s от старта",
	stayKinds: map[string]string{
		"hotel": "Отель", "apartment": "Квартира", "hostel": "Хостел",
		"camping": "Кемпинг", "friends": "У друзей", "other": "Другое",
	},
	transferKinds: map[string]string{
		"flight": "Перелёт", "train": "Поезд", "bus": "Автобус",
		"ferry": "Паром", "transfer": "Трансфер", "other": "Переезд",
	},
	departs: "отправление %s",
	arrives: "прибытие %s",
	categories: map[string]string{
		"accommodation": "Проживание", "transport": "Транспорт", "car_rental": "Аренда авто",
		"fuel": "Топливо", "tolls": "Платные дороги и виньетки", "food": "Рестораны и бары",
		"groceries": "Продукты", "sightseeing": "Достопримечательности", "activities": "Активности",
		"shopping": "Покупки", "other": "Прочее",
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

	contents:  "Дни",
	overview:  "Проживание и переезды",
	stays:     "Где ночуем",
	transfers: "Перелёты и трансферы",
	ideas:     "Идеи без дня",
	optional:  "необязательно",
	onSite:    "на месте %s",
	wantedAt:  "желательно к %s",
	late:      "позже желаемого",
	booking:   "Бронь: %s",
	dayStarts: "начало в %s",
	dayEnds:   "конец в %s",
	onTheRoad: "в пути %s",
	night:     "Ночёвка: %s",
	qrHint: "QR-код справа вверху у места показывает его на карте, слева внизу — открывает сайт места; " +
		"на экране можно нажать на коды и на адрес.",
	qrPlace: "На карте",
	qrSite:  "Сайт места",

	journal:      "Дневник путешествия",
	glance:       "Коротко о поездке",
	journey:      "Путешествие",
	route:        "Маршрут",
	dayNumber:    "День %02d",
	dayStory:     "Рассказ дня",
	highlight:    "Главный момент дня",
	endOfDay:     "Конец дня",
	moreFromDay:  "Ещё из этого дня",
	overviewPage: "Обзор",
	closingPage:  "Итоги",
	averageShort: "Средняя оценка",
	dayWord:      [3]string{"день", "дня", "дней"},
	placeWord:    [3]string{"место", "места", "мест"},
	packingTitle: "Что взять",
	packingOther: "Без категории",
	packingEmpty: "В списке пока ничего нет.",
	placeCategories: map[string]string{
		"sight": "Достопримечательность", "nature": "Природа", "museum": "Музей", "food": "Еда",
		"shopping": "Покупки", "activity": "Активность", "transport": "Транспорт", "parking": "Парковка",
		"other": "Другое",
	},
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

// trackDistanceOf - formats the length of a recording with a tenth of a
// kilometre or a mile whatever its length, as a watch shows it: a hike of
// 21.4 km is not a hike of 21.
//
// Arguments:
//   - metres: the length.
//
// Returns:
//   - the length in the reader's units.
func (l labels) trackDistanceOf(metres int) string {
	format, value := l.kilometres, float64(metres)/1000
	if l.units == domain.UnitsMiles {
		format, value = l.miles, float64(metres)/metresPerMile
	}
	return fmt.Sprintf(format, fmt.Sprintf("%.1f", value))
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

// count writes a number with its noun in the form the number asks for: in
// Russian one form after 1, 21, 31..., another after 2 to 4 and their like,
// and a third after everything else; in English one form for 1 and another
// for every other number.
//
// Arguments:
//   - n: the number.
//   - forms: the noun's forms, as dayWord holds them.
//
// Returns:
//   - the number followed by its noun.
func (l labels) count(n int, forms [3]string) string {
	if l.code != "ru" {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, forms[0])
		}
		return fmt.Sprintf("%d %s", n, forms[1])
	}
	tens, units := n%100, n%10
	switch {
	case tens >= 11 && tens <= 14:
		return fmt.Sprintf("%d %s", n, forms[2])
	case units == 1:
		return fmt.Sprintf("%d %s", n, forms[0])
	case units >= 2 && units <= 4:
		return fmt.Sprintf("%d %s", n, forms[1])
	}
	return fmt.Sprintf("%d %s", n, forms[2])
}
