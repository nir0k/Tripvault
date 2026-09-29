package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/media"
)

// fakeIdeas keeps ideas in memory, each read only by its owner, as the
// repository does.
type fakeIdeas struct {
	ideas []domain.Idea
	tags  map[uuid.UUID][]uuid.UUID
}

// find returns the index of an owner's idea, or -1.
func (f *fakeIdeas) find(ownerID, id uuid.UUID) int {
	return slices.IndexFunc(f.ideas, func(idea domain.Idea) bool { return idea.ID == id && idea.OwnerID == ownerID })
}

// List returns the owner's ideas.
func (f *fakeIdeas) List(_ context.Context, ownerID uuid.UUID) ([]domain.Idea, error) {
	var owned []domain.Idea
	for _, idea := range f.ideas {
		if idea.OwnerID == ownerID {
			owned = append(owned, idea)
		}
	}
	return owned, nil
}

// Get returns one of the owner's ideas.
func (f *fakeIdeas) Get(_ context.Context, ownerID, id uuid.UUID) (domain.Idea, error) {
	if index := f.find(ownerID, id); index >= 0 {
		return f.ideas[index], nil
	}
	return domain.Idea{}, domain.ErrNotFound
}

// Create stores an idea.
func (f *fakeIdeas) Create(_ context.Context, idea domain.Idea) error {
	f.ideas = append(f.ideas, idea)
	return nil
}

// Update replaces one of the owner's ideas.
func (f *fakeIdeas) Update(_ context.Context, idea domain.Idea) error {
	index := f.find(idea.OwnerID, idea.ID)
	if index < 0 {
		return domain.ErrNotFound
	}
	f.ideas[index] = idea
	return nil
}

// Delete removes one of the owner's ideas and names its photos' files.
func (f *fakeIdeas) Delete(_ context.Context, ownerID, id uuid.UUID) ([]string, error) {
	index := f.find(ownerID, id)
	if index < 0 {
		return nil, domain.ErrNotFound
	}
	var keys []string
	for _, photo := range f.ideas[index].Photos {
		keys = append(keys, photo.Key, photo.ThumbKey)
	}
	f.ideas = slices.Delete(f.ideas, index, index+1)
	return keys, nil
}

// AddPhoto puts a photo at the end of one of the owner's ideas, ten at most.
func (f *fakeIdeas) AddPhoto(_ context.Context, ownerID uuid.UUID, photo domain.IdeaPhoto) error {
	index := f.find(ownerID, photo.IdeaID)
	if index < 0 {
		return domain.ErrNotFound
	}
	if len(f.ideas[index].Photos) >= domain.MaxIdeaPhotos {
		return domain.NewValidationError("photos", "too_many", "an idea keeps at most 10 photos")
	}
	f.ideas[index].Photos = append(f.ideas[index].Photos, photo)
	return nil
}

// DeletePhoto removes a photo of one of the owner's ideas.
func (f *fakeIdeas) DeletePhoto(_ context.Context, ownerID, ideaID, photoID uuid.UUID) (domain.IdeaPhoto, error) {
	index := f.find(ownerID, ideaID)
	if index < 0 {
		return domain.IdeaPhoto{}, domain.ErrNotFound
	}
	photos := f.ideas[index].Photos
	at := slices.IndexFunc(photos, func(photo domain.IdeaPhoto) bool { return photo.ID == photoID })
	if at < 0 {
		return domain.IdeaPhoto{}, domain.ErrNotFound
	}
	photo := photos[at]
	f.ideas[index].Photos = slices.Delete(photos, at, at+1)
	return photo, nil
}

// SetTags records the tags of one of the owner's ideas.
func (f *fakeIdeas) SetTags(_ context.Context, ownerID, id uuid.UUID, tagIDs []uuid.UUID) error {
	if f.find(ownerID, id) < 0 {
		return domain.ErrNotFound
	}
	if f.tags == nil {
		f.tags = map[uuid.UUID][]uuid.UUID{}
	}
	f.tags[id] = tagIDs
	return nil
}

