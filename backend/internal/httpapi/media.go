package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// Files are served only from here, never from a static path: a photograph of a
// trip is readable by the people the trip is shared with and by a read-only
// link, and a URL that skipped this layer would be readable by anyone who
// guessed it. What travels back is therefore always checked twice - the reader
// against the trip, and the file against the trip it claims to belong to.

// mediaResponse is a stored file as the clients see it. The bytes are fetched
// separately, from the two paths built out of the identifier.
type mediaResponse struct {
	ID           string     `json:"id"`
	TripID       string     `json:"trip_id"`
	OriginalName string     `json:"original_name"`
	MIME         string     `json:"mime"`
	Size         int64      `json:"size"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	TakenAt      *time.Time `json:"taken_at"`
	Lat          *float64   `json:"lat"`
	Lng          *float64   `json:"lng"`
	IsPrivate    bool       `json:"is_private"`
	// IsFavorite says, in the gallery of a day or a place, whether this is one
	// of the pictures the report shows there; in the trip's own list of files it
	// says whether the file is a favourite anywhere in the report.
	IsFavorite bool      `json:"is_favorite"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// newMediaResponse maps a stored file onto the wire.
func newMediaResponse(item domain.Media) mediaResponse {
	return mediaResponse{
		ID:           item.ID.String(),
		TripID:       item.TripID.String(),
		OriginalName: item.OriginalName,
		MIME:         item.MIME,
		Size:         item.Size,
		Width:        item.Width,
		Height:       item.Height,
		TakenAt:      item.TakenAt,
		Lat:          item.Lat,
		Lng:          item.Lng,
		IsPrivate:    item.IsPrivate,
		Status:       string(item.Status),
		CreatedAt:    item.CreatedAt,
	}
}

// shown is one file in the gallery of a target, with what that link says about
// it: whether the report shows it there.
type shown struct {
	item       domain.Media
	isFavorite bool
}

// gallery is a trip's files together with where each one is shown, so a whole
// document can carry its pictures without a query per day and per place.
type gallery struct {
	byID     map[uuid.UUID]domain.Media
	byTarget map[uuid.UUID][]shown
	// favorites are the files the report shows somewhere, which is what the
	// trip's own list of files reports for each of them.
	favorites map[uuid.UUID]struct{}
}

// of returns the files shown under one target, in the order they were linked.
func (g gallery) of(targetID uuid.UUID) []mediaResponse {
	items := make([]mediaResponse, 0, len(g.byTarget[targetID]))
	for _, link := range g.byTarget[targetID] {
		response := newMediaResponse(link.item)
		response.IsFavorite = link.isFavorite
		items = append(items, response)
	}
	return items
}

// coverID names the picture a day, a place or a trip is shown by, unless that
// picture is one this reader may not see: a read-only link without private
// files must not even learn that a private one is the cover.
func (g gallery) coverID(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	if _, visible := g.byID[*id]; !visible {
		return nil
	}
	value := id.String()
	return &value
}

// galleryOf collects a trip's files and their links. includePrivate is false
// for a read-only link that was not given private files, and those files then
// disappear from every gallery and cover in the answer.
func (s *Server) galleryOf(ctx context.Context, tripID uuid.UUID, includePrivate bool) (gallery, error) {
	result := gallery{
		byID:      map[uuid.UUID]domain.Media{},
		byTarget:  map[uuid.UUID][]shown{},
		favorites: map[uuid.UUID]struct{}{},
	}
	if s.media == nil {
		return result, nil
	}
	items, err := s.media.ListByTrip(ctx, tripID)
	if err != nil {
		return gallery{}, err
	}
	for _, item := range items {
		if !item.VisibleTo(includePrivate) {
			continue
		}
		result.byID[item.ID] = item
	}
	links, err := s.media.LinksOfTrip(ctx, tripID)
	if err != nil {
		return gallery{}, err
	}
	for _, link := range links {
		item, found := result.byID[link.MediaID]
		if !found {
			continue
		}
		result.byTarget[link.TargetID] = append(result.byTarget[link.TargetID],
			shown{item: item, isFavorite: link.IsFavorite})
		if link.IsFavorite {
			result.favorites[link.MediaID] = struct{}{}
		}
	}
	return result, nil
}

// checkCover refuses a cover that is not one of the trip's own files: a cover
// is fetched like any other file, and one that belonged to another trip would
// be unreadable for everybody who can read this one.
//
// Arguments:
//   - ctx: context bounding the lookup.
//   - tripID: the trip the day, place or trip being changed belongs to.
//   - cover: the chosen file, or nil when the cover is being taken away.
//
// Returns:
//   - a *domain.ValidationError when the file is unknown or belongs elsewhere.
func (s *Server) checkCover(ctx context.Context, tripID uuid.UUID, cover *uuid.UUID) error {
	if cover == nil || s.media == nil {
		return nil
	}
	item, err := s.media.Get(ctx, *cover)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.NewValidationError("cover_media_id", "unknown_media", "must be a file of this trip")
	}
	if err != nil {
		return err
	}
	if item.TripID != tripID {
		return domain.NewValidationError("cover_media_id", "unknown_media", "must be a file of this trip")
	}
	return nil
}

