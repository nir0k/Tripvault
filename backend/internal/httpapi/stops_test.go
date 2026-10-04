package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestStopsLieOnTheLine checks a stop is placed on the line of its activity,
// changed, moved onto a new line with it, and removed, and that an activity
// without a line, a plan's time and a payer from outside the trip are refused.
func TestStopsLieOnTheLine(t *testing.T) {
	s, docs := newReportServerWithTracks(t)
	activity := docs.place.ID.String()

	recorder := send(s, http.MethodPost, "/api/v1/items/"+activity+"/stops", "good", `{"lat":63.5,"lng":-19.5}`)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"reason":"no_track"`) {
		t.Fatalf("a stop without a line: %d %s", recorder.Code, recorder.Body.String())
	}

	// A line due north, about 1.1 km.
	line := gpx([][2]float64{{63.50, -19.5}, {63.51, -19.5}})
	if recorder := importTrack(t, s, activity, "hike.gpx", line); recorder.Code != http.StatusOK {
		t.Fatalf("import the line: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder = send(s, http.MethodPost, "/api/v1/items/"+activity+"/stops", "good",
		`{"kind":"food","name":"Café","lat":63.505,"lng":-19.49,"planned_cost_amount":"20.00","actual_time":"12:40"}`)
	if recorder.Code != http.StatusOK || len(docs.stops) != 1 {
		t.Fatalf("add a stop: %d %s", recorder.Code, recorder.Body.String())
	}
	stop := docs.stops[0]
	if stop.Point.Lng != -19.5 || stop.DistanceM < 540 || stop.DistanceM > 575 || stop.Kind != domain.StopFood {
		t.Errorf("stop %+v is not halfway along the line", stop)
	}
	if !strings.Contains(recorder.Body.String(), `"stops":[{"id":"`+stop.ID.String()) {
		t.Errorf("the line in the document carries no stop: %s", recorder.Body.String())
	}

	path := "/api/v1/stops/" + stop.ID.String()
	if recorder := send(s, http.MethodPatch, path, "good", `{"name":"Hut","cost_split":"everyone","paid_by":"`+
		uuid.NewString()+`","cost_shares":[{"user_id":"`+uuid.NewString()+`"}]}`); recorder.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(recorder.Body.String(), `"reason":"unknown_member"`) {
		t.Errorf("a payer from outside the trip: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodPatch, path, "good", `{"name":"Hut","note_md":"Soup"}`); recorder.Code != http.StatusOK ||
		docs.stops[0].Name != "Hut" || docs.stops[0].NoteMD != "Soup" || docs.stops[0].DistanceM != stop.DistanceM {
		t.Errorf("change a stop: %d %+v", recorder.Code, docs.stops[0])
	}

	// A longer line puts the stop at the same point, further from a new start.
	longer := gpx([][2]float64{{63.49, -19.5}, {63.51, -19.5}})
	if recorder := importTrack(t, s, activity, "longer.gpx", longer); recorder.Code != http.StatusOK {
		t.Fatalf("replace the line: %d %s", recorder.Code, recorder.Body.String())
	}
	if moved := docs.stops[0].DistanceM; moved < stop.DistanceM+1000 {
		t.Errorf("the stop lies %d m along the new line, want about %d", moved, stop.DistanceM+1110)
	}

	if recorder := send(s, http.MethodDelete, path, "good", ""); recorder.Code != http.StatusOK || len(docs.stops) != 0 {
		t.Errorf("remove a stop: %d %d left", recorder.Code, len(docs.stops))
	}

	docs.document.Kind = domain.DocumentPlan
	recorder = send(s, http.MethodPost, "/api/v1/items/"+activity+"/stops", "good",
		`{"lat":63.505,"lng":-19.5,"actual_time":"12:40"}`)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"reason":"report_only"`) {
		t.Errorf("a plan's stop with a time: %d %s", recorder.Code, recorder.Body.String())
	}
}
