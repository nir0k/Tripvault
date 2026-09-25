package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// fakeMedia is the catalogue in memory: enough to exercise who may read and
// change a trip's files, and what a read-only link is allowed to see.
type fakeMedia struct {
	items map[uuid.UUID]domain.Media
	links map[uuid.UUID][]domain.MediaLink
	// targets says which trip a link target belongs to.
	targets map[uuid.UUID]uuid.UUID
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{
		items:   map[uuid.UUID]domain.Media{},
		links:   map[uuid.UUID][]domain.MediaLink{},
		targets: map[uuid.UUID]uuid.UUID{},
	}
}

// Create stores a file, refusing one whose sums match a file of the same trip
// as the repository does.
func (f *fakeMedia) Create(_ context.Context, item domain.Media, _ int64) error {
	for _, stored := range f.items {
		if stored.TripID != item.TripID {
			continue
		}
		for _, sum := range [][]byte{item.Checksum, item.SourceChecksum} {
			if sum != nil && (bytes.Equal(sum, stored.Checksum) || bytes.Equal(sum, stored.SourceChecksum)) {
				return domain.ErrMediaDuplicate
			}
		}
	}
	f.items[item.ID] = item
	return nil
}

func (f *fakeMedia) Get(_ context.Context, id uuid.UUID) (domain.Media, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.Media{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeMedia) ListByTrip(_ context.Context, tripID uuid.UUID) ([]domain.Media, error) {
	items := make([]domain.Media, 0, len(f.items))
	for _, item := range f.items {
		if item.TripID == tripID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeMedia) ListAll(context.Context) ([]domain.Media, error) {
	items := make([]domain.Media, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return items, nil
}

func (f *fakeMedia) Update(_ context.Context, id uuid.UUID, changes domain.MediaChanges) (domain.Media, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.Media{}, domain.ErrNotFound
	}
	if changes.IsPrivate != nil {
		item.IsPrivate = *changes.IsPrivate
	}
	f.items[id] = item
	return item, nil
}

func (f *fakeMedia) Delete(_ context.Context, id uuid.UUID) (string, error) {
	item, ok := f.items[id]
	if !ok {
		return "", domain.ErrNotFound
	}
	delete(f.items, id)
	return item.StorageKey, nil
}

func (f *fakeMedia) UsedBytes(_ context.Context, tripID uuid.UUID) (int64, error) {
	var used int64
	for _, item := range f.items {
		if item.TripID == tripID {
			used += item.Size
		}
	}
	return used, nil
}

func (f *fakeMedia) SetLinks(_ context.Context, target domain.MediaTarget, targetID uuid.UUID,
	mediaIDs []uuid.UUID) error {
	tripID, ok := f.targets[targetID]
	if !ok {
		return domain.ErrNotFound
	}
	links := make([]domain.MediaLink, 0, len(mediaIDs))
	for position, id := range mediaIDs {
		item, known := f.items[id]
		if !known || item.TripID != tripID {
			return domain.NewValidationError("media_ids", "unknown_media", "must belong to the trip")
		}
		favorite := false
		for _, existing := range f.links[targetID] {
			if existing.MediaID == id && existing.IsFavorite {
				favorite = true
			}
		}
		links = append(links, domain.MediaLink{
			MediaID: id, Target: target, TargetID: targetID, Position: position, IsFavorite: favorite,
		})
	}
	f.links[targetID] = links
	return nil
}

func (f *fakeMedia) SetFavorites(_ context.Context, _ domain.MediaTarget, targetID uuid.UUID,
	mediaIDs []uuid.UUID) error {
	links, ok := f.links[targetID]
	if !ok {
		return domain.ErrNotFound
	}
	if len(mediaIDs) > domain.MediaFavoriteLimit {
		return domain.ErrMediaFavoriteLimit
	}
	chosen := map[uuid.UUID]struct{}{}
	for _, id := range mediaIDs {
		chosen[id] = struct{}{}
	}
	marked := 0
	for i := range links {
		_, links[i].IsFavorite = chosen[links[i].MediaID]
		if links[i].IsFavorite {
			marked++
		}
	}
	if marked != len(chosen) {
		return domain.ErrMediaFavoriteUnlinked
	}
	return nil
}

func (f *fakeMedia) SetFavorite(_ context.Context, mediaIDs []uuid.UUID, favorite bool) error {
	chosen := map[uuid.UUID]struct{}{}
	for _, id := range mediaIDs {
		chosen[id] = struct{}{}
	}
	for targetID, links := range f.links {
		count := 0
		for i := range links {
			if _, held := chosen[links[i].MediaID]; held {
				links[i].IsFavorite = favorite
			}
			if links[i].IsFavorite {
				count++
			}
		}
		if count > domain.MediaFavoriteLimit {
			return domain.ErrMediaFavoriteLimit
		}
		f.links[targetID] = links
	}
	return nil
}

func (f *fakeMedia) LinksOfTrip(_ context.Context, tripID uuid.UUID) ([]domain.MediaLink, error) {
	links := make([]domain.MediaLink, 0)
	for _, target := range f.links {
		for _, link := range target {
			if item, ok := f.items[link.MediaID]; ok && item.TripID == tripID {
				links = append(links, link)
			}
		}
	}
	return links, nil
}

func (f *fakeMedia) TripOfTarget(_ context.Context, _ domain.MediaTarget, targetID uuid.UUID) (uuid.UUID, error) {
	tripID, ok := f.targets[targetID]
	if !ok {
		return uuid.Nil, domain.ErrNotFound
	}
	return tripID, nil
}

// memoryFiles is the byte store in memory, standing in for the disk. Its lock
// lets the background preview workers share it with the test.
type memoryFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (m *memoryFiles) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = data
	return int64(len(data)), nil
}

func (m *memoryFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[key]
	if !ok {
		return nil, media.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memoryFiles) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}

// hasPreviews says whether every preview width of a file is in the store.
func (m *memoryFiles) hasPreviews(storageKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, width := range media.Sizes {
		if _, ok := m.files[media.PreviewKey(storageKey, width)]; !ok {
			return false
		}
	}
	return true
}

// newMediaServer builds a server whose token "good" reads the trip with role,
// with an empty catalogue and an empty byte store.
func newMediaServer(role domain.TripRole) (*Server, *fakeTrips, *fakeMedia, *memoryFiles) {
	start := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)
	trips := &fakeTrips{trip: domain.TripSummary{
		Trip:  domain.Trip{ID: uuid.New(), Title: "Iceland", Timezone: "UTC", Currency: "EUR", Travelers: 2, StartDate: &start, EndDate: &end},
		Owner: domain.TripUser{ID: uuid.New(), DisplayName: "Ada", Email: "ada@example.com"},
		Role:  role,
	}}
	catalogue := newFakeMedia()
	files := &memoryFiles{files: map[string][]byte{}}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:           &fakeAuth{user: domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "ISK"}},
		Users:          fakeUsers{},
		Trips:          trips,
		Media:          catalogue,
		MediaFiles:     files,
		MediaMaxBytes:  1024 * 1024,
		MediaTripQuota: 2 * 1024 * 1024,
	})
	return s, trips, catalogue, files
}