// handleListMedia returns every file of a trip, newest first.
func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	items, err := s.media.ListByTrip(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "list media", err)
		return
	}
	// The links are read as well, so the gallery of the whole trip can show
	// which files the report already shows somewhere.
	links, err := s.media.LinksOfTrip(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "list media links", err)
		return
	}
	favorites := map[uuid.UUID]struct{}{}
	for _, link := range links {
		if link.IsFavorite {
			favorites[link.MediaID] = struct{}{}
		}
	}
	responses := make([]mediaResponse, 0, len(items))
	for _, item := range items {
		response := newMediaResponse(item)
		_, response.IsFavorite = favorites[item.ID]
		responses = append(responses, response)
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[mediaResponse]{Items: responses})
}

// uploadTimeout is how long one upload may take to arrive and be answered. The
// server's own deadlines are sized for ordinary API traffic, and a photograph
// of the largest allowed size sent from a slow connection needs far longer; the
// request context still ends the work when the client goes away.
const uploadTimeout = 10 * time.Minute

// extendUploadDeadlines gives an upload request the time uploadTimeout allows,
// both to read its body and to write the answer after it.
func (s *Server) extendUploadDeadlines(w http.ResponseWriter, r *http.Request) {
	deadline := time.Now().Add(uploadTimeout)
	controller := http.NewResponseController(w)
	// A recorder in a test cannot move its deadlines, and nothing else refuses to.
	for _, err := range []error{controller.SetReadDeadline(deadline), controller.SetWriteDeadline(deadline)} {
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.logger.Warn("extend the upload deadline failed",
				"request_id", RequestIDFrom(r.Context()), "error", err)
		}
	}
}

// handleUploadMedia stores the files of a multipart request against a trip.
//
// The whole request is bounded by the configured size of a single file times
// the number of parts a client may send at once, and every part is checked
// again on its own: the limit is about what lands on the disk, not about what
// somebody claims in a header.
func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	s.extendUploadDeadlines(w, r)
	used, err := s.media.UsedBytes(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "measure media", err)
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"The request must be a multipart upload with one or more file parts")
		return
	}

	stored := make([]mediaResponse, 0, 1)
	private := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The upload is malformed")
			return
		}
		// A "private" field before the files marks everything that follows as
		// private, which is how a client uploads a private photograph in one go.
		if part.FormName() == "private" {
			value, _ := io.ReadAll(io.LimitReader(part, 16))
			private = string(value) == "true"
			_ = part.Close()
			continue
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}

		item, err := s.storePart(r, trip.ID, part, private, used)
		_ = part.Close()
		if err != nil {
			s.writeMediaError(w, r, err)
			return
		}
		used += item.Size
		stored = append(stored, newMediaResponse(item))
		s.queuePreviews(item)
	}

	if len(stored) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The upload carried no file")
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, listResponse[mediaResponse]{Items: stored})
}

