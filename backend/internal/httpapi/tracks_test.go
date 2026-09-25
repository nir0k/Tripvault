package httpapi

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// gpx builds a small GPX export, the way a watch writes one.
func gpx(points [][2]float64) []byte {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><gpx version="1.1"><trk><trkseg>`)
	for _, point := range points {
		fmt.Fprintf(&body, `<trkpt lat="%f" lon="%f"/>`, point[0], point[1])
	}
	body.WriteString(`</trkseg></trk></gpx>`)
	return []byte(body.String())
}

// importTrack sends a file to a place the way the browser does.
func importTrack(t *testing.T, s *Server, itemID string, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create the file part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write the file part: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/"+itemID+"/track", body)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// TestImportTrackStoresTheRecordedLine checks a GPX export becomes the line of
// a place, with its length measured and its points counted.
func TestImportTrackStoresTheRecordedLine(t *testing.T) {
	s, docs := newReportServerWithTracks(t)
	recorder := importTrack(t, s, docs.place.ID.String(), "day1.gpx",
		gpx([][2]float64{{63.532, -19.511}, {63.500, -19.400}, {63.491, -19.364}}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("import a track: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.track == nil {
		t.Fatal("nothing was stored")
	}
	if docs.track.OriginalName != "day1.gpx" || docs.track.PointCount != 3 {
		t.Errorf("stored %q with %d points, want day1.gpx with 3", docs.track.OriginalName, docs.track.PointCount)
	}
	if docs.track.DistanceM < 8000 || docs.track.DistanceM > 9500 {
		t.Errorf("measured %d m, want about 8.6 km", docs.track.DistanceM)
	}
	if docs.track.Geometry == "" {
		t.Error("the stored line is empty")
	}

	// A second import replaces the first: a place carries one recording.
	if recorder := importTrack(t, s, docs.place.ID.String(), "again.gpx",
		gpx([][2]float64{{63.4, -19.4}, {63.41, -19.41}})); recorder.Code != http.StatusOK {
		t.Fatalf("replace the track: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.track.OriginalName != "again.gpx" {
		t.Errorf("after replacing, the place holds %q", docs.track.OriginalName)
	}
}

// TestPlanTakesAPlannedRoute checks a plan's place takes a route drawn in
// advance, the way a report's takes a recording.
func TestPlanTakesAPlannedRoute(t *testing.T) {
	s, docs := newReportServerWithTracks(t)
	docs.document.Kind = domain.DocumentPlan
	recorder := importTrack(t, s, docs.place.ID.String(), "planned.gpx",
		gpx([][2]float64{{63.5, -19.5}, {63.51, -19.51}}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("a plan: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.track == nil || docs.track.ItemID != docs.place.ID || docs.track.OriginalName != "planned.gpx" {
		t.Errorf("stored %+v, want the place's planned route", docs.track)
	}
}

// TestItemTrackIsDownloadedAsUploaded checks the file of a place's recording
// comes back as it was uploaded, and that the recording can be removed.
func TestItemTrackIsDownloadedAsUploaded(t *testing.T) {
	s, docs := newReportServerWithTracks(t)
	file := gpx([][2]float64{{63.532, -19.511}, {63.500, -19.400}})
	recorder := importTrack(t, s, docs.place.ID.String(), "Хайк.gpx", file)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import a place's track: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.track == nil || docs.track.ItemID != docs.place.ID {
		t.Fatalf("stored %+v, want the place's track", docs.track)
	}
	if docs.track.Format != "gpx" {
		t.Errorf("format %q, want gpx", docs.track.Format)
	}
	if !strings.Contains(recorder.Body.String(), `"track":{"id":"`+docs.track.ID.String()) {
		t.Errorf("the place in the document carries no track: %s", recorder.Body.String())
	}

	download := send(s, http.MethodGet, "/api/v1/tracks/"+docs.track.ID.String()+"/file", "good", "")
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), file) {
		t.Fatalf("download the track: %d %q", download.Code, download.Body.String())
	}
	if kind := download.Header().Get("Content-Type"); kind != "application/gpx+xml" {
		t.Errorf("content type %q", kind)
	}
	if disposition := download.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment;") ||
		!strings.Contains(disposition, "filename") {
		t.Errorf("content disposition %q", disposition)
	}

	if recorder := send(s, http.MethodDelete, "/api/v1/items/"+docs.place.ID.String()+"/track", "good", ""); recorder.Code != http.StatusOK {
		t.Fatalf("remove the place's track: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.track != nil {
		t.Error("the track is still there after being removed")
	}
}

// TestImportTrackRefusesWhatItCannotRead covers a file that is not a track and
// a reader who may only look.
func TestImportTrackRefusesWhatItCannotRead(t *testing.T) {
	s, docs := newReportServerWithTracks(t)
	recorder := importTrack(t, s, docs.place.ID.String(), "notes.txt", []byte("hello"))
	if recorder.Code != http.StatusUnsupportedMediaType || errorCode(t, recorder) != "unsupported_track" {
		t.Errorf("a text file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := importTrack(t, s, docs.place.ID.String(), "one.gpx",
		gpx([][2]float64{{63.5, -19.5}})); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a track of one point: %d %s", recorder.Code, recorder.Body.String())
	}

	// A viewer may read the report and nothing else.
	viewer, docs := newViewerReportServer(t)
	if recorder := importTrack(t, viewer, docs.place.ID.String(), "day1.gpx",
		gpx([][2]float64{{63.5, -19.5}, {63.51, -19.51}})); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer: %d %s", recorder.Code, recorder.Body.String())
	}
}

// newReportServerWithTracks builds a report an editor may change, with the
// track limit the deployment defaults to.
func newReportServerWithTracks(t *testing.T) (*Server, *fakeDocuments) {
	t.Helper()
	s, docs := newReportServer(domain.RoleOwner)
	s.trackMaxBytes = 10 * 1024 * 1024
	return s, docs
}

// newViewerReportServer builds the same report for somebody who may only read it.
func newViewerReportServer(t *testing.T) (*Server, *fakeDocuments) {
	t.Helper()
	s, docs := newReportServer(domain.RoleViewer)
	s.trackMaxBytes = 10 * 1024 * 1024
	return s, docs
}