// newIdeaServer builds a server whose signed-in person keeps ideas in memory.
func newIdeaServer() (*Server, *fakeIdeas, uuid.UUID) {
	ideas := &fakeIdeas{}
	user := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "ISK"}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:  &fakeAuth{user: user},
		Users: fakeUsers{},
		Ideas: ideas,
	})
	return s, ideas, user.ID
}

// decodeIdea reads an idea response, failing the test on anything else.
func decodeIdea(t *testing.T, body []byte) ideaResponse {
	t.Helper()
	var idea ideaResponse
	if err := json.Unmarshal(body, &idea); err != nil {
		t.Fatalf("response %q is not an idea: %v", body, err)
	}
	return idea
}

// TestIdeasOverHTTP checks an idea is added in the owner's currency with its
// total worked out, saved whole, tagged, listed and deleted, and that
// somebody else's idea reads as missing.
func TestIdeasOverHTTP(t *testing.T) {
	s, ideas, owner := newIdeaServer()
	body := `{"title":"Westfjords","countries":["is"],"months":[7,6],"days_min":5,"days_max":9,"days_ideal":7,
		"places":[{"name":"Ísafjörður","lat":66.07,"lng":-23.13},{"name":" "}],
		"costs":{"stay":"450.50","food":null,"other":null},
		"transports":[{"modes":["flight"],"cost":"300","minutes":180},{"modes":["car","flight"],"cost":"520"}]}`
	recorder := send(s, http.MethodPost, "/api/v1/ideas", "good", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add an idea: %d %s", recorder.Code, recorder.Body.String())
	}
	idea := decodeIdea(t, recorder.Body.Bytes())
	if idea.Currency != "ISK" || idea.Visa != "not_needed" || idea.CostMin == nil || *idea.CostMin != "750.50" ||
		*idea.CostMax != "970.50" || *idea.TransportMin != "300.00" || len(idea.Places) != 1 ||
		!slices.Equal(idea.Countries, []string{"IS"}) || !slices.Equal(idea.Months, []int{6, 7}) ||
		!slices.Equal(idea.Transports[1].Modes, []string{"car", "flight"}) || *idea.DaysIdeal != 7 {
		t.Errorf("stored %+v", idea)
	}

	path := "/api/v1/ideas/" + idea.ID
	recorder = send(s, http.MethodPut, path, "good", `{"title":"Westfjords","currency":"EUR","visa":"needed"}`)
	if updated := decodeIdea(t, recorder.Body.Bytes()); recorder.Code != http.StatusOK || updated.Currency != "EUR" ||
		updated.CostMin != nil || len(updated.Months) != 0 || len(updated.Transports) != 0 || updated.Visa != "needed" {
		t.Errorf("save the idea: %d %s", recorder.Code, recorder.Body.String())
	}
	for _, bad := range []string{`{"title":""}`, `{"title":"X","days_min":8,"days_max":3}`,
		`{"title":"X","days_max":31}`, `{"title":"X","costs":{"stay":"-5"}}`, `{"title":"X","countries":["ISL"]}`,
		`{"title":"X","transports":[{"modes":[]}]}`, `{"title":"X","transports":[{"modes":["car"],"cost":"x"}]}`} {
		if recorder := send(s, http.MethodPut, path, "good", bad); recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", bad, recorder.Code, recorder.Body.String())
		}
	}

	tag := uuid.NewString()
	if recorder := send(s, http.MethodPut, path+"/tags", "good", `{"tag_ids":["`+tag+`","`+tag+`"]}`); recorder.Code != http.StatusOK ||
		len(ideas.tags[uuid.MustParse(idea.ID)]) != 1 {
		t.Errorf("tag the idea: %d %s", recorder.Code, recorder.Body.String())
	}

	var list listResponse[ideaResponse]
	recorder = send(s, http.MethodGet, "/api/v1/ideas", "good", "")
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Errorf("list: %d %s", recorder.Code, recorder.Body.String())
	}

	// An idea of somebody else is not there for the reader.
	stranger := domain.Idea{ID: uuid.New(), OwnerID: uuid.New(), Title: "Not mine", Currency: "EUR"}
	ideas.ideas = append(ideas.ideas, stranger)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if recorder := send(s, method, "/api/v1/ideas/"+stranger.ID.String(), "good", ""); recorder.Code != http.StatusNotFound {
			t.Errorf("%s somebody else's idea: %d", method, recorder.Code)
		}
	}

	if recorder := send(s, http.MethodDelete, path, "good", ""); recorder.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", recorder.Code, recorder.Body.String())
	}
	if owned, _ := ideas.List(context.Background(), owner); len(owned) != 0 {
		t.Errorf("the idea is still there: %+v", owned)
	}
}

