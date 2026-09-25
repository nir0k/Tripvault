package httpapi

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/track"
)

// Importing the line of an activity - a hike, a walk, a descent of a canyon.
// In a report it is the recording of what was really travelled; in a plan it is
// the route somebody intends to take, drawn in an outdoor app, and a report
// copied from the plan starts with it until a recording replaces it. A place
// has none - it is somewhere seen, not something done - and neither has a day:
// the journeys to and between them are their legs.

// handleImportItemTrack reads a GPX or KML file and makes it the line of an
// activity. The length, the climb and a thinned line are read from
// the file, and the file itself is kept for downloading.
func (s *Server) handleImportItemTrack(w http.ResponseWriter, r *http.Request) {
	item, document, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	if item.Kind != domain.ItemActivity {
		s.writeDomainError(w, r, "import track",
			domain.NewValidationError("track", "activity_only", "only an activity carries a track"))
		return
	}
	s.extendUploadDeadlines(w, r)
	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"The request must be a multipart upload with a file part")
		return
	}
	data, name, ok := s.readTrackPart(w, r, reader)
	if !ok {
		return
	}

	parsed, err := track.Parse(data)
	if err != nil {
		s.writeTrackError(w, r, err)
		return
	}
	saved, err := s.documents.SaveTrack(r.Context(), domain.Track{
		ID:           uuid.Must(uuid.NewV7()),
		DocumentID:   document.ID,
		ItemID:       item.ID,
		OriginalName: name,
		Format:       parsed.Format,
		Geometry:     parsed.Geometry,
		DistanceM:    parsed.DistanceM,
		PointCount:   parsed.PointCount,
		AscentM:      parsed.AscentM,
		DescentM:     parsed.DescentM,
		StartedAt:    parsed.StartedAt,
		EndedAt:      parsed.EndedAt,
	}, data)
	if err != nil {
		s.writeDomainError(w, r, "save track", err)
		return
	}
	if !s.fillFromTrack(w, r, item, document, saved, parsed.Start) {
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// fillFromTrack gives an activity what its line knows and it does not: the
// position the line begins at and, in a report, the times the recording
// started and ended, read in the trip's time zone. A plan has no actual times,
// and a route planned in advance carries none worth keeping. What somebody
// entered is never overwritten - the watch may have been started late, or away
// from the car park the activity is marked at. Storing the activity also puts
// the day back in the order of its times, and a new position sends its legs to
// be calculated.
//
// Arguments:
//   - item: the place or activity as it was before the import.
//   - document: the plan or report it belongs to.
//   - recorded: the track as stored.
//   - start: the first point of the recording.
//
// Returns:
//   - false when the answer has already been written with an error.
func (s *Server) fillFromTrack(w http.ResponseWriter, r *http.Request, item domain.Item,
	document domain.Document, recorded domain.Track, start domain.Point) bool {
	changed := false
	if item.Lat == nil || item.Lng == nil {
		lat, lng := start.Lat, start.Lng
		item.Lat, item.Lng = &lat, &lng
		changed = true
	}
	if document.Kind == domain.DocumentReport && (item.ActualTime == nil || item.ActualEndTime == nil) {
		trip, err := s.trips.Get(r.Context(), document.TripID, principalFrom(r.Context()).user.ID)
		if err != nil {
			s.writeDomainError(w, r, "get trip", err)
			return false
		}
		location, err := time.LoadLocation(trip.Timezone)
		if err != nil {
			location = time.UTC
		}
		if from, to := recorded.ClockPeriod(location); from != nil {
			if item.ActualTime == nil {
				item.ActualTime = from
			}
			if item.ActualEndTime == nil {
				item.ActualEndTime = to
			}
			changed = true
		}
	}
	if !changed {
		return true
	}
	if err := s.documents.UpdatePlace(r.Context(), item); err != nil {
		s.writeDomainError(w, r, "fill place from track", err)
		return false
	}
	return true
}

// handleDeleteItemTrack removes the line of an activity.
func (s *Server) handleDeleteItemTrack(w http.ResponseWriter, r *http.Request) {
	item, document, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	if err := s.documents.DeleteTrack(r.Context(), item.ID); err != nil {
		s.writeDomainError(w, r, "delete track", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// handleGetTrackFile sends the file a track was imported from to somebody who
// may read its trip.
func (s *Server) handleGetTrackFile(w http.ResponseWriter, r *http.Request) {
	recorded, ok := s.trackFor(w, r)
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, recorded.DocumentID, domain.ActionView); !ok {
		return
	}
	s.writeTrackFile(w, r, recorded)
}

// handleSharedTrackFile sends the file a track was imported from to a read-only
// link, when the track belongs to the trip the link opens. A track of another
// trip is reported as missing, exactly like an unknown one.
func (s *Server) handleSharedTrackFile(w http.ResponseWriter, r *http.Request) {
	recorded, ok := s.trackFor(w, r)
	if !ok {
		return
	}
	document, err := s.documents.Document(r.Context(), recorded.DocumentID)
	if err != nil {
		s.writeDomainError(w, r, "get document", err)
		return
	}
	if document.TripID != shareFrom(r.Context()).Trip.ID {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	s.writeTrackFile(w, r, recorded)
}

// trackFor loads the track named in the path, without its file.
func (s *Server) trackFor(w http.ResponseWriter, r *http.Request) (domain.Track, bool) {
	trackID, ok := s.pathUUID(w, r, "trackID")
	if !ok {
		return domain.Track{}, false
	}
	recorded, err := s.documents.Track(r.Context(), trackID)
	if err != nil {
		s.writeDomainError(w, r, "get track", err)
		return domain.Track{}, false
	}
	return recorded, true
}

// trackMIME is the media type each kind of track file is sent with.
var trackMIME = map[string]string{
	track.FormatGPX: "application/gpx+xml",
	track.FormatKML: "application/vnd.google-earth.kml+xml",
}

// writeTrackFile sends a track's file as a download under the name it was
// uploaded with, or a name made from the format when it had none.
func (s *Server) writeTrackFile(w http.ResponseWriter, r *http.Request, recorded domain.Track) {
	file, err := s.documents.TrackFile(r.Context(), recorded.ID)
	if err != nil {
		s.writeDomainError(w, r, "get track file", err)
		return
	}
	name := file.Name
	if strings.TrimSpace(name) == "" {
		name = "track." + file.Format
	}
	w.Header().Set("Content-Type", trackMIME[file.Format])
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}

// readTrackPart reads the file of a multipart upload, bounded by the configured
// size: a recording is a few megabytes of text, and anything far larger is not
// a track this service reads.
func (s *Server) readTrackPart(w http.ResponseWriter, r *http.Request,
	reader *multipart.Reader) ([]byte, string, bool) {
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The upload is malformed")
			return nil, "", false
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, s.trackMaxBytes+1))
		name := part.FileName()
		_ = part.Close()
		if err != nil {
			s.internalError(w, r, "read track upload", err)
			return nil, "", false
		}
		if int64(len(data)) > s.trackMaxBytes {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large",
				"The file is larger than this service accepts")
			return nil, "", false
		}
		return data, name, true
	}
	s.writeError(w, r, http.StatusBadRequest, "invalid_request", "The upload carried no file")
	return nil, "", false
}

// writeTrackError maps the refusals of the parser onto responses.
func (s *Server) writeTrackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, track.ErrUnsupported):
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_track",
			"Only GPX and KML tracks are accepted")
	case errors.Is(err, track.ErrEmpty):
		s.writeDomainError(w, r, "read track",
			domain.NewValidationError("file", "empty_track", "the file holds no usable points"))
	case errors.Is(err, track.ErrTooManyPoints):
		s.writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large",
			"The track holds more points than this service reads")
	default:
		s.writeDomainError(w, r, "read track", err)
	}
}
