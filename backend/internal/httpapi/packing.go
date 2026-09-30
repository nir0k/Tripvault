package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/pdf"
)

// A plan's trip keeps a list of what to take. Like a document, every change to
// it answers with the whole list, because a move renumbers its neighbours and a
// tick changes the counts. A report has no list.

// PackingStore is the persistence of a trip's packing list.
type PackingStore interface {
	List(ctx context.Context, tripID uuid.UUID) (domain.PackingList, error)
	Category(ctx context.Context, id uuid.UUID) (domain.PackingCategory, error)
	Item(ctx context.Context, id uuid.UUID) (domain.PackingItem, error)
	CreateCategory(ctx context.Context, category domain.PackingCategory) error
	AddSections(ctx context.Context, tripID uuid.UUID, sections []domain.PackingSection) error
	UpdateCategory(ctx context.Context, category domain.PackingCategory) error
	DeleteCategory(ctx context.Context, category domain.PackingCategory) error
	ReorderCategories(ctx context.Context, tripID uuid.UUID, order []uuid.UUID) error
	CreateItem(ctx context.Context, item domain.PackingItem) error
	UpdateItem(ctx context.Context, item domain.PackingItem) error
	MoveItem(ctx context.Context, item domain.PackingItem, categoryID *uuid.UUID, position int) error
	DeleteItem(ctx context.Context, id uuid.UUID) error
	ResetPacked(ctx context.Context, tripID uuid.UUID) error
}

// packingCategoryResponse is one heading of the list.
type packingCategoryResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Icon     string `json:"icon"`
	Position int    `json:"position"`
}

// packingItemResponse is one thing to take.
type packingItemResponse struct {
	ID         string  `json:"id"`
	CategoryID *string `json:"category_id"`
	Name       string  `json:"name"`
	Quantity   int     `json:"quantity"`
	Note       string  `json:"note"`
	Packed     bool    `json:"packed"`
	// BringerID is the member who brings the item; a read-only link, which is
	// told nothing about the people of a trip, always reads null.
	BringerID *string `json:"bringer_id"`
	Position  int     `json:"position"`
}

// packingListResponse is the whole list with its counts.
type packingListResponse struct {
	Categories []packingCategoryResponse `json:"categories"`
	Items      []packingItemResponse     `json:"items"`
	Packed     int                       `json:"packed"`
	Total      int                       `json:"total"`
}

// newPackingListResponse maps a list onto the wire; withPeople false leaves
// out who brings what.
func newPackingListResponse(list domain.PackingList, withPeople bool) packingListResponse {
	response := packingListResponse{
		Categories: make([]packingCategoryResponse, 0, len(list.Categories)),
		Items:      make([]packingItemResponse, 0, len(list.Items)),
		Packed:     list.PackedCount(),
		Total:      len(list.Items),
	}
	for _, category := range list.Categories {
		response.Categories = append(response.Categories, packingCategoryResponse{
			ID: category.ID.String(), Name: category.Name, Color: string(category.Color), Icon: string(category.Icon),
			Position: category.Position,
		})
	}
	for _, item := range list.Items {
		wire := packingItemResponse{
			ID: item.ID.String(), CategoryID: formatID(item.CategoryID), Name: item.Name, Quantity: item.Quantity,
			Note: item.Note, Packed: item.Packed, Position: item.Position,
		}
		if withPeople {
			wire.BringerID = formatID(item.BringerID)
		}
		response.Items = append(response.Items, wire)
	}
	return response
}

// packingTripFor loads the trip named in the path, checks the role and that
// the trip is a plan, which is the only kind with a list.
func (s *Server) packingTripFor(w http.ResponseWriter, r *http.Request,
	action domain.TripAction) (domain.TripSummary, bool) {
	trip, ok := s.tripFor(w, r, action)
	if !ok {
		return trip, false
	}
	return trip, s.requirePlanTrip(w, r, trip)
}

// requirePlanTrip refuses a report, which has no packing list.
func (s *Server) requirePlanTrip(w http.ResponseWriter, r *http.Request, trip domain.TripSummary) bool {
	if trip.Kind != domain.DocumentPlan {
		s.writeError(w, r, http.StatusConflict, "not_a_plan", "Only a plan keeps a packing list")
		return false
	}
	return true
}

// writePackingList answers with the trip's whole list.
func (s *Server) writePackingList(w http.ResponseWriter, r *http.Request, status int, tripID uuid.UUID,
	withPeople bool) {
	list, err := s.packing.List(r.Context(), tripID)
	if err != nil {
		s.writeDomainError(w, r, "read packing list", err)
		return
	}
	writeJSON(w, s.logger, status, newPackingListResponse(list, withPeople))
}

