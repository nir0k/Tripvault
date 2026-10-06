package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/mailer"
)

// Sharing a list of ideas. A person's ideas are one list, and only its owner
// decides who reads it and who edits it: members are added from the existing
// accounts or invited by email, like a trip's. A member may leave a list on
// their own. Each list keeps a history of who added, changed and deleted its
// ideas, which every member reads.

// ideaListResponse is a list of ideas the reader may open, with what they may do with it.
type ideaListResponse struct {
	Owner tripUserResponse `json:"owner"`
	Role  string           `json:"role"`
}

// handleListIdeaLists returns the lists of ideas the reader may open: their
// own first, then the ones shared with them.
func (s *Server) handleListIdeaLists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.ideas.Lists(r.Context(), principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "list idea lists", err)
		return
	}
	items := make([]ideaListResponse, 0, len(lists))
	for _, list := range lists {
		items = append(items, ideaListResponse{Owner: newTripUserResponse(list.Owner), Role: string(list.Role)})
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[ideaListResponse]{Items: items})
}

// handleLeaveIdeaList takes the reader off a list of ideas shared with them.
func (s *Server) handleLeaveIdeaList(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := s.pathUUID(w, r, "ownerID")
	if !ok {
		return
	}
	if err := s.ideas.RemoveMember(r.Context(), ownerID, principalFrom(r.Context()).user.ID); err != nil {
		s.writeDomainError(w, r, "leave idea list", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ideaMemberResponse is one person the reader's list of ideas is shared with.
type ideaMemberResponse struct {
	User      tripUserResponse `json:"user"`
	Role      string           `json:"role"`
	CreatedAt time.Time        `json:"created_at"`
}

// newIdeaMemberResponse maps a member of a list of ideas onto the wire.
func newIdeaMemberResponse(member domain.IdeaMember) ideaMemberResponse {
	return ideaMemberResponse{User: newTripUserResponse(member.User), Role: string(member.Role),
		CreatedAt: member.CreatedAt}
}

// handleListIdeaMembers lists the people the reader's own list of ideas is shared with.
func (s *Server) handleListIdeaMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.ideas.Members(r.Context(), principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "list idea members", err)
		return
	}
	items := make([]ideaMemberResponse, 0, len(members))
	for _, member := range members {
		items = append(items, newIdeaMemberResponse(member))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[ideaMemberResponse]{Items: items})
}

// handleAddIdeaMember shares the reader's list of ideas with an existing account.
func (s *Server) handleAddIdeaMember(w http.ResponseWriter, r *http.Request) {
	var body addMemberRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	userID, err := parseUserID(body.UserID)
	if err != nil {
		s.writeDomainError(w, r, "validate idea member", err)
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate idea member", err)
		return
	}
	owner := principalFrom(r.Context()).user
	member, err := s.ideas.AddMember(r.Context(), owner.ID, userID, role)
	if err != nil {
		s.writeDomainError(w, r, "add idea member", err)
		return
	}
	if user, err := s.users.GetByID(r.Context(), userID); err == nil {
		s.queueNotificationMail(r, user, mailer.IdeasMemberAdded(user.Locale, owner.DisplayName))
	}
	writeJSON(w, s.logger, http.StatusCreated, newIdeaMemberResponse(member))
}

// handleUpdateIdeaMember switches a member of the reader's list of ideas
// between editor and viewer.
func (s *Server) handleUpdateIdeaMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	var body updateMemberRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate idea member", err)
		return
	}
	owner := principalFrom(r.Context()).user
	member, err := s.ideas.UpdateMember(r.Context(), owner.ID, userID, role)
	if err != nil {
		s.writeDomainError(w, r, "update idea member", err)
		return
	}
	if user, err := s.users.GetByID(r.Context(), userID); err == nil {
		s.queueNotificationMail(r, user, mailer.IdeasMemberRoleChanged(user.Locale, owner.DisplayName, role))
	}
	writeJSON(w, s.logger, http.StatusOK, newIdeaMemberResponse(member))
}

// handleRemoveIdeaMember takes a member's access to the reader's list of ideas away.
func (s *Server) handleRemoveIdeaMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.pathUUID(w, r, "userID")
	if !ok {
		return
	}
	owner := principalFrom(r.Context()).user
	user, userErr := s.users.GetByID(r.Context(), userID)
	if err := s.ideas.RemoveMember(r.Context(), owner.ID, userID); err != nil {
		s.writeDomainError(w, r, "remove idea member", err)
		return
	}
	if userErr == nil {
		s.queueNotificationMail(r, user, mailer.IdeasMemberRemoved(user.Locale, owner.DisplayName))
	}
	w.WriteHeader(http.StatusNoContent)
}

