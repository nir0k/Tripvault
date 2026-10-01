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

const invitationLifetime = 7 * 24 * time.Hour

// invitationTokenRequest carries a one-time token in the body, away from request logs.
type invitationTokenRequest struct {
	Token string `json:"token"`
}

// invitationStatus maps stored timestamps onto a stable wire state.
func invitationStatus(acceptedAt, revokedAt *time.Time, expiresAt, now time.Time) string {
	switch {
	case acceptedAt != nil:
		return "accepted"
	case revokedAt != nil:
		return "revoked"
	case !now.Before(expiresAt):
		return "expired"
	default:
		return "pending"
	}
}

// userInvitationResponse is an administrator invitation without its credential.
type userInvitationResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// newUserInvitationResponse maps a stored invitation onto the wire.
func newUserInvitationResponse(item domain.UserInvitation, now time.Time) userInvitationResponse {
	return userInvitationResponse{ID: item.ID.String(), Email: item.Email, DisplayName: item.DisplayName,
		IsAdmin: item.IsAdmin, Status: invitationStatus(item.AcceptedAt, item.RevokedAt, item.ExpiresAt, now),
		ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt}
}

// createUserInvitationRequest is an account an administrator invites by email.
type createUserInvitationRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	Locale      string `json:"locale"`
}

// handleCreateUserInvitation creates an account invitation and queues its email.
func (s *Server) handleCreateUserInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.mailEnabled(r) || s.invitations == nil {
		s.writeError(w, r, http.StatusConflict, "mail_disabled", "Mail delivery is not enabled")
		return
	}
	var body createUserInvitationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	email, err := domain.NormalizeEmail(body.Email)
	if err != nil {
		s.writeDomainError(w, r, "validate user invitation", err)
		return
	}
	name, err := domain.NormalizeDisplayName(body.DisplayName)
	if err != nil {
		s.writeDomainError(w, r, "validate user invitation", err)
		return
	}
	language := strings.TrimSpace(body.Locale)
	if language == "" {
		language = principalFrom(r.Context()).user.Locale
	}
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate user invitation", err)
		return
	}
	token, tokenHash, err := auth.NewActionToken()
	if err != nil {
		s.internalError(w, r, "generate user invitation token", err)
		return
	}
	now := s.now()
	invitation := domain.UserInvitation{ID: uuid.Must(uuid.NewV7()), Email: email, DisplayName: name,
		IsAdmin: body.IsAdmin, ExpiresAt: now.Add(invitationLifetime), CreatedBy: principalFrom(r.Context()).user.ID}
	link := s.mailPublicURL + "/invite/account#token=" + token
	message := mailer.Message(email, mailer.UserInvitationMail(language, name, link))
	message.DiscardAfter = &invitation.ExpiresAt
	if err := s.invitations.CreateUserInvitation(r.Context(), invitation, tokenHash, message); err != nil {
		s.writeDomainError(w, r, "create user invitation", err)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	s.audit(r.Context(), "invited an account", invitation.ID)
	writeJSON(w, s.logger, http.StatusCreated, newUserInvitationResponse(invitation, now))
}

// handleListUserInvitations lists account invitations without their tokens.
func (s *Server) handleListUserInvitations(w http.ResponseWriter, r *http.Request) {
	items, err := s.invitations.UserInvitations(r.Context())
	if err != nil {
		s.internalError(w, r, "list user invitations", err)
		return
	}
	responses := make([]userInvitationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newUserInvitationResponse(item, s.now()))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[userInvitationResponse]{Items: responses})
}

// handleRevokeUserInvitation withdraws an unused account invitation.
func (s *Server) handleRevokeUserInvitation(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "invitationID")
	if !ok {
		return
	}
	if err := s.invitations.RevokeUserInvitation(r.Context(), id, s.now()); err != nil {
		s.writeDomainError(w, r, "revoke user invitation", err)
		return
	}
	s.audit(r.Context(), "revoked an account invitation", id)
	w.WriteHeader(http.StatusNoContent)
}

