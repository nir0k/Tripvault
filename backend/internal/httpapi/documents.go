package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Every change to a document answers with the whole document, recomputed:
// a change to one element moves stay marks, schedules and totals elsewhere, and
// one round trip keeps a client from showing a half-updated plan.

// scheduleResponse is when the schedule reaches an element of a day, in
// minutes after the midnight the day starts on.
type scheduleResponse struct {
	ArrivalMinutes   int  `json:"arrival_minutes"`
	DepartureMinutes int  `json:"departure_minutes"`
	Late             bool `json:"late"`
}

// itemResponse is a place, an activity or a stay mark.
type itemResponse struct {
	ID                string            `json:"id"`
	DayID             *string           `json:"day_id"`
	Position          int               `json:"position"`
	Kind              string            `json:"kind"`
	Anchor            *string           `json:"anchor"`
	StayID            *string           `json:"stay_id"`
	Name              string            `json:"name"`
	Category          string            `json:"category"`
	ActivityType      *string           `json:"activity_type"`
	Lat               *float64          `json:"lat"`
	Lng               *float64          `json:"lng"`
	Address           string            `json:"address"`
	OSMRef            string            `json:"osm_ref"`
	DescriptionMD     string            `json:"description_md"`
	URL               string            `json:"url"`
	DesiredTime       *string           `json:"desired_time"`
	VisitMinutes      int               `json:"visit_minutes"`
	IsOptional        bool              `json:"is_optional"`
	BookingRef        string            `json:"booking_ref"`
	PlannedCostAmount *string           `json:"planned_cost_amount"`
	CostPerPerson     bool              `json:"cost_per_person"`
	CostCategory      string            `json:"cost_category"`
	Schedule          *scheduleResponse `json:"schedule"`
	// The fields below are a report's; in a plan they are empty.
	Status           string  `json:"status"`
	StoryMD          string  `json:"story_md"`
	ActualTime       *string `json:"actual_time"`
	ActualEndTime    *string `json:"actual_end_time"`
	Rating           *int    `json:"rating"`
	ActualCostAmount *string `json:"actual_cost_amount"`
	// Difficulty is an activity's, 1 to 5, or null.
	Difficulty *int `json:"difficulty"`
	// SourceItemID is the place of the plan this one was copied from.
	SourceItemID *string `json:"source_item_id"`
	// Media are the place's pictures and CoverMediaID the one it is shown by.
	Media        []mediaResponse `json:"media"`
	CoverMediaID *string         `json:"cover_media_id"`
	// Track is the line the place or activity was recorded along, or null.
	Track *trackResponse `json:"track"`
}

// legResponse is the journey between two neighbouring elements. distance_m and
// duration_s are the values the plan uses: typed ones where present.
type legResponse struct {
	ID                string     `json:"id"`
	FromItemID        string     `json:"from_item_id"`
	ToItemID          string     `json:"to_item_id"`
	Mode              string     `json:"mode"`
	DistanceM         *int       `json:"distance_m"`
	DurationS         *int       `json:"duration_s"`
	CalculatedDistM   *int       `json:"calculated_distance_m"`
	CalculatedDurS    *int       `json:"calculated_duration_s"`
	ManualDistance    bool       `json:"manual_distance"`
	ManualDuration    bool       `json:"manual_duration"`
	Geometry          string     `json:"geometry"`
	Source            string     `json:"source"`
	Error             *string    `json:"error"`
	CalculatedAt      *time.Time `json:"calculated_at"`
	PlannedCostAmount *string    `json:"planned_cost_amount"`
	ActualCostAmount  *string    `json:"actual_cost_amount"`
	Note              string     `json:"note"`
}

// modeTotalResponse sums the legs of one travel mode.
type modeTotalResponse struct {
	Mode      string `json:"mode"`
	DistanceM int    `json:"distance_m"`
	DurationS int    `json:"duration_s"`
}

// daySummaryResponse totals a day.
type daySummaryResponse struct {
	VisitMinutes  int                 `json:"visit_minutes"`
	TravelMinutes int                 `json:"travel_minutes"`
	DistanceM     int                 `json:"distance_m"`
	ByMode        []modeTotalResponse `json:"by_mode"`
	UnknownTravel bool                `json:"unknown_travel"`
	PendingLegs   int                 `json:"pending_legs"`
	EstimatedLegs int                 `json:"estimated_legs"`
	EndMinutes    int                 `json:"end_minutes"`
	PlannedCost   string              `json:"planned_cost"`
	ActualCost    string              `json:"actual_cost"`
}

// travelModes is the order modes are listed in.
var travelModes = []domain.TravelMode{domain.ModeWalk, domain.ModeCar, domain.ModeBike, domain.ModeTransit,
	domain.ModeFlight, domain.ModeOther}

// newLegResponse maps a leg onto the wire.
func newLegResponse(leg domain.Leg) legResponse {
	return legResponse{
		ID:                leg.ID.String(),
		FromItemID:        leg.FromItemID.String(),
		ToItemID:          leg.ToItemID.String(),
		Mode:              string(leg.Mode),
		DistanceM:         leg.Distance(),
		DurationS:         leg.Duration(),
		CalculatedDistM:   leg.DistanceM,
		CalculatedDurS:    leg.DurationS,
		ManualDistance:    leg.ManualDistanceM != nil,
		ManualDuration:    leg.ManualDurationS != nil,
		Geometry:          leg.Geometry,
		Source:            string(leg.Source),
		Error:             optionalString(leg.Error),
		CalculatedAt:      leg.CalculatedAt,
		PlannedCostAmount: formatMoney(leg.PlannedCost),
		ActualCostAmount:  formatMoney(leg.ActualCost),
		Note:              leg.Note,
	}
}

