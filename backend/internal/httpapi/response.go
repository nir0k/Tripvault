// Package httpapi contains the HTTP transport layer: routing, middleware and
// the handlers that expose the service as a JSON API under /api/v1.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// maxJSONBodyBytes caps a JSON request body. Every payload the API accepts is a
// few kilobytes at most; anything larger is a mistake or an attempt to make the
// server allocate.
const maxJSONBodyBytes = 64 << 10

// ErrorResponse is the single error shape every endpoint returns. Code is
// stable and machine-readable - the client translates it - while Message is an
// English explanation for people reading the API directly.
type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	// RequestID lets a user quote a failing request when reporting a problem.
	RequestID string `json:"request_id,omitempty"`
}

// listResponse wraps every collection, leaving room for paging fields without
// changing the shape clients already read.
type listResponse[T any] struct {
	Items []T `json:"items"`
}

// pageResponse wraps one page of a paged collection. NextCursor continues the
// list and is null on the last page.
type pageResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// newPageResponse builds a page, turning an empty cursor into null.
func newPageResponse[T any](items []T, nextCursor string) pageResponse[T] {
	page := pageResponse[T]{Items: items}
	if nextCursor != "" {
		page.NextCursor = &nextCursor
	}
	return page
}

// optional is a JSON field of a partial update that tells three states apart:
// absent (Set is false), null (Set with Null) and a value. PATCH bodies need
// it wherever null means "clear".
type optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON records that the field was present and whether it was null.
func (o *optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}

// writeJSON - serialises a payload as a JSON response.
//
// Encoding failures are logged rather than returned: the status line has
// already been sent by the time encoding starts.
//
// Arguments:
//   - w: the response writer.
//   - logger: destination for encoding failures.
//   - status: HTTP status code to send.
//   - payload: value to encode; nil sends an empty body.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Error("encode response body", slog.Any("error", err))
	}
}

// writeError sends the standard error envelope carrying the request ID.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.writeErrorDetails(w, r, status, code, message, nil)
}

// writeErrorDetails sends the standard error envelope with structured details.
func (s *Server) writeErrorDetails(w http.ResponseWriter, r *http.Request, status int, code, message string,
	details map[string]any) {
	writeJSON(w, s.logger, status, ErrorResponse{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: RequestIDFrom(r.Context()),
	})
}

// internalError logs an unexpected failure with its request ID and answers
// with a generic 500, so internals never leak into a response.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, action string, err error) {
	s.logger.Error(action+" failed",
		slog.String("request_id", RequestIDFrom(r.Context())),
		slog.Any("error", err))
	s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Internal server error")
}

// writeDomainError maps the errors the domain and storage layers share onto
// responses, and treats anything else as an internal failure.
//
// Arguments:
//   - w, r: the exchange being answered.
//   - action: what was being done, for the log line of an unexpected failure.
//   - err: the error to report.
func (s *Server) writeDomainError(w http.ResponseWriter, r *http.Request, action string, err error) {
	var validation *domain.ValidationError
	var removal *domain.DaysWouldBeRemovedError
	switch {
	case errors.As(err, &removal):
		days := make([]map[string]any, 0, len(removal.Days))
		for _, day := range removal.Days {
			days = append(days, map[string]any{
				"document": day.DocumentKind, "position": day.Position, "date": formatDate(day.Date), "title": day.Title,
			})
		}
		s.writeErrorDetails(w, r, http.StatusConflict, "days_would_be_removed",
			"The change removes days with content; repeat it with confirmation", map[string]any{"days": days})
	case errors.As(err, &validation):
		s.writeErrorDetails(w, r, http.StatusUnprocessableEntity, "validation_failed", validation.Error(),
			map[string]any{"field": validation.Field, "reason": validation.Code})
	case errors.Is(err, domain.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		s.writeError(w, r, http.StatusConflict, "already_exists", "A record with these details already exists")
	case errors.Is(err, domain.ErrLastAdmin):
		s.writeError(w, r, http.StatusConflict, "last_admin", "The service must keep at least one active administrator")
	case errors.Is(err, domain.ErrForbidden):
		s.writeError(w, r, http.StatusForbidden, "forbidden", "Your role does not allow that")
	case errors.Is(err, domain.ErrAlreadyMember):
		s.writeError(w, r, http.StatusConflict, "already_member", "The person already has access to the trip")
	default:
		s.internalError(w, r, action, err)
	}
}

// decodeJSON reads a JSON request body into target, enforcing a size limit and
// rejecting unknown fields so a typo in a client is reported rather than
// silently ignored. It writes the error response itself and reports success.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		message := "The request body must be a JSON object matching the documented schema"
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytesErr):
			message = "The request body is too large"
		case errors.Is(err, io.EOF):
			message = "The request body is empty"
		}
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", message)
		return false
	}
	return true
}

// pathUUID reads a UUID path parameter, answering 404 for anything that is not
// one: a malformed identifier names nothing, exactly like an unknown one.
func (s *Server) pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return uuid.Nil, false
	}
	return id, true
}
