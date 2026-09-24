package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A report is the same kind of document as a plan, edited through the same
// endpoints; what it adds is a status and a story on each place, the amounts
// actually spent, and the figures its reading mode opens with. Those totals are
// derived on every read, like the budget, so nothing has to be kept in step.

// reportCountsResponse counts the places of a report by status.
type reportCountsResponse struct {
	Planned   int `json:"planned"`
	Visited   int `json:"visited"`
	Skipped   int `json:"skipped"`
	Unplanned int `json:"unplanned"`
	Total     int `json:"total"`
}

// reportTotalsResponse is what the reading mode of a report shows above its days.
type reportTotalsResponse struct {
	Days   int                  `json:"days"`
	Nights int                  `json:"nights"`
	Places reportCountsResponse `json:"places"`
	// DistanceM and ByMode are the distance travelled, in total and per means.
	DistanceM int                 `json:"distance_m"`
	ByMode    []modeTotalResponse `json:"by_mode"`
	// PlannedCost is the snapshot the report carries, ActualCost what was really
	// spent and Difference the second minus the first.
	PlannedCost string `json:"planned_cost"`
	ActualCost  string `json:"actual_cost"`
	Difference  string `json:"difference"`
	Rated       int    `json:"rated"`
	// AverageRating is the mean of the ratings given, null when none were.
	AverageRating *float64 `json:"average_rating"`
}

// newReportTotalsResponse maps a report's totals onto the wire.
func newReportTotalsResponse(totals domain.ReportTotals) reportTotalsResponse {
	byMode := []modeTotalResponse{}
	for _, mode := range travelModes {
		if total, ok := totals.ByMode[mode]; ok {
			byMode = append(byMode, modeTotalResponse{
				Mode: string(mode), DistanceM: total.DistanceM, DurationS: total.DurationS,
			})
		}
	}
	return reportTotalsResponse{
		Days:   totals.Days,
		Nights: totals.Nights,
		Places: reportCountsResponse{
			Planned:   totals.Places.Planned,
			Visited:   totals.Places.Visited,
			Skipped:   totals.Places.Skipped,
			Unplanned: totals.Places.Unplanned,
			Total:     totals.Places.Total(),
		},
		DistanceM:     totals.DistanceM,
		ByMode:        byMode,
		PlannedCost:   totals.PlannedCost.String(),
		ActualCost:    totals.ActualCost.String(),
		Difference:    totals.Difference.String(),
		Rated:         totals.Rated,
		AverageRating: totals.AverageRating,
	}
}

// handleCreateReport writes a new report from a plan: a trip of its own, owned
// by whoever asks, with the plan's settings, budget and content copied in.
//
// Reading the plan is enough. A report is written by somebody who travelled,
// who need not be the one who planned, and it changes nothing in the plan; its
// people, links and pictures are not copied, so the new owner decides alone
// who reads it.
func (s *Server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	plan, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	if plan.Kind != domain.DocumentPlan || plan.PlanID == nil {
		s.writeError(w, r, http.StatusConflict, "not_a_plan", "Only a plan can be copied into a report")
		return
	}

	user := principalFrom(r.Context()).user
	source := plan.ID
	report, err := domain.Trip{
		ID:           uuid.Must(uuid.NewV7()),
		OwnerID:      user.ID,
		Kind:         domain.DocumentReport,
		SourceTripID: &source,
		Title:        plan.Title,
		Summary:      plan.Summary,
		StartDate:    plan.StartDate,
		EndDate:      plan.EndDate,
		Timezone:     plan.Timezone,
		Currency:     plan.Currency,
		Travelers:    plan.Travelers,
		Budget:       plan.Budget,
		Languages:    []string{domain.ContentLanguage(user.Locale)},
	}.Normalize()
	if err != nil {
		s.writeDomainError(w, r, "validate report", err)
		return
	}
	if err := s.documents.CreateReport(r.Context(), report, *plan.PlanID); err != nil {
		s.writeDomainError(w, r, "create report", err)
		return
	}
	created, err := s.trips.Get(r.Context(), report.ID, user.ID)
	if err != nil {
		s.writeDomainError(w, r, "read report", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newTripResponse(created, s.now()))
}

// documentTextRequest changes the words around a document's days. Omitted
// fields keep their value.
type documentTextRequest struct {
	IntroMD   optional[string] `json:"intro_md"`
	SummaryMD optional[string] `json:"summary_md"`
}

// handleUpdateDocument changes a document's opening and closing words.
func (s *Server) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	document, ok := s.documentFor(w, r, documentID, domain.ActionEdit)
	if !ok {
		return
	}
	var body documentTextRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	intro, summary := document.IntroMD, document.SummaryMD
	if body.IntroMD.Set {
		intro = body.IntroMD.Value
	}
	if body.SummaryMD.Set {
		summary = body.SummaryMD.Value
	}
	if err := domain.CheckDocumentText(intro, summary); err != nil {
		s.writeDomainError(w, r, "validate document", err)
		return
	}
	if err := s.documents.UpdateDocument(r.Context(), documentID, intro, summary); err != nil {
		s.writeDomainError(w, r, "update document", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}