// dayResponse is a day with its elements in schedule order.
type dayResponse struct {
	ID            string             `json:"id"`
	Position      int                `json:"position"`
	Date          *string            `json:"date"`
	Title         string             `json:"title"`
	NotesMD       string             `json:"notes_md"`
	StartTime     string             `json:"start_time"`
	DefaultMode   *string            `json:"default_mode"`
	Timezone      *string            `json:"timezone"`
	MorningAnchor bool               `json:"morning_anchor"`
	EveningAnchor bool               `json:"evening_anchor"`
	NoOvernight   bool               `json:"no_overnight"`
	Items         []itemResponse     `json:"items"`
	Legs          []legResponse      `json:"legs"`
	Summary       daySummaryResponse `json:"summary"`
	// Media are the day's pictures, in the order they were linked, and
	// CoverMediaID is the one it is shown by.
	Media        []mediaResponse `json:"media"`
	CoverMediaID *string         `json:"cover_media_id"`
}

// trackResponse is a recorded line of a place or an activity: the line
// itself and what was measured from it. The file it came from is downloaded
// separately, by the track's identifier.
type trackResponse struct {
	ID           string `json:"id"`
	OriginalName string `json:"original_name"`
	Format       string `json:"format"`
	DistanceM    int    `json:"distance_m"`
	PointCount   int    `json:"point_count"`
	// AscentM and DescentM are null when the file records no heights.
	AscentM  *int   `json:"ascent_m"`
	DescentM *int   `json:"descent_m"`
	Geometry string `json:"geometry"`
	// StartedAt and EndedAt are null when the file records no time.
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// newTrackResponse maps a track onto the wire.
func newTrackResponse(track *domain.Track) *trackResponse {
	if track == nil {
		return nil
	}
	return &trackResponse{
		ID:           track.ID.String(),
		OriginalName: track.OriginalName,
		Format:       track.Format,
		DistanceM:    track.DistanceM,
		PointCount:   track.PointCount,
		AscentM:      track.AscentM,
		DescentM:     track.DescentM,
		Geometry:     track.Geometry,
		StartedAt:    track.StartedAt,
		EndedAt:      track.EndedAt,
	}
}

// stayResponse is a place to sleep.
type stayResponse struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	Address           string   `json:"address"`
	Lat               *float64 `json:"lat"`
	Lng               *float64 `json:"lng"`
	CheckInDate       string   `json:"check_in_date"`
	CheckInTime       *string  `json:"check_in_time"`
	CheckOutDate      string   `json:"check_out_date"`
	CheckOutTime      *string  `json:"check_out_time"`
	BookingRef        string   `json:"booking_ref"`
	URL               string   `json:"url"`
	Contacts          string   `json:"contacts"`
	NotesMD           string   `json:"notes_md"`
	PlannedCostAmount *string  `json:"planned_cost_amount"`
	ActualCostAmount  *string  `json:"actual_cost_amount"`
	Nights            int      `json:"nights"`
	PricePerNight     *string  `json:"price_per_night"`
	// SourceStayID is the stay of the plan this one was copied from.
	SourceStayID *string `json:"source_stay_id"`
}

// expenseResponse is a cost tied to no place, stay or leg. day_id is null for
// an expense of the whole trip.
type expenseResponse struct {
	ID            string  `json:"id"`
	DayID         *string `json:"day_id"`
	Category      string  `json:"category"`
	PlannedAmount *string `json:"planned_amount"`
	ActualAmount  *string `json:"actual_amount"`
	SpentOn       *string `json:"spent_on"`
	Note          string  `json:"note"`
}

// newExpenseResponse maps an expense onto the wire.
func newExpenseResponse(expense domain.Expense) expenseResponse {
	return expenseResponse{
		ID:            expense.ID.String(),
		DayID:         formatID(expense.DayID),
		Category:      string(expense.Category),
		PlannedAmount: formatMoney(expense.Planned),
		ActualAmount:  formatMoney(expense.Actual),
		SpentOn:       formatDate(expense.SpentOn),
		Note:          expense.Note,
	}
}

// nightResponse is one night of the trip and the stays covering it.
type nightResponse struct {
	Date        string   `json:"date"`
	StayIDs     []string `json:"stay_ids"`
	NoOvernight bool     `json:"no_overnight"`
	Missing     bool     `json:"missing"`
}

// staySummaryResponse totals the stays.
type staySummaryResponse struct {
	Nights          int     `json:"nights"`
	Cost            string  `json:"cost"`
	AveragePerNight *string `json:"average_per_night"`
}

// documentResponse is a whole document.
type documentResponse struct {
	ID     string `json:"id"`
	TripID string `json:"trip_id"`
	Kind   string `json:"kind"`
	// SourceDocumentID is the plan a report was copied from; null otherwise.
	SourceDocumentID *string             `json:"source_document_id"`
	IntroMD          string              `json:"intro_md"`
	SummaryMD        string              `json:"summary_md"`
	Days             []dayResponse       `json:"days"`
	Unassigned       []itemResponse      `json:"unassigned"`
	Stays            []stayResponse      `json:"stays"`
	Expenses         []expenseResponse   `json:"expenses"`
	Nights           []nightResponse     `json:"nights"`
	StaySummary      staySummaryResponse `json:"stay_summary"`
	// PendingLegs counts legs waiting for a calculation across the document.
	PendingLegs int `json:"pending_legs"`
	// EstimatedLegs counts the legs holding an estimate the provider could be
	// asked about again, which is what the interface offers to retry.
	EstimatedLegs int `json:"estimated_legs"`
	// Totals are a report's figures for its reading mode; null on a plan.
	Totals *reportTotalsResponse `json:"totals"`
	// Translations are a report's words in its further languages: by language,
	// then by the element translated, then by field. A field missing here is
	// read in the original.
	Translations map[string]map[string]map[string]string `json:"translations"`
	CreatedAt    time.Time                               `json:"created_at"`
	UpdatedAt    time.Time                               `json:"updated_at"`
}

