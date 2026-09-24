package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The example trips exist so the interface can be looked at with something in
// it: one trip that has been travelled and written up, and one that is still
// only planned.
//
// They are written through the same repositories as any other request, so they
// pass the same validation and can never describe a shape the product does not
// allow. The data below is therefore a description of two trips rather than a
// set of INSERT statements that would drift away from the schema.
//
// Every account here has a published password, which is why the configuration
// refuses the flag outside development.

// demoPassword is the password of every example account. It is printed on
// start-up; the trips are throwaway and so is it.
const demoPassword = "demo-password"

// TripStore is the trip persistence the example trips need.
type TripStore interface {
	Create(ctx context.Context, trip domain.Trip) (domain.TripSummary, error)
	Get(ctx context.Context, tripID, userID uuid.UUID) (domain.TripSummary, error)
	AddMember(ctx context.Context, tripID, userID uuid.UUID, role domain.TripRole) (domain.TripMember, error)
	CreateShareLink(ctx context.Context, link domain.ShareLink, tokenHash []byte) error
	AdminList(ctx context.Context, filter domain.AdminTripFilter) (domain.TripPage, error)
}

// DocumentStore is the document persistence the example trips need.
type DocumentStore interface {
	Content(ctx context.Context, id uuid.UUID) (domain.DocumentContent, error)
	CreateReport(ctx context.Context, report domain.Trip, planID uuid.UUID) error
	UpdateDocument(ctx context.Context, id uuid.UUID, intro, summary string) error
	UpdateDay(ctx context.Context, day domain.Day) error
	CreatePlace(ctx context.Context, place domain.Item, position *int) error
	UpdatePlace(ctx context.Context, place domain.Item) error
	CreateStay(ctx context.Context, stay domain.Stay) error
	UpdateStay(ctx context.Context, stay domain.Stay) error
	CreateExpense(ctx context.Context, expense domain.Expense) error
	UpdateExpense(ctx context.Context, expense domain.Expense) error
	UpdateLeg(ctx context.Context, leg domain.Leg) error
	SaveTranslations(ctx context.Context, documentID uuid.UUID, language string, translations []domain.Translation) error
}

// demoPlace is a place of an example plan, with every field a place can carry.
type demoPlace struct {
	Name     string
	Category domain.PlaceCategory
	// Activity makes the element an activity of that type rather than a place.
	Activity domain.ActivityType
	// Difficulty is an activity's level from 1 to 5, zero for none.
	Difficulty  int
	Lat, Lng    float64
	Address     string
	URL         string
	BookingRef  string
	Description string
	// DesiredTime is an arrival time in HH:MM form, empty for none.
	DesiredTime  string
	VisitMinutes int
	Optional     bool
	// Cost is a decimal amount, empty for a place that costs nothing.
	Cost         string
	PerPerson    bool
	CostCategory domain.CostCategory
	// Report holds what the report says about the place, on a trip that has one.
	Report *demoReport
}

// demoReport is how a place turned out, for the report copied from the plan.
type demoReport struct {
	Status     domain.ItemStatus
	Story      string
	Rating     int
	ActualTime string
	// ActualEnd is when the place was left, in HH:MM form, empty for none.
	ActualEnd  string
	ActualCost string
}

// demoStay is a place to sleep of an example plan.
type demoStay struct {
	Name       string
	Kind       domain.StayKind
	Address    string
	Lat, Lng   float64
	CheckIn    string
	CheckOut   string
	CheckInAt  string
	CheckOutAt string
	BookingRef string
	URL        string
	Contacts   string
	Notes      string
	Cost       string
	ActualCost string
}

// demoExpense is a cost of an example plan that belongs to no place.
type demoExpense struct {
	Note     string
	Category domain.CostCategory
	Amount   string
	// Day is the day's number from one, or 0 for the whole trip.
	Day        int
	ActualCost string
}

// demoDay is a day of an example plan.
type demoDay struct {
	Title  string
	Notes  string
	Places []demoPlace
}

