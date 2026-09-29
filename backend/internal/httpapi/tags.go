package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TagStore is the persistence of a person's own tags and of the trips they are
// put on.
type TagStore interface {
	List(ctx context.Context, userID uuid.UUID) ([]domain.Tag, error)
	Create(ctx context.Context, tag domain.Tag) (domain.Tag, error)
	Update(ctx context.Context, userID, tagID uuid.UUID, name string, color domain.TagColor) (domain.Tag, error)
	Delete(ctx context.Context, userID, tagID uuid.UUID) error
	SetTripTags(ctx context.Context, userID, tripID uuid.UUID, tagIDs []uuid.UUID) error
}

// tagResponse is a tag as the list of one's tags shows it.
type tagResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	TripCount int       `json:"trip_count"`
	IdeaCount int       `json:"idea_count"`
	CreatedAt time.Time `json:"created_at"`
}

// tripTagResponse is a tag as a trip wears it.
type tripTagResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// newTagResponse maps a tag onto the wire.
func newTagResponse(tag domain.Tag) tagResponse {
	return tagResponse{ID: tag.ID.String(), Name: tag.Name, Color: string(tag.Color), TripCount: tag.TripCount,
		IdeaCount: tag.IdeaCount, CreatedAt: tag.CreatedAt}
}

// wireTripTags renders the tags a trip wears as a list, never null.
func wireTripTags(tags []domain.TripTag) []tripTagResponse {
	items := make([]tripTagResponse, 0, len(tags))
	for _, tag := range tags {
		items = append(items, tripTagResponse{ID: tag.ID.String(), Name: tag.Name, Color: string(tag.Color)})
	}
	return items
}

// createTagRequest is the body of POST /api/v1/tags. Without a colour the tag
// takes the one the reader's tags use least.
type createTagRequest struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// updateTagRequest is the body of PATCH /api/v1/tags/{tagID}; an omitted field
// is left as it is.
type updateTagRequest struct {
	Name  optional[string] `json:"name"`
	Color optional[string] `json:"color"`
}

// setTripTagsRequest is the body of PUT /api/v1/trips/{tripID}/tags.
type setTripTagsRequest struct {
	TagIDs []string `json:"tag_ids"`
}

// handleListTags lists the reader's own tags by name.
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.tags.List(r.Context(), principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "list tags", err)
		return
	}
	items := make([]tagResponse, 0, len(tags))
	for _, tag := range tags {
		items = append(items, newTagResponse(tag))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[tagResponse]{Items: items})
}

// handleCreateTag adds a tag to the reader's list. A name the reader already
// uses, in any case, answers 409 already_exists.
func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	var body createTagRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	name, err := domain.NormalizeTagName(body.Name)
	if err == nil && body.Color != "" {
		err = domain.ValidateTagColor(domain.TagColor(body.Color))
	}
	if err != nil {
		s.writeDomainError(w, r, "validate tag", err)
		return
	}
	tag, err := s.tags.Create(r.Context(), domain.Tag{
		ID: uuid.Must(uuid.NewV7()), UserID: principalFrom(r.Context()).user.ID, Name: name,
		Color: domain.TagColor(body.Color),
	})
	if err != nil {
		s.writeDomainError(w, r, "create tag", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newTagResponse(tag))
}

// handleUpdateTag renames one of the reader's tags or changes its colour;
// every trip wearing it shows the change.
func (s *Server) handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	tagID, ok := s.pathUUID(w, r, "tagID")
	if !ok {
		return
	}
	var body updateTagRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	name, color, err := tagChanges(body)
	if err != nil {
		s.writeDomainError(w, r, "validate tag", err)
		return
	}
	tag, err := s.tags.Update(r.Context(), principalFrom(r.Context()).user.ID, tagID, name, color)
	if err != nil {
		s.writeDomainError(w, r, "update tag", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newTagResponse(tag))
}

// handleDeleteTag removes one of the reader's tags, and so takes it off every
// trip it was put on.
func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	tagID, ok := s.pathUUID(w, r, "tagID")
	if !ok {
		return
	}
	if err := s.tags.Delete(r.Context(), principalFrom(r.Context()).user.ID, tagID); err != nil {
		s.writeDomainError(w, r, "delete tag", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetTripTags replaces the reader's tags on a trip. Anybody who can open
// the trip may tag it, a viewer included: the tags are theirs, not the trip's.
func (s *Server) handleSetTripTags(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	var body setTripTagsRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tagIDs, err := parseTagIDs(body.TagIDs)
	if err != nil {
		s.writeDomainError(w, r, "validate trip tags", err)
		return
	}
	userID := principalFrom(r.Context()).user.ID
	if err := s.tags.SetTripTags(r.Context(), userID, trip.ID, tagIDs); err != nil {
		s.writeDomainError(w, r, "set trip tags", err)
		return
	}
	updated, err := s.trips.Get(r.Context(), trip.ID, userID)
	if err != nil {
		s.writeDomainError(w, r, "get trip", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newTripResponse(updated, s.now()))
}

// tagChanges validates a change to a tag. A field sent null or empty is
// refused rather than read as "keep": a tag always has a name and a colour.
//
// Arguments:
//   - body: the change as sent.
//
// Returns:
//   - the normalised name and the colour, each empty when not changed.
//   - the first *domain.ValidationError found.
func tagChanges(body updateTagRequest) (string, domain.TagColor, error) {
	var name string
	var color domain.TagColor
	if body.Name.Set {
		normalized, err := domain.NormalizeTagName(body.Name.Value)
		if err != nil {
			return "", "", err
		}
		name = normalized
	}
	if body.Color.Set {
		color = domain.TagColor(body.Color.Value)
		if err := domain.ValidateTagColor(color); err != nil {
			return "", "", err
		}
	}
	return name, color, nil
}

// parseTagIDs reads the identifiers of a trip's tags, dropping repeats.
//
// Arguments:
//   - values: the identifiers as sent.
//
// Returns:
//   - the distinct identifiers in the order first given.
//   - a *domain.ValidationError on tag_ids for a malformed identifier or too
//     many tags.
func parseTagIDs(values []string) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]bool, len(values))
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, domain.NewValidationError("tag_ids", "invalid_id", "must be tag identifiers")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > domain.MaxTripTags {
		return nil, domain.NewValidationError("tag_ids", "too_many", "a trip wears at most 100 tags")
	}
	return ids, nil
}