// newTranslationsResponse groups a report's translations the way a reader
// looks them up: language, element, field.
func newTranslationsResponse(translations []domain.Translation) map[string]map[string]map[string]string {
	response := make(map[string]map[string]map[string]string)
	for _, translation := range translations {
		elements := response[translation.Lang]
		if elements == nil {
			elements = make(map[string]map[string]string)
			response[translation.Lang] = elements
		}
		id := translation.TargetID.String()
		if elements[id] == nil {
			elements[id] = make(map[string]string)
		}
		elements[id][translation.Field] = translation.Value
	}
	return response
}

// formatMoney renders an optional amount.
func formatMoney(amount *domain.Money) *string {
	if amount == nil {
		return nil
	}
	value := amount.String()
	return &value
}

// formatClock renders an optional time of day.
func formatClock(clock *domain.ClockTime) *string {
	if clock == nil {
		return nil
	}
	value := clock.String()
	return &value
}

// optionalString renders an empty string as null.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// newItemResponse maps an element onto the wire. A stay mark shows its stay's
// name and position, since it has none of its own.
func newItemResponse(item domain.Item, stays map[uuid.UUID]domain.Stay, schedule *domain.ItemSchedule,
	pictures gallery) itemResponse {
	response := itemResponse{
		ID:                item.ID.String(),
		DayID:             formatID(item.DayID),
		Position:          item.Position,
		Kind:              string(item.Kind),
		Anchor:            optionalString(string(item.Anchor)),
		StayID:            formatID(item.StayID),
		Name:              item.Name,
		Category:          string(item.Category),
		ActivityType:      optionalString(string(item.ActivityType)),
		Lat:               item.Lat,
		Lng:               item.Lng,
		Address:           item.Address,
		OSMRef:            item.OSMRef,
		DescriptionMD:     item.DescriptionMD,
		URL:               item.URL,
		DesiredTime:       formatClock(item.DesiredTime),
		VisitMinutes:      item.VisitMinutes,
		IsOptional:        item.IsOptional,
		BookingRef:        item.BookingRef,
		PlannedCostAmount: formatMoney(item.PlannedCost),
		CostPerPerson:     item.CostPerPerson,
		CostCategory:      string(item.CostCategory),
		Status:            string(item.Status),
		StoryMD:           item.StoryMD,
		ActualTime:        formatClock(item.ActualTime),
		ActualEndTime:     formatClock(item.ActualEndTime),
		Rating:            item.Rating,
		Difficulty:        item.Difficulty,
		ActualCostAmount:  formatMoney(item.ActualCost),
		SourceItemID:      formatID(item.SourceItemID),
		Media:             pictures.of(item.ID),
		CoverMediaID:      pictures.coverID(item.CoverMediaID),
	}
	if item.Kind == domain.ItemStayAnchor && item.StayID != nil {
		if stay, ok := stays[*item.StayID]; ok {
			response.Name, response.Address, response.Lat, response.Lng = stay.Name, stay.Address, stay.Lat, stay.Lng
		}
	}
	if schedule != nil {
		response.Schedule = &scheduleResponse{
			ArrivalMinutes:   schedule.ArrivalMinutes,
			DepartureMinutes: schedule.DepartureMinutes,
			Late:             schedule.Late,
		}
	}
	return response
}

// newStayResponse maps a stay onto the wire.
func newStayResponse(stay domain.Stay) stayResponse {
	return stayResponse{
		ID:                stay.ID.String(),
		Name:              stay.Name,
		Kind:              string(stay.Kind),
		Address:           stay.Address,
		Lat:               stay.Lat,
		Lng:               stay.Lng,
		CheckInDate:       stay.CheckInDate.Format(domain.DateLayout),
		CheckInTime:       formatClock(stay.CheckInTime),
		CheckOutDate:      stay.CheckOutDate.Format(domain.DateLayout),
		CheckOutTime:      formatClock(stay.CheckOutTime),
		BookingRef:        stay.BookingRef,
		URL:               stay.URL,
		Contacts:          stay.Contacts,
		NotesMD:           stay.NotesMD,
		PlannedCostAmount: formatMoney(stay.PlannedCost),
		ActualCostAmount:  formatMoney(stay.ActualCost),
		Nights:            stay.Nights(),
		PricePerNight:     formatMoney(stay.PricePerNight()),
		SourceStayID:      formatID(stay.SourceStayID),
	}
}

