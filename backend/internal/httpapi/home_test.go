package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestHomeIsSetAndValidated checks a home is stored with a circle that still
// hides it, a radius out of range is refused, and no home reads as nulls.
func TestHomeIsSetAndValidated(t *testing.T) {
	s := newTestServer(domain.User{ID: uuid.New(), IsActive: true})

	empty := send(s, http.MethodGet, "/api/v1/me/home", "good", "")
	if empty.Code != http.StatusOK || empty.Body.String() != "{\"home\":null,\"zone\":null}\n" {
		t.Errorf("no home: %d %s", empty.Code, empty.Body.String())
	}

	refused := send(s, http.MethodPut, "/api/v1/me/home", "good", `{"lat":64.1466,"lng":-21.9426,"radius_m":50}`)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Errorf("a radius of 50 m: %d %s", refused.Code, refused.Body.String())
	}

	set := send(s, http.MethodPut, "/api/v1/me/home", "good", `{"lat":64.1466,"lng":-21.9426,"radius_m":500}`)
	if set.Code != http.StatusOK {
		t.Fatalf("set the home: %d %s", set.Code, set.Body.String())
	}
	var body homeResponse
	if err := json.Unmarshal(set.Body.Bytes(), &body); err != nil || body.Home == nil || body.Zone == nil {
		t.Fatalf("response %s: %v", set.Body.String(), err)
	}
	zones := domain.PrivacyZones{{Center: domain.Point{Lat: body.Zone.Lat, Lng: body.Zone.Lng}, RadiusM: body.Zone.RadiusM}}
	if body.Home.Lat != 64.1466 || body.Zone.RadiusM != 500 || !zones.Contains(domain.Point{Lat: 64.1466, Lng: -21.9426}) {
		t.Errorf("home %+v, zone %+v", body.Home, body.Zone)
	}

	if deleted := send(s, http.MethodDelete, "/api/v1/me/home", "good", ""); deleted.Code != http.StatusNoContent {
		t.Errorf("forget the home: %d %s", deleted.Code, deleted.Body.String())
	}
}
