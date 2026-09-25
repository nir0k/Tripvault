package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/geocoding"
	"github.com/nir0k/tripvault/backend/internal/googlelink"
	"github.com/nir0k/tripvault/backend/internal/routing"
)

// maxLegsPerCalculation bounds how many legs one request calculates, so a
// request never waits on dozens of provider calls; the rest stay pending and
// the client asks again.
const maxLegsPerCalculation = 25

// calculateLegs calculates a document's pending legs, and with force also the
// named legs whatever their state, forgetting their cached routes first. A
// forced calculation replaces a route chosen among the alternatives; nothing
// else touches one.
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
		wanted := (leg.Source == domain.LegPending && !leg.Pinned) || forced ||
			(retryEstimates && leg.RetryableEstimate() && !leg.Pinned)
		if !wanted || done >= maxLegsPerCalculation {
			continue
		}
		done++
		from, to := domain.LegEnds(items[leg.FromItemID], items[leg.ToItemID], stays, content.Tracks)
		if forced && from != nil && to != nil {
			if err := s.routing.Forget(ctx, leg.Mode, *from, *to, leg.Route()); err != nil {
				return err
			}
		}
		calculation := s.routing.Calculate(ctx, leg.Mode, from, to, leg.Route())
		if _, err := s.documents.SaveLegCalculation(ctx, leg.ID, leg.Input, calculation, false, s.now()); err != nil {
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
	// RoutePreference is what a road route is optimised for.
	RoutePreference optional[string] `json:"route_preference"`
	// Via replaces the points a road route passes through; null or an empty
	// list routes it straight from one end to the other.
	Via optional[[]pointBody] `json:"via"`
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

// handleUpdateLeg changes a leg's mode, typed distance and time, cost, note or
// the way it is routed. A new mode, preference or set of via points sends the
// leg back to pending.
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
	if body.RoutePreference.Set {
		leg.Preference = domain.RoutePreference(body.RoutePreference.Value)
		if body.RoutePreference.Null {
			leg.Preference = domain.RouteFastest
		}
	}
	if body.Via.Set {
		leg.Via = nil
		for _, point := range body.Via.Value {
			leg.Via = append(leg.Via, domain.Point{Lat: point.Lat, Lng: point.Lng})
		}
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

// legEnds reads where a leg starts and ends, as its calculation sees them.
//
// Returns:
//   - both ends, or a validation error naming the leg when either is unknown.
//   - an error when the document cannot be read.
func (s *Server) legEnds(ctx context.Context, leg domain.Leg) (domain.Point, domain.Point, error) {
	content, err := s.documents.Content(ctx, leg.DocumentID)
	if err != nil {
		return domain.Point{}, domain.Point{}, err
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
	if start == nil || end == nil {
		return domain.Point{}, domain.Point{}, domain.NewValidationError("leg", "missing_coordinates",
			"both ends of the leg need a position")
	}
	return *start, *end, nil
}

// writeRoutingError answers a request the routing provider could not serve.
func (s *Server) writeRoutingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, routing.ErrNotRouted):
		s.writeDomainError(w, r, "route leg",
			domain.NewValidationError("mode", "not_routed", "only a road leg has routes to choose from"))
	case errors.Is(err, routing.ErrDisabled):
		s.writeError(w, r, http.StatusServiceUnavailable, "routing_disabled", "Road routing is not configured")
	case errors.Is(err, routing.ErrDailyLimit), errors.Is(err, routing.ErrRateLimited):
		s.writeError(w, r, http.StatusServiceUnavailable, "routing_limited", "Road routing is busy; try again later")
	case errors.Is(err, routing.ErrNoRoute):
		s.writeError(w, r, http.StatusUnprocessableEntity, "no_route", "No road joins the two ends of the leg")
	case errors.Is(err, routing.ErrUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "routing_unavailable", "Road routing did not answer")
	default:
		s.writeDomainError(w, r, "route leg", err)
	}
}

