package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A share link is read by people with no account, so the token is the whole
// credential. Three rules keep it out of places it would outlive its usefulness:
// it travels in the fragment of the URL, which browsers never send to a server;
// in requests it travels in a header rather than the query, which proxies log;
// and the request logger records only the method, the path and the status, never
// a header or a query - which is why adding either to that log would leak tokens.

// shareTokenHeader carries the token of a share link.
const shareTokenHeader = "X-Share-Token"

// shareKey is where requireShareToken puts what the token opens.
const shareKey contextKey = "share"

// shareFrom returns the access placed on the request by requireShareToken.
// Handlers behind that middleware can rely on it being present.
func shareFrom(ctx context.Context) domain.ShareAccess {
	access, _ := ctx.Value(shareKey).(domain.ShareAccess)
	return access
}

// shareLinkResponse is a link as its owner sees it afterwards: everything but
// the token, which cannot be shown again.
type shareLinkResponse struct {
	ID                  string     `json:"id"`
	Label               string     `json:"label"`
	IncludePrivateMedia bool       `json:"include_private_media"`
	ExpiresAt           *time.Time `json:"expires_at"`
	LastUsedAt          *time.Time `json:"last_used_at"`
	UseCount            int64      `json:"use_count"`
	CreatedAt           time.Time  `json:"created_at"`
}

// createdShareLinkResponse adds the token, shown once. The interface builds the
// address around it from its own origin, which the server does not know when it
// sits behind a proxy.
type createdShareLinkResponse struct {
	shareLinkResponse
	Token string `json:"token"`
}

// newShareLinkResponse maps a link onto the wire.
func newShareLinkResponse(link domain.ShareLink) shareLinkResponse {
	return shareLinkResponse{
		ID:                  link.ID.String(),
		Label:               link.Label,
		IncludePrivateMedia: link.IncludePrivateMedia,
		ExpiresAt:           link.ExpiresAt,
		LastUsedAt:          link.LastUsedAt,
		UseCount:            link.UseCount,
		CreatedAt:           link.CreatedAt,
	}
}

// handleListShareLinks returns a trip's live links, without their tokens.
func (s *Server) handleListShareLinks(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageShareLinks)
	if !ok {
		return
	}
	links, err := s.trips.ShareLinks(r.Context(), trip.ID)
	if err != nil {
		s.writeDomainError(w, r, "list share links", err)
		return
	}
	items := make([]shareLinkResponse, 0, len(links))
	for _, link := range links {
		items = append(items, newShareLinkResponse(link))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[shareLinkResponse]{Items: items})
}

// createShareLinkRequest describes a new link. Every field is optional: the
// default is a link without private media and without an end. It opens the
// trip's one document, the plan or the report, so there is no scope to choose.
type createShareLinkRequest struct {
	Label               string  `json:"label"`
	IncludePrivateMedia bool    `json:"include_private_media"`
	ExpiresAt           *string `json:"expires_at"`
}

// handleCreateShareLink mints a link and answers with its token, once.
func (s *Server) handleCreateShareLink(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageShareLinks)
	if !ok {
		return
	}
	var body createShareLinkRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	// The creation time is set here and stored as given, so the answer describes
	// the row exactly without reading it back.
	link := domain.ShareLink{
		ID:                  uuid.Must(uuid.NewV7()),
		TripID:              trip.ID,
		Label:               body.Label,
		IncludePrivateMedia: body.IncludePrivateMedia,
		CreatedBy:           principalFrom(r.Context()).user.ID,
		CreatedAt:           s.now(),
	}
	if body.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			s.writeDomainError(w, r, "validate share link",
				domain.NewValidationError("expires_at", "invalid_date", "must be an RFC 3339 timestamp"))
			return
		}
		link.ExpiresAt = &expires
	}
	link, err := link.Normalize(s.now())
	if err != nil {
		s.writeDomainError(w, r, "validate share link", err)
		return
	}

	token, hash, err := auth.NewShareToken()
	if err != nil {
		s.internalError(w, r, "generate share token", err)
		return
	}
	if err := s.trips.CreateShareLink(r.Context(), link, hash); err != nil {
		s.writeDomainError(w, r, "create share link", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, createdShareLinkResponse{
		shareLinkResponse: newShareLinkResponse(link),
		Token:             token,
	})
}

