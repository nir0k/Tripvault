package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// maxLegsPerCalculation bounds how many legs one request calculates, so a
// request never waits on dozens of provider calls; the rest stay pending and
// the client asks again.
const maxLegsPerCalculation = 25

// calculateLegs calculates a document's pending legs, and with force also the
// named legs whatever their state, forgetting their cached routes first.
//
// The provider is called outside any transaction; a leg that changed while its
// calculation ran keeps waiting instead of receiving a stale result.
func (s *Server) calculateLegs(ctx context.Context, documentID uuid.UUID, force map[uuid.UUID]bool,
	retryEstimates bool) error {
	content, err := s.documents.Content(ctx, documentID)
	if err != nil {
		return err
	}
	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	items := make(map[uuid.UUID]domain.Item, len(content.Items))
	for _, item := range content.Items {
		items[item.ID] = item
	}

	done := 0
	for _, leg := range content.Legs {
		forced := force[leg.ID]
		wanted := leg.Source == domain.LegPending || forced ||
			(retryEstimates && leg.RetryableEstimate())
		if !wanted || done >= maxLegsPerCalculation {
			continue
		}
		done++
		from, to := domain.LegEnds(items[leg.FromItemID], items[leg.ToItemID], stays, content.Tracks)
		if forced && from != nil && to != nil {
			if err := s.routing.Forget(ctx, leg.Mode, *from, *to); err != nil {
				return err
			}
		}
		calculation := s.routing.Calculate(ctx, leg.Mode, from, to)
		if err := s.documents.SaveLegCalculation(ctx, leg.ID, leg.Input, calculation, s.now()); err != nil {
			return err
		}
	}
	return nil
}

// handleCalculateLegs calculates the legs waiting after a change. The client
// calls it a moment after edits settle, so a burst of moves costs one round of
// provider requests.
func (s *Server) handleCalculateLegs(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	if err := s.calculateLegs(r.Context(), documentID, nil, false); err != nil {
		s.writeDomainError(w, r, "calculate legs", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// handleRetryEstimatedLegs asks the provider again about the legs that came back
// as estimates.
//
// It exists because a plan full of straight lines is the normal state of a trip
// planned before the routing service was configured, or while its daily limit
// was spent: those legs hold an estimate and a reason, and nothing would ever
// ask again. Walking the days one by one is what this replaces.
//
// Legs with no road route between their points are left alone, and so are
// flights: neither becomes a road because a key was entered. One call takes a
// bounded number of legs, so a long trip is retried over a few presses, which
// the count in the answer lets the interface follow.
func (s *Server) handleRetryEstimatedLegs(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, documentID, domain.ActionEdit); !ok {
		return
	}
	if err := s.calculateLegs(r.Context(), documentID, nil, true); err != nil {
		s.writeDomainError(w, r, "retry estimated legs", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, documentID)
}

// legFor loads the leg named in the path and checks the role on its trip.
func (s *Server) legFor(w http.ResponseWriter, r *http.Request,
	action domain.TripAction) (domain.Leg, domain.Document, bool) {
	legID, ok := s.pathUUID(w, r, "legID")
	if !ok {
		return domain.Leg{}, domain.Document{}, false
	}
	leg, err := s.documents.Leg(r.Context(), legID)
	if err != nil {
		s.writeDomainError(w, r, "get leg", err)
		return domain.Leg{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, leg.DocumentID, action)
	return leg, document, ok
}

// updateLegRequest is the body of PATCH /api/v1/legs/{legID}. distance_m and
// duration_s set typed values; null returns to the calculated one.
type updateLegRequest struct {
	Mode              optional[string] `json:"mode"`
	DistanceM         optional[int]    `json:"distance_m"`
	DurationS         optional[int]    `json:"duration_s"`
	PlannedCostAmount optional[string] `json:"planned_cost_amount"`
	// ActualCostAmount is refused on a plan.
	ActualCostAmount optional[string] `json:"actual_cost_amount"`
	Note             optional[string] `json:"note"`
	// ResetManual drops both typed values at once.
	ResetManual bool `json:"reset_manual"`
}

// applyNullableInt applies a clearable whole number.
func applyNullableInt(change optional[int], target **int) {
	if !change.Set {
		return
	}
	*target = nil
	if !change.Null {
		value := change.Value
		*target = &value
	}
}

// handleUpdateLeg changes a leg's mode, typed distance and time, cost or note.
// A new mode sends the leg back to pending.
func (s *Server) handleUpdateLeg(w http.ResponseWriter, r *http.Request) {
	leg, document, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body updateLegRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Mode.Set {
		leg.Mode = domain.TravelMode(body.Mode.Value)
	}
	if body.ResetManual {
		leg.ManualDistanceM, leg.ManualDurationS = nil, nil
	}
	applyNullableInt(body.DistanceM, &leg.ManualDistanceM)
	applyNullableInt(body.DurationS, &leg.ManualDurationS)
	if err := applyNullableMoney("planned_cost_amount", body.PlannedCostAmount, &leg.PlannedCost); err != nil {
		s.writeDomainError(w, r, "validate leg", err)
		return
	}
	if err := applyNullableMoney("actual_cost_amount", body.ActualCostAmount, &leg.ActualCost); err != nil {
		s.writeDomainError(w, r, "validate leg", err)
		return
	}
	if body.Note.Set {
		leg.Note = body.Note.Value
	}
	leg, err := leg.Normalize(document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate leg", err)
		return
	}
	if err := s.documents.UpdateLeg(r.Context(), leg); err != nil {
		s.writeDomainError(w, r, "update leg", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, leg.DocumentID)
}

// handleRecalculateLeg calculates one leg again, bypassing the route cache:
// the "retry" next to an estimate.
func (s *Server) handleRecalculateLeg(w http.ResponseWriter, r *http.Request) {
	leg, _, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	if err := s.calculateLegs(r.Context(), leg.DocumentID, map[uuid.UUID]bool{leg.ID: true}, false); err != nil {
		s.writeDomainError(w, r, "recalculate leg", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, leg.DocumentID)
}

// handleRecalculateDay calculates every leg of a day again, bypassing the
// route cache.
func (s *Server) handleRecalculateDay(w http.ResponseWriter, r *http.Request) {
	day, _, ok := s.dayFor(w, r)
	if !ok {
		return
	}
	content, err := s.documents.Content(r.Context(), day.DocumentID)
	if err != nil {
		s.writeDomainError(w, r, "read document", err)
		return
	}
	force := make(map[uuid.UUID]bool)
	for _, leg := range content.Legs {
		if leg.DayID == day.ID {
			force[leg.ID] = true
		}
	}
	if err := s.calculateLegs(r.Context(), day.DocumentID, force, false); err != nil {
		s.writeDomainError(w, r, "recalculate day", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, day.DocumentID)
}