// routeBody is one road route on the wire.
type routeBody struct {
	DistanceM int    `json:"distance_m"`
	DurationS int    `json:"duration_s"`
	Geometry  string `json:"geometry"`
}

// routeListResponse lists the routes a leg may take, the best first.
type routeListResponse struct {
	Items []routeBody `json:"items"`
}

// handleLegAlternatives asks the provider for the routes a road leg could take,
// for its editor to choose one. Nothing is stored: the choice is sent back to
// handlePinLegRoute. Alternatives run between the two ends; a leg routed
// through points of its own has only the one route those points make.
func (s *Server) handleLegAlternatives(w http.ResponseWriter, r *http.Request) {
	leg, _, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	from, to, err := s.legEnds(r.Context(), leg)
	if err != nil {
		s.writeDomainError(w, r, "read leg ends", err)
		return
	}
	calculations, err := s.routing.Alternatives(r.Context(), leg.Mode, from, to, leg.Route().Preference)
	if err != nil {
		s.writeRoutingError(w, r, err)
		return
	}
	response := routeListResponse{Items: make([]routeBody, 0, len(calculations))}
	for _, calculation := range calculations {
		if calculation.DistanceM == nil || calculation.DurationS == nil {
			continue
		}
		response.Items = append(response.Items, routeBody{
			DistanceM: *calculation.DistanceM, DurationS: *calculation.DurationS, Geometry: calculation.Geometry,
		})
	}
	writeJSON(w, s.logger, http.StatusOK, response)
}

// pinTolerance is how far from a leg's end a chosen route may start or finish:
// a provider snaps a pin to the nearest road, which on a waterfall or a
// viewpoint can be a few kilometres away.
const pinTolerance = 6000.0

// validateChosenRoute checks that a route sent back for a leg is one: a line
// that runs from near its start to near its end, with a plausible length and
// time. The route came from the provider through the browser, so it is only
// checked, not believed.
func validateChosenRoute(body routeBody, from, to domain.Point) error {
	if body.DistanceM < 0 || body.DistanceM > 50_000_000 {
		return domain.NewValidationError("distance_m", "out_of_range", "must be between 0 and 50 000 km")
	}
	if body.DurationS < 0 || body.DurationS > 7*24*3600 {
		return domain.NewValidationError("duration_s", "out_of_range", "must be between 0 and 7 days")
	}
	if len(body.Geometry) > maxRouteGeometryLength {
		return domain.NewValidationError("geometry", "too_long", "the line is too long")
	}
	line := domain.DecodePolyline(body.Geometry, 5)
	if len(line) < 2 {
		return domain.NewValidationError("geometry", "invalid_line", "must be an encoded line of at least two points")
	}
	if routing.Haversine(line[0], from) > pinTolerance || routing.Haversine(line[len(line)-1], to) > pinTolerance {
		return domain.NewValidationError("geometry", "wrong_ends", "must run between the ends of the leg")
	}
	return nil
}

// maxRouteGeometryLength bounds the encoded line of a chosen route, about as
// long as the longest a provider sends for a day's drive.
const maxRouteGeometryLength = 1 << 20

// handlePinLegRoute stores the route somebody chose among the alternatives. It
// is kept - not calculated again, not replaced by an estimate - until the leg's
// ends, mode or routing change or it is recalculated on purpose.
func (s *Server) handlePinLegRoute(w http.ResponseWriter, r *http.Request) {
	leg, _, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body routeBody
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if !leg.Mode.Routed() || len(leg.Via) > 0 {
		s.writeDomainError(w, r, "pin leg route",
			domain.NewValidationError("mode", "not_routed", "only a road leg between two points takes a chosen route"))
		return
	}
	from, to, err := s.legEnds(r.Context(), leg)
	if err != nil {
		s.writeDomainError(w, r, "read leg ends", err)
		return
	}
	if err := validateChosenRoute(body, from, to); err != nil {
		s.writeDomainError(w, r, "pin leg route", err)
		return
	}
	distance, duration := body.DistanceM, body.DurationS
	calculation := domain.LegCalculation{
		DistanceM: &distance, DurationS: &duration, Geometry: body.Geometry, Source: domain.LegProvider,
	}
	stored, err := s.documents.SaveLegCalculation(r.Context(), leg.ID, leg.Input, calculation, true, s.now())
	if err != nil {
		s.writeDomainError(w, r, "pin leg route", err)
		return
	}
	if !stored {
		s.writeError(w, r, http.StatusConflict, "leg_changed", "The leg changed while the route was chosen")
		return
	}
	s.writeDocument(w, r, http.StatusOK, leg.DocumentID)
}