// userInvitationPreviewResponse describes the account offered to a token holder.
type userInvitationPreviewResponse struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// handlePreviewUserInvitation verifies and describes an account invitation.
func (s *Server) handlePreviewUserInvitation(w http.ResponseWriter, r *http.Request) {
	var body invitationTokenRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item, err := s.invitations.UserInvitationByToken(r.Context(), auth.HashActionToken(body.Token))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !item.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "preview user invitation", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, userInvitationPreviewResponse{Email: item.Email,
		DisplayName: item.DisplayName, IsAdmin: item.IsAdmin, ExpiresAt: item.ExpiresAt})
}

// acceptUserInvitationRequest creates the account offered by an invitation.
type acceptUserInvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
	Locale   string `json:"locale"`
}

// handleAcceptUserInvitation consumes an administrator invitation and creates the account.
func (s *Server) handleAcceptUserInvitation(w http.ResponseWriter, r *http.Request) {
	var body acceptUserInvitationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tokenHash := auth.HashActionToken(body.Token)
	invitation, err := s.invitations.UserInvitationByToken(r.Context(), tokenHash)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !invitation.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "check user invitation", err)
		return
	}
	if err := auth.CheckPasswordStrength("password", body.Password); err != nil {
		s.writeDomainError(w, r, "validate invited account", err)
		return
	}
	language := strings.TrimSpace(body.Locale)
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate invited account", err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		s.internalError(w, r, "hash invited password", err)
		return
	}
	created, err := s.invitations.AcceptUserInvitation(r.Context(), tokenHash, hash, language, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "accept user invitation", err)
		return
	}
	if administrator, err := s.users.GetByID(r.Context(), invitation.CreatedBy); err == nil {
		s.queueNotificationMail(r, administrator,
			mailer.UserInvitationAccepted(administrator.Locale, created.DisplayName))
	}
	writeJSON(w, s.logger, http.StatusCreated, newUserResponse(created))
}

// tripInvitationResponse is a trip invitation without its credential.
type tripInvitationResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// newTripInvitationResponse maps a stored trip invitation onto the wire.
func newTripInvitationResponse(item domain.TripInvitation, now time.Time) tripInvitationResponse {
	return tripInvitationResponse{ID: item.ID.String(), Email: item.Email, Role: string(item.Role),
		Status:    invitationStatus(item.AcceptedAt, item.RevokedAt, item.ExpiresAt, now),
		ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt}
}

// createTripInvitationRequest names the email, role and message language.
type createTripInvitationRequest struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Locale string `json:"locale"`
}

// handleCreateTripInvitation invites one address to a trip owned by the reader.
func (s *Server) handleCreateTripInvitation(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
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
		s.writeDomainError(w, r, "validate trip invitation", err)
		return
	}
	role := domain.TripRole(body.Role)
	if err := domain.ValidateMemberRole(role); err != nil {
		s.writeDomainError(w, r, "validate trip invitation", err)
		return
	}
	language := strings.TrimSpace(body.Locale)
	if language == "" {
		language = principalFrom(r.Context()).user.Locale
	}
	if err := domain.ValidateLocale(language); err != nil {
		s.writeDomainError(w, r, "validate trip invitation", err)
		return
	}
	token, tokenHash, err := auth.NewActionToken()
	if err != nil {
		s.internalError(w, r, "generate trip invitation token", err)
		return
	}
	now := s.now()
	inviter := principalFrom(r.Context()).user
	invitation := domain.TripInvitation{ID: uuid.Must(uuid.NewV7()), TripID: trip.ID, TripTitle: trip.Title,
		Email: email, Role: role, ExpiresAt: now.Add(invitationLifetime), CreatedBy: inviter.ID}
	link := s.mailPublicURL + "/invite/trip#token=" + token
	message := mailer.Message(email, mailer.TripInvitationMail(language, trip.Title, inviter.DisplayName, link))
	message.DiscardAfter = &invitation.ExpiresAt
	if err := s.invitations.CreateTripInvitation(r.Context(), invitation, tokenHash, message); err != nil {
		s.writeDomainError(w, r, "create trip invitation", err)
		return
	}
	if s.wakeMail != nil {
		s.wakeMail()
	}
	writeJSON(w, s.logger, http.StatusCreated, newTripInvitationResponse(invitation, now))
}