// picture renders a PNG of the given size for an upload.
func picture(t *testing.T, width, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			canvas.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		t.Fatalf("encode picture: %v", err)
	}
	return out.Bytes()
}

// upload sends one file as a browser does, with the optional private flag.
func upload(t *testing.T, s *Server, tripID string, name string, data []byte, private bool) *httptest.ResponseRecorder {
	t.Helper()
	return uploadFrom(t, s, tripID, name, data, private, "")
}

// uploadFrom sends one file the way a browser that shrank it does, naming the
// SHA-256 of the original in hex; an empty source sends no sum.
func uploadFrom(t *testing.T, s *Server, tripID string, name string, data []byte, private bool,
	source string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if private {
		if err := form.WriteField("private", "true"); err != nil {
			t.Fatalf("write the private field: %v", err)
		}
	}
	if source != "" {
		if err := form.WriteField("source_checksum", source); err != nil {
			t.Fatalf("write the source checksum: %v", err)
		}
	}
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

	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID+"/media", body)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// uploadedIDs reads the identifiers out of an upload's answer.
func uploadedIDs(t *testing.T, recorder *httptest.ResponseRecorder) []mediaResponse {
	t.Helper()
	var body listResponse[mediaResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response %q is not a list of files: %v", recorder.Body.String(), err)
	}
	return body.Items
}

