package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// errCompositeRoute refuses to choose the route of a leg with changes: its
// parts are routed the default way, each on its own.
var errCompositeRoute = domain.NewValidationError("leg", "composite_leg",
	"a leg with changes is routed part by part; remove the changes to choose its route")

// segmentResponse is one part of a leg with changes on the wire.
type segmentResponse struct {
	ID              string  `json:"id"`
	Mode            string  `json:"mode"`
	DistanceM       *int    `json:"distance_m"`
	DurationS       *int    `json:"duration_s"`
	CalculatedDistM *int    `json:"calculated_distance_m"`
	CalculatedDurS  *int    `json:"calculated_duration_s"`
	ManualDistance  bool    `json:"manual_distance"`
	ManualDuration  bool    `json:"manual_duration"`
	Geometry        string  `json:"geometry"`
	Source          string  `json:"source"`
	Error           *string `json:"error"`
	// TicketID is the ticket the part travels on, or null.
	TicketID *string `json:"ticket_id"`
	// Stop is the change where the part ends; null for the last part.
	Stop *stopBody `json:"stop"`
}

// stopBody is a change on the way on the wire: where it is and how long it is
// waited at.
type stopBody struct {
	Name        string   `json:"name"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	WaitMinutes int      `json:"wait_minutes"`
}

// ticketResponse is a ticket of a leg with changes on the wire.
type ticketResponse struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	PlannedCostAmount *string `json:"planned_cost_amount"`
	ActualCostAmount  *string `json:"actual_cost_amount"`
}

// newSegmentResponses renders a leg's parts as a list, never null.
func newSegmentResponses(segments []domain.LegSegment) []segmentResponse {
	items := make([]segmentResponse, len(segments))
	for index, segment := range segments {
		item := segmentResponse{
			ID: segment.ID.String(), Mode: string(segment.Mode),
			DistanceM: segment.Distance(), DurationS: segment.Duration(),
			CalculatedDistM: segment.DistanceM, CalculatedDurS: segment.DurationS,
			ManualDistance: segment.ManualDistanceM != nil, ManualDuration: segment.ManualDurationS != nil,
			Geometry: segment.Geometry, Source: string(segment.Source), Error: optionalString(segment.Error),
			TicketID: formatID(segment.TicketID),
		}
		if index < len(segments)-1 {
			item.Stop = &stopBody{Name: segment.StopName, Lat: segment.StopLat, Lng: segment.StopLng,
				WaitMinutes: segment.WaitMinutes}
		}
		items[index] = item
	}
	return items
}

// newTicketResponses renders a leg's tickets as a list, never null.
func newTicketResponses(tickets []domain.LegTicket) []ticketResponse {
	items := make([]ticketResponse, len(tickets))
	for index, ticket := range tickets {
		items[index] = ticketResponse{ID: ticket.ID.String(), Name: ticket.Name,
			PlannedCostAmount: formatMoney(ticket.PlannedCost), ActualCostAmount: formatMoney(ticket.ActualCost)}
	}
	return items
}

// segmentRequest is one part of PUT /api/v1/legs/{legID}/segments. ticket is
// the position of its ticket in the request's list of tickets, or null.
type segmentRequest struct {
	Mode      string    `json:"mode"`
	DistanceM *int      `json:"distance_m"`
	DurationS *int      `json:"duration_s"`
	Ticket    *int      `json:"ticket"`
	Stop      *stopBody `json:"stop"`
}

// ticketRequest is one ticket of PUT /api/v1/legs/{legID}/segments.
type ticketRequest struct {
	Name              string  `json:"name"`
	PlannedCostAmount *string `json:"planned_cost_amount"`
	ActualCostAmount  *string `json:"actual_cost_amount"`
}

// setSegmentsRequest is the body of PUT /api/v1/legs/{legID}/segments.
type setSegmentsRequest struct {
	Segments []segmentRequest `json:"segments"`
	Tickets  []ticketRequest  `json:"tickets"`
}

// parseLegParts turns the request into a leg's parts and tickets with fresh
// identifiers, the tickets referred to by position.
//
// Arguments:
//   - body: the request.
//
// Returns:
//   - the parts and tickets, not yet normalised.
//   - a *domain.ValidationError for a ticket position or an amount that is wrong.
func parseLegParts(body setSegmentsRequest) ([]domain.LegSegment, []domain.LegTicket, error) {
	tickets := make([]domain.LegTicket, len(body.Tickets))
	for index, request := range body.Tickets {
		ticket := domain.LegTicket{ID: uuid.Must(uuid.NewV7()), Name: request.Name}
		for _, amount := range []struct {
			field  string
			value  *string
			target **domain.Money
		}{
			{"tickets.planned_cost_amount", request.PlannedCostAmount, &ticket.PlannedCost},
			{"tickets.actual_cost_amount", request.ActualCostAmount, &ticket.ActualCost},
		} {
			if amount.value == nil {
				continue
			}
			money, err := domain.ParseMoney(amount.field, *amount.value)
			if err != nil {
				return nil, nil, err
			}
			*amount.target = &money
		}
		tickets[index] = ticket
	}
	segments := make([]domain.LegSegment, len(body.Segments))
	for index, request := range body.Segments {
		segment := domain.LegSegment{
			ID: uuid.Must(uuid.NewV7()), Mode: domain.TravelMode(request.Mode),
			ManualDistanceM: request.DistanceM, ManualDurationS: request.DurationS,
		}
		if request.Ticket != nil {
			if *request.Ticket < 0 || *request.Ticket >= len(tickets) {
				return nil, nil, domain.NewValidationError("segments.ticket", "unknown_ticket",
					"must be the position of one of the tickets")
			}
			segment.TicketID = &tickets[*request.Ticket].ID
		}
		if request.Stop != nil {
			segment.StopName, segment.StopLat, segment.StopLng = request.Stop.Name, request.Stop.Lat, request.Stop.Lng
			segment.WaitMinutes = request.Stop.WaitMinutes
		}
		segments[index] = segment
	}
	return segments, tickets, nil
}

// carryCalculations gives each new part the calculation of an old part that
// went the same way between the same points, so saving a change does not send
// the unchanged parts back to the provider. The input is worked out as the
// reconciliation will, so a carried part is not reset again.
//
// Arguments:
//   - segments: the new parts, in order.
//   - old: the parts the leg had.
//   - start, end: where the whole leg starts and ends.
//
// Returns:
//   - the parts, those that match an old one with its calculation.
func carryCalculations(segments, old []domain.LegSegment, start, end *domain.Point) []domain.LegSegment {
	byInput := make(map[string]domain.LegSegment, len(old))
	for _, segment := range old {
		if segment.Source != domain.LegPending {
			byInput[segment.Input] = segment
		}
	}
	starts, ends := domain.SegmentEnds(segments, start, end)
	for index := range segments {
		segment := &segments[index]
		segment.Input = domain.SegmentInput(segment.Mode, starts[index], ends[index])
		segment.Source = domain.LegPending
		if previous, ok := byInput[segment.Input]; ok {
			segment.DistanceM, segment.DurationS, segment.Geometry = previous.DistanceM, previous.DurationS, previous.Geometry
			segment.Source, segment.Error, segment.CalculatedAt = previous.Source, previous.Error, previous.CalculatedAt
		}
	}
	return segments
}

// collapseLeg turns a leg back into one travelled one way: the only part left -
// or the first, when none is - gives it its mode and typed values, and its
// tickets add up to its cost.
func collapseLeg(leg domain.Leg, segments []domain.LegSegment, tickets []domain.LegTicket) domain.Leg {
	if len(segments) > 0 {
		leg.Mode = segments[0].Mode
		leg.ManualDistanceM, leg.ManualDurationS = segments[0].ManualDistanceM, segments[0].ManualDurationS
	}
	leg.PlannedCost, leg.ActualCost = nil, nil
	for _, ticket := range tickets {
		if ticket.PlannedCost != nil {
			sum := *ticket.PlannedCost
			if leg.PlannedCost != nil {
				sum += *leg.PlannedCost
			}
			leg.PlannedCost = &sum
		}
		if ticket.ActualCost != nil {
			sum := *ticket.ActualCost
			if leg.ActualCost != nil {
				sum += *leg.ActualCost
			}
			leg.ActualCost = &sum
		}
	}
	leg.Segments, leg.Tickets = nil, nil
	leg.Via, leg.Preference = nil, domain.RouteFastest
	return leg
}

// handleSetLegSegments replaces the parts and tickets of a leg. Two parts or
// more make it a journey with changes; one or none make it a plain leg again,
// which takes the part's mode and typed values and its tickets' cost. Parts that
// kept their mode and ends keep their calculation; the others wait for one.
func (s *Server) handleSetLegSegments(w http.ResponseWriter, r *http.Request) {
	leg, document, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body setSegmentsRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	segments, tickets, err := parseLegParts(body)
	if err == nil {
		segments, tickets, err = domain.NormalizeSegments(document.Kind, segments, tickets)
	}
	if err != nil {
		s.writeDomainError(w, r, "validate leg parts", err)
		return
	}
	if len(segments) < 2 {
		leg = collapseLeg(leg, segments, tickets)
		if leg, err = leg.Normalize(document.Kind); err != nil {
			s.writeDomainError(w, r, "validate leg", err)
			return
		}
	} else {
		start, end, err := s.legEndPoints(r.Context(), leg)
		if err != nil {
			s.writeDomainError(w, r, "read leg ends", err)
			return
		}
		leg.Segments = carryCalculations(segments, leg.Segments, start, end)
		leg.Tickets = tickets
	}
	if err := s.documents.SaveLegParts(r.Context(), leg); err != nil {
		s.writeDomainError(w, r, "save leg parts", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, leg.DocumentID)
}

// legEndPoints reads where a leg starts and ends, each nil when unknown.
func (s *Server) legEndPoints(ctx context.Context, leg domain.Leg) (*domain.Point, *domain.Point, error) {
	content, err := s.documents.Content(ctx, leg.DocumentID)
	if err != nil {
		return nil, nil, err
	}
	stays := make(map[uuid.UUID]domain.Stay, len(content.Stays))
	for _, stay := range content.Stays {
		stays[stay.ID] = stay
	}
	var from, to domain.Item
	for _, item := range content.Items {
		switch item.ID {
		case leg.FromItemID:
			from = item
		case leg.ToItemID:
			to = item
		}
	}
	start, end := domain.LegEnds(from, to, stays, content.Tracks)
	return start, end, nil
}

// calculateSegments calculates the parts of a composite leg that wait, or all of
// them when forced, as calculateLegs does for plain legs.
//
// Arguments:
//   - ctx: context bounding the lookups and the provider requests.
//   - leg: the composite leg.
//   - start, end: where the whole leg starts and ends.
//   - forced: recalculate every part, forgetting the cached routes first.
//   - retryEstimates: also ask again for estimates whose reason may have passed.
//   - budget: how many parts may still be calculated in this round.
//
// Returns:
//   - how many parts were calculated.
//   - an error if the cache or the store fails.
func (s *Server) calculateSegments(ctx context.Context, leg domain.Leg, start, end *domain.Point, forced,
	retryEstimates bool, budget int) (int, error) {
	starts, ends := domain.SegmentEnds(leg.Segments, start, end)
	done := 0
	for index, segment := range leg.Segments {
		retry := retryEstimates && segment.Source == domain.LegEstimate &&
			domain.Leg{Source: segment.Source, Error: segment.Error}.RetryableEstimate()
		if segment.Source != domain.LegPending && !forced && !retry || done >= budget {
			continue
		}
		done++
		from, to := starts[index], ends[index]
		if forced && from != nil && to != nil {
			if err := s.routing.Forget(ctx, segment.Mode, *from, *to, domain.LegRoute{}); err != nil {
				return done, err
			}
		}
		calculation := s.routing.Calculate(ctx, segment.Mode, from, to, domain.LegRoute{})
		if _, err := s.documents.SaveSegmentCalculation(ctx, segment.ID, segment.Input, calculation, s.now()); err != nil {
			return done, err
		}
	}
	return done, nil
}

// updateCompositeLeg applies PATCH /api/v1/legs/{legID} to a leg with changes.
// Its note is its own; its mode, distance, time and route belong to its parts,
// which are changed with PUT /legs/{legID}/segments. A cost cleared from the
// budget clears what its tickets cost, since that is what the budget showed.
func (s *Server) updateCompositeLeg(w http.ResponseWriter, r *http.Request, leg domain.Leg, document domain.Document,
	body updateLegRequest) {
	if body.Mode.Set || body.DistanceM.Set || body.DurationS.Set || body.ResetManual || body.RoutePreference.Set ||
		body.Via.Set || (body.PlannedCostAmount.Set && !body.PlannedCostAmount.Null) ||
		(body.ActualCostAmount.Set && !body.ActualCostAmount.Null) {
		s.writeDomainError(w, r, "validate leg", domain.NewValidationError("leg", "composite_leg",
			"a leg with changes is changed part by part, with PUT /legs/{legID}/segments"))
		return
	}
	if body.PlannedCostAmount.Set || body.ActualCostAmount.Set {
		for index := range leg.Tickets {
			if body.PlannedCostAmount.Set {
				leg.Tickets[index].PlannedCost = nil
			}
			if body.ActualCostAmount.Set {
				leg.Tickets[index].ActualCost = nil
			}
		}
		if err := s.documents.SaveLegParts(r.Context(), leg); err != nil {
			s.writeDomainError(w, r, "save leg parts", err)
			return
		}
	}
	if body.Note.Set {
		folded := leg
		folded.Note = body.Note.Value
		folded, err := folded.Normalize(document.Kind)
		if err != nil {
			s.writeDomainError(w, r, "validate leg", err)
			return
		}
		if err := s.documents.UpdateLegNote(r.Context(), leg.ID, folded.Note); err != nil {
			s.writeDomainError(w, r, "update leg", err)
			return
		}
	}
	s.writeDocument(w, r, http.StatusOK, leg.DocumentID)
}
