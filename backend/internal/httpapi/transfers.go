package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A transfer is a booked journey between two towns - a flight, a train, a
// ferry - kept beside the stays rather than inside a day. Writing one answers
// with the whole document, like every other change to it, because the budget
// and the days that show it move with it.

// transferResponse is a booked journey between two places.
type transferResponse struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	FromName      string   `json:"from_name"`
	FromAddress   string   `json:"from_address"`
	FromLat       *float64 `json:"from_lat"`
	FromLng       *float64 `json:"from_lng"`
	ToName        string   `json:"to_name"`
	ToAddress     string   `json:"to_address"`
	ToLat         *float64 `json:"to_lat"`
	ToLng         *float64 `json:"to_lng"`
	DepartureDate string   `json:"departure_date"`
	DepartureTime *string  `json:"departure_time"`
	// ArrivalDate is null when the transfer arrives on the day it departs.
	ArrivalDate       *string `json:"arrival_date"`
	ArrivalTime       *string `json:"arrival_time"`
	BookingRef        string  `json:"booking_ref"`
	URL               string  `json:"url"`
	NotesMD           string  `json:"notes_md"`
	PlannedCostAmount *string `json:"planned_cost_amount"`
	CostPerPerson     bool    `json:"cost_per_person"`
	ActualCostAmount  *string `json:"actual_cost_amount"`
	// SourceTransferID is the transfer of the plan this one was copied from.
	SourceTransferID *string `json:"source_transfer_id"`
}

// newTransferResponse maps a transfer onto the wire.
func newTransferResponse(transfer domain.Transfer) transferResponse {
	return transferResponse{
		ID:                transfer.ID.String(),
		Kind:              string(transfer.Kind),
		Name:              transfer.Name,
		FromName:          transfer.FromName,
		FromAddress:       transfer.FromAddress,
		FromLat:           transfer.FromLat,
		FromLng:           transfer.FromLng,
		ToName:            transfer.ToName,
		ToAddress:         transfer.ToAddress,
		ToLat:             transfer.ToLat,
		ToLng:             transfer.ToLng,
		DepartureDate:     transfer.DepartureDate.Format(domain.DateLayout),
		DepartureTime:     formatClock(transfer.DepartureTime),
		ArrivalDate:       formatDate(transfer.ArrivalDate),
		ArrivalTime:       formatClock(transfer.ArrivalTime),
		BookingRef:        transfer.BookingRef,
		URL:               transfer.URL,
		NotesMD:           transfer.NotesMD,
		PlannedCostAmount: formatMoney(transfer.PlannedCost),
		CostPerPerson:     transfer.CostPerPerson,
		ActualCostAmount:  formatMoney(transfer.ActualCost),
		SourceTransferID:  formatID(transfer.SourceTransferID),
	}
}

// transferFields are the editable fields of a transfer, shared by creation and
// change.
type transferFields struct {
	Kind              optional[string]  `json:"kind"`
	Name              optional[string]  `json:"name"`
	FromName          optional[string]  `json:"from_name"`
	FromAddress       optional[string]  `json:"from_address"`
	FromLat           optional[float64] `json:"from_lat"`
	FromLng           optional[float64] `json:"from_lng"`
	ToName            optional[string]  `json:"to_name"`
	ToAddress         optional[string]  `json:"to_address"`
	ToLat             optional[float64] `json:"to_lat"`
	ToLng             optional[float64] `json:"to_lng"`
	DepartureDate     optional[string]  `json:"departure_date"`
	DepartureTime     optional[string]  `json:"departure_time"`
	ArrivalDate       optional[string]  `json:"arrival_date"`
	ArrivalTime       optional[string]  `json:"arrival_time"`
	BookingRef        optional[string]  `json:"booking_ref"`
	URL               optional[string]  `json:"url"`
	NotesMD           optional[string]  `json:"notes_md"`
	PlannedCostAmount optional[string]  `json:"planned_cost_amount"`
	CostPerPerson     optional[bool]    `json:"cost_per_person"`
	// ActualCostAmount is refused on a plan.
	ActualCostAmount optional[string] `json:"actual_cost_amount"`
}