// sendPhoto uploads a picture to an idea as the browser does.
func sendPhoto(t *testing.T, s *Server, ideaID string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatalf("create the file part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write the file part: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ideas/"+ideaID+"/photos", body)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// TestIdeaPhotosOverHTTP checks a picture becomes a photo of an idea with a
// preview, both served to the owner, that an idea keeps ten, and that the
// files go with the photo and with the idea.
func TestIdeaPhotosOverHTTP(t *testing.T) {
	ideas := &fakeIdeas{}
	files, err := media.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	user := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: &fakeAuth{user: user}, Users: fakeUsers{}, Ideas: ideas, MediaFiles: files,
		MediaMaxBytes: 1024 * 1024,
	})
	recorder := send(s, http.MethodPost, "/api/v1/ideas", "good", `{"title":"Westfjords"}`)
	idea := decodeIdea(t, recorder.Body.Bytes())

	recorder = sendPhoto(t, s, idea.ID, picture(t, 600, 400))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add a photo: %d %s", recorder.Code, recorder.Body.String())
	}
	withPhoto := decodeIdea(t, recorder.Body.Bytes())
	if len(withPhoto.Photos) != 1 || withPhoto.Photos[0].Width != 600 || withPhoto.Photos[0].Height != 400 {
		t.Fatalf("photos %+v", withPhoto.Photos)
	}
	path := "/api/v1/ideas/" + idea.ID + "/photos/" + withPhoto.Photos[0].ID
	for _, query := range []string{"", "?size=preview"} {
		got := send(s, http.MethodGet, path+query, "good", "")
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/jpeg" || got.Body.Len() == 0 {
			t.Errorf("read %q: %d %s", query, got.Code, got.Header().Get("Content-Type"))
		}
		if _, height, err := media.Dimensions(got.Body.Bytes()); query != "" && (err != nil || height != 320) {
			t.Errorf("the preview is %d high, want 480 wide and so 320 high: %v", height, err)
		}
	}
	if recorder := sendPhoto(t, s, idea.ID, []byte("not a picture")); recorder.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text as a photo: %d", recorder.Code)
	}

	stored := ideas.ideas[0].Photos[0]
	if recorder := send(s, http.MethodDelete, path, "good", ""); recorder.Code != http.StatusOK {
		t.Fatalf("delete the photo: %d %s", recorder.Code, recorder.Body.String())
	}
	for _, key := range []string{stored.Key, stored.ThumbKey} {
		if _, err := files.Open(t.Context(), key); !errors.Is(err, media.ErrNotFound) {
			t.Errorf("%s is still on disk: %v", key, err)
		}
	}

	for range domain.MaxIdeaPhotos {
		if recorder := sendPhoto(t, s, idea.ID, picture(t, 40, 30)); recorder.Code != http.StatusCreated {
			t.Fatalf("add a photo: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if recorder := sendPhoto(t, s, idea.ID, picture(t, 40, 30)); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("an eleventh photo: %d %s", recorder.Code, recorder.Body.String())
	}
	kept := ideas.ideas[0].Photos[0]
	if recorder := send(s, http.MethodDelete, "/api/v1/ideas/"+idea.ID, "good", ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete the idea: %d", recorder.Code)
	}
	if _, err := files.Open(t.Context(), kept.Key); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("the photo of a deleted idea is still on disk: %v", err)
	}
}