// handleRevokeShareLink stops a link working, at once and for everybody.
func (s *Server) handleRevokeShareLink(w http.ResponseWriter, r *http.Request) {
	linkID, ok := s.pathUUID(w, r, "linkID")
	if !ok {
		return
	}
	// The link names its trip, so the role is checked on the trip that owns it.
	tripID, ok := s.shareLinkTrip(w, r, linkID)
	if !ok {
		return
	}
	if err := s.trips.RevokeShareLink(r.Context(), tripID, linkID, s.now()); err != nil {
		s.writeDomainError(w, r, "revoke share link", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shareLinkTrip finds the trip a link belongs to and checks the reader may
// manage its links. A link of a trip the reader cannot manage is reported as
// missing, exactly like an unknown one.
func (s *Server) shareLinkTrip(w http.ResponseWriter, r *http.Request, linkID uuid.UUID) (uuid.UUID, bool) {
	tripID, err := s.trips.ShareLinkTrip(r.Context(), linkID)
	if err != nil {
		s.writeDomainError(w, r, "find share link", err)
		return uuid.Nil, false
	}
	if _, ok := s.tripAccess(w, r, tripID, domain.ActionManageShareLinks); !ok {
		return uuid.Nil, false
	}
	return tripID, true
}

// requireShareToken resolves the X-Share-Token header and places what it opens
// on the request. A missing, unknown, revoked or expired token is refused the
// same way: there is nothing to read and nothing to tell apart.
func (s *Server) requireShareToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get(shareTokenHeader))
		if token == "" {
			s.writeError(w, r, http.StatusUnauthorized, "invalid_share_token", "This link does not work")
			return
		}
		access, err := s.trips.ShareAccess(r.Context(), auth.HashShareToken(token), s.now())
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				s.writeError(w, r, http.StatusUnauthorized, "invalid_share_token", "This link does not work")
				return
			}
			s.internalError(w, r, "resolve share token", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), shareKey, access)))
	})
}

// sharedTripResponse is the trip as a link shows it: enough to read its
// documents, nothing about the people who have access or the account behind it.
type sharedTripResponse struct {
	Title     string  `json:"title"`
	Summary   string  `json:"summary"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Timezone  string  `json:"timezone"`
	Currency  string  `json:"currency"`
	Travelers int     `json:"travelers"`
	Status    string  `json:"status"`
	DayCount  *int    `json:"day_count"`
	// OwnerName says whose trip it is, without naming their account.
	OwnerName string `json:"owner_name"`
	// Languages and Translations are a report's, as the trip itself gives them.
	Languages    []string                `json:"languages"`
	Translations domain.TripTranslations `json:"translations"`
}

// sharedResponse is what a share token opens.
type sharedResponse struct {
	Trip sharedTripResponse `json:"trip"`
	// Label is the owner's own note on the link, such as "For my parents".
	Label string `json:"label"`
	// Kind is what the trip holds, plan or report, and so what the link opens.
	Kind                string `json:"kind"`
	IncludePrivateMedia bool   `json:"include_private_media"`
}

// handleShared returns the trip a token opens and what may be read of it.
//
// This is the request a reader's page starts with, so it is where an opening of
// the link is counted; the pictures and files read after it are not. A count
// that cannot be written costs a log line rather than the page.
func (s *Server) handleShared(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	trip := access.Trip
	if err := s.trips.RecordShareVisit(r.Context(), access.Link.ID, s.now()); err != nil {
		s.logger.Warn("record share visit failed",
			slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("error", err))
	}
	writeJSON(w, s.logger, http.StatusOK, sharedResponse{
		Trip: sharedTripResponse{
			Title:        trip.Title,
			Summary:      trip.Summary,
			StartDate:    formatDate(trip.StartDate),
			EndDate:      formatDate(trip.EndDate),
			Timezone:     trip.Timezone,
			Currency:     trip.Currency,
			Travelers:    trip.Travelers,
			Status:       string(trip.StatusAt(s.now())),
			DayCount:     trip.DayCount(),
			OwnerName:    trip.Owner.DisplayName,
			Languages:    wireLanguages(trip.Languages),
			Translations: wireTripTranslations(trip.Translations),
		},
		Label:               access.Link.Label,
		Kind:                string(trip.Kind),
		IncludePrivateMedia: access.Link.IncludePrivateMedia,
	})
}

// handleSharedDocument returns the one document a link opens, plan or report,
// read-only: a trip holds exactly one, so the link needs no say in which.
func (s *Server) handleSharedDocument(w http.ResponseWriter, r *http.Request) {
	access := shareFrom(r.Context())
	documentID := access.Trip.DocumentID()
	if documentID == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return
	}
	content, err := s.documents.Content(r.Context(), *documentID)
	if err != nil {
		s.writeDomainError(w, r, "read shared document", err)
		return
	}
	// A link shows private files only when it was created to.
	pictures, err := s.galleryOf(r.Context(), access.Trip.ID, access.Link.IncludePrivateMedia)
	if err != nil {
		s.writeDomainError(w, r, "read shared media", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newDocumentResponse(content, access.Trip, pictures))
}
