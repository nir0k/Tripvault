package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// A person's ideas: somewhere they would like to go one day. A person's ideas
// are one list, which its owner may share: a viewer reads it, an editor also
// adds, changes and deletes its ideas. Every request reads the reader's role
// from the database, and an idea of a list not shared with the reader reads as
// missing. The ideas come whole - the reader's own and those shared with them -
// since a person keeps tens of ideas, and the interface filters them itself.

// IdeaStore is the persistence of people's lists of ideas.
type IdeaStore interface {
	List(ctx context.Context, readerID uuid.UUID) ([]domain.Idea, error)
	Get(ctx context.Context, readerID, id uuid.UUID) (domain.Idea, error)
	Lists(ctx context.Context, readerID uuid.UUID) ([]domain.IdeaList, error)
	ListRole(ctx context.Context, readerID, ownerID uuid.UUID) (domain.TripRole, error)
	Create(ctx context.Context, actorID uuid.UUID, idea domain.Idea) error
	Update(ctx context.Context, actorID uuid.UUID, idea domain.Idea) error
	Delete(ctx context.Context, actorID, id uuid.UUID) ([]string, error)
	SetTags(ctx context.Context, readerID, id uuid.UUID, tagIDs []uuid.UUID) error
	AddPhoto(ctx context.Context, actorID uuid.UUID, photo domain.IdeaPhoto) error
	DeletePhoto(ctx context.Context, actorID, ideaID, photoID uuid.UUID) (domain.IdeaPhoto, error)
	History(ctx context.Context, ownerID uuid.UUID, ideaID *uuid.UUID) ([]domain.IdeaChange, error)
	Members(ctx context.Context, ownerID uuid.UUID) ([]domain.IdeaMember, error)
	AddMember(ctx context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error)
	UpdateMember(ctx context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error)
	RemoveMember(ctx context.Context, ownerID, userID uuid.UUID) error
}

// ideaCostsBody are an idea's rough costs besides getting there, as decimal
// strings; null is a cost nobody gave.
type ideaCostsBody struct {
	Stay  *string `json:"stay"`
	Food  *string `json:"food"`
	Other *string `json:"other"`
}