// newDocumentResponse assembles a document with its schedules and totals.
func newDocumentResponse(content domain.DocumentContent, trip domain.TripSummary,
	pictures gallery) documentResponse {
	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}

	response := documentResponse{
		ID:               content.Document.ID.String(),
		TripID:           content.Document.TripID.String(),
		Kind:             string(content.Document.Kind),
		SourceDocumentID: formatID(content.Document.SourceDocumentID),
		IntroMD:          content.Document.IntroMD,
		SummaryMD:        content.Document.SummaryMD,
		Days:             make([]dayResponse, 0, len(content.Days)),
		Unassigned:       []itemResponse{},
		Stays:            make([]stayResponse, 0, len(content.Stays)),
		Expenses:         make([]expenseResponse, 0, len(content.Expenses)),
		Nights:           []nightResponse{},
		Translations:     newTranslationsResponse(content.Translations),
		CreatedAt:        content.Document.CreatedAt,
		UpdatedAt:        content.Document.UpdatedAt,
	}

	for _, day := range content.Days {
		items := domain.DayItems(content.Items, &day.ID)
		opening := domain.OpeningLeg(content.Legs, items)
		legs := domain.DayLegs(content.Legs, items)
		schedules, summary := domain.ScheduleDay(day, items, opening, legs, trip.Travelers, content.Tracks)
		// A separate expense of the day is part of what the day costs, so the
		// plan's day total and the budget screen agree.
		dayExpenses := domain.DayExpenses(content.Expenses, &day.ID)
		summary.PlannedCost += domain.PlannedTotal(dayExpenses)
		summary.ActualCost += domain.ActualTotal(dayExpenses)
		dayItems := make([]itemResponse, 0, len(items))
		for index, item := range items {
			entry := newItemResponse(item, stays, &schedules[index], pictures)
			entry.Track = newTrackResponse(domain.TrackOfItem(content.Tracks, item.ID))
			dayItems = append(dayItems, entry)
		}
		// The leg in from the day before comes first, as it is travelled first.
		dayLegs := make([]legResponse, 0, len(legs)+1)
		pending, estimated := 0, 0
		for _, leg := range append([]*domain.Leg{opening}, legs...) {
			if leg == nil {
				continue
			}
			if leg.Source == domain.LegPending {
				pending++
			}
			if leg.RetryableEstimate() {
				estimated++
			}
			dayLegs = append(dayLegs, newLegResponse(*leg))
		}
		response.PendingLegs += pending
		response.EstimatedLegs += estimated
		byMode := []modeTotalResponse{}
		for _, mode := range travelModes {
			if total, ok := summary.ByMode[mode]; ok {
				byMode = append(byMode, modeTotalResponse{Mode: string(mode), DistanceM: total.DistanceM, DurationS: total.DurationS})
			}
		}
		var mode *string
		if day.DefaultMode != nil {
			mode = optionalString(string(*day.DefaultMode))
		}
		response.Days = append(response.Days, dayResponse{
			ID:            day.ID.String(),
			Position:      day.Position,
			Date:          formatDate(day.Date),
			Title:         day.Title,
			NotesMD:       day.NotesMD,
			StartTime:     day.StartTime.String(),
			DefaultMode:   mode,
			Timezone:      day.Timezone,
			MorningAnchor: day.MorningAnchor,
			EveningAnchor: day.EveningAnchor,
			NoOvernight:   day.NoOvernight,
			Items:         dayItems,
			Legs:          dayLegs,
			Media:         pictures.of(day.ID),
			CoverMediaID:  pictures.coverID(day.CoverMediaID),
			Summary: daySummaryResponse{
				VisitMinutes:  summary.VisitMinutes,
				TravelMinutes: summary.TravelMinutes,
				DistanceM:     summary.DistanceM,
				ByMode:        byMode,
				UnknownTravel: summary.UnknownTravel,
				PendingLegs:   pending,
				EstimatedLegs: estimated,
				EndMinutes:    summary.EndMinutes,
				PlannedCost:   summary.PlannedCost.String(),
				ActualCost:    summary.ActualCost.String(),
			},
		})
	}

	for _, item := range domain.DayItems(content.Items, nil) {
		response.Unassigned = append(response.Unassigned, newItemResponse(item, stays, nil, pictures))
	}
	for _, stay := range content.Stays {
		response.Stays = append(response.Stays, newStayResponse(stay))
	}
	for _, expense := range content.Expenses {
		response.Expenses = append(response.Expenses, newExpenseResponse(expense))
	}
	for _, night := range domain.Nights(trip.StartDate, trip.EndDate, content.Days, content.Stays) {
		ids := make([]string, 0, len(night.StayIDs))
		for _, id := range night.StayIDs {
			ids = append(ids, id.String())
		}
		response.Nights = append(response.Nights, nightResponse{
			Date:        night.Date.Format(domain.DateLayout),
			StayIDs:     ids,
			NoOvernight: night.NoOvernight,
			Missing:     night.Missing(),
		})
	}
	if content.Document.Kind == domain.DocumentReport {
		totals := newReportTotalsResponse(domain.BuildReportTotals(trip.Trip, content))
		response.Totals = &totals
	}
	summary := domain.SummarizeStays(content.Stays)
	response.StaySummary = staySummaryResponse{
		Nights:          summary.Nights,
		Cost:            summary.Cost.String(),
		AveragePerNight: formatMoney(summary.AveragePerNight),
	}
	return response
}

// documentFor loads a document on behalf of the signed-in person and checks
// their role on its trip allows the action. It writes the error itself.
func (s *Server) documentFor(w http.ResponseWriter, r *http.Request, documentID uuid.UUID,
	action domain.TripAction) (domain.Document, bool) {
	document, err := s.documents.Document(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "get document", err)
		return domain.Document{}, false
	}
	_, ok := s.tripAccess(w, r, document.TripID, action)
	return document, ok
}

