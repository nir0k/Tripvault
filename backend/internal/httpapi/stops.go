package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/track"
)

// The stops along the line of an activity: a café halfway up a hike, a rest
// by a lake. A stop is placed by a point, which the server moves onto the line
// and measures along it from the line's own file, so the distance and the
// slopes up to it agree with the whole line's. Every change answers with the
// whole document, as the other changes of a plan or a report do.

// stopResponse is a stop along the line of an activity.
type stopResponse struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	NoteMD string  `json:"note_md"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	// DistanceM is how far along the line from its start the stop lies.
	DistanceM int `json:"distance_m"`
	// GradesTo are the metres of the line up to the stop at each slope from -50
	// to +50 percent, which the reader times the way to it with; null without
	// heights.
	GradesTo   []int   `json:"grades_to"`
	ActualTime *string `json:"actual_time"`
	// The cost, as a place's.
	PlannedCostAmount *string             `json:"planned_cost_amount"`
	ActualCostAmount  *string             `json:"actual_cost_amount"`
	CostPerPerson     bool                `json:"cost_per_person"`
	CostCategory      string              `json:"cost_category"`
	CostNote          string              `json:"cost_note"`
	PaidBy            *string             `json:"paid_by"`
	CostSplit         string              `json:"cost_split"`
	CostShares        []costShareResponse `json:"cost_shares"`
}

// newStopResponses maps the stops of a line onto the wire, never null.
func newStopResponses(stops []domain.Stop) []stopResponse {
	response := make([]stopResponse, 0, len(stops))
	for _, stop := range stops {
		response = append(response, stopResponse{
			ID:                stop.ID.String(),
			Kind:              string(stop.Kind),
			Name:              stop.Name,
			NoteMD:            stop.NoteMD,
			Lat:               stop.Point.Lat,
			Lng:               stop.Point.Lng,
			DistanceM:         stop.DistanceM,
			GradesTo:          stop.GradesTo,
			ActualTime:        formatClock(stop.ActualTime),
			PlannedCostAmount: formatMoney(stop.PlannedCost),
			ActualCostAmount:  formatMoney(stop.ActualCost),
			CostPerPerson:     stop.CostPerPerson,
			CostCategory:      string(stop.CostCategory),
			CostNote:          stop.CostNote,
			PaidBy:            formatID(stop.PaidBy),
			CostSplit:         string(stop.CostSplit),
			CostShares:        newCostShares(stop.CostShares),
		})
	}
	return response
}

// stopFields are the fields of a stop a request may set; an absent field is
// left as it is.
type stopFields struct {
	Kind              optional[string]           `json:"kind"`
	Name              optional[string]           `json:"name"`
	NoteMD            optional[string]           `json:"note_md"`
	Lat               optional[float64]          `json:"lat"`
	Lng               optional[float64]          `json:"lng"`
	ActualTime        optional[string]           `json:"actual_time"`
	PlannedCostAmount optional[string]           `json:"planned_cost_amount"`
	ActualCostAmount  optional[string]           `json:"actual_cost_amount"`
	CostPerPerson     optional[bool]             `json:"cost_per_person"`
	CostCategory      optional[string]           `json:"cost_category"`
	CostNote          optional[string]           `json:"cost_note"`
	PaidBy            optional[string]           `json:"paid_by"`
	CostSplit         optional[string]           `json:"cost_split"`
	CostShares        optional[[]costShareInput] `json:"cost_shares"`
}

// moves says whether a change puts the stop at another point.
func (f stopFields) moves() bool {
	return f.Lat.Set || f.Lng.Set
}

// apply writes the given fields onto a stop and validates the result. The
// point is taken as given; placing it on the line is the handler's.
func (f stopFields) apply(stop domain.Stop, kind domain.DocumentKind) (domain.Stop, error) {
	if f.Kind.Set {
		stop.Kind = domain.StopKind(f.Kind.Value)
	}
	if f.Name.Set {
		stop.Name = f.Name.Value
	}
	if f.NoteMD.Set {
		stop.NoteMD = f.NoteMD.Value
	}
	if f.Lat.Set {
		stop.Point.Lat = f.Lat.Value
	}
	if f.Lng.Set {
		stop.Point.Lng = f.Lng.Value
	}
	if err := applyNullableClock("actual_time", f.ActualTime, &stop.ActualTime); err != nil {
		return stop, err
	}
	if err := applyNullableMoney("planned_cost_amount", f.PlannedCostAmount, &stop.PlannedCost); err != nil {
		return stop, err
	}
	if err := applyNullableMoney("actual_cost_amount", f.ActualCostAmount, &stop.ActualCost); err != nil {
		return stop, err
	}
	if f.CostPerPerson.Set {
		stop.CostPerPerson = f.CostPerPerson.Value
	}
	if f.CostCategory.Set {
		stop.CostCategory = domain.CostCategory(f.CostCategory.Value)
	}
	if f.CostNote.Set {
		stop.CostNote = f.CostNote.Value
	}
	// The payer and the shares are read the way a place's are.
	split := placeFields{PaidBy: f.PaidBy, CostSplit: f.CostSplit, CostShares: f.CostShares}
	cost := stop.CostItem(domain.Item{})
	if err := split.applySplit(&cost); err != nil {
		return stop, err
	}
	stop.PaidBy, stop.CostSplit, stop.CostShares = cost.PaidBy, cost.CostSplit, cost.CostShares
	return stop.Normalize(kind)
}

// touchesSplit says whether a change reaches what a split is checked against.
func (f stopFields) touchesSplit() bool {
	return f.CostSplit.Set || f.CostShares.Set || f.PaidBy.Set || f.PlannedCostAmount.Set ||
		f.ActualCostAmount.Set || f.CostPerPerson.Set
}

// handleCreateStop adds a stop to the line of an activity, at the point of the
// line nearest to the one given.
func (s *Server) handleCreateStop(w http.ResponseWriter, r *http.Request) {
	item, document, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	var body stopFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if !body.Lat.Set || !body.Lng.Set || body.Lat.Null || body.Lng.Null {
		s.writeDomainError(w, r, "validate stop",
			domain.NewValidationError("lat", "required", "a stop needs the point it lies at"))
		return
	}
	stop, err := body.apply(domain.Stop{ID: uuid.Must(uuid.NewV7()), DocumentID: document.ID, ItemID: item.ID},
		document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate stop", err)
		return
	}
	if err := s.placeStop(r.Context(), &stop); err != nil {
		s.writeStopError(w, r, err)
		return
	}
	if stop.PaidBy != nil || stop.CostSplit != domain.SplitNone {
		if err := s.checkCostSplit(r, document, stop.CostItem(item), domain.Item{}); err != nil {
			s.writeDomainError(w, r, "check cost split", err)
			return
		}
	}
	if err := s.documents.CreateStop(r.Context(), stop); err != nil {
		s.writeDomainError(w, r, "create stop", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// handleUpdateStop changes a stop; a new point is placed on the line again.
func (s *Server) handleUpdateStop(w http.ResponseWriter, r *http.Request) {
	stored, document, ok := s.stopFor(w, r)
	if !ok {
		return
	}
	var body stopFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	stop, err := body.apply(stored, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate stop", err)
		return
	}
	if body.moves() {
		if err := s.placeStop(r.Context(), &stop); err != nil {
			s.writeStopError(w, r, err)
			return
		}
	}
	if body.touchesSplit() {
		if err := s.checkCostSplit(r, document, stop.CostItem(domain.Item{}),
			stored.CostItem(domain.Item{})); err != nil {
			s.writeDomainError(w, r, "check cost split", err)
			return
		}
	}
	if err := s.documents.UpdateStop(r.Context(), stop); err != nil {
		s.writeDomainError(w, r, "update stop", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// handleDeleteStop removes a stop.
func (s *Server) handleDeleteStop(w http.ResponseWriter, r *http.Request) {
	stop, document, ok := s.stopFor(w, r)
	if !ok {
		return
	}
	if err := s.documents.DeleteStop(r.Context(), stop.ID); err != nil {
		s.writeDomainError(w, r, "delete stop", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// stopFor loads the stop named in the path and checks its trip may be edited.
func (s *Server) stopFor(w http.ResponseWriter, r *http.Request) (domain.Stop, domain.Document, bool) {
	stopID, ok := s.pathUUID(w, r, "stopID")
	if !ok {
		return domain.Stop{}, domain.Document{}, false
	}
	stop, err := s.documents.Stop(r.Context(), stopID)
	if err != nil {
		s.writeDomainError(w, r, "get stop", err)
		return domain.Stop{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, stop.DocumentID, domain.ActionEdit)
	if !ok {
		return domain.Stop{}, domain.Document{}, false
	}
	return stop, document, true
}

// errNoLine reports an activity without a line, which has nowhere to put a stop.
var errNoLine = domain.NewValidationError("item_id", "no_track", "only an activity with a track has stops")

// placeStop moves a stop onto the line of its activity, read from the file the
// line was imported from, and measures where along the line it lies.
//
// Arguments:
//   - ctx: context bounding the reads.
//   - stop: the stop, its point the one asked for; changed in place.
//
// Returns:
//   - errNoLine for an activity without a line, or the error reading the line.
func (s *Server) placeStop(ctx context.Context, stop *domain.Stop) error {
	content, err := s.documents.Content(ctx, stop.DocumentID)
	if err != nil {
		return err
	}
	line := domain.TrackOfItem(content.Tracks, stop.ItemID)
	if line == nil {
		return errNoLine
	}
	file, err := s.documents.TrackFile(ctx, line.ID)
	if err != nil {
		return err
	}
	position, err := track.Locate(file.Data, stop.Point)
	if err != nil {
		return err
	}
	stop.Point, stop.DistanceM, stop.GradesTo = position.Point, position.DistanceM, position.Grades
	return nil
}

// moveStops places the stops of an activity on its new line, read from the
// file just imported. A stop that cannot be placed keeps where it was.
//
// Arguments:
//   - ctx: context bounding the writes.
//   - stops: the activity's stops.
//   - data: the new line's file.
//
// Returns:
//   - an error if the stops cannot be stored.
func (s *Server) moveStops(ctx context.Context, stops []domain.Stop, data []byte) error {
	if len(stops) == 0 {
		return nil
	}
	moved := make([]domain.Stop, 0, len(stops))
	for _, stop := range stops {
		position, err := track.Locate(data, stop.Point)
		if err != nil {
			continue
		}
		stop.Point, stop.DistanceM, stop.GradesTo = position.Point, position.DistanceM, position.Grades
		moved = append(moved, stop)
	}
	return s.documents.MoveStops(ctx, moved)
}

// writeStopError answers a stop that cannot be placed on its line.
func (s *Server) writeStopError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errNoLine) {
		s.writeDomainError(w, r, "place stop", err)
		return
	}
	s.writeTrackError(w, r, err)
}
