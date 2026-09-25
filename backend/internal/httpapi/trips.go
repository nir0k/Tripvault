package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// tripUserResponse identifies a person inside a trip.
type tripUserResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// newTripUserResponse maps a trip participant onto the wire.
func newTripUserResponse(user domain.TripUser) tripUserResponse {
	return tripUserResponse{ID: user.ID.String(), DisplayName: user.DisplayName, Email: user.Email}
}

// tripResponse is a trip as a participant sees it.
type tripResponse struct {
	ID string `json:"id"`
	// Kind is what the trip holds: a plan, or a report.
	Kind string `json:"kind"`
	// SourceTripID is the plan a report was copied from, given only to somebody
	// who may open that plan.
	SourceTripID *string          `json:"source_trip_id"`
	Title        string           `json:"title"`
	Summary      string           `json:"summary"`
	StartDate    *string          `json:"start_date"`
	EndDate      *string          `json:"end_date"`
	Timezone     string           `json:"timezone"`
	Currency     string           `json:"currency"`
	Travelers    int              `json:"travelers"`
	BudgetAmount *string          `json:"budget_amount"`
	Status       string           `json:"status"`
	DayCount     *int             `json:"day_count"`
	Role         string           `json:"role"`
	Owner        tripUserResponse `json:"owner"`
	PlanID       *string          `json:"plan_id"`
	ReportID     *string          `json:"report_id"`
	// CoverMediaID is the picture the trip is shown by, out of its own files.
	CoverMediaID *string `json:"cover_media_id"`
	// CoverCrop is the part of the cover shown; null means its middle.
	CoverCrop *coverCropBody `json:"cover_crop"`
	// Languages are a report's languages, the original first; empty for a plan.
	Languages []string `json:"languages"`
	// Translations are the report's title and summary in its further
	// languages, field by language.
	Translations domain.TripTranslations `json:"translations"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

// coverCropBody is the frame of a cover on the wire, as fractions of the
// picture's width and height from its top left corner.
type coverCropBody struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// newCoverCropBody renders a cover's frame, nil for one shown by its middle.
func newCoverCropBody(crop *domain.CoverCrop) *coverCropBody {
	if crop == nil {
		return nil
	}
	return &coverCropBody{X: crop.X, Y: crop.Y, W: crop.W, H: crop.H}
}

// formatDate renders an optional calendar date in the API's form.
func formatDate(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(domain.DateLayout)
	return &formatted
}

// formatID renders an optional identifier.
func formatID(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	formatted := value.String()
	return &formatted
}

// wireLanguages renders a trip's languages as a list, never null.
func wireLanguages(languages []string) []string {
	if languages == nil {
		return []string{}
	}
	return languages
}

// wireTripTranslations renders a trip's own translations as an object, never
// null.
func wireTripTranslations(translations domain.TripTranslations) domain.TripTranslations {
	if translations == nil {
		return domain.TripTranslations{}
	}
	return translations
}

// newTripResponse maps a trip onto the wire, deriving its status at now.
func newTripResponse(trip domain.TripSummary, now time.Time) tripResponse {
	response := tripResponse{
		ID:           trip.ID.String(),
		Kind:         string(trip.Kind),
		Title:        trip.Title,
		Summary:      trip.Summary,
		StartDate:    formatDate(trip.StartDate),
		EndDate:      formatDate(trip.EndDate),
		Timezone:     trip.Timezone,
		Currency:     trip.Currency,
		Travelers:    trip.Travelers,
		Status:       string(trip.StatusAt(now)),
		DayCount:     trip.DayCount(),
		Role:         string(trip.Role),
		Owner:        newTripUserResponse(trip.Owner),
		PlanID:       formatID(trip.PlanID),
		ReportID:     formatID(trip.ReportID),
		CoverMediaID: formatID(trip.CoverMediaID),
		CoverCrop:    newCoverCropBody(trip.CoverCrop),
		Languages:    wireLanguages(trip.Languages),
		Translations: wireTripTranslations(trip.Translations),
		CreatedAt:    trip.CreatedAt,
		UpdatedAt:    trip.UpdatedAt,
	}
	if trip.Budget != nil {
		amount := trip.Budget.String()
		response.BudgetAmount = &amount
	}
	if trip.SourceVisible {
		response.SourceTripID = formatID(trip.SourceTripID)
	}
	return response
}

// tripFor loads the trip named in the path on behalf of the signed-in person
// and checks their role allows the action. It writes the error response itself.
func (s *Server) tripFor(w http.ResponseWriter, r *http.Request, action domain.TripAction) (domain.TripSummary, bool) {
	tripID, ok := s.pathUUID(w, r, "tripID")
	if !ok {
		return domain.TripSummary{}, false
	}
	return s.tripAccess(w, r, tripID, action)
}

// tripAccess loads a trip on behalf of the signed-in person and checks their
// role allows the action. It writes the error response itself.
//
// The role is read from the database on every request, never from the token,
// so a withdrawn role takes effect at once.
func (s *Server) tripAccess(w http.ResponseWriter, r *http.Request, tripID uuid.UUID,
	action domain.TripAction) (domain.TripSummary, bool) {
	trip, err := s.trips.Get(r.Context(), tripID, principalFrom(r.Context()).user.ID)
	if err != nil {
		s.writeDomainError(w, r, "get trip", err)
		return domain.TripSummary{}, false
	}
	if !trip.Role.Can(action) {
		s.writeDomainError(w, r, "check trip role", domain.ErrForbidden)
		return domain.TripSummary{}, false
	}
	return trip, true
}

// queryInt reads an optional integer query parameter, answering a validation
// failure for anything that is not a number.
func (s *Server) queryInt(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		s.writeDomainError(w, r, "read query", domain.NewValidationError(name, "invalid_number", "must be an integer"))
		return 0, false
	}
	return value, true
}

// handleListTrips returns one page of the trips the signed-in person owns or
// was given access to, most relevant or most recently changed first.
func (s *Server) handleListTrips(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	year, ok := s.queryInt(w, r, "year")
	if !ok {
		return
	}
	limit, ok := s.queryInt(w, r, "limit")
	if !ok {
		return
	}
	filter, err := domain.TripFilter{
		Scope:  domain.TripScope(query.Get("scope")),
		Kind:   domain.DocumentKind(query.Get("kind")),
		Year:   year,
		Query:  query.Get("q"),
		Sort:   domain.TripSort(query.Get("sort")),
		Limit:  limit,
		Cursor: query.Get("cursor"),
	}.Normalize()
	if err != nil {
		s.writeDomainError(w, r, "validate trip filter", err)
		return
	}

	now := s.now()
	page, err := s.trips.List(r.Context(), principalFrom(r.Context()).user.ID, filter, now)
	if err != nil {
		s.writeDomainError(w, r, "list trips", err)
		return
	}
	items := make([]tripResponse, 0, len(page.Items))
	for _, trip := range page.Items {
		items = append(items, newTripResponse(trip, now))
	}
	writeJSON(w, s.logger, http.StatusOK, newPageResponse(items, page.NextCursor))
}

// handleTripYears lists the years the signed-in person's plans or reports touch,
// for the year filter of each list.
func (s *Server) handleTripYears(w http.ResponseWriter, r *http.Request) {
	kind := domain.DocumentKind(r.URL.Query().Get("kind"))
	if kind != "" {
		if err := domain.ValidateDocumentKind("kind", kind); err != nil {
			s.writeDomainError(w, r, "validate trip filter", err)
			return
		}
	}
	years, err := s.trips.Years(r.Context(), principalFrom(r.Context()).user.ID, kind)
	if err != nil {
		s.internalError(w, r, "list trip years", err)
		return
	}
	if years == nil {
		years = []int{}
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[int]{Items: years})
}

// createTripRequest is the body of POST /api/v1/trips.
type createTripRequest struct {
	Title        string  `json:"title"`
	Summary      string  `json:"summary"`
	StartDate    *string `json:"start_date"`
	EndDate      *string `json:"end_date"`
	Timezone     string  `json:"timezone"`
	Currency     string  `json:"currency"`
	Travelers    *int    `json:"travelers"`
	BudgetAmount *string `json:"budget_amount"`
	// Kind is what the trip holds: plan, or report for one written from scratch.
	Kind string `json:"kind"`
	// Languages are a report's languages, the original first; omitted, a
	// report is written in its author's language.
	Languages []string `json:"languages"`
}

// parseOptionalDate reads a date that may be absent.
func parseOptionalDate(field string, value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	date, err := domain.ParseDate(field, *value)
	if err != nil {
		return nil, err
	}
	return &date, nil
}

// parseOptionalMoney reads an amount that may be absent.
func parseOptionalMoney(field string, value *string) (*domain.Money, error) {
	if value == nil {
		return nil, nil
	}
	amount, err := domain.ParseMoney(field, *value)
	if err != nil {
		return nil, err
	}
	return &amount, nil
}

// handleCreateTrip creates a plan or a report owned by the signed-in person,
// with its document and one empty day per date, which is what the "New plan"
// and "New report" buttons do.
func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user

	var body createTripRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}

	trip := domain.Trip{
		ID:        uuid.Must(uuid.NewV7()),
		OwnerID:   user.ID,
		Kind:      domain.DocumentKind(body.Kind),
		Title:     body.Title,
		Summary:   body.Summary,
		Timezone:  body.Timezone,
		Currency:  body.Currency,
		Travelers: 1,
		Languages: body.Languages,
	}
	if trip.Kind == domain.DocumentReport && len(trip.Languages) == 0 {
		trip.Languages = []string{domain.ContentLanguage(user.Locale)}
	}
	if trip.Currency == "" {
		trip.Currency = user.DefaultCurrency
	}
	if body.Travelers != nil {
		trip.Travelers = *body.Travelers
	}
	var err error
	if trip.StartDate, err = parseOptionalDate("start_date", body.StartDate); err != nil {
		s.writeDomainError(w, r, "validate trip", err)
		return
	}
	if trip.EndDate, err = parseOptionalDate("end_date", body.EndDate); err != nil {
		s.writeDomainError(w, r, "validate trip", err)
		return
	}
	if trip.Budget, err = parseOptionalMoney("budget_amount", body.BudgetAmount); err != nil {
		s.writeDomainError(w, r, "validate trip", err)
		return
	}
	if trip, err = trip.Normalize(); err != nil {
		s.writeDomainError(w, r, "validate trip", err)
		return
	}

	created, err := s.trips.Create(r.Context(), trip)
	if err != nil {
		s.writeDomainError(w, r, "create trip", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newTripResponse(created, s.now()))
}

// handleGetTrip returns one trip the signed-in person can see.
func (s *Server) handleGetTrip(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newTripResponse(trip, s.now()))
}

// updateTripRequest is the body of PATCH /api/v1/trips/{tripID}. Omitted fields
// stay as they are; null clears the budget. A null date is refused, because a
// trip is planned over a period and its days take their dates from it.
type updateTripRequest struct {
	Title        optional[string] `json:"title"`
	Summary      optional[string] `json:"summary"`
	StartDate    optional[string] `json:"start_date"`
	EndDate      optional[string] `json:"end_date"`
	Timezone     optional[string] `json:"timezone"`
	Currency     optional[string] `json:"currency"`
	Travelers    optional[int]    `json:"travelers"`
	BudgetAmount optional[string] `json:"budget_amount"`
	// CoverMediaID is the picture the trip is shown by; null takes it away.
	CoverMediaID optional[string] `json:"cover_media_id"`
	// CoverCrop is the part of the cover shown; null means its middle. A new
	// cover sent without one is shown by its middle, since the old frame was
	// chosen for another picture.
	CoverCrop optional[coverCropBody] `json:"cover_crop"`
	// Languages are a report's languages, the original first. A language left
	// out loses its translations.
	Languages optional[[]string] `json:"languages"`
	// Confirm accepts that a shorter period removes days holding content.
	Confirm bool `json:"confirm"`
}

// applyOptionalDate applies a date field of a partial update. A null leaves the
// field empty, which validation then refuses.
func applyOptionalDate(field string, change optional[string], target **time.Time) error {
	if !change.Set {
		return nil
	}
	if change.Null {
		*target = nil
		return nil
	}
	date, err := domain.ParseDate(field, change.Value)
	if err != nil {
		return err
	}
	*target = &date
	return nil
}

// sameID reports whether two optional identifiers name the same thing.
func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// applyTripChanges applies a partial update onto a stored trip. A null on a
// field that cannot be cleared is treated as an empty value, which validation
// then refuses.
func applyTripChanges(trip domain.Trip, body updateTripRequest) (domain.Trip, error) {
	previousCover := trip.CoverMediaID
	if err := applyCover(body.CoverMediaID, &trip.CoverMediaID); err != nil {
		return trip, err
	}
	if !sameID(previousCover, trip.CoverMediaID) {
		trip.CoverCrop = nil
	}
	if body.CoverCrop.Set {
		trip.CoverCrop = nil
		if !body.CoverCrop.Null {
			value := body.CoverCrop.Value
			trip.CoverCrop = &domain.CoverCrop{X: value.X, Y: value.Y, W: value.W, H: value.H}
		}
	}
	if body.Title.Set {
		trip.Title = body.Title.Value
	}
	if body.Summary.Set {
		trip.Summary = body.Summary.Value
	}
	if body.Timezone.Set {
		trip.Timezone = body.Timezone.Value
		// An empty zone would silently become UTC; a change must name one.
		if strings.TrimSpace(trip.Timezone) == "" {
			return trip, domain.NewValidationError("timezone", "invalid_timezone", "must be an IANA time zone name")
		}
	}
	if body.Currency.Set {
		trip.Currency = body.Currency.Value
	}
	if body.Travelers.Set {
		trip.Travelers = body.Travelers.Value
	}
	if body.Languages.Set {
		trip.Languages = body.Languages.Value
		// A report always names its original; clearing the list is refused
		// rather than read as "English".
		if trip.Kind == domain.DocumentReport && len(trip.Languages) == 0 {
			return trip, domain.NewValidationError("languages", "required", "must name the original language")
		}
	}
	if err := applyOptionalDate("start_date", body.StartDate, &trip.StartDate); err != nil {
		return trip, err
	}
	if err := applyOptionalDate("end_date", body.EndDate, &trip.EndDate); err != nil {
		return trip, err
	}
	if body.BudgetAmount.Set {
		trip.Budget = nil
		if !body.BudgetAmount.Null {
			amount, err := domain.ParseMoney("budget_amount", body.BudgetAmount.Value)
			if err != nil {
				return trip, err
			}
			trip.Budget = &amount
		}
	}
	return trip.Normalize()
}

// handleUpdateTrip changes a trip's title, dates, currency, travellers or
// budget. Owners and editors may. New dates reshape the documents; a period
// that drops days with content answers
// 409 days_would_be_removed until the request carries "confirm": true.
func (s *Server) handleUpdateTrip(w http.ResponseWriter, r *http.Request) {
	current, ok := s.tripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body updateTripRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	trip, err := applyTripChanges(current.Trip, body)
	if err != nil {
		s.writeDomainError(w, r, "validate trip", err)
		return
	}
	if err := s.checkCover(r.Context(), trip.ID, trip.CoverMediaID); err != nil {
		s.writeDomainError(w, r, "check trip cover", err)
		return
	}
	if err := s.trips.Update(r.Context(), trip, body.Confirm); err != nil {
		s.writeDomainError(w, r, "update trip", err)
		return
	}
	updated, err := s.trips.Get(r.Context(), trip.ID, principalFrom(r.Context()).user.ID)
	if err != nil {
		s.writeDomainError(w, r, "get trip", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newTripResponse(updated, s.now()))
}

// handleDeleteTrip deletes a trip for everybody, with everything in it: its
// documents, its costs and its files. Only the owner may, and there is no undo.
func (s *Server) handleDeleteTrip(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionDelete)
	if !ok {
		return
	}
	keys, err := s.trips.Delete(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "delete trip", err)
		return
	}
	// The rows are gone, so the files are unreachable either way; a leftover on
	// disk is worth a log line rather than a failed request.
	for _, key := range keys {
		if s.mediaFiles == nil {
			break
		}
		if err := media.DeleteWithPreviews(r.Context(), s.mediaFiles, key); err != nil {
			s.logger.Error("delete media file failed", "error", err, "key", key)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// memberResponse is one person with access to a trip.
type memberResponse struct {
	User      tripUserResponse `json:"user"`
	Role      string           `json:"role"`
	CreatedAt time.Time        `json:"created_at"`
}

// newMemberResponse maps a trip member onto the wire.
func newMemberResponse(member domain.TripMember) memberResponse {
	return memberResponse{User: newTripUserResponse(member.User), Role: string(member.Role), CreatedAt: member.CreatedAt}
}

// handleListMembers lists everybody with access to a trip. Every participant
// may see who else takes part.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	members, err := s.trips.Members(r.Context(), trip.ID)
	if err != nil {
		s.internalError(w, r, "list members", err)
		return
	}
	items := make([]memberResponse, 0, len(members))
	for _, member := range members {
		items = append(items, newMemberResponse(member))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[memberResponse]{Items: items})
}

// addMemberRequest is the body of POST /api/v1/trips/{tripID}/members.
type addMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// parseUserID reads a person's identifier from a request body.
func parseUserID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, domain.NewValidationError("user_id", "unknown_user", "no such account")
	}
	return id, nil
}

// handleAddMember gives an existing account access to a trip. Only the owner
// decides who sees a trip.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
	var body addMemberRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	userID, err := parseUserID(body.UserID)
	if err != nil {
		s.writeDomainError(w, r, "validate member", err)
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate member", err)
		return
	}
	member, err := s.trips.AddMember(r.Context(), trip.ID, userID, role)
	if err != nil {
		s.writeDomainError(w, r, "add member", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newMemberResponse(member))
}

// updateMemberRequest is the body of PATCH /api/v1/trips/{tripID}/members/{userID}.
type updateMemberRequest struct {
	Role string `json:"role"`
}

// handleUpdateMember switches a member between editor and viewer.
func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var body updateMemberRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate member", err)
		return
	}
	member, err := s.trips.UpdateMember(r.Context(), trip.ID, userID, role)
	if err != nil {
		s.writeDomainError(w, r, "update member", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newMemberResponse(member))
}

// handleRemoveMember takes a member's access away.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	if err := s.trips.RemoveMember(r.Context(), trip.ID, userID); err != nil {
		s.writeDomainError(w, r, "remove member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// minUserSearchLength keeps the people search from listing everybody on a
// single typed letter.
const minUserSearchLength = 2

// handleSearchUsers finds active accounts by name or email so an owner can add
// them to a trip. It shows only what identifies a person.
func (s *Server) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < minUserSearchLength {
		writeJSON(w, s.logger, http.StatusOK, listResponse[tripUserResponse]{Items: []tripUserResponse{}})
		return
	}
	if len([]rune(query)) > 200 {
		s.writeDomainError(w, r, "validate search", domain.NewValidationError("q", "too_long", "must be at most 200 characters"))
		return
	}
	users, err := s.users.Search(r.Context(), query, principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "search users", err)
		return
	}
	items := make([]tripUserResponse, 0, len(users))
	for _, user := range users {
		items = append(items, newTripUserResponse(user))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[tripUserResponse]{Items: items})
}