// demoLeg overrides what a leg of an example plan costs and says.
type demoLeg struct {
	// Day is the day's number from one; From names the element the leg starts at.
	Day  int
	From string
	Mode domain.TravelMode
	// DistanceM and DurationS are typed by hand, as somebody who knows the road
	// would; zero leaves the calculated value.
	DistanceM int
	DurationS int
	Cost      string
	Note      string
}

// demoTrip is a whole example trip.
type demoTrip struct {
	Title     string
	Summary   string
	Start     string
	End       string
	Timezone  string
	Currency  string
	Travelers int
	Budget    string
	Days      []demoDay
	Stays     []demoStay
	Expenses  []demoExpense
	Legs      []demoLeg
	// Ideas are the plan's unassigned places.
	Ideas []demoPlace
	// Report says whether the trip has been written up, and what its words are.
	// The report is a trip of its own, copied from the plan.
	Report *demoTripReport
}

// demoTripReport is the prose around a report's days, and the one place that was
// not in the plan.
type demoTripReport struct {
	Intro     string
	Summary   string
	DayNotes  map[int]string
	Unplanned *demoUnplanned
	// Translation is the report in its second language; nil keeps it in one.
	Translation *demoTranslation
}

// demoTranslation is part of a report in a further language. It is left
// incomplete on purpose, so the example shows the original standing in for
// what nobody translated.
type demoTranslation struct {
	Lang      string
	Title     string
	Summary   string
	Intro     string
	Closing   string
	DayTitles map[int]string
	DayNotes  map[int]string
	// Places are keyed by their name in the original.
	Places map[string]demoPlaceTranslation
}

// demoPlaceTranslation is a place of the report in a further language.
type demoPlaceTranslation struct {
	Name        string
	Description string
	Story       string
}

// demoUnplanned is a place added straight into the report.
type demoUnplanned struct {
	Day int
	demoPlace
}

// SeedDemo - fills an empty instance with two example trips.
//
// It does nothing once any trip exists, so restarting the service never doubles
// the examples. The trips belong to the first administrator, who is joined by an
// editor and a viewer so the roles can be tried out.
//
// Arguments:
//   - ctx: context bounding the work.
//   - users: account persistence, for the two extra accounts.
//   - trips: trip persistence.
//   - documents: document persistence.
//   - logger: destination for the outcome and the accounts' password.
//
// Returns:
//   - an error if the instance cannot be read or a record cannot be written.
func SeedDemo(ctx context.Context, users UserStore, trips TripStore, documents DocumentStore,
	logger *slog.Logger) error {
	existing, err := trips.AdminList(ctx, domain.AdminTripFilter{Limit: 1})
	if err != nil {
		return fmt.Errorf("look for existing trips: %w", err)
	}
	if len(existing.Items) > 0 {
		return nil
	}

	owner, err := firstAdmin(ctx, users)
	if err != nil {
		return err
	}
	if owner == uuid.Nil {
		logger.Warn("example trips need an administrator to own them",
			slog.String("hint", "set TRIPVAULT_ADMIN_EMAIL and TRIPVAULT_ADMIN_PASSWORD"))
		return nil
	}

	editor, err := demoAccount(ctx, users, "editor@example.com", "Erik the Editor")
	if err != nil {
		return err
	}
	viewer, err := demoAccount(ctx, users, "viewer@example.com", "Vera the Viewer")
	if err != nil {
		return err
	}

	for _, trip := range []demoTrip{icelandTrip(), lisbonTrip()} {
		if err := writeDemoTrip(ctx, trips, documents, owner, editor, viewer, trip, logger); err != nil {
			return fmt.Errorf("create the example trip %q: %w", trip.Title, err)
		}
	}
	logger.Info("created the example trips",
		slog.String("accounts", "editor@example.com, viewer@example.com"),
		slog.String("password", demoPassword))
	return nil
}

// firstAdmin finds the account the example trips belong to.
func firstAdmin(ctx context.Context, users UserStore) (uuid.UUID, error) {
	all, err := users.List(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read accounts: %w", err)
	}
	for _, user := range all {
		if user.IsAdmin {
			return user.ID, nil
		}
	}
	return uuid.Nil, nil
}

