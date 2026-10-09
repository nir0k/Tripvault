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

// fakeIdeas keeps ideas in memory, each read only by its owner and the
// members of the owner's list, as the repository does.
type fakeIdeas struct {
	ideas   []domain.Idea
	tags    map[uuid.UUID][]uuid.UUID
	members map[uuid.UUID]map[uuid.UUID]domain.TripRole
	changes []domain.IdeaChange
}

// role tells what a reader may do with an owner's list, empty for nothing.
func (f *fakeIdeas) role(readerID, ownerID uuid.UUID) domain.TripRole {
	if readerID == ownerID {
		return domain.RoleOwner
	}
	return f.members[ownerID][readerID]
}

// find returns the index of an idea the reader may open, or -1.
func (f *fakeIdeas) find(readerID, id uuid.UUID) int {
	return slices.IndexFunc(f.ideas, func(idea domain.Idea) bool {
		return idea.ID == id && f.role(readerID, idea.OwnerID) != ""
	})
}

// editable returns the index of an idea the actor may change, or -1.
func (f *fakeIdeas) editable(actorID, id uuid.UUID) int {
	index := f.find(actorID, id)
	if index >= 0 && !f.role(actorID, f.ideas[index].OwnerID).Can(domain.ActionEdit) {
		return -1
	}
	return index
}

// log records a change of an idea.
func (f *fakeIdeas) log(idea domain.Idea, actorID uuid.UUID, action domain.IdeaAction) {
	id := idea.ID
	f.changes = append(f.changes, domain.IdeaChange{ID: uuid.New(), OwnerID: idea.OwnerID, IdeaID: &id,
		IdeaTitle: idea.Title, User: &domain.TripUser{ID: actorID}, Action: action})
}

// List returns the ideas the reader may open, with the reader's role.
func (f *fakeIdeas) List(_ context.Context, readerID uuid.UUID) ([]domain.Idea, error) {
	var shown []domain.Idea
	for _, idea := range f.ideas {
		if role := f.role(readerID, idea.OwnerID); role != "" {
			idea.Role = role
			shown = append(shown, idea)
		}
	}
	return shown, nil
}

// Get returns an idea the reader may open, with the reader's role.
func (f *fakeIdeas) Get(_ context.Context, readerID, id uuid.UUID) (domain.Idea, error) {
	if index := f.find(readerID, id); index >= 0 {
		idea := f.ideas[index]
		idea.Role = f.role(readerID, idea.OwnerID)
		return idea, nil
	}
	return domain.Idea{}, domain.ErrNotFound
}

// Lists returns the reader's own list and the ones shared with them.
func (f *fakeIdeas) Lists(_ context.Context, readerID uuid.UUID) ([]domain.IdeaList, error) {
	lists := []domain.IdeaList{{Owner: domain.TripUser{ID: readerID}, Role: domain.RoleOwner}}
	for ownerID, members := range f.members {
		if role, ok := members[readerID]; ok {
			lists = append(lists, domain.IdeaList{Owner: domain.TripUser{ID: ownerID}, Role: role})
		}
	}
	return lists, nil
}

// ListRole returns the reader's role on a list.
func (f *fakeIdeas) ListRole(_ context.Context, readerID, ownerID uuid.UUID) (domain.TripRole, error) {
	if role := f.role(readerID, ownerID); role != "" {
		return role, nil
	}
	return "", domain.ErrNotFound
}

// Create stores an idea written by the actor.
func (f *fakeIdeas) Create(_ context.Context, actorID uuid.UUID, idea domain.Idea) error {
	if !f.role(actorID, idea.OwnerID).Can(domain.ActionEdit) {
		return domain.ErrNotFound
	}
	idea.CreatedBy = &domain.TripUser{ID: actorID}
	idea.UpdatedBy = idea.CreatedBy
	f.ideas = append(f.ideas, idea)
	f.log(idea, actorID, domain.IdeaCreated)
	return nil
}