// writeDocument answers with the whole document as it now stands. Reading it
// back through the trip keeps a document of a trip deleted meanwhile hidden.
func (s *Server) writeDocument(w http.ResponseWriter, r *http.Request, status int, documentID uuid.UUID) {
	document, err := s.documents.Document(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "get document", err)
		return
	}
	trip, err := s.trips.Get(r.Context(), document.TripID, principalFrom(r.Context()).user.ID)
	if err != nil {
		s.writeDomainError(w, r, "get trip", err)
		return
	}
	content, err := s.documents.Content(r.Context(), documentID)
	if err != nil {
		s.writeDomainError(w, r, "read document", err)
		return
	}
	// Somebody who may read the trip may read its private files as well; only a
	// read-only link is ever kept from them.
	pictures, err := s.galleryOf(r.Context(), trip.ID, true)
	if err != nil {
		s.writeDomainError(w, r, "read media", err)
		return
	}
	writeJSON(w, s.logger, status, newDocumentResponse(content, trip, pictures))
}

// handleGetDocument returns a whole document.
func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionView); !ok {
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// dayFields are the editable fields of a day, shared by creation and change.
// Omitted fields keep their value; null returns the mode and zone to the
// trip's.
type dayFields struct {
	Title         optional[string] `json:"title"`
	NotesMD       optional[string] `json:"notes_md"`
	StartTime     optional[string] `json:"start_time"`
	DefaultMode   optional[string] `json:"default_mode"`
	Timezone      optional[string] `json:"timezone"`
	MorningAnchor optional[bool]   `json:"morning_anchor"`
	EveningAnchor optional[bool]   `json:"evening_anchor"`
	NoOvernight   optional[bool]   `json:"no_overnight"`
	// CoverMediaID is the picture the day is shown by; null takes it away.
	CoverMediaID optional[string] `json:"cover_media_id"`
}

// apply writes the given fields onto a day and validates the result.
func (f dayFields) apply(day domain.Day) (domain.Day, error) {
	if f.Title.Set {
		day.Title = f.Title.Value
	}
	if f.NotesMD.Set {
		day.NotesMD = f.NotesMD.Value
	}
	if f.StartTime.Set {
		clock, err := domain.ParseClockTime("start_time", f.StartTime.Value)
		if err != nil {
			return day, err
		}
		day.StartTime = clock
	}
	if f.DefaultMode.Set {
		day.DefaultMode = nil
		if !f.DefaultMode.Null {
			mode := domain.TravelMode(f.DefaultMode.Value)
			day.DefaultMode = &mode
		}
	}
	if f.Timezone.Set {
		day.Timezone = nil
		if !f.Timezone.Null {
			day.Timezone = &f.Timezone.Value
		}
	}
	if f.MorningAnchor.Set {
		day.MorningAnchor = f.MorningAnchor.Value
	}
	if f.EveningAnchor.Set {
		day.EveningAnchor = f.EveningAnchor.Value
	}
	if err := applyCover(f.CoverMediaID, &day.CoverMediaID); err != nil {
		return day, err
	}
	if f.NoOvernight.Set {
		day.NoOvernight = f.NoOvernight.Value
	}
	return day.Normalize()
}

// createDayRequest is the body of POST /api/v1/documents/{documentID}/days.
type createDayRequest struct {
	dayFields
	// Position inserts the day before the day now there; omitted appends.
	Position *int `json:"position"`
}

// handleCreateDay inserts a day into a document. On a trip with dates the trip
// grows by a day.
func (s *Server) handleCreateDay(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	var body createDayRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	day, err := body.apply(domain.Day{
		ID: uuid.Must(uuid.NewV7()), DocumentID: documentID, StartTime: domain.DefaultDayStart,
		MorningAnchor: true, EveningAnchor: true,
	})
	if err != nil {
		s.writeDomainError(w, r, "validate day", err)
		return
	}
	if err := s.documents.AddDay(r.Context(), day, body.Position); err != nil {
		s.writeDomainError(w, r, "add day", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, documentID)
}

// dayFor loads the day named in the path and checks its trip may be edited.
func (s *Server) dayFor(w http.ResponseWriter, r *http.Request) (domain.Day, domain.Document, bool) {
	dayID, ok := s.pathUUID(w, r, "dayID")
	if !ok {
		return domain.Day{}, domain.Document{}, false
	}
	day, err := s.documents.Day(r.Context(), dayID)
	if err != nil {
		s.writeDomainError(w, r, "get day", err)
		return domain.Day{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, day.DocumentID, domain.ActionEdit)
	return day, document, ok
}

// handleUpdateDay changes a day's title, notes, start time, defaults or stay
// mark switches.
func (s *Server) handleUpdateDay(w http.ResponseWriter, r *http.Request) {
	day, document, ok := s.dayFor(w, r)
	if !ok {
		return
	}
	var body dayFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := body.apply(day)
	if err != nil {
		s.writeDomainError(w, r, "validate day", err)
		return
	}
	if err := s.checkCover(r.Context(), document.TripID, updated.CoverMediaID); err != nil {
		s.writeDomainError(w, r, "check day cover", err)
		return
	}
	if err := s.documents.UpdateDay(r.Context(), updated); err != nil {
		s.writeDomainError(w, r, "update day", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, day.DocumentID)
}

// handleDeleteDay removes a day. A plan day's places go to the unassigned list.
// When the other document would lose a day with content, the request needs
// ?confirm=true.
func (s *Server) handleDeleteDay(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.dayFor(w, r)
	if !ok {
		return
	}
	confirm := r.URL.Query().Get("confirm") == "true"
	if err := s.documents.DeleteDay(r.Context(), day.ID, confirm); err != nil {
		s.writeDomainError(w, r, "delete day", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, day.DocumentID)
}

// handleDuplicateDay copies a day with its places right after it.
func (s *Server) handleDuplicateDay(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.dayFor(w, r)
	if !ok {
		return
	}
	if _, err := s.documents.DuplicateDay(r.Context(), day.ID); err != nil {
		s.writeDomainError(w, r, "duplicate day", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, day.DocumentID)
}

// reorderDaysRequest is the body of POST /api/v1/documents/{documentID}/days:reorder.
type reorderDaysRequest struct {
	DayIDs []uuid.UUID `json:"day_ids"`
}

// handleReorderDays puts a document's days in a new order.
func (s *Server) handleReorderDays(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	var body reorderDaysRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if err := s.documents.ReorderDays(r.Context(), documentID, body.DayIDs); err != nil {
		s.writeDomainError(w, r, "reorder days", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// placeFields are the editable fields of a place, shared by creation and
// change. null clears the coordinates, the desired time and the cost.
type placeFields struct {
	// Kind makes the element a place or an activity; it may be changed later.
	Kind              optional[string]  `json:"kind"`
	ActivityType      optional[string]  `json:"activity_type"`
	Name              optional[string]  `json:"name"`
	Category          optional[string]  `json:"category"`
	Lat               optional[float64] `json:"lat"`
	Lng               optional[float64] `json:"lng"`
	Address           optional[string]  `json:"address"`
	OSMRef            optional[string]  `json:"osm_ref"`
	DescriptionMD     optional[string]  `json:"description_md"`
	URL               optional[string]  `json:"url"`
	DesiredTime       optional[string]  `json:"desired_time"`
	VisitMinutes      optional[int]     `json:"visit_minutes"`
	IsOptional        optional[bool]    `json:"is_optional"`
	BookingRef        optional[string]  `json:"booking_ref"`
	PlannedCostAmount optional[string]  `json:"planned_cost_amount"`
	CostPerPerson     optional[bool]    `json:"cost_per_person"`
	CostCategory      optional[string]  `json:"cost_category"`
	// Difficulty is an activity's, 1 to 5; null takes it away.
	Difficulty optional[int] `json:"difficulty"`
	// The fields below are refused on a plan.
	Status           optional[string] `json:"status"`
	StoryMD          optional[string] `json:"story_md"`
	ActualTime       optional[string] `json:"actual_time"`
	ActualEndTime    optional[string] `json:"actual_end_time"`
	Rating           optional[int]    `json:"rating"`
	ActualCostAmount optional[string] `json:"actual_cost_amount"`
	// CoverMediaID is the picture the place is shown by; null takes it away.
	CoverMediaID optional[string] `json:"cover_media_id"`
}

// applyCover applies a change of cover: an identifier picks a picture, null
// takes the cover away, and an absent field leaves it alone. That the picture
// belongs to the same trip is checked by the handler, which knows the trip.
func applyCover(change optional[string], target **uuid.UUID) error {
	if !change.Set {
		return nil
	}
	*target = nil
	if change.Null || change.Value == "" {
		return nil
	}
	id, err := uuid.Parse(change.Value)
	if err != nil {
		return domain.NewValidationError("cover_media_id", "invalid_id", "must be the identifier of a file")
	}
	*target = &id
	return nil
}

// applyNullableFloat applies a clearable number.
func applyNullableFloat(change optional[float64], target **float64) {
	if !change.Set {
		return
	}
	*target = nil
	if !change.Null {
		value := change.Value
		*target = &value
	}
}

// applyNullableClock applies a clearable time of day.
func applyNullableClock(field string, change optional[string], target **domain.ClockTime) error {
	if !change.Set {
		return nil
	}
	*target = nil
	if change.Null || change.Value == "" {
		return nil
	}
	clock, err := domain.ParseClockTime(field, change.Value)
	if err != nil {
		return err
	}
	*target = &clock
	return nil
}

// applyNullableMoney applies a clearable amount.
func applyNullableMoney(field string, change optional[string], target **domain.Money) error {
	if !change.Set {
		return nil
	}
	*target = nil
	if change.Null {
		return nil
	}
	amount, err := domain.ParseMoney(field, change.Value)
	if err != nil {
		return err
	}
	*target = &amount
	return nil
}

// apply writes the given fields onto a place or an activity and validates the
// result. A new place without a visit time takes its category's default, a new
// activity its type's.
func (f placeFields) apply(place domain.Item, creating bool, kind domain.DocumentKind) (domain.Item, error) {
	if err := applyCover(f.CoverMediaID, &place.CoverMediaID); err != nil {
		return place, err
	}
	if f.Kind.Set {
		place.Kind = domain.ItemKind(f.Kind.Value)
	}
	if f.ActivityType.Set {
		place.ActivityType = domain.ActivityType(f.ActivityType.Value)
	}
	if f.Name.Set {
		place.Name = f.Name.Value
	}
	if f.Category.Set {
		place.Category = domain.PlaceCategory(f.Category.Value)
	}
	applyNullableFloat(f.Lat, &place.Lat)
	applyNullableFloat(f.Lng, &place.Lng)
	if f.Address.Set {
		place.Address = f.Address.Value
	}
	if f.OSMRef.Set {
		place.OSMRef = f.OSMRef.Value
	}
	if f.DescriptionMD.Set {
		place.DescriptionMD = f.DescriptionMD.Value
	}
	if f.URL.Set {
		place.URL = f.URL.Value
	}
	if err := applyNullableClock("desired_time", f.DesiredTime, &place.DesiredTime); err != nil {
		return place, err
	}
	switch {
	case f.VisitMinutes.Set:
		place.VisitMinutes = f.VisitMinutes.Value
	case creating && place.Kind == domain.ItemActivity:
		place.VisitMinutes = place.ActivityType.DefaultVisitMinutes()
	case creating:
		place.VisitMinutes = domain.PlaceCategory(f.Category.Value).DefaultVisitMinutes()
	}
	if f.IsOptional.Set {
		place.IsOptional = f.IsOptional.Value
	}
	if f.BookingRef.Set {
		place.BookingRef = f.BookingRef.Value
	}
	if err := applyNullableMoney("planned_cost_amount", f.PlannedCostAmount, &place.PlannedCost); err != nil {
		return place, err
	}
	if f.CostPerPerson.Set {
		place.CostPerPerson = f.CostPerPerson.Value
	}
	if f.CostCategory.Set {
		place.CostCategory = domain.CostCategory(f.CostCategory.Value)
	}
	if f.Status.Set {
		place.Status = domain.ItemStatus(f.Status.Value)
	}
	if f.StoryMD.Set {
		place.StoryMD = f.StoryMD.Value
	}
	if err := applyNullableClock("actual_time", f.ActualTime, &place.ActualTime); err != nil {
		return place, err
	}
	if err := applyNullableClock("actual_end_time", f.ActualEndTime, &place.ActualEndTime); err != nil {
		return place, err
	}
	applyNullableInt(f.Difficulty, &place.Difficulty)
	applyNullableInt(f.Rating, &place.Rating)
	if err := applyNullableMoney("actual_cost_amount", f.ActualCostAmount, &place.ActualCost); err != nil {
		return place, err
	}
	return place.NormalizePlace(kind)
}

// createPlaceRequest is the body of both place creation endpoints.
type createPlaceRequest struct {
	placeFields
	// Position inserts the place before the place now there; omitted appends.
	Position *int `json:"position"`
}

// createPlace validates and stores a new place in a day or the unassigned list.
func (s *Server) createPlace(w http.ResponseWriter, r *http.Request, document domain.Document, dayID *uuid.UUID) {
	var body createPlaceRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	place, err := body.apply(domain.Item{ID: uuid.Must(uuid.NewV7()), DocumentID: document.ID, DayID: dayID},
		true, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate place", err)
		return
	}
	if err := s.documents.CreatePlace(r.Context(), place, body.Position); err != nil {
		s.writeDomainError(w, r, "create place", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, document.ID)
}

// handleCreateUnassignedPlace adds a place to a plan's unassigned list.
func (s *Server) handleCreateUnassignedPlace(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	document, ok := s.documentFor(w, r, documentID, domain.ActionEdit)
	if !ok {
		return
	}
	s.createPlace(w, r, document, nil)
}

// handleCreateDayPlace adds a place to a day.
func (s *Server) handleCreateDayPlace(w http.ResponseWriter, r *http.Request) {
	day, document, ok := s.dayFor(w, r)
	if !ok {
		return
	}
	s.createPlace(w, r, document, &day.ID)
}

// placeFor loads the place named in the path and checks its trip may be edited.
// Stay marks are refused: they follow the stays and cannot be edited.
func (s *Server) placeFor(w http.ResponseWriter, r *http.Request) (domain.Item, domain.Document, bool) {
	itemID, ok := s.pathUUID(w, r, "itemID")
	if !ok {
		return domain.Item{}, domain.Document{}, false
	}
	item, err := s.documents.Item(r.Context(), itemID)
	if err != nil {
		s.writeDomainError(w, r, "get item", err)
		return domain.Item{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, item.DocumentID, domain.ActionEdit)
	if !ok {
		return domain.Item{}, domain.Document{}, false
	}
	if !item.Kind.IsVisit() {
		s.writeDomainError(w, r, "check item", domain.NewValidationError("item_id", "stay_anchor",
			"stay marks follow the stays and cannot be changed directly"))
		return domain.Item{}, domain.Document{}, false
	}
	return item, document, true
}

// handleUpdatePlace changes a place's fields.
func (s *Server) handleUpdatePlace(w http.ResponseWriter, r *http.Request) {
	place, document, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	var body placeFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := body.apply(place, false, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate place", err)
		return
	}
	if err := s.checkCover(r.Context(), document.TripID, updated.CoverMediaID); err != nil {
		s.writeDomainError(w, r, "check place cover", err)
		return
	}
	if err := s.documents.UpdatePlace(r.Context(), updated); err != nil {
		s.writeDomainError(w, r, "update place", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, place.DocumentID)
}

// handleDeletePlace removes a place.
func (s *Server) handleDeletePlace(w http.ResponseWriter, r *http.Request) {
	place, _, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	if err := s.documents.DeletePlace(r.Context(), place); err != nil {
		s.writeDomainError(w, r, "delete place", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, place.DocumentID)
}

// placeTarget names where a place moves or is copied to: a day, or the
// unassigned list when to_day_id is null or omitted.
type placeTarget struct {
	ToDayID  *uuid.UUID `json:"to_day_id"`
	Position *int       `json:"position"`
}

// movePlaceRequest is the body of POST /api/v1/documents/{documentID}/items:move.
type movePlaceRequest struct {
	placeTarget
	ItemID uuid.UUID `json:"item_id"`
}

// handleMovePlace moves a place within a day, to another day, or between a
// day and the unassigned list.
func (s *Server) handleMovePlace(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	var body movePlaceRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	position := -1
	if body.Position != nil {
		position = *body.Position
	}
	if err := s.documents.MovePlace(r.Context(), documentID, body.ItemID, body.ToDayID, position); err != nil {
		s.writeDomainError(w, r, "move place", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// handleCopyPlace copies a place into a day or the unassigned list.
func (s *Server) handleCopyPlace(w http.ResponseWriter, r *http.Request) {
	place, _, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	var body placeTarget
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if err := s.documents.CopyPlace(r.Context(), place, uuid.Must(uuid.NewV7()), body.ToDayID, body.Position); err != nil {
		s.writeDomainError(w, r, "copy place", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, place.DocumentID)
}

// stayFields are the editable fields of a stay, shared by creation and change.
type stayFields struct {
	Name              optional[string]  `json:"name"`
	Kind              optional[string]  `json:"kind"`
	Address           optional[string]  `json:"address"`
	Lat               optional[float64] `json:"lat"`
	Lng               optional[float64] `json:"lng"`
	CheckInDate       optional[string]  `json:"check_in_date"`
	CheckInTime       optional[string]  `json:"check_in_time"`
	CheckOutDate      optional[string]  `json:"check_out_date"`
	CheckOutTime      optional[string]  `json:"check_out_time"`
	BookingRef        optional[string]  `json:"booking_ref"`
	URL               optional[string]  `json:"url"`
	Contacts          optional[string]  `json:"contacts"`
	NotesMD           optional[string]  `json:"notes_md"`
	PlannedCostAmount optional[string]  `json:"planned_cost_amount"`
	// ActualCostAmount is refused on a plan.
	ActualCostAmount optional[string] `json:"actual_cost_amount"`
}

// applyRequiredDate applies a date that cannot be cleared; a new stay must set it.
func applyRequiredDate(field string, change optional[string], target *time.Time, creating bool) error {
	if !change.Set {
		if creating {
			return domain.NewValidationError(field, "required", "must be set")
		}
		return nil
	}
	date, err := domain.ParseDate(field, change.Value)
	if err != nil {
		return err
	}
	*target = date
	return nil
}

// apply writes the given fields onto a stay and validates the result.
func (f stayFields) apply(stay domain.Stay, creating bool, kind domain.DocumentKind) (domain.Stay, error) {
	if f.Name.Set {
		stay.Name = f.Name.Value
	}
	if f.Kind.Set {
		stay.Kind = domain.StayKind(f.Kind.Value)
	}
	if f.Address.Set {
		stay.Address = f.Address.Value
	}
	applyNullableFloat(f.Lat, &stay.Lat)
	applyNullableFloat(f.Lng, &stay.Lng)
	if err := applyRequiredDate("check_in_date", f.CheckInDate, &stay.CheckInDate, creating); err != nil {
		return stay, err
	}
	if err := applyRequiredDate("check_out_date", f.CheckOutDate, &stay.CheckOutDate, creating); err != nil {
		return stay, err
	}
	if err := applyNullableClock("check_in_time", f.CheckInTime, &stay.CheckInTime); err != nil {
		return stay, err
	}
	if err := applyNullableClock("check_out_time", f.CheckOutTime, &stay.CheckOutTime); err != nil {
		return stay, err
	}
	if f.BookingRef.Set {
		stay.BookingRef = f.BookingRef.Value
	}
	if f.URL.Set {
		stay.URL = f.URL.Value
	}
	if f.Contacts.Set {
		stay.Contacts = f.Contacts.Value
	}
	if f.NotesMD.Set {
		stay.NotesMD = f.NotesMD.Value
	}
	if err := applyNullableMoney("planned_cost_amount", f.PlannedCostAmount, &stay.PlannedCost); err != nil {
		return stay, err
	}
	if err := applyNullableMoney("actual_cost_amount", f.ActualCostAmount, &stay.ActualCost); err != nil {
		return stay, err
	}
	return stay.Normalize(kind)
}

// handleCreateStay adds a stay to a document; its marks appear in the days it
// covers.
func (s *Server) handleCreateStay(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	document, ok := s.documentFor(w, r, documentID, domain.ActionEdit)
	if !ok {
		return
	}
	var body stayFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	stay, err := body.apply(domain.Stay{ID: uuid.Must(uuid.NewV7()), DocumentID: documentID}, true, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate stay", err)
		return
	}
	if err := s.documents.CreateStay(r.Context(), stay); err != nil {
		s.writeDomainError(w, r, "create stay", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, documentID)
}

// stayFor loads the stay named in the path and checks the role on its trip.
func (s *Server) stayFor(w http.ResponseWriter, r *http.Request,
	action domain.TripAction) (domain.Stay, domain.Document, bool) {
	stayID, ok := s.pathUUID(w, r, "stayID")
	if !ok {
		return domain.Stay{}, domain.Document{}, false
	}
	stay, err := s.documents.Stay(r.Context(), stayID)
	if err != nil {
		s.writeDomainError(w, r, "get stay", err)
		return domain.Stay{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, stay.DocumentID, action)
	return stay, document, ok
}

// handleUpdateStay changes a stay; its marks follow new dates.
func (s *Server) handleUpdateStay(w http.ResponseWriter, r *http.Request) {
	stay, document, ok := s.stayFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body stayFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := body.apply(stay, false, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate stay", err)
		return
	}
	if err := s.documents.UpdateStay(r.Context(), updated); err != nil {
		s.writeDomainError(w, r, "update stay", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, stay.DocumentID)
}

// handleDeleteStay removes a stay with its marks.
func (s *Server) handleDeleteStay(w http.ResponseWriter, r *http.Request) {
	stay, _, ok := s.stayFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	if err := s.documents.DeleteStay(r.Context(), stay); err != nil {
		s.writeDomainError(w, r, "delete stay", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, stay.DocumentID)
}