// googleLinkRequest is the body of POST /api/v1/legs/{legID}:google-link.
type googleLinkRequest struct {
	URL string `json:"url"`
}

// viaListResponse lists the points a leg would be routed through.
type viaListResponse struct {
	Via []pointBody `json:"via"`
}

// endpointRadius is how near one of the leg's own ends a position read from a
// link without structure is taken to be that end rather than a point between.
const endpointRadius = 2000.0

// linkVia picks, from the positions a route link names, the ones a leg passes
// through on its way: the link's own start and end are the leg's, and are
// dropped. A link whose structure was read says which positions are its start
// and end; one read by its coordinates alone is trimmed of the positions near
// the leg's ends.
func linkVia(route googlelink.Route, from, to domain.Point) []domain.Point {
	points := route.Points
	if route.Labelled {
		if len(points) > 0 && points[0].Stop {
			points = points[1:]
		}
		if len(points) > 0 && points[len(points)-1].Stop {
			points = points[:len(points)-1]
		}
	} else {
		for len(points) > 0 && routing.Haversine(points[0].Point, from) <= endpointRadius {
			points = points[1:]
		}
		for len(points) > 0 && routing.Haversine(points[len(points)-1].Point, to) <= endpointRadius {
			points = points[:len(points)-1]
		}
	}
	via := make([]domain.Point, len(points))
	for index, point := range points {
		via[index] = point.Point
	}
	return via
}

// handleLegGoogleLink reads the points a route drawn in Google Maps passes
// through, for a leg to be routed the same way. A short link is expanded
// first, asking only Google's own hosts where it leads. Nothing is stored: the
// editor shows the points and saves them with the leg.
func (s *Server) handleLegGoogleLink(w http.ResponseWriter, r *http.Request) {
	leg, _, ok := s.legFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body googleLinkRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.URL)
	notALink := domain.NewValidationError("url", "not_a_route_link", "must be a link to a route in Google Maps")
	if text == "" || len(text) > maxLinkLength {
		s.writeDomainError(w, r, "read route link", notALink)
		return
	}
	if geocoding.IsShortLink(text) {
		if s.expander == nil {
			s.writeDomainError(w, r, "read route link", notALink)
			return
		}
		expanded, err := s.expander.Expand(r.Context(), text)
		if errors.Is(err, geocoding.ErrUnavailable) {
			s.writeError(w, r, http.StatusServiceUnavailable, "link_unreachable", "The short link could not be opened")
			return
		}
		if err != nil {
			s.writeDomainError(w, r, "read route link", notALink)
			return
		}
		text = expanded
	}
	route, err := googlelink.Parse(text)
	switch {
	case errors.Is(err, googlelink.ErrNoPoints):
		s.writeDomainError(w, r, "read route link",
			domain.NewValidationError("url", "no_points", "the link names no positions"))
		return
	case err != nil:
		s.writeDomainError(w, r, "read route link", notALink)
		return
	}
	from, to, err := s.legEnds(r.Context(), leg)
	if err != nil {
		s.writeDomainError(w, r, "read leg ends", err)
		return
	}
	via := linkVia(route, from, to)
	if len(via) > domain.MaxLegVia {
		s.writeDomainError(w, r, "read route link",
			domain.NewValidationError("via", "too_many", "a leg passes through at most 25 points"))
		return
	}
	writeJSON(w, s.logger, http.StatusOK, viaListResponse{Via: newPointBodies(via)})
}
