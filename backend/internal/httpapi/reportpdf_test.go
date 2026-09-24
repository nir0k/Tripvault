package httpapi

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
	"github.com/nir0k/tripvault/backend/internal/staticmap"
)

// newReportPDFServer builds a server whose trip has a report with one day, one
// place and one photograph on each.
func newReportPDFServer(t *testing.T, role domain.TripRole) (*Server, *fakeDocuments, *fakeTrips) {
	t.Helper()

	_, trips := newTripServer(role)
	documentID, dayID := uuid.New(), uuid.New()
	docs := &fakeDocuments{
		document: domain.Document{ID: documentID, TripID: trips.trip.ID, Kind: domain.DocumentReport,
			IntroMD: "It rained the whole way."},
		day: domain.Day{ID: dayID, DocumentID: documentID, StartTime: domain.DefaultDayStart,
			Title: "Reykjavík", Date: trips.trip.StartDate},
		place: domain.Item{ID: uuid.New(), DocumentID: documentID, DayID: &dayID, Kind: domain.ItemPlace,
			Name: "The old harbour", Status: domain.StatusVisited, StoryMD: "Bright and empty."},
	}
	docs.trips = trips
	trips.trip.Kind = domain.DocumentReport
	trips.trip.ReportID = &documentID

	catalogue := newFakePDFMedia(t, trips.trip.ID, dayID, docs.place.ID)
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:       &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true, Locale: "ru"}},
		Users:      fakeUsers{},
		Trips:      trips,
		Documents:  docs,
		Media:      catalogue,
		MediaFiles: fakePDFFiles{bytes: catalogue.bytes},
	})
	return s, docs, trips
}

// fakePDFMedia is the catalogue: which files the trip has and where each is
// shown. Their bytes live in fakePDFFiles, exactly as they do in the service.
type fakePDFMedia struct {
	items []domain.Media
	links []domain.MediaLink
	bytes map[string][]byte
}

// newFakePDFMedia builds one public photograph on the day and one private one on
// the place, which is what tells the two kinds of reader apart.
func newFakePDFMedia(t *testing.T, tripID, dayID, placeID uuid.UUID) *fakePDFMedia {
	t.Helper()

	// The two pictures differ, because a document holds one object per distinct
	// picture: two identical ones would be stored once and counted once.
	publicBytes := samplePicture(t, 90)
	privateBytes := samplePicture(t, 200)

	public := domain.Media{ID: uuid.New(), TripID: tripID, StorageKey: "public.jpg",
		OriginalName: "harbour.jpg", MIME: "image/jpeg", Size: int64(len(publicBytes)), Status: domain.MediaReady}
	private := domain.Media{ID: uuid.New(), TripID: tripID, StorageKey: "private.jpg",
		OriginalName: "passport.jpg", MIME: "image/jpeg", Size: int64(len(privateBytes)),
		IsPrivate: true, Status: domain.MediaReady}

	return &fakePDFMedia{
		items: []domain.Media{public, private},
		links: []domain.MediaLink{
			{MediaID: public.ID, Target: domain.MediaTargetDay, TargetID: dayID},
			{MediaID: private.ID, Target: domain.MediaTargetItem, TargetID: placeID},
		},
		bytes: map[string][]byte{"public.jpg": publicBytes, "private.jpg": privateBytes},
	}
}

// samplePicture encodes a small JPEG whose colours depend on tint, so that two
// of them are two pictures rather than the same one twice.
func samplePicture(t *testing.T, tint uint8) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := range 40 {
		for y := range 30 {
			picture.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: tint, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		t.Fatalf("encode the sample photograph: %v", err)
	}
	return encoded.Bytes()
}

// ListByTrip returns the catalogue of the trip.
func (f *fakePDFMedia) ListByTrip(context.Context, uuid.UUID) ([]domain.Media, error) {
	return f.items, nil
}

// LinksOfTrip returns where each file is shown.
func (f *fakePDFMedia) LinksOfTrip(context.Context, uuid.UUID) ([]domain.MediaLink, error) {
	return f.links, nil
}

// TestReportPicturesPrefersFavorites checks the rule the document is written
// by: the favourites of a day or a place when it has any, and the first few of
// its gallery while it has none, so a report written before anybody chose is
// still a report with photographs in it.
func TestReportPicturesPrefersFavorites(t *testing.T) {
	gallery := make([]shown, 0, domain.MediaFavoriteLimit+2)
	for i := range domain.MediaFavoriteLimit + 2 {
		gallery = append(gallery, shown{
			item:       domain.Media{ID: uuid.New()},
			isFavorite: i == domain.MediaFavoriteLimit+1,
		})
	}

	chosen := reportPictures(gallery)
	if len(chosen) != 1 || chosen[0].ID != gallery[len(gallery)-1].item.ID {
		t.Errorf("with one favourite the report shows %d pictures, want that one", len(chosen))
	}

	for i := range gallery {
		gallery[i].isFavorite = false
	}
	chosen = reportPictures(gallery)
	if len(chosen) != domain.MediaFavoriteLimit || chosen[0].ID != gallery[0].item.ID {
		t.Errorf("with none marked the report shows %d pictures, want the first %d",
			len(chosen), domain.MediaFavoriteLimit)
	}
}