// apply writes the given fields onto a transfer and validates the result.
func (f transferFields) apply(transfer domain.Transfer, creating bool,
	kind domain.DocumentKind) (domain.Transfer, error) {
	if f.Kind.Set {
		transfer.Kind = domain.TransferKind(f.Kind.Value)
	}
	for _, text := range []struct {
		change optional[string]
		target *string
	}{
		{f.Name, &transfer.Name},
		{f.FromName, &transfer.FromName},
		{f.FromAddress, &transfer.FromAddress},
		{f.ToName, &transfer.ToName},
		{f.ToAddress, &transfer.ToAddress},
		{f.BookingRef, &transfer.BookingRef},
		{f.URL, &transfer.URL},
		{f.NotesMD, &transfer.NotesMD},
	} {
		if text.change.Set {
			*text.target = text.change.Value
		}
	}
	applyNullableFloat(f.FromLat, &transfer.FromLat)
	applyNullableFloat(f.FromLng, &transfer.FromLng)
	applyNullableFloat(f.ToLat, &transfer.ToLat)
	applyNullableFloat(f.ToLng, &transfer.ToLng)
	if err := applyRequiredDate("departure_date", f.DepartureDate, &transfer.DepartureDate, creating); err != nil {
		return transfer, err
	}
	if err := applyNullableDate("arrival_date", f.ArrivalDate, &transfer.ArrivalDate); err != nil {
		return transfer, err
	}
	if err := applyNullableClock("departure_time", f.DepartureTime, &transfer.DepartureTime); err != nil {
		return transfer, err
	}
	if err := applyNullableClock("arrival_time", f.ArrivalTime, &transfer.ArrivalTime); err != nil {
		return transfer, err
	}
	if err := applyNullableMoney("planned_cost_amount", f.PlannedCostAmount, &transfer.PlannedCost); err != nil {
		return transfer, err
	}
	if f.CostPerPerson.Set {
		transfer.CostPerPerson = f.CostPerPerson.Value
	}
	if err := applyNullableMoney("actual_cost_amount", f.ActualCostAmount, &transfer.ActualCost); err != nil {
		return transfer, err
	}
	return transfer.Normalize(kind)
}

// handleCreateTransfer adds a transfer to a document.
func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	documentID, ok := s.pathUUID(w, r, "documentID")
	if !ok {
		return
	}
	document, ok := s.documentFor(w, r, documentID, domain.ActionEdit)
	if !ok {
		return
	}
	var body transferFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	transfer, err := body.apply(domain.Transfer{ID: uuid.Must(uuid.NewV7()), DocumentID: documentID}, true,
		document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate transfer", err)
		return
	}
	if err := s.documents.CreateTransfer(r.Context(), transfer); err != nil {
		s.writeDomainError(w, r, "create transfer", err)
		return
	}
	s.writeDocument(w, r, http.StatusCreated, documentID)
}

// transferFor loads the transfer named in the path and checks the role on its
// trip.
func (s *Server) transferFor(w http.ResponseWriter, r *http.Request,
	action domain.TripAction) (domain.Transfer, domain.Document, bool) {
	transferID, ok := s.pathUUID(w, r, "transferID")
	if !ok {
		return domain.Transfer{}, domain.Document{}, false
	}
	transfer, err := s.documents.Transfer(r.Context(), transferID)
	if err != nil {
		s.writeDomainError(w, r, "get transfer", err)
		return domain.Transfer{}, domain.Document{}, false
	}
	document, ok := s.documentFor(w, r, transfer.DocumentID, action)
	return transfer, document, ok
}

// handleUpdateTransfer changes a transfer.
func (s *Server) handleUpdateTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, document, ok := s.transferFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body transferFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := body.apply(transfer, false, document.Kind)
	if err != nil {
		s.writeDomainError(w, r, "validate transfer", err)
		return
	}
	if err := s.documents.UpdateTransfer(r.Context(), updated); err != nil {
		s.writeDomainError(w, r, "update transfer", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, transfer.DocumentID)
}

// handleDeleteTransfer removes a transfer.
func (s *Server) handleDeleteTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, _, ok := s.transferFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	if err := s.documents.DeleteTransfer(r.Context(), transfer.ID); err != nil {
		s.writeDomainError(w, r, "delete transfer", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, transfer.DocumentID)
}