// TestUploadStoresAPictureAndItsMetadata checks the answer describes the file
// as stored: the size it was given, the size it is shown at, and its bytes
// waiting in the store under a key of the trip.
func TestUploadStoresAPictureAndItsMetadata(t *testing.T) {
	s, trips, catalogue, files := newMediaServer(domain.RoleEditor)
	data := picture(t, 40, 20)

	recorder := upload(t, s, trips.trip.ID.String(), "beach.png", data, false)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", recorder.Code, recorder.Body.String())
	}
	items := uploadedIDs(t, recorder)
	if len(items) != 1 {
		t.Fatalf("the answer holds %d files, want 1", len(items))
	}
	stored := items[0]
	if stored.MIME != "image/png" || stored.Size != int64(len(data)) {
		t.Errorf("stored as %q of %d bytes, want image/png of %d", stored.MIME, stored.Size, len(data))
	}
	if stored.Width != 40 || stored.Height != 20 {
		t.Errorf("stored as %dx%d, want 40x20", stored.Width, stored.Height)
	}
	if stored.OriginalName != "beach.png" || stored.IsPrivate {
		t.Errorf("stored as %q, private=%v", stored.OriginalName, stored.IsPrivate)
	}
	if len(catalogue.items) != 1 || len(files.files) != 1 {
		t.Errorf("catalogue holds %d rows and the store %d files, want one each",
			len(catalogue.items), len(files.files))
	}
	for key := range files.files {
		if !strings.HasPrefix(key, trips.trip.ID.String()+"/") {
			t.Errorf("stored under %q, which is not a key of the trip", key)
		}
	}
}