// storePart reads one uploaded file, checks what it is and how much room is
// left, writes the bytes and catalogues them.
//
// The bytes are read into memory first because everything decided about a file
// - its type, its size, its checksum and what its EXIF says - is decided from
// the bytes themselves, and the size limit keeps that bounded.
func (s *Server) storePart(r *http.Request, tripID uuid.UUID, part *multipart.Part, private bool,
	used int64) (domain.Media, error) {
	// One byte over the limit is read on purpose, so a file exactly at the
	// limit is accepted and the first byte above it is noticed.
	data, err := io.ReadAll(io.LimitReader(part, s.mediaMaxBytes+1))
	if err != nil {
		return domain.Media{}, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > s.mediaMaxBytes {
		return domain.Media{}, domain.ErrMediaTooLarge
	}
	if len(data) == 0 {
		return domain.Media{}, domain.NewValidationError("file", "empty_file", "the file is empty")
	}
	if s.mediaTripQuota > 0 && used+int64(len(data)) > s.mediaTripQuota {
		return domain.Media{}, domain.ErrMediaQuota
	}

	header := data
	if len(header) > media.SniffLength {
		header = header[:media.SniffLength]
	}
	mime, extension, ok := media.DetectContentType(header)
	if !ok {
		return domain.Media{}, domain.ErrMediaUnsupported
	}

	width, height, err := media.Dimensions(data)
	if err != nil {
		return domain.Media{}, domain.ErrMediaUnsupported
	}
	meta := media.ReadMetadata(data)
	// A picture stored on its side is described by the size it is shown at, so
	// a gallery lays out its tiles without decoding anything.
	if meta.Orientation >= 5 {
		width, height = height, width
	}

	checksum := sha256.Sum256(data)
	id := uuid.Must(uuid.NewV7())
	uploader := principalFrom(r.Context()).user.ID
	item, err := domain.Media{
		ID:           id,
		TripID:       tripID,
		StorageKey:   storageKey(tripID, id, extension),
		OriginalName: part.FileName(),
		MIME:         mime,
		Size:         int64(len(data)),
		Checksum:     checksum[:],
		Width:        width,
		Height:       height,
		TakenAt:      meta.TakenAt,
		Lat:          meta.Lat,
		Lng:          meta.Lng,
		IsPrivate:    private,
		Status:       domain.MediaReady,
		UploadedBy:   &uploader,
	}.Normalize()
	if err != nil {
		return domain.Media{}, err
	}

	if _, err := s.mediaFiles.Put(r.Context(), item.StorageKey, bytes.NewReader(data)); err != nil {
		return domain.Media{}, fmt.Errorf("store upload: %w", err)
	}
	if err := s.media.Create(r.Context(), item, s.mediaTripQuota); err != nil {
		// The catalogue is what makes a file reachable, so bytes without a row
		// are rubbish and go at once.
		_ = s.mediaFiles.Delete(r.Context(), item.StorageKey)
		return domain.Media{}, err
	}
	item.CreatedAt = s.now()
	return item, nil
}

// storageKey lays the files of a trip out under its own directory, so a trip's
// media can be found on disk without the database.
func storageKey(tripID, mediaID uuid.UUID, extension string) string {
	return tripID.String() + "/" + mediaID.String() + extension
}

// handleUpdateMedia changes whether a file is private.
func (s *Server) handleUpdateMedia(w http.ResponseWriter, r *http.Request) {
	item, ok := s.mediaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	var body struct {
		IsPrivate *bool `json:"is_private"`
	}
	if !s.decodeJSON(w, r, &body) {
		return
	}
	updated, err := s.media.Update(r.Context(), item.ID, domain.MediaChanges{IsPrivate: body.IsPrivate})
	if err != nil {
		s.writeDomainError(w, r, "update media", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newMediaResponse(updated))
}

// handleDeleteMedia removes a file from the trip and from the disk.
func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	item, ok := s.mediaFor(w, r, domain.ActionEdit)
	if !ok {
		return
	}
	key, err := s.media.Delete(r.Context(), item.ID)
	if err != nil {
		s.writeDomainError(w, r, "delete media", err)
		return
	}
	if err := media.DeleteWithPreviews(r.Context(), s.mediaFiles, key); err != nil {
		// The file is unreachable either way; a leftover on disk is worth a log
		// line, not a failed request.
		s.logger.Error("delete media file failed", "error", err, "key", key)
	}
	w.WriteHeader(http.StatusNoContent)
}

// setMediaLinksRequest names the gallery of one target, in order, and may name
// which of its files the report shows. Leaving the favourites out keeps the
// ones already marked, so a reorder does not have to repeat them.
type setMediaLinksRequest struct {
	TargetType      string    `json:"target_type"`
	TargetID        string    `json:"target_id"`
	MediaIDs        []string  `json:"media_ids"`
	FavoriteMediaID *[]string `json:"favorite_media_ids"`
}

// handleSetMediaLinks makes a trip, a day or a place show exactly these files.
func (s *Server) handleSetMediaLinks(w http.ResponseWriter, r *http.Request) {
	var body setMediaLinksRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	target, err := domain.ParseMediaTarget(body.TargetType)
	if err != nil {
		s.writeDomainError(w, r, "read media link", err)
		return
	}
	targetID, err := uuid.Parse(body.TargetID)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	mediaIDs, err := parseMediaIDs(body.MediaIDs)
	if err != nil {
		s.writeDomainError(w, r, "read media link", err)
		return
	}

	tripID, err := s.media.TripOfTarget(r.Context(), target, targetID)
	if err != nil {
		s.writeDomainError(w, r, "find media target", err)
		return
	}
	if _, ok := s.tripAccess(w, r, tripID, domain.ActionEdit); !ok {
		return
	}
	// The favourites are read and checked against the gallery before anything is
	// written: the two are stored in two calls, and a request refused halfway
	// would leave the gallery changed and the favourites not.
	var favoriteIDs []uuid.UUID
	if body.FavoriteMediaID != nil {
		favoriteIDs, err = parseMediaIDs(*body.FavoriteMediaID)
		if err != nil {
			s.writeDomainError(w, r, "read favourite media", err)
			return
		}
		if target == domain.MediaTargetTrip {
			s.writeDomainError(w, r, "mark favourite media", domain.ErrMediaFavoriteTarget)
			return
		}
		if len(favoriteIDs) > domain.MediaFavoriteLimit {
			s.writeDomainError(w, r, "mark favourite media", domain.ErrMediaFavoriteLimit)
			return
		}
		shown := make(map[uuid.UUID]struct{}, len(mediaIDs))
		for _, id := range mediaIDs {
			shown[id] = struct{}{}
		}
		for _, id := range favoriteIDs {
			if _, hangs := shown[id]; !hangs {
				s.writeDomainError(w, r, "mark favourite media", domain.ErrMediaFavoriteUnlinked)
				return
			}
		}
	}

	if err := s.media.SetLinks(r.Context(), target, targetID, mediaIDs); err != nil {
		s.writeDomainError(w, r, "link media", err)
		return
	}
	if body.FavoriteMediaID != nil {
		if err := s.media.SetFavorites(r.Context(), target, targetID, favoriteIDs); err != nil {
			s.writeDomainError(w, r, "mark favourite media", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// setMediaFavoritesRequest marks files as the report's favourites, or stops
// marking them, wherever in the report they hang.
type setMediaFavoritesRequest struct {
	MediaIDs []string `json:"media_ids"`
	Favorite bool     `json:"favorite"`
}

// handleSetMediaFavorites marks a batch of files chosen in the gallery of a
// trip. The gallery names files rather than the places they are shown in, so
// the mark lands on every day and place of the report they hang on; a day or a
// place that would end up showing more than a report has room for is refused
// and nothing is changed.
func (s *Server) handleSetMediaFavorites(w http.ResponseWriter, r *http.Request) {
	var body setMediaFavoritesRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	mediaIDs, err := parseMediaIDs(body.MediaIDs)
	if err != nil {
		s.writeDomainError(w, r, "read favourite media", err)
		return
	}
	if len(mediaIDs) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Every file is checked against its own trip: a batch is not a reason to
	// take the first one's answer for the rest.
	trips := map[uuid.UUID]struct{}{}
	for _, id := range mediaIDs {
		item, err := s.media.Get(r.Context(), id)
		if err != nil {
			s.writeDomainError(w, r, "get media", err)
			return
		}
		if _, held := trips[item.TripID]; held {
			continue
		}
		if _, ok := s.tripAccess(w, r, item.TripID, domain.ActionEdit); !ok {
			return
		}
		trips[item.TripID] = struct{}{}
	}

	if err := s.media.SetFavorite(r.Context(), mediaIDs, body.Favorite); err != nil {
		s.writeDomainError(w, r, "mark favourite media", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseMediaIDs reads a list of file identifiers as the API spells them.
func parseMediaIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, raw := range values {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, domain.NewValidationError("media_ids", "invalid_id",
				"must be identifiers of this trip's files")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// handleGetMediaFile serves the file itself to somebody with an account.
func (s *Server) handleGetMediaFile(w http.ResponseWriter, r *http.Request) {
	item, ok := s.mediaFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	s.writeMediaFile(w, r, item)
}

// handleGetMediaThumbnail serves a preview to somebody with an account.
func (s *Server) handleGetMediaThumbnail(w http.ResponseWriter, r *http.Request) {
	item, ok := s.mediaFor(w, r, domain.ActionView)
	if !ok {
		return
	}
	s.writeMediaThumbnail(w, r, item)
}

// handleSharedMediaFile serves a file to a read-only link.
func (s *Server) handleSharedMediaFile(w http.ResponseWriter, r *http.Request) {
	item, ok := s.sharedMedia(w, r)
	if !ok {
		return
	}
	s.writeMediaFile(w, r, item)
}

// handleSharedMediaThumbnail serves a preview to a read-only link.
func (s *Server) handleSharedMediaThumbnail(w http.ResponseWriter, r *http.Request) {
	item, ok := s.sharedMedia(w, r)
	if !ok {
		return
	}
	s.writeMediaThumbnail(w, r, item)
}

// mediaFor loads the file named in the path and checks the reader's role on the
// trip it belongs to. A file of a trip the reader cannot open is reported as
// missing, exactly like an unknown one.
func (s *Server) mediaFor(w http.ResponseWriter, r *http.Request, action domain.TripAction) (domain.Media, bool) {
	mediaID, ok := s.pathUUID(w, r, "mediaID")
	if !ok {
		return domain.Media{}, false
	}
	item, err := s.media.Get(r.Context(), mediaID)
	if err != nil {
		s.writeDomainError(w, r, "get media", err)
		return domain.Media{}, false
	}
	if _, ok := s.tripAccess(w, r, item.TripID, action); !ok {
		return domain.Media{}, false
	}
	return item, true
}

// sharedMedia loads the file named in the path for a read-only link: it must
// belong to the trip that link opens, and a private file needs a link that was
// created to show private files.
func (s *Server) sharedMedia(w http.ResponseWriter, r *http.Request) (domain.Media, bool) {
	access := shareFrom(r.Context())
	mediaID, ok := s.pathUUID(w, r, "mediaID")
	if !ok {
		return domain.Media{}, false
	}
	item, err := s.media.Get(r.Context(), mediaID)
	if err != nil {
		s.writeDomainError(w, r, "get shared media", err)
		return domain.Media{}, false
	}
	if item.TripID != access.Trip.ID || !item.VisibleTo(access.Link.IncludePrivateMedia) {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return domain.Media{}, false
	}
	return item, true
}

// mediaCacheControl lets a browser keep a stored file or preview but ask again
// before every use. The bytes never change, so the answer is almost always an
// empty 304; asking is what keeps a picture from outliving the reader's access -
// a revoked link, a removed member, an account signed out of this browser -
// which a copy kept without asking would. Vary on the credential is no answer:
// an access token changes every few minutes and would empty the cache with it.
const mediaCacheControl = "private, no-cache"

// writeMediaFile sends the stored bytes, letting the browser cache them: the
// bytes under a key never change, so the checksum is a complete validator.
func (s *Server) writeMediaFile(w http.ResponseWriter, r *http.Request, item domain.Media) {
	tag := `"` + hex.EncodeToString(item.Checksum) + `"`
	if match := r.Header.Get("If-None-Match"); match == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	file, err := s.mediaFiles.Open(r.Context(), item.StorageKey)
	if err != nil {
		if errors.Is(err, media.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		}
		s.internalError(w, r, "open media", err)
		return
	}
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type", item.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(item.Size, 10))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", mediaCacheControl)
	// A stored file is never rendered as a page; it is shown or downloaded.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Warn("send media failed", "error", err, "media_id", item.ID.String())
	}
}

// writeMediaThumbnail serves a preview of the requested width. Previews are
// rendered in the background after an upload and kept in the store; one still
// missing is rendered here, every width at once, so the original - often
// megabytes of camera output - is decoded once rather than once per width or
// per reader. A browser then keeps what it has seen under the ETag.
func (s *Server) writeMediaThumbnail(w http.ResponseWriter, r *http.Request, item domain.Media) {
	size := media.Sizes[1]
	if raw := r.URL.Query().Get("size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || !media.HasSize(value) {
			s.writeDomainError(w, r, "read preview size",
				domain.NewValidationError("size", "invalid_size", "must be one of the offered preview widths"))
			return
		}
		size = value
	}

	// The renderer's version is part of the tag, so a browser holding a
	// preview an older renderer made fetches the new one.
	tag := `"` + hex.EncodeToString(item.Checksum) + "-" + strconv.Itoa(size) +
		"-v" + strconv.Itoa(media.RendererVersion) + `"`
	if match := r.Header.Get("If-None-Match"); match == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	preview, ok := s.storedPreview(r.Context(), item, size)
	if !ok {
		previews, err := s.previewsFor(r.Context(), item)
		switch {
		case err == nil:
			preview = previews[size]
		case errors.Is(err, media.ErrNotFound):
			s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
			return
		case errors.Is(err, media.ErrNoThumbnail):
			s.writeError(w, r, http.StatusUnsupportedMediaType, "no_preview",
				"No preview can be made of this kind of file")
			return
		case r.Context().Err() != nil:
			return
		default:
			s.internalError(w, r, "render preview", err)
			return
		}
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(preview)))
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", mediaCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(preview); err != nil {
		s.logger.Warn("send preview failed", "error", err, "media_id", item.ID.String())
	}
}

// storedPreview reads a preview rendered earlier. Anything short of a clean
// read - nothing stored yet, or a store that fails - is answered by rendering
// the preview again, so it reports only whether one was found.
func (s *Server) storedPreview(ctx context.Context, item domain.Media, size int) ([]byte, bool) {
	file, err := s.mediaFiles.Open(ctx, media.PreviewKey(item.StorageKey, size))
	if err != nil {
		if !errors.Is(err, media.ErrNotFound) {
			s.logger.Warn("open stored preview failed", "error", err, "media_id", item.ID.String())
		}
		return nil, false
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// writeMediaError maps the failures of an upload onto responses; anything else
// is handled the way every other endpoint handles it.
func (s *Server) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrMediaTooLarge):
		s.writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large",
			"The file is larger than this service accepts")
	case errors.Is(err, domain.ErrMediaQuota):
		s.writeError(w, r, http.StatusConflict, "media_quota",
			"The trip has no space left for more files")
	case errors.Is(err, domain.ErrMediaUnsupported):
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_file",
			"Only JPEG, PNG and WebP pictures are accepted")
	default:
		s.writeDomainError(w, r, "store media", err)
	}
}