// ideaChangeResponse is one entry of a list's history.
type ideaChangeResponse struct {
	ID string `json:"id"`
	// IdeaID is null once the idea is deleted; IdeaTitle is the title it had then.
	IdeaID    *string           `json:"idea_id"`
	IdeaTitle string            `json:"idea_title"`
	User      *tripUserResponse `json:"user"`
	Action    string            `json:"action"`
	CreatedAt time.Time         `json:"created_at"`
}

// handleIdeaHistory returns the latest changes of a list of ideas, the newest
// first: of one idea with ?idea_id=, otherwise of the list named by
// ?owner_id=, the reader's own when it is left out. Every member reads it.
func (s *Server) handleIdeaHistory(w http.ResponseWriter, r *http.Request) {
	reader := principalFrom(r.Context()).user
	ownerID := reader.ID
	var ideaID *uuid.UUID
	if raw := r.URL.Query().Get("idea_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			s.writeDomainError(w, r, "validate idea history",
				domain.NewValidationError("idea_id", "invalid_id", "must be an identifier"))
			return
		}
		idea, err := s.ideas.Get(r.Context(), reader.ID, id)
		if err != nil {
			s.writeDomainError(w, r, "get idea", err)
			return
		}
		ownerID, ideaID = idea.OwnerID, &id
	} else if raw := r.URL.Query().Get("owner_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			s.writeDomainError(w, r, "validate idea history",
				domain.NewValidationError("owner_id", "invalid_id", "must be an identifier"))
			return
		}
		if _, err := s.ideas.ListRole(r.Context(), reader.ID, id); err != nil {
			s.writeDomainError(w, r, "check idea list role", err)
			return
		}
		ownerID = id
	}
	changes, err := s.ideas.History(r.Context(), ownerID, ideaID)
	if err != nil {
		s.internalError(w, r, "list idea history", err)
		return
	}
	items := make([]ideaChangeResponse, 0, len(changes))
	for _, change := range changes {
		item := ideaChangeResponse{ID: change.ID.String(), IdeaTitle: change.IdeaTitle,
			User: optionalUser(change.User), Action: string(change.Action), CreatedAt: change.CreatedAt}
		if change.IdeaID != nil {
			id := change.IdeaID.String()
			item.IdeaID = &id
		}
		items = append(items, item)
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[ideaChangeResponse]{Items: items})
}

// newIdeaInvitationResponse maps a stored invitation to a list of ideas onto
// the wire, in the shape of a trip's.
func newIdeaInvitationResponse(item domain.IdeaInvitation, now time.Time) tripInvitationResponse {
	return tripInvitationResponse{ID: item.ID.String(), Email: item.Email, Role: string(item.Role),
		Status:    invitationStatus(item.AcceptedAt, item.RevokedAt, item.ExpiresAt, now),
		ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt}
}

// handleCreateIdeaInvitation invites one address to the reader's list of ideas.
func (s *Server) handleCreateIdeaInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.mailEnabled(r) || s.invitations == nil {
		s.writeError(w, r, http.StatusConflict, "mail_disabled", "Mail delivery is not enabled")
		return
	}
	var body createTripInvitationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		s.writeDomainError(w, r, "validate idea invitation", err)
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate idea invitation", err)
		return
	}
	owner := principalFrom(r.Context()).user
	language := strings.TrimSpace(body.Locale)
	if language == "" {
		language = owner.Locale
	}
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate idea invitation", err)
		return
	}
	token, tokenHash, err := auth.NewActionToken()
	if err != nil {
		s.internalError(w, r, "generate idea invitation token", err)
		return
	}
	now := s.now()
	invitation := domain.IdeaInvitation{ID: uuid.Must(uuid.NewV7()), OwnerID: owner.ID, OwnerName: owner.DisplayName,
		Email: email, Role: role, ExpiresAt: now.Add(invitationLifetime), CreatedAt: now}
	link := s.mailPublicURL + "/invite/ideas#token=" + token
	message := mailer.Message(email, mailer.IdeasInvitationMail(language, owner.DisplayName, link))
	message.DiscardAfter = &invitation.ExpiresAt
	if err := s.invitations.CreateIdeaInvitation(r.Context(), invitation, tokenHash, message); err != nil {
		s.writeDomainError(w, r, "create idea invitation", err)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	writeJSON(w, s.logger, http.StatusCreated, newIdeaInvitationResponse(invitation, now))
}