// demoAccount creates one of the extra accounts, or finds it if it is there.
func demoAccount(ctx context.Context, users UserStore, email, name string) (uuid.UUID, error) {
	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return uuid.Nil, err
	}
	user, err := users.Create(ctx, domain.User{
		ID:              uuid.Must(uuid.NewV7()),
		Email:           email,
		DisplayName:     name,
		PasswordHash:    hash,
		IsActive:        true,
		Theme:           domain.ThemeAuto,
		Units:           domain.UnitsKilometres,
		DefaultCurrency: domain.DefaultCurrency,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create %s: %w", email, err)
	}
	return user.ID, nil
}

// writeDemoTrip creates one example trip with its plan and, when it has one, its
// report.
func writeDemoTrip(ctx context.Context, trips TripStore, documents DocumentStore,
	owner, editor, viewer uuid.UUID, demo demoTrip, logger *slog.Logger) error {
	start, err := domain.ParseDate("start_date", demo.Start)
	if err != nil {
		return err
	}
	end, err := domain.ParseDate("end_date", demo.End)
	if err != nil {
		return err
	}
	budget, err := domain.ParseMoney("budget_amount", demo.Budget)
	if err != nil {
		return err
	}

	trip, err := domain.Trip{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, Kind: domain.DocumentPlan, Title: demo.Title,
		Summary: demo.Summary, StartDate: &start, EndDate: &end, Timezone: demo.Timezone,
		Currency: demo.Currency, Travelers: demo.Travelers, Budget: &budget,
	}.Normalize()
	if err != nil {
		return err
	}
	// The plan comes with one day per date of the trip, which is what the days
	// below describe.
	summary, err := trips.Create(ctx, trip)
	if err != nil {
		return err
	}
	if summary.PlanID == nil {
		return fmt.Errorf("the trip was created without a plan")
	}
	planID := *summary.PlanID

	if err := shareDemoTrip(ctx, trips, trip.ID, owner, editor, viewer, demo.Title, logger); err != nil {
		return err
	}
	if err := writePlan(ctx, documents, planID, demo); err != nil {
		return err
	}
	if demo.Report == nil {
		return nil
	}

	// The report is copied from the finished plan, the way a person would make
	// it, and is a trip of its own with its own people and link.
	report := trip
	report.ID = uuid.Must(uuid.NewV7())
	report.Kind = domain.DocumentReport
	report.SourceTripID = &trip.ID
	report.Languages = []string{"en"}
	if demo.Report.Translation != nil {
		report.Languages = append(report.Languages, demo.Report.Translation.Lang)
	}
	if err := documents.CreateReport(ctx, report, planID); err != nil {
		return err
	}
	written, err := trips.Get(ctx, report.ID, owner)
	if err != nil {
		return err
	}
	if written.ReportID == nil {
		return fmt.Errorf("the report was created without its document")
	}
	if err := shareDemoTrip(ctx, trips, report.ID, owner, editor, viewer, demo.Title+" (report)", logger); err != nil {
		return err
	}
	if err := writeReport(ctx, documents, *written.ReportID, demo); err != nil {
		return err
	}
	return translateReport(ctx, documents, report.ID, *written.ReportID, demo.Report.Translation)
}

// translateReport writes the example report's words in its second language,
// through the same call the editor uses.
func translateReport(ctx context.Context, documents DocumentStore, tripID, reportID uuid.UUID,
	translation *demoTranslation) error {
	if translation == nil {
		return nil
	}
	content, err := documents.Content(ctx, reportID)
	if err != nil {
		return err
	}
	var entries []domain.Translation
	add := func(target domain.TranslationTarget, id uuid.UUID, field, value string) {
		if value != "" {
			entries = append(entries, domain.Translation{Target: target, TargetID: id, Field: field,
				Lang: translation.Lang, Value: value})
		}
	}
	add(domain.TranslateTrip, tripID, "title", translation.Title)
	add(domain.TranslateTrip, tripID, "summary", translation.Summary)
	add(domain.TranslateDocument, reportID, "intro_md", translation.Intro)
	add(domain.TranslateDocument, reportID, "summary_md", translation.Closing)
	for index, day := range content.Days {
		add(domain.TranslateDay, day.ID, "title", translation.DayTitles[index+1])
		add(domain.TranslateDay, day.ID, "notes_md", translation.DayNotes[index+1])
	}
	for _, item := range content.Items {
		place, ok := translation.Places[item.Name]
		if !item.Kind.IsVisit() || !ok {
			continue
		}
		add(domain.TranslateItem, item.ID, "name", place.Name)
		add(domain.TranslateItem, item.ID, "description_md", place.Description)
		add(domain.TranslateItem, item.ID, "story_md", place.Story)
	}
	for index, entry := range entries {
		if entries[index], err = entry.Normalize(); err != nil {
			return err
		}
	}
	return documents.SaveTranslations(ctx, reportID, translation.Lang, entries)
}

