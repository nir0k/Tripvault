package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// sendShared sends a request authenticated by a share token rather than an
// account, the way a browser opening a link does.
func sendShared(s *Server, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		request.Header.Set(shareTokenHeader, token)
	}
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// createLink creates a share link on the fake trip and returns the answer.
func createLink(t *testing.T, s *Server, tripID, body string) createdShareLinkResponse {
	t.Helper()
	recorder := send(s, http.MethodPost, "/api/v1/trips/"+tripID+"/share-links", "good", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create a link: %d %s", recorder.Code, recorder.Body.String())
	}
	var created createdShareLinkResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("response %q is not a link: %v", recorder.Body.String(), err)
	}
	return created
}

// TestShareLinkRoles checks only the owner creates, lists and revokes links:
// editing a trip does not decide who may see it.
func TestShareLinkRoles(t *testing.T) {
	for _, role := range []domain.TripRole{domain.RoleViewer, domain.RoleEditor, domain.RoleOwner} {
		s, trips := newTripServer(role)
		base := "/api/v1/trips/" + trips.trip.ID.String() + "/share-links"

		want := http.StatusForbidden
		if role == domain.RoleOwner {
			want = http.StatusOK
		}
		if recorder := send(s, http.MethodGet, base, "good", ""); recorder.Code != want {
			t.Errorf("%s lists links: %d %s", role, recorder.Code, recorder.Body.String())
		}
		if role == domain.RoleOwner {
			want = http.StatusCreated
		}
		if recorder := send(s, http.MethodPost, base, "good", `{}`); recorder.Code != want {
			t.Errorf("%s creates a link: %d %s", role, recorder.Code, recorder.Body.String())
		}
		if role != domain.RoleOwner && len(trips.shares) != 0 {
			t.Errorf("%s created a link", role)
		}
	}
}