// handleListTripInvitations lists a trip owner's invitations without credentials.
func (s *Server) handleListTripInvitations(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
	items, err := s.invitations.TripInvitations(r.Context(), trip.ID)
	if err != nil {
		s.internalError(w, r, "list trip invitations", err)
		return
	}
	responses := make([]tripInvitationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newTripInvitationResponse(item, s.now()))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[tripInvitationResponse]{Items: responses})
}

// handleRevokeTripInvitation withdraws an unused invitation from its trip.
func (s *Server) handleRevokeTripInvitation(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.tripFor(w, r, domain.ActionManageMembers)
	if !ok {
		return
	}
	id, ok := s.pathUUID(w, r, "invitationID")
	if !ok {
		return
	}
	if err := s.invitations.RevokeTripInvitation(r.Context(), trip.ID, id, s.now()); err != nil {
		s.writeDomainError(w, r, "revoke trip invitation", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tripInvitationPreviewResponse describes a trip offered to a token holder.
type tripInvitationPreviewResponse struct {
	TripID    string    `json:"trip_id"`
	TripTitle string    `json:"trip_title"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handlePreviewTripInvitation verifies and describes a trip invitation.
func (s *Server) handlePreviewTripInvitation(w http.ResponseWriter, r *http.Request) {
	var body invitationTokenRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item, err := s.invitations.TripInvitationByToken(r.Context(), auth.HashActionToken(body.Token))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !item.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "preview trip invitation", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, tripInvitationPreviewResponse{TripID: item.TripID.String(),
		TripTitle: item.TripTitle, Email: item.Email, Role: string(item.Role), ExpiresAt: item.ExpiresAt})
}

// handleAcceptTripInvitation lets the signed-in account matching its email join the trip.
func (s *Server) handleAcceptTripInvitation(w http.ResponseWriter, r *http.Request) {
	var body invitationTokenRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tripID, err := s.invitations.AcceptTripInvitation(r.Context(), auth.HashActionToken(body.Token),
		principalFrom(r.Context()).user, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "accept trip invitation", err)
		return
	}
	s.notifyTripInvitationAccepted(r, tripID, principalFrom(r.Context()).user)
	writeJSON(w, s.logger, http.StatusOK, map[string]string{"trip_id": tripID.String()})
}

// registerTripInvitationRequest creates an account for a trip invitee.
type registerTripInvitationRequest struct {
	Token       string `json:"token"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Locale      string `json:"locale"`
}

// handleRegisterTripInvitation creates an account and membership from one invitation.
func (s *Server) handleRegisterTripInvitation(w http.ResponseWriter, r *http.Request) {
	var body registerTripInvitationRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	tokenHash := auth.HashActionToken(body.Token)
	invitation, err := s.invitations.TripInvitationByToken(r.Context(), tokenHash)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !invitation.Redeemable(s.now())) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.internalError(w, r, "check trip invitation", err)
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
	created, tripID, err := s.invitations.RegisterTripInvitation(r.Context(), tokenHash, user, s.now())
	if errors.Is(err, domain.ErrTokenInvalid) {
		s.writeError(w, r, http.StatusGone, "invitation_closed", "This invitation is invalid, expired, withdrawn or already used")
		return
	}
	if err != nil {
		s.writeDomainError(w, r, "register invited member", err)
		return
	}
	s.notifyTripInvitationAccepted(r, tripID, created)
	writeJSON(w, s.logger, http.StatusCreated, struct {
		User   userResponse `json:"user"`
		TripID string       `json:"trip_id"`
	}{User: newUserResponse(created), TripID: tripID.String()})
}

// notifyTripInvitationAccepted tells the trip owner that an invited person joined.
func (s *Server) notifyTripInvitationAccepted(r *http.Request, tripID uuid.UUID, accepted domain.User) {
	trip, err := s.trips.Get(r.Context(), tripID, accepted.ID)
	if err != nil {
		return
	}
	owner, err := s.users.GetByID(r.Context(), trip.OwnerID)
	if err != nil {
		return
	}
	s.queueNotificationMail(r, owner,
		mailer.TripInvitationAccepted(owner.Locale, accepted.DisplayName, trip.Title))
}