// shareDemoTrip joins the editor and the viewer to a trip and hands it a link.
func shareDemoTrip(ctx context.Context, trips TripStore, tripID, owner, editor, viewer uuid.UUID, title string,
	logger *slog.Logger) error {
	for id, role := range map[uuid.UUID]domain.TripRole{editor: domain.RoleEditor, viewer: domain.RoleViewer} {
		if _, err := trips.AddMember(ctx, tripID, id, role); err != nil {
			return err
		}
	}
	return writeDemoShareLink(ctx, trips, tripID, owner, title, logger)
}

// writeDemoShareLink hands the trip a read-only link and logs where it opens.
func writeDemoShareLink(ctx context.Context, trips TripStore, tripID, owner uuid.UUID, title string,
	logger *slog.Logger) error {
	token, hash, err := auth.NewShareToken()
	if err != nil {
		return err
	}
	link, err := domain.ShareLink{
		ID: uuid.Must(uuid.NewV7()), TripID: tripID, Label: "For the family",
		CreatedBy: owner, CreatedAt: time.Now(),
	}.Normalize(time.Now())
	if err != nil {
		return err
	}
	if err := trips.CreateShareLink(ctx, link, hash); err != nil {
		return err
	}
	// The service does not know its own public address, so the path is logged and
	// the reader puts their host in front of it.
	logger.Info("example share link", slog.String("trip", title), slog.String("path", "/s#token="+token))
	return nil
}

// writePlan fills a plan: the words of each day, the stays, the places, the
// separate expenses, the unassigned ideas and the legs' own figures.
func writePlan(ctx context.Context, documents DocumentStore, planID uuid.UUID, demo demoTrip) error {
	content, err := documents.Content(ctx, planID)
	if err != nil {
		return err
	}
	if len(content.Days) < len(demo.Days) {
		return fmt.Errorf("the plan has %d days, the example describes %d", len(content.Days), len(demo.Days))
	}

	for index, day := range demo.Days {
		stored := content.Days[index]
		stored.Title, stored.NotesMD = day.Title, day.Notes
		normalized, err := stored.Normalize()
		if err != nil {
			return err
		}
		if err := documents.UpdateDay(ctx, normalized); err != nil {
			return err
		}
	}

	// The stays come first so their marks are in place before the legs are drawn
	// between the elements of each day.
	for _, stay := range demo.Stays {
		if err := writeStay(ctx, documents, planID, stay); err != nil {
			return err
		}
	}
	for index, day := range demo.Days {
		dayID := content.Days[index].ID
		for _, place := range day.Places {
			if err := writePlace(ctx, documents, planID, &dayID, place); err != nil {
				return err
			}
		}
	}
	for _, idea := range demo.Ideas {
		if err := writePlace(ctx, documents, planID, nil, idea); err != nil {
			return err
		}
	}
	for _, expense := range demo.Expenses {
		if err := writeExpense(ctx, documents, planID, content.Days, expense, false); err != nil {
			return err
		}
	}
	return writeLegs(ctx, documents, planID, demo.Legs)
}

