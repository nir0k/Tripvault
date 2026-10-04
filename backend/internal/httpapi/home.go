package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// A person's home is read and changed apart from the rest of the profile: the
// account is also what an administrator lists, and the home must never travel
// with it. Only the owner reads it back, here.

// homeBody is a home as its owner sets it and reads it back.
type homeBody struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM int     `json:"radius_m"`
}

// zoneBody is the circle actually hidden, its centre moved off the home, shown
// to the owner so they see what a reader from outside will not.
type zoneBody struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	RadiusM int     `json:"radius_m"`
}

// homeResponse is the owner's home and the circle hiding it, or two nulls when
// they named none.
type homeResponse struct {
	Home *homeBody `json:"home"`
	Zone *zoneBody `json:"zone"`
}

// newHomeResponse maps an account's home onto the wire.
func newHomeResponse(user domain.User) homeResponse {
	if user.Home == nil {
		return homeResponse{}
	}
	return homeResponse{
		Home: &homeBody{Lat: user.Home.Home.Lat, Lng: user.Home.Home.Lng, RadiusM: user.Home.Zone.RadiusM},
		Zone: &zoneBody{Lat: user.Home.Zone.Center.Lat, Lng: user.Home.Zone.Center.Lng, RadiusM: user.Home.Zone.RadiusM},
	}
}

// handleGetHome returns the signed-in person's home.
func (s *Server) handleGetHome(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, newHomeResponse(principalFrom(r.Context()).user))
}

// handleSetHome stores the signed-in person's home. The circle is drawn anew
// only when the point or the radius changes: drawing it on every save would
// let somebody who kept the old documents lay the circles over each other and
// narrow the home down.
func (s *Server) handleSetHome(w http.ResponseWriter, r *http.Request) {
	user := principalFrom(r.Context()).user
	var body homeBody
	if !s.decodeJSON(w, r, &body) {
		return
	}
	point := domain.Point{Lat: body.Lat, Lng: body.Lng}
	if user.Home != nil && user.Home.Home == point && user.Home.Zone.RadiusM == body.RadiusM {
		writeJSON(w, s.logger, http.StatusOK, newHomeResponse(user))
		return
	}
	home, err := domain.NewHomeZone(point, body.RadiusM)
	if err != nil {
		s.writeDomainError(w, r, "validate home", err)
		return
	}
	updated, err := s.users.SetHome(r.Context(), user.ID, &home)
	if err != nil {
		s.writeDomainError(w, r, "set home", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newHomeResponse(updated))
}

// handleDeleteHome forgets the signed-in person's home; their trips are shown
// whole again from the next request on.
func (s *Server) handleDeleteHome(w http.ResponseWriter, r *http.Request) {
	if _, err := s.users.SetHome(r.Context(), principalFrom(r.Context()).user.ID, nil); err != nil {
		s.writeDomainError(w, r, "delete home", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// privacyZones reads the circles hidden from a reader of a trip from outside
// it, writing the failure itself.
//
// Arguments:
//   - w, r: the exchange being answered.
//   - tripID: the trip being read.
//
// Returns:
//   - the circles, and false when they could not be read.
func (s *Server) privacyZones(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) (domain.PrivacyZones, bool) {
	zones, err := s.users.PrivacyZones(r.Context(), tripID)
	if err != nil {
		s.internalError(w, r, "list privacy zones", err)
		return nil, false
	}
	return zones, true
}