// TestUploadRefusesWhatItCannotServe covers the three refusals of an upload:
// a file that is not a picture, one above the size limit, and one that would
// take a trip past its allowance.
func TestUploadRefusesWhatItCannotServe(t *testing.T) {
	s, trips, catalogue, files := newMediaServer(domain.RoleEditor)
	id := trips.trip.ID.String()

	recorder := upload(t, s, id, "notes.pdf", []byte("%PDF-1.7\n..."), false)
	if recorder.Code != http.StatusUnsupportedMediaType || errorCode(t, recorder) != "unsupported_file" {
		t.Errorf("a PDF: %d %s", recorder.Code, recorder.Body.String())
	}

	// A picture compresses well, so the limit is lowered instead of feeding the
	// test a photograph: what matters is the byte count, not the picture.
	s.mediaMaxBytes = 2048
	recorder = upload(t, s, id, "huge.png", picture(t, 900, 900), false)
	if recorder.Code != http.StatusRequestEntityTooLarge || errorCode(t, recorder) != "file_too_large" {
		t.Errorf("a file over the limit: %d %s", recorder.Code, recorder.Body.String())
	}

	if len(catalogue.items) != 0 || len(files.files) != 0 {
		t.Errorf("a refused upload left %d rows and %d files behind", len(catalogue.items), len(files.files))
	}
	s.mediaMaxBytes = 1024 * 1024

	// A trip already at its allowance takes nothing more.
	catalogue.items[uuid.New()] = domain.Media{
		ID: uuid.New(), TripID: trips.trip.ID, Size: 2 * 1024 * 1024, Status: domain.MediaReady,
	}
	recorder = upload(t, s, id, "one-more.png", picture(t, 10, 10), false)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "media_quota" {
		t.Errorf("a trip out of room: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestUploadRefusesADuplicate checks a trip stores a picture once: the same
// bytes again, or another shrinking of the same original, are refused, while
// another trip may hold the picture too.
func TestUploadRefusesADuplicate(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleEditor)
	tripID := trips.trip.ID.String()
	original := strings.Repeat("ab", sha256.Size)
	if recorder := uploadFrom(t, s, tripID, "a.png", picture(t, 10, 10), false, original); recorder.Code != http.StatusCreated {
		t.Fatalf("the first upload: %d %s", recorder.Code, recorder.Body.String())
	}

	for name, recorder := range map[string]*httptest.ResponseRecorder{
		"the same bytes":    upload(t, s, tripID, "copy.png", picture(t, 10, 10), false),
		"the same original": uploadFrom(t, s, tripID, "smaller.png", picture(t, 8, 8), false, original),
	} {
		if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "duplicate_media" {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
	if len(catalogue.items) != 1 {
		t.Errorf("%d files are catalogued, want only the first", len(catalogue.items))
	}

	// A sum that is not one is refused before anything is read.
	if recorder := uploadFrom(t, s, tripID, "b.png", picture(t, 12, 12), false, "nope"); recorder.Code != http.StatusBadRequest {
		t.Errorf("a malformed sum: %d %s", recorder.Code, recorder.Body.String())
	}

	// Another trip is another gallery.
	other := domain.Media{ID: uuid.New(), TripID: uuid.New(), Checksum: []byte("elsewhere")}
	catalogue.items[other.ID] = other
	if recorder := upload(t, s, tripID, "c.png", picture(t, 14, 14), false); recorder.Code != http.StatusCreated {
		t.Errorf("a new picture: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestMediaRolesAreEnforced checks reading a trip is enough to see its files
// and that changing them needs the right to edit.
func TestMediaRolesAreEnforced(t *testing.T) {
	for _, role := range []domain.TripRole{domain.RoleViewer, domain.RoleEditor, domain.RoleOwner, ""} {
		s, trips, catalogue, _ := newMediaServer(role)
		item := domain.Media{
			ID: uuid.New(), TripID: trips.trip.ID, StorageKey: "k", MIME: "image/png", Size: 10,
			Status: domain.MediaReady,
		}
		catalogue.items[item.ID] = item
		base := "/api/v1/media/" + item.ID.String()

		list := send(s, http.MethodGet, "/api/v1/trips/"+trips.trip.ID.String()+"/media", "good", "")
		want := http.StatusOK
		if role == "" {
			want = http.StatusNotFound
		}
		if list.Code != want {
			t.Errorf("%q lists files: %d %s", role, list.Code, list.Body.String())
		}

		patch := send(s, http.MethodPatch, base, "good", `{"is_private":true}`)
		switch role {
		case domain.RoleEditor, domain.RoleOwner:
			want = http.StatusOK
		case domain.RoleViewer:
			want = http.StatusForbidden
		default:
			want = http.StatusNotFound
		}
		if patch.Code != want {
			t.Errorf("%q marks a file private: %d %s", role, patch.Code, patch.Body.String())
		}

		remove := send(s, http.MethodDelete, base, "good", "")
		if role == domain.RoleEditor || role == domain.RoleOwner {
			want = http.StatusNoContent
		}
		if remove.Code != want {
			t.Errorf("%q deletes a file: %d %s", role, remove.Code, remove.Body.String())
		}
	}
}

// TestSharedMediaHidesPrivateFiles checks the rule a read-only link turns on:
// a private file is served only through a link created to show private files,
// and a file of another trip is never served at all.
func TestSharedMediaHidesPrivateFiles(t *testing.T) {
	for _, includePrivate := range []bool{false, true} {
		s, trips, catalogue, files := newMediaServer(domain.RoleOwner)
		trips.shares = map[string]domain.ShareLink{}
		data := picture(t, 20, 20)

		open := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "open.png", data, false))[0]
		// Another picture: the same bytes twice would be refused as a copy.
		hidden := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "hidden.png", picture(t, 21, 21), true))[0]
		if !hidden.IsPrivate {
			t.Fatal("the private flag of the upload was not stored")
		}

		body := `{"include_private_media":false}`
		if includePrivate {
			body = `{"include_private_media":true}`
		}
		link := createLink(t, s, trips.trip.ID.String(), body)

		if recorder := sendShared(s, "/api/v1/shared/media/"+open.ID, link.Token); recorder.Code != http.StatusOK {
			t.Errorf("a public file through a link: %d %s", recorder.Code, recorder.Body.String())
		}
		want := http.StatusNotFound
		if includePrivate {
			want = http.StatusOK
		}
		if recorder := sendShared(s, "/api/v1/shared/media/"+hidden.ID, link.Token); recorder.Code != want {
			t.Errorf("a private file through a link with private=%v: %d %s",
				includePrivate, recorder.Code, recorder.Body.String())
		}

		// A file of another trip is not part of what the link opens.
		other := domain.Media{ID: uuid.New(), TripID: uuid.New(), StorageKey: "other", MIME: "image/png",
			Size: 10, Status: domain.MediaReady}
		catalogue.items[other.ID] = other
		files.files["other"] = data
		if recorder := sendShared(s, "/api/v1/shared/media/"+other.ID.String(), link.Token); recorder.Code != http.StatusNotFound {
			t.Errorf("a file of another trip: %d %s", recorder.Code, recorder.Body.String())
		}
	}
}

// TestThumbnailServesOfferedSizesOnly checks a preview is rendered at the
// offered widths and refused for anything else, so the sizes stay a closed set.
// The file is uploaded by an editor and read back by a viewer, which is how a
// shared trip is used.
func TestThumbnailServesOfferedSizesOnly(t *testing.T) {
	s, trips, _, _ := newMediaServer(domain.RoleEditor)
	stored := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "wide.png", picture(t, 600, 300), false))[0]
	trips.trip.Role = domain.RoleViewer
	base := "/api/v1/media/" + stored.ID + "/thumbnail"

	recorder := send(s, http.MethodGet, base+"?size=320", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("a preview at an offered size: %d %s", recorder.Code, recorder.Body.String())
	}
	if kind := recorder.Header().Get("Content-Type"); kind != "image/jpeg" {
		t.Errorf("preview served as %q, want image/jpeg", kind)
	}
	tag := recorder.Header().Get("ETag")
	if tag == "" {
		t.Fatal("a preview came without an ETag, so a browser would fetch it again every time")
	}

	// A browser that already holds the preview is told so rather than sent it
	// a second time.
	request := httptest.NewRequest(http.MethodGet, base+"?size=320", nil)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("If-None-Match", tag)
	cached := httptest.NewRecorder()
	s.routes().ServeHTTP(cached, request)
	if cached.Code != http.StatusNotModified {
		t.Errorf("a preview the browser already holds: %d", cached.Code)
	}

	if recorder := send(s, http.MethodGet, base+"?size=321", "good", ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a preview at a size that is not offered: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestPreviewIsKeptAndGoesWithItsFile checks a preview is rendered once and
// read back from the store afterwards, and that deleting the file takes its
// previews with it: a preview left behind would keep a copy of a removed
// photograph on the disk.
func TestPreviewIsKeptAndGoesWithItsFile(t *testing.T) {
	s, trips, catalogue, files := newMediaServer(domain.RoleEditor)
	stored := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "wide.png", picture(t, 600, 300), false))[0]
	id := uuid.MustParse(stored.ID)
	key := media.PreviewKey(catalogue.items[id].StorageKey, 320)
	base := "/api/v1/media/" + stored.ID + "/thumbnail?size=320"
	// A preview the first renderer made, named without a version.
	legacy := ".previews/" + catalogue.items[id].StorageKey + "@320.jpg"
	files.files[legacy] = []byte("blurry preview")

	if recorder := send(s, http.MethodGet, base, "good", ""); recorder.Code != http.StatusOK {
		t.Fatalf("render a preview: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, ok := files.files[key]; !ok {
		t.Fatal("the rendered preview was not kept in the store")
	}
	if _, ok := files.files[legacy]; ok {
		t.Error("the preview of the older renderer stayed beside the new one")
	}
	// A preview asked for at one width renders the others with it, so the
	// original is read from the store once.
	if !files.hasPreviews(catalogue.items[id].StorageKey) {
		t.Error("a preview asked for at one width did not keep the others")
	}

	// The kept preview is what the next reader gets, not a new render.
	files.files[key] = []byte("kept preview")
	if recorder := send(s, http.MethodGet, base, "good", ""); recorder.Body.String() != "kept preview" {
		t.Errorf("the second read rendered the preview again instead of reading the kept one")
	}

	if recorder := send(s, http.MethodDelete, "/api/v1/media/"+stored.ID, "good", ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete the file: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(files.files) != 0 {
		t.Errorf("the store still holds %d objects of a deleted file", len(files.files))
	}
}

// TestBackgroundRendersPreviews checks a running server renders the previews of
// a picture stored before it started without being asked, and those of a fresh
// upload before anybody opens the gallery.
func TestBackgroundRendersPreviews(t *testing.T) {
	s, trips, catalogue, files := newMediaServer(domain.RoleEditor)
	s.workContext = t.Context()

	older := domain.Media{ID: uuid.New(), TripID: trips.trip.ID, StorageKey: "older.png", Status: domain.MediaReady}
	catalogue.items[older.ID] = older
	files.files[older.StorageKey] = picture(t, 300, 200)

	s.startPreviewWork()
	waitFor(t, "previews of a picture stored earlier", func() bool {
		return files.hasPreviews(older.StorageKey) && !s.backfillRunning.Load()
	})

	stored := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "wide.png", picture(t, 600, 300), false))[0]
	key := catalogue.items[uuid.MustParse(stored.ID)].StorageKey
	waitFor(t, "previews of a fresh upload", func() bool {
		return files.hasPreviews(key)
	})

	// The progress is the administrators' business alone.
	if recorder := send(s, http.MethodGet, "/api/v1/admin/previews", "good", ""); recorder.Code != http.StatusForbidden {
		t.Fatalf("the preview progress read by an editor: %d", recorder.Code)
	}
	s.auth.(*fakeAuth).user.IsAdmin = true
	var status previewStatusResponse
	waitFor(t, "the upload's wave to end", func() bool {
		recorder := send(s, http.MethodGet, "/api/v1/admin/previews", "good", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("read the preview progress: %d %s", recorder.Code, recorder.Body.String())
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode the preview progress: %v", err)
		}
		return !status.Active
	})
	// The walk and the upload were two waves: the upload arrived at an idle
	// background, so the bar measures it alone.
	if !status.Background || status.Total != 1 || status.Done != 1 || status.StartedAt == nil {
		t.Errorf("the last wave is %+v, want one file done", status)
	}
	if status.Rendered != 2 || status.Failed != 0 || status.Rendering != 0 || status.Queued != 0 {
		t.Errorf("the counters are %+v, want two files rendered and nothing left", status)
	}
	if status.Backfill.Running || status.Backfill.Checked != 1 || status.Backfill.Total != 1 {
		t.Errorf("the walk is %+v, want it over after one file", status.Backfill)
	}
}

// TestUploadQueueKeepsEveryUpload checks an import faster than the rendering
// loses nothing: every upload waits its turn, however many there are, and the
// progress counts each one until the last is done.
func TestUploadQueueKeepsEveryUpload(t *testing.T) {
	s, trips, catalogue, files := newMediaServer(domain.RoleEditor)
	s.workContext = t.Context()
	// The workers are held back until everything is uploaded, as they are when
	// a small server falls behind an import.
	s.previewsRunning.Store(true)

	const uploads = 300
	for i := range uploads {
		// Each upload is another picture: a trip refuses the same one twice.
		recorder := upload(t, s, trips.trip.ID.String(), "photo.png", picture(t, 8+i%20, 8+i/20), false)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("upload %d: %d %s", i, recorder.Code, recorder.Body.String())
		}
	}
	if queued := s.uploads.length(); queued != uploads {
		t.Fatalf("%d uploads are queued, want all %d", queued, uploads)
	}

	for range s.previewWorkers {
		go s.renderQueuedPreviews()
	}
	waitFor(t, "every upload's previews", func() bool {
		s.progress.mu.Lock()
		defer s.progress.mu.Unlock()
		return s.progress.waveDone == s.progress.waveTotal
	})
	if s.progress.waveTotal != uploads || s.progress.rendered != uploads {
		t.Errorf("the wave counted %d files and rendered %d, want %d", s.progress.waveTotal, s.progress.rendered, uploads)
	}
	for _, item := range catalogue.items {
		if !files.hasPreviews(item.StorageKey) {
			t.Errorf("%s has no previews", item.StorageKey)
		}
	}
}

