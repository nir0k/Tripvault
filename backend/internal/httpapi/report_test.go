package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// newReportServer is newDocumentServer with the fake document turned into the
// trip's report, which is what the report endpoints edit.
func newReportServer(role domain.TripRole) (*Server, *fakeDocuments) {
	s, docs, _ := newRoutingServer(role)
	docs.document.Kind = domain.DocumentReport
	// A report is a trip of its own, holding the report and nothing else.
	docs.trips.trip.Kind = domain.DocumentReport
	docs.trips.trip.PlanID = nil
	docs.trips.trip.ReportID = &docs.document.ID
	return s, docs
}

// decodeDocument reads a document response, failing the test on anything else.
func decodeDocument(t *testing.T, body []byte) documentResponse {
	t.Helper()
	var document documentResponse
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("response %q is not a document: %v", body, err)
	}
	return document
}

// TestCreateReport checks a report is written from a plan as a trip of its own,
// owned by whoever asked - a viewer of the plan included - and that only a plan
// can be copied.
func TestCreateReport(t *testing.T) {
	for _, role := range []domain.TripRole{domain.RoleOwner, domain.RoleViewer} {
		s, docs := newDocumentServer(role)
		path := "/api/v1/trips/" + docs.document.TripID.String() + "/reports"

		recorder := send(s, http.MethodPost, path, "good", "")
		if recorder.Code != http.StatusCreated {
			t.Fatalf("%s: create from the plan: %d %s", role, recorder.Code, recorder.Body.String())
		}
		var created tripResponse
		_ = json.Unmarshal(recorder.Body.Bytes(), &created)
		plan := docs.trips.trip
		if created.Kind != "report" || created.Role != "owner" || created.Title != plan.Title ||
			created.SourceTripID == nil || *created.SourceTripID != plan.ID.String() {
			t.Errorf("%s: the report is %+v", role, created)
		}
		copied := docs.copied
		if copied == nil || copied.ID == plan.ID || copied.Budget == nil || *copied.Budget != *plan.Budget ||
			copied.Travelers != plan.Travelers {
			t.Errorf("%s: the report copied %+v", role, copied)
		}
	}

	// A report cannot be copied into another report.
	s, docs := newReportServer(domain.RoleOwner)
	recorder := send(s, http.MethodPost, "/api/v1/trips/"+docs.document.TripID.String()+"/reports", "good", "")
	if recorder.Code != http.StatusConflict || docs.changed != 0 {
		t.Errorf("a report was copied: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestReportFieldsOverHTTP checks a status, a story, a rating and an amount
// spent reach a report and are refused on a plan.
func TestReportFieldsOverHTTP(t *testing.T) {
	s, docs := newReportServer(domain.RoleOwner)
	path := "/api/v1/items/" + docs.place.ID.String()
	body := `{"status":"visited","story_md":"It rained","rating":4,"actual_cost_amount":"18.50"}`

	recorder := send(s, http.MethodPatch, path, "good", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("mark a place visited: %d %s", recorder.Code, recorder.Body.String())
	}

	// The same body on a plan is refused, and nothing is stored.
	plan, planDocs := newDocumentServer(domain.RoleOwner)
	recorder = send(plan, http.MethodPatch, "/api/v1/items/"+planDocs.place.ID.String(), "good", body)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a plan accepted a report's fields: %d %s", recorder.Code, recorder.Body.String())
	}
	if planDocs.changed != 0 {
		t.Error("a plan stored a report's fields")
	}
	if code := errorCode(t, recorder); code != "validation_failed" {
		t.Errorf("error code %q", code)
	}
}

// TestReportDocumentCarriesTotals checks a report answers with the figures its
// reading mode needs and a plan does not.
func TestReportDocumentCarriesTotals(t *testing.T) {
	s, docs := newReportServer(domain.RoleOwner)
	docs.place.Status = domain.StatusVisited

	recorder := send(s, http.MethodGet, "/api/v1/documents/"+docs.document.ID.String(), "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("read the report: %d %s", recorder.Code, recorder.Body.String())
	}
	document := decodeDocument(t, recorder.Body.Bytes())
	if document.Totals == nil {
		t.Fatal("a report carries no totals")
	}
	if document.Totals.Places.Visited != 1 || document.Totals.Places.Total != 1 {
		t.Errorf("places %+v", document.Totals.Places)
	}

	plan, planDocs := newDocumentServer(domain.RoleOwner)
	recorder = send(plan, http.MethodGet, "/api/v1/documents/"+planDocs.document.ID.String(), "good", "")
	if decodeDocument(t, recorder.Body.Bytes()).Totals != nil {
		t.Error("a plan carries a report's totals")
	}
}

// TestDocumentTextRoundTrip checks the words around the days are stored and
// their length is bounded.
func TestDocumentTextRoundTrip(t *testing.T) {
	s, docs := newReportServer(domain.RoleOwner)
	path := "/api/v1/documents/" + docs.document.ID.String()

	recorder := send(s, http.MethodPatch, path, "good", `{"intro_md":"We flew in on Friday."}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("store the introduction: %d %s", recorder.Code, recorder.Body.String())
	}
	if document := decodeDocument(t, recorder.Body.Bytes()); document.IntroMD != "We flew in on Friday." {
		t.Errorf("the introduction came back as %q", document.IntroMD)
	}
	// An omitted field keeps its value rather than clearing it.
	recorder = send(s, http.MethodPatch, path, "good", `{"summary_md":"Worth it."}`)
	document := decodeDocument(t, recorder.Body.Bytes())
	if document.IntroMD != "We flew in on Friday." || document.SummaryMD != "Worth it." {
		t.Errorf("document words: %q / %q", document.IntroMD, document.SummaryMD)
	}
}
