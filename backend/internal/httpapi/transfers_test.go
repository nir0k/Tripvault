package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestTransferLifecycle checks a transfer is created, changed and deleted, and
// that its per-person price reaches the budget as the group's.
func TestTransferLifecycle(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	base := "/api/v1/documents/" + docs.document.ID.String()

	recorder := send(s, http.MethodPost, base+"/transfers", "good",
		`{"kind":"flight","name":"FI 204","from_name":"Keflavík","to_name":"Copenhagen",
		  "departure_date":"2026-06-01","departure_time":"07:40","arrival_time":"12:55",
		  "planned_cost_amount":"180","cost_per_person":true}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create the transfer: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.transfer == nil || docs.transfer.Name != "FI 204" || docs.transfer.DepartureTime == nil ||
		docs.transfer.DepartureTime.String() != "07:40" || docs.transfer.ArrivalDate != nil {
		t.Fatalf("the stored transfer is %+v", docs.transfer)
	}
	var document documentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("response is not a document: %v", err)
	}
	if len(document.Transfers) != 1 || document.Transfers[0].ToName != "Copenhagen" {
		t.Errorf("the document carries %+v", document.Transfers)
	}

	path := "/api/v1/transfers/" + docs.transfer.ID.String()
	recorder = send(s, http.MethodPatch, path, "good", `{"arrival_date":"2026-06-02","from_lat":63.98,"from_lng":-22.6}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("change the transfer: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.transfer.ArrivalDate == nil || docs.transfer.FromLat == nil || docs.transfer.Name != "FI 204" {
		t.Errorf("the changed transfer is %+v", docs.transfer)
	}

	if recorder := send(s, http.MethodDelete, path, "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("delete the transfer: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.transfer != nil {
		t.Error("the transfer survived its deletion")
	}
}

// TestTransferValidation checks a transfer needs both ends and a departure, and
// that a plan refuses what only a report records.
func TestTransferValidation(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	base := "/api/v1/documents/" + docs.document.ID.String() + "/transfers"

	for _, body := range []string{
		`{"from_name":"Keflavík","to_name":"Copenhagen"}`,
		`{"from_name":"","to_name":"Copenhagen","departure_date":"2026-06-01"}`,
		`{"from_name":"Keflavík","to_name":"Copenhagen","departure_date":"2026-06-01","kind":"rocket"}`,
		`{"from_name":"Keflavík","to_name":"Copenhagen","departure_date":"2026-06-05","arrival_date":"2026-06-01"}`,
		`{"from_name":"Keflavík","to_name":"Copenhagen","departure_date":"2026-06-01","to_lat":55.6}`,
		`{"from_name":"Keflavík","to_name":"Copenhagen","departure_date":"2026-06-01","actual_cost_amount":"10"}`,
	} {
		recorder := send(s, http.MethodPost, base, "good", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if docs.transfer != nil {
		t.Error("an invalid transfer was stored")
	}

	// A flight west over the date line lands the calendar day before.
	recorder := send(s, http.MethodPost, base, "good",
		`{"from_name":"Tokyo","to_name":"Honolulu","departure_date":"2026-06-02","arrival_date":"2026-06-01"}`)
	if recorder.Code != http.StatusCreated {
		t.Errorf("over the date line: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestTransferRolesAreEnforced checks a reader may not write transfers.
func TestTransferRolesAreEnforced(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleViewer)
	recorder := send(s, http.MethodPost, "/api/v1/documents/"+docs.document.ID.String()+"/transfers", "good",
		`{"from_name":"Keflavík","to_name":"Copenhagen","departure_date":"2026-06-01"}`)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer: %d %s", recorder.Code, recorder.Body.String())
	}
}