// ideaPlaceBody is one place an idea goes to.
type ideaPlaceBody struct {
	Name string   `json:"name"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
}

// ideaTransportBody is one way of getting there: one way of travelling, or
// several mixed, with its cost as a decimal string and its time in minutes.
type ideaTransportBody struct {
	Modes   []string `json:"modes"`
	Cost    *string  `json:"cost"`
	Minutes *int     `json:"minutes"`
}

// ideaPhotoResponse is one picture of an idea: its identifier and the size it
// is shown at.
type ideaPhotoResponse struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ideaResponse is an idea as a member of its list reads it.
type ideaResponse struct {
	ID string `json:"id"`
	// Owner is the person whose list the idea is in, Role what the reader may
	// do with it: owner, editor or viewer.
	Owner tripUserResponse `json:"owner"`
	Role  string           `json:"role"`
	// CreatedBy and UpdatedBy are who wrote the idea and who changed it last;
	// null once their account is deleted.
	CreatedBy     *tripUserResponse   `json:"created_by"`
	UpdatedBy     *tripUserResponse   `json:"updated_by"`
	Title         string              `json:"title"`
	Countries     []string            `json:"countries"`
	Places        []ideaPlaceBody     `json:"places"`
	Months        []int               `json:"months"`
	DaysMin       *int                `json:"days_min"`
	DaysMax       *int                `json:"days_max"`
	DaysIdeal     *int                `json:"days_ideal"`
	DescriptionMD string              `json:"description_md"`
	Currency      string              `json:"currency"`
	Costs         ideaCostsBody       `json:"costs"`
	Transports    []ideaTransportBody `json:"transports"`
	// TransportMin and TransportMax are the cheapest and the dearest way of
	// getting there, CostMin and CostMax the whole trip with them; each null
	// while nothing it sums is priced.
	TransportMin *string `json:"transport_min"`
	TransportMax *string `json:"transport_max"`
	CostMin      *string `json:"cost_min"`
	CostMax      *string `json:"cost_max"`
	Visa         string  `json:"visa"`
	// Photos are the idea's pictures in their order, each served by
	// /ideas/{ideaID}/photos/{photoID}.
	Photos []ideaPhotoResponse `json:"photos"`
	// Tags are the reader's own tags on the idea.
	Tags      []tripTagResponse `json:"tags"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// optionalUser maps a person a record may name onto the wire, or null.
func optionalUser(user *domain.TripUser) *tripUserResponse {
	if user == nil {
		return nil
	}
	response := newTripUserResponse(*user)
	return &response
}

// newIdeaResponse maps an idea onto the wire, every list present even empty.
func newIdeaResponse(idea domain.Idea) ideaResponse {
	countries, months := idea.Countries, idea.Months
	if countries == nil {
		countries = []string{}
	}
	if months == nil {
		months = []int{}
	}
	places := make([]ideaPlaceBody, 0, len(idea.Places))
	for _, place := range idea.Places {
		places = append(places, ideaPlaceBody{Name: place.Name, Lat: place.Lat, Lng: place.Lng})
	}
	transports := make([]ideaTransportBody, 0, len(idea.Transports))
	for _, transport := range idea.Transports {
		modes := make([]string, 0, len(transport.Modes))
		for _, mode := range transport.Modes {
			modes = append(modes, string(mode))
		}
		transports = append(transports, ideaTransportBody{
			Modes: modes, Cost: formatMoney(transport.Cost), Minutes: transport.Minutes,
		})
	}
	photos := make([]ideaPhotoResponse, 0, len(idea.Photos))
	for _, photo := range idea.Photos {
		photos = append(photos, ideaPhotoResponse{ID: photo.ID.String(), Width: photo.Width, Height: photo.Height})
	}
	transportMin, transportMax := idea.TransportRange()
	costMin, costMax := idea.TotalRange()
	return ideaResponse{
		ID: idea.ID.String(), Owner: newTripUserResponse(idea.Owner), Role: string(idea.Role),
		CreatedBy: optionalUser(idea.CreatedBy), UpdatedBy: optionalUser(idea.UpdatedBy), Title: idea.Title, Countries: countries, Places: places, Photos: photos,
		Months: months, DaysMin: idea.DaysMin, DaysMax: idea.DaysMax, DaysIdeal: idea.DaysIdeal,
		DescriptionMD: idea.DescriptionMD, Currency: idea.Currency,
		Costs: ideaCostsBody{
			Stay: formatMoney(idea.Costs.Stay), Food: formatMoney(idea.Costs.Food), Other: formatMoney(idea.Costs.Other),
		},
		Transports: transports, TransportMin: formatMoney(transportMin), TransportMax: formatMoney(transportMax),
		CostMin: formatMoney(costMin), CostMax: formatMoney(costMax), Visa: string(idea.Visa),
		Tags: wireTripTags(idea.Tags), CreatedAt: idea.CreatedAt, UpdatedAt: idea.UpdatedAt,
	}
}

// ideaRequest is the whole of an idea, as it is created and as it is saved:
// the form edits every field at once, so a change sends them all.
type ideaRequest struct {
	Title         string              `json:"title"`
	Countries     []string            `json:"countries"`
	Places        []ideaPlaceBody     `json:"places"`
	Months        []int               `json:"months"`
	DaysMin       *int                `json:"days_min"`
	DaysMax       *int                `json:"days_max"`
	DaysIdeal     *int                `json:"days_ideal"`
	DescriptionMD string              `json:"description_md"`
	Currency      string              `json:"currency"`
	Costs         ideaCostsBody       `json:"costs"`
	Transports    []ideaTransportBody `json:"transports"`
	Visa          string              `json:"visa"`
}

// ideaFrom reads a request into a normalised idea. A currency left empty is
// the default currency of the person writing it.
//
// Arguments:
//   - body: the idea as sent.
//   - idea: the idea it goes into, with its identifier and owner set.
//   - currency: the writer's default currency.
//
// Returns:
//   - the normalised idea.
//   - the first *domain.ValidationError found.
func ideaFrom(body ideaRequest, idea domain.Idea, currency string) (domain.Idea, error) {
	idea.Title, idea.Countries, idea.Months = body.Title, body.Countries, body.Months
	idea.DaysMin, idea.DaysMax, idea.DaysIdeal = body.DaysMin, body.DaysMax, body.DaysIdeal
	idea.DescriptionMD, idea.Currency, idea.Visa = body.DescriptionMD, body.Currency, domain.VisaRequirement(body.Visa)
	if idea.Currency == "" {
		idea.Currency = currency
	}
	for _, place := range body.Places {
		idea.Places = append(idea.Places, domain.IdeaPlace{Name: place.Name, Lat: place.Lat, Lng: place.Lng})
	}
	var err error
	for _, transport := range body.Transports {
		cost, err := parseOptionalMoney("transports", transport.Cost)
		if err != nil {
			return idea, err
		}
		modes := make([]domain.TravelMode, 0, len(transport.Modes))
		for _, mode := range transport.Modes {
			modes = append(modes, domain.TravelMode(mode))
		}
		idea.Transports = append(idea.Transports, domain.IdeaTransport{Modes: modes, Cost: cost, Minutes: transport.Minutes})
	}
	costs := []struct {
		field  string
		value  *string
		target **domain.Money
	}{
		{"costs.stay", body.Costs.Stay, &idea.Costs.Stay},
		{"costs.food", body.Costs.Food, &idea.Costs.Food},
		{"costs.other", body.Costs.Other, &idea.Costs.Other},
	}
	for _, cost := range costs {
		if *cost.target, err = parseOptionalMoney(cost.field, cost.value); err != nil {
			return idea, err
		}
	}
	return idea.Normalize()
}

// writeIdea answers with an idea as the reader reads it.
func (s *Server) writeIdea(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	idea, err := s.ideas.Get(r.Context(), principalFrom(r.Context()).user.ID, id)
	if err != nil {
		s.writeDomainError(w, r, "get idea", err)
		return
	}
	writeJSON(w, s.logger, status, newIdeaResponse(idea))
}

// ideaFor loads the idea named in the path on behalf of the reader and checks
// their role allows the action: reading for any member of its list, changing
// for its owner and its editors. It writes the error response itself.
func (s *Server) ideaFor(w http.ResponseWriter, r *http.Request, action domain.TripAction) (domain.Idea, bool) {
	id, ok := s.pathUUID(w, r, "ideaID")
	if !ok {
		return domain.Idea{}, false
	}
	idea, err := s.ideas.Get(r.Context(), principalFrom(r.Context()).user.ID, id)
	if err != nil {
		s.writeDomainError(w, r, "get idea", err)
		return domain.Idea{}, false
	}
	if !idea.Role.Can(action) {
		s.writeDomainError(w, r, "check idea role", domain.ErrForbidden)
		return domain.Idea{}, false
	}
	return idea, true
}

// handleListIdeas returns every idea the reader may open - their own and
// those of the lists shared with them - the last changed first.
func (s *Server) handleListIdeas(w http.ResponseWriter, r *http.Request) {
	ideas, err := s.ideas.List(r.Context(), principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "list ideas", err)
		return
	}
	items := make([]ideaResponse, 0, len(ideas))
	for _, idea := range ideas {
		items = append(items, newIdeaResponse(idea))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[ideaResponse]{Items: items})
}