// writePlace adds one place to a day, or to the unassigned list when dayID is nil.
func writePlace(ctx context.Context, documents DocumentStore, documentID uuid.UUID, dayID *uuid.UUID,
	place demoPlace) error {
	kind := domain.ItemPlace
	if place.Activity != "" {
		kind = domain.ItemActivity
	}
	item := domain.Item{
		ID: uuid.Must(uuid.NewV7()), DocumentID: documentID, DayID: dayID, Kind: kind, ActivityType: place.Activity,
		Name: place.Name, Category: place.Category, Address: place.Address, URL: place.URL,
		BookingRef: place.BookingRef, DescriptionMD: place.Description, VisitMinutes: place.VisitMinutes,
		IsOptional: place.Optional, CostPerPerson: place.PerPerson, CostCategory: place.CostCategory,
	}
	if place.Difficulty > 0 {
		difficulty := place.Difficulty
		item.Difficulty = &difficulty
	}
	if place.Lat != 0 || place.Lng != 0 {
		lat, lng := place.Lat, place.Lng
		item.Lat, item.Lng = &lat, &lng
	}
	if place.DesiredTime != "" {
		clock, err := domain.ParseClockTime("desired_time", place.DesiredTime)
		if err != nil {
			return err
		}
		item.DesiredTime = &clock
	}
	if place.Cost != "" {
		amount, err := domain.ParseMoney("planned_cost_amount", place.Cost)
		if err != nil {
			return err
		}
		item.PlannedCost = &amount
	}
	normalized, err := item.NormalizePlace(domain.DocumentPlan)
	if err != nil {
		return err
	}
	return documents.CreatePlace(ctx, normalized, nil)
}

// writeStay adds one place to sleep.
func writeStay(ctx context.Context, documents DocumentStore, documentID uuid.UUID, stay demoStay) error {
	checkIn, err := domain.ParseDate("check_in_date", stay.CheckIn)
	if err != nil {
		return err
	}
	checkOut, err := domain.ParseDate("check_out_date", stay.CheckOut)
	if err != nil {
		return err
	}
	item := domain.Stay{
		ID: uuid.Must(uuid.NewV7()), DocumentID: documentID, Name: stay.Name, Kind: stay.Kind,
		Address: stay.Address, CheckInDate: checkIn, CheckOutDate: checkOut, BookingRef: stay.BookingRef,
		URL: stay.URL, Contacts: stay.Contacts, NotesMD: stay.Notes,
	}
	if stay.Lat != 0 || stay.Lng != 0 {
		lat, lng := stay.Lat, stay.Lng
		item.Lat, item.Lng = &lat, &lng
	}
	for _, at := range []struct {
		value  string
		target **domain.ClockTime
	}{{stay.CheckInAt, &item.CheckInTime}, {stay.CheckOutAt, &item.CheckOutTime}} {
		if at.value == "" {
			continue
		}
		clock, err := domain.ParseClockTime("check_in_time", at.value)
		if err != nil {
			return err
		}
		*at.target = &clock
	}
	if stay.Cost != "" {
		amount, err := domain.ParseMoney("planned_cost_amount", stay.Cost)
		if err != nil {
			return err
		}
		item.PlannedCost = &amount
	}
	normalized, err := item.Normalize(domain.DocumentPlan)
	if err != nil {
		return err
	}
	return documents.CreateStay(ctx, normalized)
}

// writeExpense adds one cost that belongs to no place. In a report it also
// carries what was really spent.
func writeExpense(ctx context.Context, documents DocumentStore, documentID uuid.UUID, days []domain.Day,
	expense demoExpense, report bool) error {
	item := domain.Expense{
		ID: uuid.Must(uuid.NewV7()), DocumentID: documentID, Category: expense.Category, Note: expense.Note,
	}
	if expense.Day > 0 {
		if expense.Day > len(days) {
			return fmt.Errorf("the expense %q names day %d of %d", expense.Note, expense.Day, len(days))
		}
		dayID := days[expense.Day-1].ID
		item.DayID = &dayID
	}
	if expense.Amount != "" {
		amount, err := domain.ParseMoney("planned_amount", expense.Amount)
		if err != nil {
			return err
		}
		item.Planned = &amount
	}
	kind := domain.DocumentPlan
	if report {
		kind = domain.DocumentReport
		if expense.ActualCost != "" {
			amount, err := domain.ParseMoney("actual_amount", expense.ActualCost)
			if err != nil {
				return err
			}
			item.Actual = &amount
		}
	}
	normalized, err := item.Normalize(kind)
	if err != nil {
		return err
	}
	return documents.CreateExpense(ctx, normalized)
}