// checkBringer refuses a bringer who is not a member of the trip. The one the
// item already names is let through, so an item can be edited after its
// bringer's membership changed in the meantime.
func (s *Server) checkBringer(r *http.Request, tripID uuid.UUID, bringer, stored *uuid.UUID) error {
	if bringer == nil || (stored != nil && *stored == *bringer) {
		return nil
	}
	members, err := s.trips.Members(r.Context(), tripID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.User.ID == *bringer {
			return nil
		}
	}
	return domain.NewValidationError("bringer_id", "not_a_member", "must be a member of the trip")
}

// handleGetPacking returns a plan's packing list.
func (s *Server) handleGetPacking(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	s.writePackingList(w, r, http.StatusOK, trip.ID, true)
}

// handleSharedPacking returns the list of the plan a read-only link opens,
// without who brings what.
func (s *Server) handleSharedPacking(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	if !access.Opens(domain.DocumentPlan) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	s.writePackingList(w, r, http.StatusOK, access.Trip.ID, false)
}

// packingCategoryRequest is the body that creates a category. Without a
// colour it takes the next one of the palette; without an icon, "other".
type packingCategoryRequest struct {
	Name  string             `json:"name"`
	Color domain.TagColor    `json:"color"`
	Icon  domain.PackingIcon `json:"icon"`
}

// packingCategoryPatch is the body that changes a category: only the fields
// sent are changed.
type packingCategoryPatch struct {
	Name  *string             `json:"name"`
	Color *domain.TagColor    `json:"color"`
	Icon  *domain.PackingIcon `json:"icon"`
}

// handleCreatePackingCategory adds a category at the end of the list.
func (s *Server) handleCreatePackingCategory(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body packingCategoryRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	category, err := domain.PackingCategory{
		ID: uuid.Must(uuid.NewV7()), TripID: trip.ID, Name: body.Name, Color: body.Color, Icon: body.Icon,
	}.Normalize()
	if err != nil {
		s.writeDomainError(w, r, "validate packing category", err)
		return
	}
	if err := s.packing.CreateCategory(r.Context(), category); err != nil {
		s.writeDomainError(w, r, "create packing category", err)
		return
	}
	s.writePackingList(w, r, http.StatusCreated, trip.ID, true)
}

// packingAddRequest is the body that adds categories with their things at
// once, as a template of the interface does.
type packingAddRequest struct {
	Categories []packingSectionRequest `json:"categories"`
}

// packingSectionRequest is one category of an addition with its things.
type packingSectionRequest struct {
	packingCategoryRequest
	Items []packingSectionItemRequest `json:"items"`
}

// packingSectionItemRequest is one thing of an addition; nobody brings it and
// it is not packed yet.
type packingSectionItemRequest struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Note     string `json:"note"`
}