// handleGetIdea returns one idea the reader may open.
func (s *Server) handleGetIdea(w http.ResponseWriter, r *http.Request) {
	idea, ok := s.ideaFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newIdeaResponse(idea))
}

// createIdeaRequest is a new idea and the list it goes into: the reader's own
// unless owner_id names a list the reader edits.
type createIdeaRequest struct {
	ideaRequest
	OwnerID string `json:"owner_id"`
}

// handleCreateIdea adds an idea to the reader's list, or to a list they edit.
func (s *Server) handleCreateIdea(w http.ResponseWriter, r *http.Request) {
	var body createIdeaRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	user := principalFrom(r.Context()).user
	ownerID := user.ID
	if body.OwnerID != "" {
		parsed, err := uuid.Parse(body.OwnerID)
		if err != nil {
			s.writeDomainError(w, r, "validate idea",
				domain.NewValidationError("owner_id", "unknown_list", "no such list of ideas"))
			return
		}
		ownerID = parsed
	}
	role, err := s.ideas.ListRole(r.Context(), user.ID, ownerID)
	if err != nil {
		s.writeDomainError(w, r, "check idea list role", err)
		return
	}
	if !role.Can(domain.ActionEdit) {
		s.writeDomainError(w, r, "check idea list role", domain.ErrForbidden)
		return
	}
	idea, err := ideaFrom(body.ideaRequest, domain.Idea{ID: uuid.Must(uuid.NewV7()), OwnerID: ownerID},
		user.DefaultCurrency)
	if err != nil {
		s.writeDomainError(w, r, "validate idea", err)
		return
	}
	if err := s.ideas.Create(r.Context(), user.ID, idea); err != nil {
		s.writeDomainError(w, r, "create idea", err)
		return
	}
	s.writeIdea(w, r, http.StatusCreated, idea.ID)
}