// writeLegs gives the named legs their mode, their typed figures, their cost and
// their note. A leg the plan does not have is a mistake in the example, not
// something to pass over.
func writeLegs(ctx context.Context, documents DocumentStore, documentID uuid.UUID, legs []demoLeg) error {
	if len(legs) == 0 {
		return nil
	}
	content, err := documents.Content(ctx, documentID)
	if err != nil {
		return err
	}
	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	labels := make(map[uuid.UUID]string, len(content.Items))
	for _, item := range content.Items {
		labels[item.ID] = domain.ItemLabel(item, stays)
	}

	for _, wanted := range legs {
		if wanted.Day > len(content.Days) {
			return fmt.Errorf("a leg names day %d of %d", wanted.Day, len(content.Days))
		}
		dayID := content.Days[wanted.Day-1].ID
		found := false
		for _, leg := range content.Legs {
			if leg.DayID != dayID || labels[leg.FromItemID] != wanted.From {
				continue
			}
			found = true
			if wanted.Mode != "" {
				leg.Mode = wanted.Mode
			}
			if wanted.DistanceM > 0 {
				distance := wanted.DistanceM
				leg.ManualDistanceM = &distance
			}
			if wanted.DurationS > 0 {
				duration := wanted.DurationS
				leg.ManualDurationS = &duration
			}
			if wanted.Cost != "" {
				amount, err := domain.ParseMoney("planned_cost_amount", wanted.Cost)
				if err != nil {
					return err
				}
				leg.PlannedCost = &amount
			}
			leg.Note = wanted.Note
			normalized, err := leg.Normalize(domain.DocumentPlan)
			if err != nil {
				return err
			}
			if err := documents.UpdateLeg(ctx, normalized); err != nil {
				return err
			}
			break
		}
		if !found {
			return fmt.Errorf("day %d has no leg leaving %q", wanted.Day, wanted.From)
		}
	}
	return nil
}

// writeReport fills in how the trip went, in a report copied from its plan.
func writeReport(ctx context.Context, documents DocumentStore, reportID uuid.UUID, demo demoTrip) error {
	if err := documents.UpdateDocument(ctx, reportID, demo.Report.Intro, demo.Report.Summary); err != nil {
		return err
	}

	content, err := documents.Content(ctx, reportID)
	if err != nil {
		return err
	}
	for index, notes := range demo.Report.DayNotes {
		if index > len(content.Days) {
			return fmt.Errorf("the report names day %d of %d", index, len(content.Days))
		}
		day := content.Days[index-1]
		day.NotesMD = notes
		normalized, err := day.Normalize()
		if err != nil {
			return err
		}
		if err := documents.UpdateDay(ctx, normalized); err != nil {
			return err
		}
	}

	// The places of the report are the plan's copies, matched by name.
	outcomes := map[string]demoReport{}
	for _, day := range demo.Days {
		for _, place := range day.Places {
			if place.Report != nil {
				outcomes[place.Name] = *place.Report
			}
		}
	}
	for _, item := range content.Items {
		outcome, ok := outcomes[item.Name]
		if !item.Kind.IsVisit() || !ok {
			continue
		}
		updated, err := applyOutcome(item, outcome)
		if err != nil {
			return err
		}
		if err := documents.UpdatePlace(ctx, updated); err != nil {
			return err
		}
	}

	if err := writeActualCosts(ctx, documents, content, demo); err != nil {
		return err
	}

	if extra := demo.Report.Unplanned; extra != nil {
		if extra.Day > len(content.Days) {
			return fmt.Errorf("the unplanned place names day %d of %d", extra.Day, len(content.Days))
		}
		if err := writeUnplanned(ctx, documents, reportID, content.Days[extra.Day-1].ID, *extra); err != nil {
			return err
		}
	}
	return nil
}