// handleAddPacking adds categories with their things in one transaction. A
// category the list has under the same name is filled rather than repeated,
// and a thing it holds already is left out.
func (s *Server) handleAddPacking(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body packingAddRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	sections := make([]domain.PackingSection, 0, len(body.Categories))
	for _, wire := range body.Categories {
		section := domain.PackingSection{Category: domain.PackingCategory{
			ID: uuid.Must(uuid.NewV7()), TripID: trip.ID, Name: wire.Name, Color: wire.Color, Icon: wire.Icon,
		}}
		for _, item := range wire.Items {
			section.Items = append(section.Items, domain.PackingItem{
				ID: uuid.Must(uuid.NewV7()), TripID: trip.ID, Name: item.Name, Quantity: item.Quantity, Note: item.Note,
			})
		}
		sections = append(sections, section)
	}
	sections, err := domain.ValidatePackingSections(sections)
	if err != nil {
		s.writeDomainError(w, r, "validate packing categories", err)
		return
	}
	if err := s.packing.AddSections(r.Context(), trip.ID, sections); err != nil {
		s.writeDomainError(w, r, "add packing categories", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, trip.ID, true)
}

// reorderRequest names every element of a list in its new order.
type reorderRequest struct {
	Order []string `json:"order"`
}

// handleReorderPackingCategories puts the categories in a new order.
func (s *Server) handleReorderPackingCategories(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body reorderRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	order := make([]uuid.UUID, 0, len(body.Order))
	for _, raw := range body.Order {
		id, err := uuid.Parse(raw)
		if err != nil {
			s.writeDomainError(w, r, "validate order",
				domain.NewValidationError("order", "invalid_id", "must list identifiers"))
			return
		}
		order = append(order, id)
	}
	if err := s.packing.ReorderCategories(r.Context(), trip.ID, order); err != nil {
		s.writeDomainError(w, r, "reorder packing categories", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, trip.ID, true)
}

// packingCategoryFor loads the category named in the path and checks the role
// on its trip.
func (s *Server) packingCategoryFor(w http.ResponseWriter, r *http.Request) (domain.PackingCategory, bool) {
	id, ok := s.pathUUID(w, r, "categoryID")
	if !ok {
		return domain.PackingCategory{}, false
	}
	category, err := s.packing.Category(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "get packing category", err)
		return domain.PackingCategory{}, false
	}
	_, ok = s.tripAccess(w, r, category.TripID, domain.ActionEdit)
	return category, ok
}

// handleUpdatePackingCategory renames a category or changes its colour or icon.
func (s *Server) handleUpdatePackingCategory(w http.ResponseWriter, r *http.Request) {
	category, ok := s.packingCategoryFor(w, r)
	if !ok {
		return
	}
	var body packingCategoryPatch
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Name != nil {
		category.Name = *body.Name
	}
	if body.Color != nil {
		category.Color = *body.Color
		// An empty colour would be read as "pick one" by Normalize; a change
		// must name a colour of the palette.
		if category.Color == "" {
			s.writeDomainError(w, r, "validate packing category", domain.ValidateTagColor(category.Color))
			return
		}
	}
	if body.Icon != nil {
		category.Icon = *body.Icon
	}
	category, err := category.Normalize()
	if err != nil {
		s.writeDomainError(w, r, "validate packing category", err)
		return
	}
	if err := s.packing.UpdateCategory(r.Context(), category); err != nil {
		s.writeDomainError(w, r, "update packing category", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, category.TripID, true)
}

// handleDeletePackingCategory removes a category with its items.
func (s *Server) handleDeletePackingCategory(w http.ResponseWriter, r *http.Request) {
	category, ok := s.packingCategoryFor(w, r)
	if !ok {
		return
	}
	if err := s.packing.DeleteCategory(r.Context(), category); err != nil {
		s.writeDomainError(w, r, "delete packing category", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, category.TripID, true)
}

// packingItemFields are the editable fields of an item. A null bringer_id
// names nobody.
type packingItemFields struct {
	Name      optional[string] `json:"name"`
	Quantity  optional[int]    `json:"quantity"`
	Note      optional[string] `json:"note"`
	Packed    optional[bool]   `json:"packed"`
	BringerID optional[string] `json:"bringer_id"`
}

// apply writes the given fields onto an item and validates the result.
func (f packingItemFields) apply(item domain.PackingItem) (domain.PackingItem, error) {
	if f.Name.Set {
		item.Name = f.Name.Value
	}
	if f.Quantity.Set {
		item.Quantity = f.Quantity.Value
		if f.Quantity.Null || item.Quantity == 0 {
			item.Quantity = 1
		}
	}
	if f.Note.Set {
		item.Note = f.Note.Value
	}
	if f.Packed.Set {
		item.Packed = f.Packed.Value
	}
	if err := applyNullableUUID("bringer_id", f.BringerID, &item.BringerID); err != nil {
		return item, err
	}
	return item.Normalize()
}

// createPackingItemRequest adds the category to the item's fields; a null or
// missing category puts the item among the ones without one.
type createPackingItemRequest struct {
	packingItemFields
	CategoryID optional[string] `json:"category_id"`
}

// handleCreatePackingItem adds an item at the end of its category.
func (s *Server) handleCreatePackingItem(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body createPackingItemRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item := domain.PackingItem{ID: uuid.Must(uuid.NewV7()), TripID: trip.ID}
	if err := applyNullableUUID("category_id", body.CategoryID, &item.CategoryID); err != nil {
		s.writeDomainError(w, r, "validate packing item", err)
		return
	}
	item, err := body.apply(item)
	if err == nil {
		err = s.checkBringer(r, trip.ID, item.BringerID, nil)
	}
	if err != nil {
		s.writeDomainError(w, r, "validate packing item", err)
		return
	}
	if err := s.packing.CreateItem(r.Context(), item); err != nil {
		s.writeDomainError(w, r, "create packing item", err)
		return
	}
	s.writePackingList(w, r, http.StatusCreated, trip.ID, true)
}

// packingItemFor loads the item named in the path and checks the role on its
// trip.
func (s *Server) packingItemFor(w http.ResponseWriter, r *http.Request) (domain.PackingItem, bool) {
	id, ok := s.pathUUID(w, r, "itemID")
	if !ok {
		return domain.PackingItem{}, false
	}
	item, err := s.packing.Item(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "get packing item", err)
		return domain.PackingItem{}, false
	}
	_, ok = s.tripAccess(w, r, item.TripID, domain.ActionEdit)
	return item, ok
}

// handleUpdatePackingItem changes an item: its name, quantity, note, tick or
// bringer.
func (s *Server) handleUpdatePackingItem(w http.ResponseWriter, r *http.Request) {
	stored, ok := s.packingItemFor(w, r)
	if !ok {
		return
	}
	var body packingItemFields
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item, err := body.apply(stored)
	if err == nil {
		err = s.checkBringer(r, stored.TripID, item.BringerID, stored.BringerID)
	}
	if err != nil {
		s.writeDomainError(w, r, "validate packing item", err)
		return
	}
	if err := s.packing.UpdateItem(r.Context(), item); err != nil {
		s.writeDomainError(w, r, "update packing item", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, stored.TripID, true)
}

// movePackingItemRequest names where an item goes: a category, or null for
// the items without one, and its position there.
type movePackingItemRequest struct {
	CategoryID *string `json:"category_id"`
	Position   int     `json:"position"`
}

// handleMovePackingItem moves an item within its category or to another.
func (s *Server) handleMovePackingItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.packingItemFor(w, r)
	if !ok {
		return
	}
	var body movePackingItemRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	var categoryID *uuid.UUID
	if body.CategoryID != nil {
		id, err := uuid.Parse(*body.CategoryID)
		if err != nil {
			s.writeDomainError(w, r, "validate move",
				domain.NewValidationError("category_id", "invalid_id", "must be an identifier"))
			return
		}
		categoryID = &id
	}
	if err := s.packing.MoveItem(r.Context(), item, categoryID, body.Position); err != nil {
		s.writeDomainError(w, r, "move packing item", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, item.TripID, true)
}

// handleDeletePackingItem removes an item.
func (s *Server) handleDeletePackingItem(w http.ResponseWriter, r *http.Request) {
	item, ok := s.packingItemFor(w, r)
	if !ok {
		return
	}
	if err := s.packing.DeleteItem(r.Context(), item.ID); err != nil {
		s.writeDomainError(w, r, "delete packing item", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, item.TripID, true)
}

// handleResetPacking takes the tick off every item, to pack again.
func (s *Server) handleResetPacking(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	if err := s.packing.ResetPacked(r.Context(), trip.ID); err != nil {
		s.writeDomainError(w, r, "reset packing list", err)
		return
	}
	s.writePackingList(w, r, http.StatusOK, trip.ID, true)
}

// handlePackingPDF returns a plan's packing list as a checklist to print, in
// the reader's language, every box empty.
func (s *Server) handlePackingPDF(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.packingTripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	members, err := s.trips.Members(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "list members", err)
		return
	}
	bringers := make(map[uuid.UUID]string, len(members))
	for _, member := range members {
		bringers[member.User.ID] = member.User.DisplayName
	}
	s.writePackingPDF(w, r, trip, bringers, principalFrom(r.Context()).user.Locale)
}

// handleSharedPackingPDF returns the checklist of the plan a read-only link
// opens, in the language it asks for and without who brings what.
func (s *Server) handleSharedPackingPDF(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	if !access.Opens(domain.DocumentPlan) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	s.writePackingPDF(w, r, access.Trip, nil, r.URL.Query().Get("lang"))
}

// writePackingPDF renders a trip's list and sends it back.
func (s *Server) writePackingPDF(w http.ResponseWriter, r *http.Request, trip domain.TripSummary,
	bringers map[uuid.UUID]string, language string) {
	list, err := s.packing.List(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "read packing list", err)
		return
	}
	var document bytes.Buffer
	if err := pdf.RenderPacking(&document, pdf.Packing{
		Trip: trip.Trip, List: list, Bringers: bringers, Language: language,
	}); err != nil {
		s.internalError(w, r, "render the packing list", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", strconv.Itoa(document.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(packingFilename(trip.Title)))
	if _, err := w.Write(document.Bytes()); err != nil {
		s.logger.Warn("packing list transfer interrupted",
			slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("error", err))
	}
}

// packingFilename names the checklist after its trip, apart from the plan's
// own document.
func packingFilename(title string) string {
	name := pdfFilename(title)
	return name[:len(name)-len(".pdf")] + "-packing.pdf"
}