// handleUpdateIdea saves every field of an idea the reader may change.
func (s *Server) handleUpdateIdea(w http.ResponseWriter, r *http.Request) {
	current, ok := s.ideaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body ideaRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	user := principalFrom(r.Context()).user
	idea, err := ideaFrom(body, domain.Idea{ID: current.ID, OwnerID: current.OwnerID}, user.DefaultCurrency)
	if err != nil {
		s.writeDomainError(w, r, "validate idea", err)
		return
	}
	if err := s.ideas.Update(r.Context(), user.ID, idea); err != nil {
		s.writeDomainError(w, r, "update idea", err)
		return
	}
	s.writeIdea(w, r, http.StatusOK, idea.ID)
}

// handleDeleteIdea removes an idea the reader may change, whoever wrote it.
func (s *Server) handleDeleteIdea(w http.ResponseWriter, r *http.Request) {
	idea, ok := s.ideaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	keys, err := s.ideas.Delete(r.Context(), principalFrom(r.Context()).user.ID, idea.ID)
	if err != nil {
		s.writeDomainError(w, r, "delete idea", err)
		return
	}
	for _, key := range keys {
		s.deleteAvatarBytes(r, key)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetIdeaTags replaces the reader's own tags on an idea they may open; a
// viewer tags too, since nobody else sees the tags.
func (s *Server) handleSetIdeaTags(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "ideaID")
	if !ok {
		return
	}
	var body setTripTagsRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tagIDs, err := parseTagIDs(body.TagIDs)
	if err != nil {
		s.writeDomainError(w, r, "validate idea tags", err)
		return
	}
	if err := s.ideas.SetTags(r.Context(), principalFrom(r.Context()).user.ID, id, tagIDs); err != nil {
		s.writeDomainError(w, r, "set idea tags", err)
		return
	}
	s.writeIdea(w, r, http.StatusOK, id)
}

// The widths an idea's photo is kept at: one to look at, one for the previews
// in the table and the grid.
const (
	ideaPhotoWidth   = 2048
	ideaPreviewWidth = 480
)

// ideaPhotoKey lays a photo of an idea out under the idea's own name.
func ideaPhotoKey(ideaID, photoID uuid.UUID, suffix string) string {
	return "ideas/" + ideaID.String() + "/" + photoID.String() + suffix + ".jpg"
}

// handleAddIdeaPhoto renders an uploaded picture into a photo of an idea the
// reader may change and its preview, both without the camera's metadata, and
// adds it at the end of the idea's photos. An idea keeps at most ten.
func (s *Server) handleAddIdeaPhoto(w http.ResponseWriter, r *http.Request) {
	idea, ok := s.ideaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	ideaID := idea.ID
	if s.mediaFiles == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "unavailable", "File storage is not configured")
		return
	}
	s.extendUploadDeadlines(w, r)
	data, ok := s.readAvatarPart(w, r)
	if !ok {
		return
	}
	picture, err := media.Thumbnail(data, ideaPhotoWidth)
	if err != nil {
		s.writeMediaError(w, r, domain.ErrMediaUnsupported)
		return
	}
	preview, err := media.Thumbnail(data, ideaPreviewWidth)
	if err != nil {
		s.writeMediaError(w, r, domain.ErrMediaUnsupported)
		return
	}
	width, height, err := media.Dimensions(picture)
	if err != nil {
		s.internalError(w, r, "measure idea photo", err)
		return
	}

	photo := domain.IdeaPhoto{ID: uuid.Must(uuid.NewV7()), IdeaID: ideaID, Width: width, Height: height}
	photo.Key, photo.ThumbKey = ideaPhotoKey(ideaID, photo.ID, ""), ideaPhotoKey(ideaID, photo.ID, "-preview")
	release, err := s.reserveStorage(r.Context(), int64(len(picture)+len(preview)))
	if err != nil {
		s.writeStorageError(w, r, "reserve idea photo storage", err)
		return
	}
	defer release()
	for key, bytesOf := range map[string][]byte{photo.Key: picture, photo.ThumbKey: preview} {
		if _, err := s.mediaFiles.Put(r.Context(), key, bytes.NewReader(bytesOf)); err != nil {
			s.deleteAvatarBytes(r, photo.Key)
			s.deleteAvatarBytes(r, photo.ThumbKey)
			s.internalError(w, r, "store idea photo", err)
			return
		}
	}
	if err := s.ideas.AddPhoto(r.Context(), principalFrom(r.Context()).user.ID, photo); err != nil {
		// The row is what makes the bytes reachable, so bytes without it go at once.
		s.deleteAvatarBytes(r, photo.Key)
		s.deleteAvatarBytes(r, photo.ThumbKey)
		s.writeDomainError(w, r, "add idea photo", err)
		return
	}
	s.writeIdea(w, r, http.StatusCreated, ideaID)
}