// SetFavorites marks the favourites of one target; the PDF only reads them.
func (f *fakePDFMedia) SetFavorites(_ context.Context, _ domain.MediaTarget, targetID uuid.UUID,
	mediaIDs []uuid.UUID) error {
	chosen := map[uuid.UUID]struct{}{}
	for _, id := range mediaIDs {
		chosen[id] = struct{}{}
	}
	for i := range f.links {
		if f.links[i].TargetID == targetID {
			_, f.links[i].IsFavorite = chosen[f.links[i].MediaID]
		}
	}
	return nil
}

// SetFavorite marks files wherever they hang.
func (f *fakePDFMedia) SetFavorite(_ context.Context, mediaIDs []uuid.UUID, favorite bool) error {
	for _, id := range mediaIDs {
		for i := range f.links {
			if f.links[i].MediaID == id {
				f.links[i].IsFavorite = favorite
			}
		}
	}
	return nil
}

// fakePDFFiles is the store the bytes are read from.
type fakePDFFiles struct {
	bytes map[string][]byte
}

// Open returns the bytes of one stored file.
func (f fakePDFFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, held := f.bytes[key]
	if !held {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// Put is not used by these tests.
func (f fakePDFFiles) Put(context.Context, string, io.Reader) (int64, error) { return 0, nil }

// Delete is not used by these tests.
func (f fakePDFFiles) Delete(context.Context, string) error { return nil }

// The rest of the two interfaces is not reached by these tests.
func (f *fakePDFMedia) Create(context.Context, domain.Media, int64) error { return nil }

// Get returns one file of the catalogue.
func (f *fakePDFMedia) Get(_ context.Context, id uuid.UUID) (domain.Media, error) {
	for _, item := range f.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.Media{}, domain.ErrNotFound
}

// Update is not used by these tests.
func (f *fakePDFMedia) Update(context.Context, uuid.UUID, domain.MediaChanges) (domain.Media, error) {
	return domain.Media{}, domain.ErrNotFound
}

// Delete is not used by these tests.
func (f *fakePDFMedia) Delete(context.Context, uuid.UUID) (string, error) { return "", nil }

// UsedBytes is not used by these tests.
func (f *fakePDFMedia) UsedBytes(context.Context, uuid.UUID) (int64, error) { return 0, nil }

// SetLinks is not used by these tests.
func (f *fakePDFMedia) SetLinks(context.Context, domain.MediaTarget, uuid.UUID, []uuid.UUID) error {
	return nil
}

// TripOfTarget is not used by these tests.
func (f *fakePDFMedia) TripOfTarget(context.Context, domain.MediaTarget, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, domain.ErrNotFound
}

// countImages says how many pictures a rendered document holds.
func countImages(document []byte) int {
	return bytes.Count(document, []byte("/Subtype /Image"))
}

// TestReportPDFIsRenderedForTheTrip checks the document comes back as a PDF,
// named after the trip, with its photographs in it.
func TestReportPDFIsRenderedForTheTrip(t *testing.T) {
	s, _, trips := newReportPDFServer(t, domain.RoleViewer)

	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/report/pdf", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/pdf" {
		t.Errorf("the document was sent as %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="Iceland.pdf"` {
		t.Errorf("the document is offered as %q", got)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("what came back is not a PDF")
	}
	// Both photographs: somebody who may read the trip may see all of its files.
	if images := countImages(recorder.Body.Bytes()); images != 2 {
		t.Errorf("the document holds %d photographs, want 2", images)
	}
}

// TestReportPDFUsesStoredPreviews checks a photograph is taken from the preview
// the gallery already rendered: the original here cannot be decoded at all, so
// the document holds both pictures only if it never needed to.
func TestReportPDFUsesStoredPreviews(t *testing.T) {
	s, _, trips := newReportPDFServer(t, domain.RoleViewer)
	files := s.mediaFiles.(fakePDFFiles).bytes
	files["public.jpg"] = []byte("not a picture")
	files[media.PreviewKey("public.jpg", pdfPhotoWidth)] = samplePicture(t, 120)

	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/report/pdf", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if images := countImages(recorder.Body.Bytes()); images != 2 {
		t.Errorf("the document holds %d photographs, want 2", images)
	}
}

// tileSource hands out plain tiles, or fails every one of them.
type tileSource struct{ fail bool }

// Tile returns a white tile, or an error when the source is down.
func (s tileSource) Tile(context.Context, int, int, int) (image.Image, error) {
	if s.fail {
		return nil, errors.New("the tile server is not answering")
	}
	return image.NewGray(image.Rect(0, 0, staticmap.TileSize, staticmap.TileSize)), nil
}

// TestReportPDFDrawsMaps checks a report with a place on the map is drawn with
// the map of the trip and of its day, and that a tile server that does not
// answer costs the maps their background rather than the document.
func TestReportPDFDrawsMaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source tileSource
		images int
	}{
		// Two photographs and the backgrounds of two maps.
		{"tiles", tileSource{}, 4},
		{"no tiles", tileSource{fail: true}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, docs, trips := newReportPDFServer(t, domain.RoleViewer)
			lat, lng := 64.1520, -21.9510
			docs.place.Lat, docs.place.Lng = &lat, &lng
			s.mapTiles = tc.source

			recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/report/pdf", "good", "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
			}
			if images := countImages(recorder.Body.Bytes()); images != tc.images {
				t.Errorf("the document holds %d pictures, want %d", images, tc.images)
			}
		})
	}
}