// handleListIdeaInvitations lists the invitations to the reader's list of
// ideas without their credentials.
func (s *Server) handleListIdeaInvitations(w http.ResponseWriter, r *http.Request) {
	items, err := s.invitations.IdeaInvitations(r.Context(), principalFrom(r.Context()).user.ID)
	if err != nil {
		s.internalError(w, r, "list idea invitations", err)
		return
	}
	responses := make([]tripInvitationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newIdeaInvitationResponse(item, s.now()))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[tripInvitationResponse]{Items: responses})
}

// handleRevokeIdeaInvitation withdraws an unused invitation to the reader's list of ideas.
func (s *Server) handleRevokeIdeaInvitation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "invitationID")
	if !ok {
		return
	}
	if err := s.invitations.RevokeIdeaInvitation(r.Context(), principalFrom(r.Context()).user.ID, id,
		s.now()); err != nil {
		s.writeDomainError(w, r, "revoke idea invitation", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ideaInvitationPreviewResponse describes a list of ideas offered to a token holder.
type ideaInvitationPreviewResponse struct {
	OwnerName string    `json:"owner_name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handlePreviewIdeaInvitation verifies and describes an invitation to a list of ideas.
func (s *Server) handlePreviewIdeaInvitation(w http.ResponseWriter, r *http.Request) {
	var body invitationTokenRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item, err := s.invitations.IdeaInvitationByToken(r.Context(), auth.HashActionToken(body.Token))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !item.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "preview idea invitation", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, ideaInvitationPreviewResponse{OwnerName: item.OwnerName,
		Email: item.Email, Role: string(item.Role), ExpiresAt: item.ExpiresAt})
}

// handleAcceptIdeaInvitation lets the signed-in account matching its email
// join the list of ideas.
func (s *Server) handleAcceptIdeaInvitation(w http.ResponseWriter, r *http.Request) {
	var body invitationTokenRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	user := principalFrom(r.Context()).user
	invitation, err := s.invitations.AcceptIdeaInvitation(r.Context(), auth.HashActionToken(body.Token), user, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "accept idea invitation", err)
		return
	}
	s.notifyIdeaInvitationAccepted(r, invitation.OwnerID, user)
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"owner_id": invitation.OwnerID.String()})
}

// handleRegisterIdeaInvitation creates an account and its membership of a list
// of ideas from one invitation.
func (s *Server) handleRegisterIdeaInvitation(w http.ResponseWriter, r *http.Request) {
	var body registerTripInvitationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tokenHash := auth.HashActionToken(body.Token)
	invitation, err := s.invitations.IdeaInvitationByToken(r.Context(), tokenHash)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !invitation.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "check idea invitation", err)
		return
	}
	name, err := domain.NormalizeDisplayName(body.DisplayName)
	if err != nil {
		s.writeDomainError(w, r, "validate invited member", err)
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate invited member", err)
		return
	}
	language := strings.TrimSpace(body.Locale)
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate invited member", err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.internalError(w, r, "hash invited member password", err)
		return
	}
	user := domain.User{ID: uuid.Must(uuid.NewV7()), DisplayName: name, PasswordHash: hash, IsActive: true,
		EmailNotifications: true, Locale: language, Theme: domain.ThemeAuto, Units: domain.UnitsKilometres,
		DefaultCurrency: domain.DefaultCurrency}
	created, accepted, err := s.invitations.RegisterIdeaInvitation(r.Context(), tokenHash, user, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "register invited member", err)
		return
	}
	s.notifyIdeaInvitationAccepted(r, accepted.OwnerID, created)
	writeJSON(w, s.logger, http.StatusCreated, struct {
		User    userResponse `json:"user"`
		OwnerID string       `json:"owner_id"`
	}{User: newUserResponse(created), OwnerID: accepted.OwnerID.String()})
}

// notifyIdeaInvitationAccepted tells the owner of a list of ideas that an
// invited person joined it.
func (s *Server) notifyIdeaInvitationAccepted(r *http.Request, ownerID uuid.UUID, accepted domain.User) {
	owner, err := s.users.GetByID(r.Context(), ownerID)
	if err != nil {
		return
	}
	s.queueNotificationMail(r, owner, mailer.IdeasInvitationAccepted(owner.Locale, accepted.DisplayName))
}