// TestShareLinkLifecycle checks the token is shown once, the link is listed
// without it, and revoking stops it working at once.
func TestShareLinkLifecycle(t *testing.T) {
	s, trips := newTripServer(domain.RoleOwner)
	tripID := trips.trip.ID.String()

	created := createLink(t, s, tripID, `{"label":"For my parents"}`)
	if created.Token == "" || created.Label != "For my parents" {
		t.Fatalf("the created link is %+v", created)
	}
	if created.IncludePrivateMedia {
		t.Error("private media are shown by default")
	}
	if created.CreatedAt.IsZero() || created.ExpiresAt != nil || created.UseCount != 0 {
		t.Errorf("a fresh link describes itself as %+v", created.shareLinkResponse)
	}

	// The list describes the link but can never show its token again.
	recorder := send(s, http.MethodGet, "/api/v1/trips/"+tripID+"/share-links", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list links: %d %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, created.Token) || strings.Contains(body, `"token"`) {
		t.Errorf("the list carries the token: %s", body)
	}

	if recorder := sendShared(s, "/api/v1/shared", created.Token); recorder.Code != http.StatusOK {
		t.Fatalf("open the link: %d %s", recorder.Code, recorder.Body.String())
	}
	// Only opening the link is a visit; what the page reads after it is not.
	sendShared(s, "/api/v1/shared/document", created.Token)
	if visits := trips.visits[uuid.MustParse(created.ID)]; visits != 1 {
		t.Errorf("one opening was counted as %d visits", visits)
	}

	recorder = send(s, http.MethodDelete, "/api/v1/share-links/"+created.ID, "good", "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke the link: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = sendShared(s, "/api/v1/shared", created.Token)
	if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != "invalid_share_token" {
		t.Errorf("a revoked link still opens: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestShareTokenRefusals checks every unusable token is refused the same way.
func TestShareTokenRefusals(t *testing.T) {
	s, _ := newTripServer(domain.RoleOwner)
	for name, token := range map[string]string{"absent": "", "unknown": "not-a-real-token"} {
		recorder := sendShared(s, "/api/v1/shared", token)
		if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != "invalid_share_token" {
			t.Errorf("%s token: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
}

// TestShareLinkExpiry checks an expired link stops opening the trip, and that a
// date already past is refused when the link is created.
func TestShareLinkExpiry(t *testing.T) {
	s, trips := newTripServer(domain.RoleOwner)
	tripID := trips.trip.ID.String()
	now := time.Now()

	recorder := send(s, http.MethodPost, "/api/v1/trips/"+tripID+"/share-links", "good",
		`{"expires_at":"`+now.Add(-time.Hour).Format(time.RFC3339)+`"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a link that has already expired: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodPost, "/api/v1/trips/"+tripID+"/share-links", "good",
		`{"expires_at":"tomorrow"}`); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a date that is not a timestamp: %d %s", recorder.Code, recorder.Body.String())
	}

	created := createLink(t, s, tripID, `{"expires_at":"`+now.Add(time.Hour).Format(time.RFC3339)+`"}`)
	if recorder := sendShared(s, "/api/v1/shared", created.Token); recorder.Code != http.StatusOK {
		t.Fatalf("open a link that has not expired: %d %s", recorder.Code, recorder.Body.String())
	}
	// Moving the server's clock past the expiry is what a later visit sees.
	s.now = func() time.Time { return now.Add(2 * time.Hour) }
	recorder = sendShared(s, "/api/v1/shared", created.Token)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("an expired link still opens: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestSharedOpensTheTripsDocument checks a link opens the one document its trip
// holds - a plan's link the plan, a report's link the report - and says which.
func TestSharedOpensTheTripsDocument(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	plan := createLink(t, s, docs.document.TripID.String(), `{}`)
	recorder := sendShared(s, "/api/v1/shared", plan.Token)
	var shared sharedResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &shared); err != nil {
		t.Fatalf("response %q is not a shared trip: %v", recorder.Body.String(), err)
	}
	if shared.Kind != "plan" {
		t.Errorf("a plan's link reports kind=%s", shared.Kind)
	}
	if shared.Trip.Title != "Iceland" || shared.Trip.OwnerName == "" {
		t.Errorf("the shared trip is %+v", shared.Trip)
	}
	if kind := sharedDocumentKind(t, s, plan.Token); kind != "plan" {
		t.Errorf("a plan's link opened a %s", kind)
	}

	s, docs = newReportServer(domain.RoleOwner)
	report := createLink(t, s, docs.document.TripID.String(), `{}`)
	if kind := sharedDocumentKind(t, s, report.Token); kind != "report" {
		t.Errorf("a report's link opened a %s", kind)
	}
}

// sharedDocumentKind reads the document a link opens and returns its kind.
func sharedDocumentKind(t *testing.T, s *Server, token string) string {
	t.Helper()
	recorder := sendShared(s, "/api/v1/shared/document", token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read the shared document: %d %s", recorder.Code, recorder.Body.String())
	}
	var document struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("response %q is not a document: %v", recorder.Body.String(), err)
	}
	return document.Kind
}

// TestSharedIsReadOnly checks a share token opens nothing but the shared reads:
// it is not a way into the application's own routes.
func TestSharedIsReadOnly(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	tripID := docs.document.TripID.String()
	token := createLink(t, s, tripID, `{}`).Token

	// Every one of these needs an account; the share token is not one.
	for _, path := range []string{
		"/api/v1/trips",
		"/api/v1/trips/" + tripID,
		"/api/v1/documents/" + docs.document.ID.String(),
		"/api/v1/trips/" + tripID + "/budget",
	} {
		if recorder := sendShared(s, path, token); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s answered %d to a share token: %s", path, recorder.Code, recorder.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents/"+docs.document.ID.String()+"/days",
		strings.NewReader(`{"title":"Arrival"}`))
	request.Header.Set(shareTokenHeader, token)
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || docs.changed != 0 {
		t.Errorf("a share token changed the plan: %d %s", recorder.Code, recorder.Body.String())
	}
}