// TestPlainTextAttribution checks the credit the interface shows as HTML is
// printed as words.
func TestPlainTextAttribution(t *testing.T) {
	got := plainText(`&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors`)
	if got != "© OpenStreetMap contributors" {
		t.Errorf("plainText() = %q", got)
	}
}

// TestReportPDFWithoutPhotographs checks the parameter that leaves them out,
// which is the difference between a small file and a large one.
func TestReportPDFWithoutPhotographs(t *testing.T) {
	s, _, trips := newReportPDFServer(t, domain.RoleOwner)

	path := "/api/v1/trips/" + trips.trip.ID.String() + "/report/pdf?photos=false"
	recorder := send(s, http.MethodGet, path, "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if images := countImages(recorder.Body.Bytes()); images != 0 {
		t.Errorf("the document holds %d photographs, want none", images)
	}
}

// TestReportPDFNeedsAReport checks a plan says it has no report rather than
// answering with an empty document.
func TestReportPDFNeedsAReport(t *testing.T) {
	s, _, trips := newReportPDFServer(t, domain.RoleOwner)
	trips.trip.Kind = domain.DocumentPlan
	trips.trip.PlanID, trips.trip.ReportID = trips.trip.ReportID, nil

	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/report/pdf", "good", "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
}

// TestReportPDFNeedsAccess checks somebody with no role on the trip cannot take
// a copy of it. A trip they cannot reach answers as a missing one, exactly as
// every other endpoint does: the answer must not say the trip exists.
func TestReportPDFNeedsAccess(t *testing.T) {
	s, _, trips := newReportPDFServer(t, "")

	recorder := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/report/pdf", "good", "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
}

// TestPDFFilename checks a title becomes a filename every system will save, and
// that one written in an alphabet it keeps nothing of still gets a name.
func TestPDFFilename(t *testing.T) {
	cases := map[string]string{
		"Iceland: the south coast": "Iceland-the-south-coast.pdf",
		"  Lisbon  in  spring  ":   "Lisbon-in-spring.pdf",
		"Исландия":                 "report.pdf",
		"":                         "report.pdf",
		"../../etc/passwd":         "etcpasswd.pdf",
	}
	for title, want := range cases {
		if got := pdfFilename(title); got != want {
			t.Errorf("pdfFilename(%q) = %q, want %q", title, got, want)
		}
	}

	if got := pdfFilename(string(make([]byte, 0)) + "A" + string(bytes.Repeat([]byte("b"), 200))); len(got) > 64 {
		t.Errorf("a very long title became a name of %d characters", len(got))
	}
}

// TestSharedReportPDFHonoursTheLink checks a read-only link renders the same
// document the screen shows: with the private photograph only when the link was
// made to show private files.
func TestSharedReportPDFHonoursTheLink(t *testing.T) {
	cases := map[string]struct {
		body   string
		images int
	}{
		"without private files": {`{}`, 1},
		"with them":             {`{"include_private_media":true}`, 2},
	}

	for name, test := range cases {
		s, _, trips := newReportPDFServer(t, domain.RoleOwner)
		created := createLink(t, s, trips.trip.ID.String(), test.body)

		recorder := sendShared(s, "/api/v1/shared/report/pdf", created.Token)
		if recorder.Code != http.StatusOK {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body.String())
			continue
		}
		if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("%PDF-")) {
			t.Errorf("%s: what came back is not a PDF", name)
			continue
		}
		if images := countImages(recorder.Body.Bytes()); images != test.images {
			t.Errorf("%s: the document holds %d photographs, want %d", name, images, test.images)
		}
	}
}

// TestSharedReportPDFOfAPlan checks a plan's link has no report to hand over in
// any form.
func TestSharedReportPDFOfAPlan(t *testing.T) {
	s, docs := newDocumentServer(domain.RoleOwner)
	created := createLink(t, s, docs.trips.trip.ID.String(), `{}`)

	recorder := sendShared(s, "/api/v1/shared/report/pdf", created.Token)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}

	// And a request with no token at all is refused before anything is rendered.
	if recorder := sendShared(s, "/api/v1/shared/report/pdf", ""); recorder.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d %s", recorder.Code, recorder.Body.String())
	}
}