// TestRenderWorkersFollowTheProcessors checks the background takes half the
// processors and the renderings leave room above it, down to a single one.
func TestRenderWorkersFollowTheProcessors(t *testing.T) {
	for _, tc := range []struct{ processors, background, total int }{
		{1, 1, 3},
		{2, 1, 3},
		{4, 2, 4},
		{16, 8, 10},
	} {
		background, total := renderWorkers(tc.processors)
		if background != tc.background || total != tc.total {
			t.Errorf("%d processors gave %d workers and %d renderings, want %d and %d",
				tc.processors, background, total, tc.background, tc.total)
		}
	}
}

// waitFor polls a condition the background is expected to make true.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSetMediaLinksBuildsAGallery checks a day shows exactly the files it is
// given, in the order they were taken, and refuses a file of another trip.
func TestSetMediaLinksBuildsAGallery(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleEditor)
	first := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "a.png", picture(t, 10, 10), false))[0]
	second := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "b.png", picture(t, 12, 12), false))[0]

	dayID := uuid.New()
	catalogue.targets[dayID] = trips.trip.ID
	body := `{"target_type":"day","target_id":"` + dayID.String() + `","media_ids":["` + second.ID + `","` + first.ID + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusNoContent {
		t.Fatalf("set a gallery: %d %s", recorder.Code, recorder.Body.String())
	}
	links := catalogue.links[dayID]
	if len(links) != 2 || links[0].MediaID.String() != second.ID || links[1].MediaID.String() != first.ID {
		t.Errorf("the gallery is %+v, want the two files in the order they were given", links)
	}

	// However they were linked, the day shows its pictures in the order they
	// were taken.
	earlier, later := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 6, 20, 17, 0, 0, 0, time.UTC)
	for id, taken := range map[string]time.Time{first.ID: later, second.ID: earlier} {
		item := catalogue.items[uuid.MustParse(id)]
		item.TakenAt = &taken
		catalogue.items[item.ID] = item
	}
	body = `{"target_type":"day","target_id":"` + dayID.String() + `","media_ids":["` + first.ID + `","` + second.ID + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusNoContent {
		t.Fatalf("relink the gallery: %d %s", recorder.Code, recorder.Body.String())
	}
	pictures, err := s.galleryOf(context.Background(), trips.trip.ID, true)
	if err != nil {
		t.Fatalf("read the gallery: %v", err)
	}
	if shown := pictures.of(dayID); len(shown) != 2 || shown[0].ID != second.ID || shown[1].ID != first.ID {
		t.Errorf("the day shows %+v, want the picture taken first to come first", shown)
	}

	// A file of another trip has nothing to do with this day.
	stranger := domain.Media{ID: uuid.New(), TripID: uuid.New(), Status: domain.MediaReady}
	catalogue.items[stranger.ID] = stranger
	body = `{"target_type":"day","target_id":"` + dayID.String() + `","media_ids":["` + stranger.ID.String() + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a file of another trip: %d %s", recorder.Code, recorder.Body.String())
	}

	// A target nobody knows is missing, not forbidden.
	body = `{"target_type":"day","target_id":"` + uuid.NewString() + `","media_ids":[]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusNotFound {
		t.Errorf("an unknown day: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestFavoritesChooseWhatTheReportShows checks the two ways a favourite is
// marked - naming them beside the gallery of one place, and marking files
// chosen in the gallery of the whole trip - and the rules around both: seven at
// a time, a report rather than a plan, and only where the file already hangs.
func TestFavoritesChooseWhatTheReportShows(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleEditor)
	first := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "a.png", picture(t, 10, 10), false))[0]
	second := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "b.png", picture(t, 12, 12), false))[0]

	dayID := uuid.New()
	catalogue.targets[dayID] = trips.trip.ID
	// The trip itself is a target as well, to reach the rule that its gallery
	// has no favourites rather than the answer that it does not exist.
	catalogue.targets[trips.trip.ID] = trips.trip.ID
	gallery := `"media_ids":["` + first.ID + `","` + second.ID + `"]`
	body := `{"target_type":"day","target_id":"` + dayID.String() + `",` + gallery +
		`,"favorite_media_ids":["` + second.ID + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusNoContent {
		t.Fatalf("mark a favourite: %d %s", recorder.Code, recorder.Body.String())
	}
	if links := catalogue.links[dayID]; len(links) != 2 || links[0].IsFavorite || !links[1].IsFavorite {
		t.Fatalf("the gallery is %+v, want only the second file marked", links)
	}

	// Reordering the gallery without naming favourites keeps the one marked.
	body = `{"target_type":"day","target_id":"` + dayID.String() + `","media_ids":["` + second.ID + `","` + first.ID + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusNoContent {
		t.Fatalf("reorder the gallery: %d %s", recorder.Code, recorder.Body.String())
	}
	if links := catalogue.links[dayID]; len(links) != 2 || !links[0].IsFavorite || links[1].IsFavorite {
		t.Errorf("after a reorder the gallery is %+v, want the same file still marked", links)
	}

	// The gallery of the whole trip marks files rather than places.
	body = `{"media_ids":["` + first.ID + `"],"favorite":true}`
	if recorder := send(s, http.MethodPost, "/api/v1/media-favorites", "good", body); recorder.Code != http.StatusNoContent {
		t.Fatalf("mark a batch: %d %s", recorder.Code, recorder.Body.String())
	}
	favorites := 0
	for _, link := range catalogue.links[dayID] {
		if link.IsFavorite {
			favorites++
		}
	}
	if favorites != 2 {
		t.Errorf("%d files are marked, want both", favorites)
	}

	// More than a report shows, a file that hangs elsewhere, and a trip's own
	// gallery are all refused.
	tooMany := make([]string, 0, domain.MediaFavoriteLimit+1)
	for range domain.MediaFavoriteLimit + 1 {
		tooMany = append(tooMany, `"`+second.ID+`"`)
	}
	refusals := map[string]string{
		"over the limit": `{"target_type":"day","target_id":"` + dayID.String() + `",` + gallery +
			`,"favorite_media_ids":[` + strings.Join(tooMany, ",") + `]}`,
		"a file that hangs elsewhere": `{"target_type":"day","target_id":"` + dayID.String() + `",` + gallery +
			`,"favorite_media_ids":["` + uuid.NewString() + `"]}`,
		"the gallery of a trip": `{"target_type":"trip","target_id":"` + trips.trip.ID.String() + `",` + gallery +
			`,"favorite_media_ids":["` + second.ID + `"]}`,
	}
	for name, refused := range refusals {
		if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", refused); recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}

	// A refused request leaves the gallery alone: the links and the favourites
	// are stored one after the other, and a half-applied call would empty a day
	// while saying it changed nothing.
	body = `{"target_type":"day","target_id":"` + dayID.String() + `","media_ids":[],"favorite_media_ids":["` + first.ID + `"]}`
	if recorder := send(s, http.MethodPut, "/api/v1/media-links", "good", body); recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a favourite outside the gallery it is sent with: %d %s", recorder.Code, recorder.Body.String())
	}
	if links := catalogue.links[dayID]; len(links) != 2 {
		t.Errorf("after the refusal the gallery holds %d files, want both still there", len(links))
	}
}