// ideaPhotoFor finds a photo, named in the path, of an idea the reader may open.
func (s *Server) ideaPhotoFor(w http.ResponseWriter, r *http.Request) (domain.IdeaPhoto, bool) {
	idea, ok := s.ideaFor(w, r, domain.ActionView)
	if !ok {
		return domain.IdeaPhoto{}, false
	}
	photoID, ok := s.pathUUID(w, r, "photoID")
	if !ok {
		return domain.IdeaPhoto{}, false
	}
	for _, photo := range idea.Photos {
		if photo.ID == photoID {
			return photo, true
		}
	}
	s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
	return domain.IdeaPhoto{}, false
}

// handleGetIdeaPhoto serves a photo of an idea the reader may open, or its
// preview with ?size=preview. The bytes under a photo never change, so the
// photo's identifier is its ETag; it is still checked against the reader on
// every request.
func (s *Server) handleGetIdeaPhoto(w http.ResponseWriter, r *http.Request) {
	photo, ok := s.ideaPhotoFor(w, r)
	if !ok {
		return
	}
	key := photo.Key
	if r.URL.Query().Get("size") == "preview" {
		key = photo.ThumbKey
	}
	tag := `"` + key + `"`
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	file, err := s.mediaFiles.Open(r.Context(), key)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		}
		s.internalError(w, r, "open idea photo", err)
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Warn("send idea photo failed", "error", err, "photo_id", photo.ID.String())
	}
}

// handleDeleteIdeaPhoto removes a photo of an idea the reader may change with its files.
func (s *Server) handleDeleteIdeaPhoto(w http.ResponseWriter, r *http.Request) {
	idea, ok := s.ideaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	ideaID := idea.ID
	photoID, ok := s.pathUUID(w, r, "photoID")
	if !ok {
		return
	}
	photo, err := s.ideas.DeletePhoto(r.Context(), principalFrom(r.Context()).user.ID, ideaID, photoID)
	if err != nil {
		s.writeDomainError(w, r, "delete idea photo", err)
		return
	}
	s.deleteAvatarBytes(r, photo.Key)
	s.deleteAvatarBytes(r, photo.ThumbKey)
	s.writeIdea(w, r, http.StatusOK, ideaID)
}