// Update replaces an idea the actor may change.
func (f *fakeIdeas) Update(_ context.Context, actorID uuid.UUID, idea domain.Idea) error {
	index := f.editable(actorID, idea.ID)
	if index < 0 {
		return domain.ErrNotFound
	}
	idea.CreatedBy = f.ideas[index].CreatedBy
	idea.UpdatedBy = &domain.TripUser{ID: actorID}
	f.ideas[index] = idea
	f.log(idea, actorID, domain.IdeaUpdated)
	return nil
}

// Delete removes an idea the actor may change and names its photos' files.
func (f *fakeIdeas) Delete(_ context.Context, actorID, id uuid.UUID) ([]string, error) {
	index := f.editable(actorID, id)
	if index < 0 {
		return nil, domain.ErrNotFound
	}
	var keys []string
	for _, photo := range f.ideas[index].Photos {
		keys = append(keys, photo.Key, photo.ThumbKey)
	}
	f.log(f.ideas[index], actorID, domain.IdeaDeleted)
	f.ideas = slices.Delete(f.ideas, index, index+1)
	return keys, nil
}

// AddPhoto puts a photo at the end of an idea the actor may change, ten at most.
func (f *fakeIdeas) AddPhoto(_ context.Context, actorID uuid.UUID, photo domain.IdeaPhoto) error {
	index := f.editable(actorID, photo.IdeaID)
	if index < 0 {
		return domain.ErrNotFound
	}
	if len(f.ideas[index].Photos) >= domain.MaxIdeaPhotos {
		return domain.NewValidationError("photos", "too_many", "an idea keeps at most 10 photos")
	}
	f.ideas[index].Photos = append(f.ideas[index].Photos, photo)
	return nil
}