// TestFavoritesNeedEditingRights checks a reader who may only look cannot
// choose what the report shows.
func TestFavoritesNeedEditingRights(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleViewer)
	item := domain.Media{ID: uuid.New(), TripID: trips.trip.ID, Status: domain.MediaReady}
	catalogue.items[item.ID] = item

	body := `{"media_ids":["` + item.ID.String() + `"],"favorite":true}`
	if recorder := send(s, http.MethodPost, "/api/v1/media-favorites", "good", body); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer marking a favourite: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestSharedGalleryHidesPrivateFiles checks a link without private files sees
// neither them in a gallery nor their identifier as a cover: a reference the
// reader cannot fetch would only render as a broken picture.
func TestSharedGalleryHidesPrivateFiles(t *testing.T) {
	s, trips, catalogue, _ := newMediaServer(domain.RoleOwner)
	open := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "open.png", picture(t, 20, 20), false))[0]
	hidden := uploadedIDs(t, upload(t, s, trips.trip.ID.String(), "hidden.png", picture(t, 21, 21), true))[0]

	dayID := uuid.New()
	catalogue.targets[dayID] = trips.trip.ID
	if err := catalogue.SetLinks(context.Background(), domain.MediaTargetDay, dayID,
		[]uuid.UUID{uuid.MustParse(hidden.ID), uuid.MustParse(open.ID)}); err != nil {
		t.Fatalf("build the gallery: %v", err)
	}

	hiddenCover := uuid.MustParse(hidden.ID)
	for _, includePrivate := range []bool{false, true} {
		pictures, err := s.galleryOf(context.Background(), trips.trip.ID, includePrivate)
		if err != nil {
			t.Fatalf("read the gallery: %v", err)
		}
		want := 1
		if includePrivate {
			want = 2
		}
		if got := len(pictures.of(dayID)); got != want {
			t.Errorf("a link with private=%v sees %d files in the gallery, want %d", includePrivate, got, want)
		}
		cover := pictures.coverID(&hiddenCover)
		if includePrivate != (cover != nil) {
			t.Errorf("a link with private=%v sees the private cover as %v", includePrivate, cover)
		}
	}
}

// TestDeletingATripTakesItsFilesWithIt checks the bytes go with the rows: a
// trip that is gone must not leave its photographs on the disk for good, since
// nothing can reach them afterwards.
func TestDeletingATripTakesItsFilesWithIt(t *testing.T) {
	s, trips, _, files := newMediaServer(domain.RoleOwner)
	upload(t, s, trips.trip.ID.String(), "one.png", picture(t, 16, 16), false)
	upload(t, s, trips.trip.ID.String(), "two.png", picture(t, 17, 17), true)
	if len(files.files) != 2 {
		t.Fatalf("the store holds %d files before the deletion, want 2", len(files.files))
	}
	// The repository hands the keys back; the fake plays the same part.
	trips.mediaKeys = []string{}
	for key := range files.files {
		trips.mediaKeys = append(trips.mediaKeys, key)
	}

	if recorder := send(s, http.MethodDelete, "/api/v1/trips/"+trips.trip.ID.String(), "good", ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete the trip: %d %s", recorder.Code, recorder.Body.String())
	}
	if !trips.deleted {
		t.Error("the trip was not deleted")
	}
	if len(files.files) != 0 {
		t.Errorf("the store still holds %d files of a deleted trip", len(files.files))
	}
}