// writeActualCosts fills in what the report's stays and separate expenses really
// cost. Without this the report's total would count only the places, and the
// comparison with the plan would be nonsense: the accommodation and the car hire
// are most of what a trip costs.
func writeActualCosts(ctx context.Context, documents DocumentStore, content domain.DocumentContent,
	demo demoTrip) error {
	spentOnStay := map[string]string{}
	for _, stay := range demo.Stays {
		if stay.ActualCost != "" {
			spentOnStay[stay.Name] = stay.ActualCost
		}
	}
	for _, stay := range content.Stays {
		spent, ok := spentOnStay[stay.Name]
		if !ok {
			continue
		}
		amount, err := domain.ParseMoney("actual_cost_amount", spent)
		if err != nil {
			return err
		}
		stay.ActualCost = &amount
		normalized, err := stay.Normalize(domain.DocumentReport)
		if err != nil {
			return err
		}
		if err := documents.UpdateStay(ctx, normalized); err != nil {
			return err
		}
	}

	spentOnExpense := map[string]string{}
	for _, expense := range demo.Expenses {
		if expense.ActualCost != "" {
			spentOnExpense[expense.Note] = expense.ActualCost
		}
	}
	for _, expense := range content.Expenses {
		spent, ok := spentOnExpense[expense.Note]
		if !ok {
			continue
		}
		amount, err := domain.ParseMoney("actual_amount", spent)
		if err != nil {
			return err
		}
		expense.Actual = &amount
		normalized, err := expense.Normalize(domain.DocumentReport)
		if err != nil {
			return err
		}
		if err := documents.UpdateExpense(ctx, normalized); err != nil {
			return err
		}
	}
	return nil
}

// applyOutcome writes onto a place of a report how it turned out.
func applyOutcome(item domain.Item, outcome demoReport) (domain.Item, error) {
	item.Status, item.StoryMD = outcome.Status, outcome.Story
	if outcome.Rating > 0 {
		rating := outcome.Rating
		item.Rating = &rating
	}
	if outcome.ActualTime != "" {
		clock, err := domain.ParseClockTime("actual_time", outcome.ActualTime)
		if err != nil {
			return item, err
		}
		item.ActualTime = &clock
	}
	if outcome.ActualEnd != "" {
		clock, err := domain.ParseClockTime("actual_end_time", outcome.ActualEnd)
		if err != nil {
			return item, err
		}
		item.ActualEndTime = &clock
	}
	if outcome.ActualCost != "" {
		amount, err := domain.ParseMoney("actual_cost_amount", outcome.ActualCost)
		if err != nil {
			return item, err
		}
		item.ActualCost = &amount
	}
	return item.NormalizePlace(domain.DocumentReport)
}

// writeUnplanned adds the one place that was never in the plan.
func writeUnplanned(ctx context.Context, documents DocumentStore, reportID, dayID uuid.UUID,
	extra demoUnplanned) error {
	item := domain.Item{
		ID: uuid.Must(uuid.NewV7()), DocumentID: reportID, DayID: &dayID, Kind: domain.ItemPlace,
		Name: extra.Name, Category: extra.Category, Address: extra.Address,
		DescriptionMD: extra.Description, VisitMinutes: extra.VisitMinutes,
		CostCategory: extra.CostCategory, Status: domain.StatusUnplanned,
	}
	if extra.Lat != 0 || extra.Lng != 0 {
		lat, lng := extra.Lat, extra.Lng
		item.Lat, item.Lng = &lat, &lng
	}
	if extra.Report != nil {
		updated, err := applyOutcome(item, *extra.Report)
		if err != nil {
			return err
		}
		updated.Status = domain.StatusUnplanned
		normalized, err := updated.NormalizePlace(domain.DocumentReport)
		if err != nil {
			return err
		}
		return documents.CreatePlace(ctx, normalized, nil)
	}
	normalized, err := item.NormalizePlace(domain.DocumentReport)
	if err != nil {
		return err
	}
	return documents.CreatePlace(ctx, normalized, nil)
}