// DeletePhoto removes a photo of an idea the actor may change.
func (f *fakeIdeas) DeletePhoto(_ context.Context, actorID, ideaID, photoID uuid.UUID) (domain.IdeaPhoto, error) {
	index := f.editable(actorID, ideaID)
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

// SetTags records the tags of an idea the reader may open.
func (f *fakeIdeas) SetTags(_ context.Context, readerID, id uuid.UUID, tagIDs []uuid.UUID) error {
	if f.find(readerID, id) < 0 {
		return domain.ErrNotFound
	}
	if f.tags == nil {
		f.tags = map[uuid.UUID][]uuid.UUID{}
	}
	f.tags[id] = tagIDs
	return nil
}

// History returns the changes of a list, or of one idea of it.
func (f *fakeIdeas) History(_ context.Context, ownerID uuid.UUID, ideaID *uuid.UUID) ([]domain.IdeaChange, error) {
	var changes []domain.IdeaChange
	for _, change := range f.changes {
		if change.OwnerID == ownerID && (ideaID == nil || *change.IdeaID == *ideaID) {
			changes = append(changes, change)
		}
	}
	return changes, nil
}

// Members returns the members of an owner's list.
func (f *fakeIdeas) Members(_ context.Context, ownerID uuid.UUID) ([]domain.IdeaMember, error) {
	var members []domain.IdeaMember
	for userID, role := range f.members[ownerID] {
		members = append(members, domain.IdeaMember{OwnerID: ownerID, User: domain.TripUser{ID: userID}, Role: role})
	}
	return members, nil
}

// AddMember shares an owner's list with a person.
func (f *fakeIdeas) AddMember(_ context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error) {
	if f.role(userID, ownerID) != "" {
		return domain.IdeaMember{}, domain.ErrAlreadyMember
	}
	if f.members == nil {
		f.members = map[uuid.UUID]map[uuid.UUID]domain.TripRole{}
	}
	if f.members[ownerID] == nil {
		f.members[ownerID] = map[uuid.UUID]domain.TripRole{}
	}
	f.members[ownerID][userID] = role
	return domain.IdeaMember{OwnerID: ownerID, User: domain.TripUser{ID: userID}, Role: role}, nil
}

// UpdateMember changes a member's role.
func (f *fakeIdeas) UpdateMember(_ context.Context, ownerID, userID uuid.UUID, role domain.TripRole) (domain.IdeaMember, error) {
	if _, ok := f.members[ownerID][userID]; !ok {
		return domain.IdeaMember{}, domain.ErrNotFound
	}
	f.members[ownerID][userID] = role
	return domain.IdeaMember{OwnerID: ownerID, User: domain.TripUser{ID: userID}, Role: role}, nil
}

// RemoveMember takes a member off a list.
func (f *fakeIdeas) RemoveMember(_ context.Context, ownerID, userID uuid.UUID) error {
	if _, ok := f.members[ownerID][userID]; !ok {
		return domain.ErrNotFound
	}
	delete(f.members[ownerID], userID)
	return nil
}

// newIdeaServer builds a server whose signed-in person keeps ideas in memory.
func newIdeaServer() (*Server, *fakeIdeas, uuid.UUID) {
	ideas := &fakeIdeas{}
	user := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "ISK"}
	s := newHandlerServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
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
	s := newHandlerServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
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

// TestSharedIdeasOverHTTP checks the owner of a list of ideas shares it: a
// viewer reads it and its history but changes nothing, an editor adds an idea
// to it and changes and deletes the owner's, a person it is not shared with
// finds nothing, and only the owner decides who is a member.
func TestSharedIdeasOverHTTP(t *testing.T) {
	ideas := &fakeIdeas{}
	owner := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	editor := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	viewer := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	stranger := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	signedIn := &fakeAuth{user: owner}
	s := newHandlerServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth: signedIn, Users: fakeUsers{}, Ideas: ideas,
	})
	as := func(user domain.User, method, path, body string) *httptest.ResponseRecorder {
		signedIn.user = user
		return send(s, method, path, "good", body)
	}

	idea := decodeIdea(t, as(owner, http.MethodPost, "/api/v1/ideas", `{"title":"Lofoten"}`).Body.Bytes())
	path := "/api/v1/ideas/" + idea.ID
	for user, role := range map[domain.User]string{editor: "editor", viewer: "viewer"} {
		if recorder := as(owner, http.MethodPost, "/api/v1/ideas/members", `{"user_id":"`+user.ID.String()+`","role":"`+role+`"}`); recorder.Code != http.StatusCreated {
			t.Fatalf("share with the %s: %d %s", role, recorder.Code, recorder.Body.String())
		}
	}
	if recorder := as(owner, http.MethodPost, "/api/v1/ideas/members", `{"user_id":"`+viewer.ID.String()+`","role":"editor"}`); recorder.Code != http.StatusConflict {
		t.Errorf("share twice: %d", recorder.Code)
	}

	// The viewer reads the idea and the history, and changes nothing.
	if read := as(viewer, http.MethodGet, path, ""); read.Code != http.StatusOK || decodeIdea(t, read.Body.Bytes()).Role != "viewer" {
		t.Errorf("the viewer reads: %d %s", read.Code, read.Body.String())
	}
	for _, request := range [][3]string{{http.MethodPut, path, `{"title":"Mine now"}`}, {http.MethodDelete, path, ""},
		{http.MethodPost, "/api/v1/ideas", `{"title":"X","owner_id":"` + owner.ID.String() + `"}`}} {
		if recorder := as(viewer, request[0], request[1], request[2]); recorder.Code != http.StatusForbidden {
			t.Errorf("the viewer %s %s: %d", request[0], request[1], recorder.Code)
		}
	}
	if recorder := as(viewer, http.MethodPut, path+"/tags", `{"tag_ids":[]}`); recorder.Code != http.StatusOK {
		t.Errorf("the viewer tags: %d", recorder.Code)
	}
	if recorder := as(viewer, http.MethodPost, "/api/v1/ideas/members", `{"user_id":"`+stranger.ID.String()+`","role":"viewer"}`); recorder.Code != http.StatusCreated {
		t.Errorf("the viewer shares their own list: %d", recorder.Code)
	}
	if len(ideas.members[owner.ID]) != 2 {
		t.Errorf("a viewer changed the owner's members: %+v", ideas.members[owner.ID])
	}

	// The editor adds an idea to the owner's list and changes the owner's.
	added := as(editor, http.MethodPost, "/api/v1/ideas", `{"title":"Senja","owner_id":"`+owner.ID.String()+`"}`)
	if added.Code != http.StatusCreated {
		t.Fatalf("the editor adds: %d %s", added.Code, added.Body.String())
	}
	if created := decodeIdea(t, added.Body.Bytes()); created.Role != "editor" || created.CreatedBy == nil ||
		created.CreatedBy.ID != editor.ID.String() || ideas.ideas[1].OwnerID != owner.ID {
		t.Errorf("the editor's idea: %+v", created)
	}
	if recorder := as(editor, http.MethodPut, path, `{"title":"Lofoten in winter"}`); recorder.Code != http.StatusOK ||
		decodeIdea(t, recorder.Body.Bytes()).UpdatedBy.ID != editor.ID.String() {
		t.Errorf("the editor changes: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := as(editor, http.MethodPost, "/api/v1/ideas/members", `{"user_id":"`+stranger.ID.String()+`","role":"viewer"}`); recorder.Code != http.StatusCreated ||
		ideas.members[owner.ID][stranger.ID] != "" {
		t.Errorf("an editor shared the owner's list: %d", recorder.Code)
	}

	// A person the list is not shared with finds nothing of it.
	other := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	for _, request := range [][2]string{{http.MethodGet, path}, {http.MethodDelete, path},
		{http.MethodGet, "/api/v1/ideas/history?owner_id=" + owner.ID.String()}} {
		if recorder := as(other, request[0], request[1], ""); recorder.Code != http.StatusNotFound {
			t.Errorf("a stranger %s %s: %d", request[0], request[1], recorder.Code)
		}
	}
	var list listResponse[ideaResponse]
	if err := json.Unmarshal(as(other, http.MethodGet, "/api/v1/ideas", "").Body.Bytes(), &list); err != nil || len(list.Items) != 0 {
		t.Errorf("a stranger lists %+v: %v", list.Items, err)
	}

	var history listResponse[ideaChangeResponse]
	recorder := as(viewer, http.MethodGet, "/api/v1/ideas/history?idea_id="+idea.ID, "")
	if err := json.Unmarshal(recorder.Body.Bytes(), &history); err != nil || len(history.Items) != 2 ||
		history.Items[1].Action != "updated" || history.Items[1].User.ID != editor.ID.String() {
		t.Errorf("the idea's history: %d %s", recorder.Code, recorder.Body.String())
	}

	if recorder := as(editor, http.MethodDelete, path, ""); recorder.Code != http.StatusNoContent {
		t.Errorf("the editor deletes the owner's idea: %d", recorder.Code)
	}
	// The viewer leaves, and the list is theirs no more.
	if recorder := as(viewer, http.MethodDelete, "/api/v1/ideas/lists/"+owner.ID.String(), ""); recorder.Code != http.StatusNoContent {
		t.Errorf("the viewer leaves: %d", recorder.Code)
	}
	if recorder := as(viewer, http.MethodGet, "/api/v1/ideas/"+ideas.ideas[0].ID.String(), ""); recorder.Code != http.StatusNotFound {
		t.Errorf("a former viewer reads: %d", recorder.Code)
	}
	if recorder := as(owner, http.MethodDelete, "/api/v1/ideas/members/"+editor.ID.String(), ""); recorder.Code != http.StatusNoContent {
		t.Errorf("the owner removes the editor: %d", recorder.Code)
	}
	if recorder := as(editor, http.MethodPut, "/api/v1/ideas/"+ideas.ideas[0].ID.String(), `{"title":"Back"}`); recorder.Code != http.StatusNotFound {
		t.Errorf("a former editor changes: %d", recorder.Code)
	}
}
