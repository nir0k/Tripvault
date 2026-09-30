package httpapi

import (
	"crypto/sha256"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// The files a place or an activity carries besides its pictures: a ticket, a
// booking, a timetable. They are read by the members of the trip alone - a
// read-only link never lists them nor opens them, since a ticket carries names
// and numbers - and every one is downloaded, never shown inside the
// application.

// attachmentResponse is one file attached to a place, without its bytes.
type attachmentResponse struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"original_name"`
	Description  string    `json:"description"`
	MIME         string    `json:"mime"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
}

// newAttachmentResponses maps the attachments of one place onto the wire.
func newAttachmentResponses(attachments []domain.Attachment) []attachmentResponse {
	out := make([]attachmentResponse, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, attachmentResponse{
			ID:           attachment.ID.String(),
			OriginalName: attachment.OriginalName,
			Description:  attachment.Description,
			MIME:         attachment.MIME,
			Size:         attachment.Size,
			CreatedAt:    attachment.CreatedAt,
		})
	}
	return out
}

// handleCreateAttachment attaches one file to a place or an activity, with the
// line describing it sent as a "description" field before the file. What the
// file is is decided from its bytes against the kinds a place may carry, and
// its name has to agree; the whole document is answered, as after any other
// change of a place.
func (s *Server) handleCreateAttachment(w http.ResponseWriter, r *http.Request) {
	item, document, ok := s.placeFor(w, r)
	if !ok {
		return
	}
	s.extendUploadDeadlines(w, r)
	limitUpload(w, r, s.attachmentMaxBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request",
			"The request must be a multipart upload with a file part")
		return
	}
	fields := map[string]string{}
	data, uploaded, ok := s.readFilePart(w, r, reader, s.attachmentMaxBytes, fields)
	if !ok {
		return
	}
	description, err := domain.AttachmentDescription(fields["description"])
	if err != nil {
		s.writeDomainError(w, r, "check attachment", err)
		return
	}
	if len(data) == 0 {
		s.writeDomainError(w, r, "check attachment",
			domain.NewValidationError("file", "empty_file", "the file is empty"))
		return
	}
	name, err := domain.AttachmentName(uploaded)
	if err != nil {
		s.writeDomainError(w, r, "check attachment", err)
		return
	}
	kind, ok := media.DetectAttachment(data, name)
	if !ok {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_attachment",
			"Only text documents and pictures named after their kind can be attached")
		return
	}
	checksum := sha256.Sum256(data)
	uploader := principalFrom(r.Context()).user.ID
	err = s.documents.CreateAttachment(r.Context(), domain.Attachment{
		ID:           uuid.Must(uuid.NewV7()),
		DocumentID:   document.ID,
		ItemID:       item.ID,
		OriginalName: name,
		Description:  description,
		MIME:         kind.MIME,
		Size:         int64(len(data)),
		Checksum:     checksum[:],
		UploadedBy:   &uploader,
	}, data, s.mediaTripQuota)
	switch {
	case errors.Is(err, domain.ErrAttachmentDuplicate):
		s.writeError(w, r, http.StatusConflict, "duplicate_attachment", "The place already carries this file")
		return
	case errors.Is(err, domain.ErrMediaQuota):
		s.writeError(w, r, http.StatusConflict, "media_quota", "The trip has no space left for more files")
		return
	case err != nil:
		s.writeDomainError(w, r, "store attachment", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, document.ID)
}

// attachmentChangeRequest is the body that changes an attachment: only the
// line describing it, since the file itself is replaced by attaching another.
type attachmentChangeRequest struct {
	Description string `json:"description"`
}

// handleUpdateAttachment changes the line describing an attachment, for
// somebody who may edit the trip.
func (s *Server) handleUpdateAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, ok := s.attachmentFor(w, r)
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, attachment.DocumentID, domain.ActionEdit); !ok {
		return
	}
	var body attachmentChangeRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	description, err := domain.AttachmentDescription(body.Description)
	if err != nil {
		s.writeDomainError(w, r, "check attachment", err)
		return
	}
	if err := s.documents.SetAttachmentDescription(r.Context(), attachment.ID, description); err != nil {
		s.writeDomainError(w, r, "describe attachment", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, attachment.DocumentID)
}

// handleDeleteAttachment removes a file from its place, for somebody who may
// edit the trip.
func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, ok := s.attachmentFor(w, r)
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, attachment.DocumentID, domain.ActionEdit); !ok {
		return
	}
	if err := s.documents.DeleteAttachment(r.Context(), attachment.ID); err != nil {
		s.writeDomainError(w, r, "delete attachment", err)
		return
	}
	s.writeDocument(w, r, http.StatusOK, attachment.DocumentID)
}

// handleGetAttachmentFile sends an attached file to a member of its trip.
//
// It is always a download: the file is never rendered by the browser under the
// application's origin, whatever it is, and a sandbox policy keeps a saved web
// page opened by mistake from running as the application.
func (s *Server) handleGetAttachmentFile(w http.ResponseWriter, r *http.Request) {
	attachment, ok := s.attachmentFor(w, r)
	if !ok {
		return
	}
	if _, ok := s.documentFor(w, r, attachment.DocumentID, domain.ActionView); !ok {
		return
	}
	file, err := s.documents.AttachmentFile(r.Context(), attachment.ID)
	if err != nil {
		s.writeDomainError(w, r, "get attachment file", err)
		return
	}
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}

// attachmentFor loads the attachment named in the path, without its bytes.
func (s *Server) attachmentFor(w http.ResponseWriter, r *http.Request) (domain.Attachment, bool) {
	id, ok := s.pathUUID(w, r, "attachmentID")
	if !ok {
		return domain.Attachment{}, false
	}
	attachment, err := s.documents.Attachment(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "get attachment", err)
		return domain.Attachment{}, false
	}
	return attachment, true
}
