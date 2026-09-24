package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// newAvatarServer builds a server whose token "good" signs in as one account,
// with a byte store to watch and an account store that remembers pictures.
func newAvatarServer(lastAdmin bool) (*Server, domain.User, fakeUsers, *memoryFiles) {
	user := domain.User{ID: uuid.New(), IsActive: true, DefaultCurrency: "EUR"}
	users := fakeUsers{
		avatars:   map[uuid.UUID]string{},
		deleted:   map[uuid.UUID]struct{}{},
		lastAdmin: lastAdmin,
	}
	files := &memoryFiles{files: map[string][]byte{}}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:          &fakeAuth{user: user},
		Users:         users,
		MediaFiles:    files,
		MediaMaxBytes: 1024 * 1024,
	})
	return s, user, users, files
}

// sendAvatar uploads one file the way a browser does.
func sendAvatar(t *testing.T, s *Server, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("build the upload: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write the upload: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close the upload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/me/avatar", &body)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// TestAvatarIsStoredRenderedAndServed checks the picture an account is shown by
// from both ends: what lands in the store is a rendered square rather than the
// bytes that were sent, and reading it back answers the same picture and then a
// 304 for a client that already holds it.
func TestAvatarIsStoredRenderedAndServed(t *testing.T) {
	s, user, users, files := newAvatarServer(false)

	sent := picture(t, 400, 400)
	recorder := sendAvatar(t, s, "face.png", sent)
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload an avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	var stored struct {
		HasAvatar bool `json:"has_avatar"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &stored); err != nil {
		t.Fatalf("read the account: %v", err)
	}
	if !stored.HasAvatar {
		t.Error("the account reports no picture after one was stored")
	}

	key := avatarKey(user.ID)
	kept, held := files.files[key]
	if !held {
		t.Fatalf("nothing was stored under %q, the store holds %d objects", key, len(files.files))
	}
	if bytes.Equal(kept, sent) {
		t.Error("the bytes that were sent were stored as they came, not rendered")
	}
	if users.avatars[user.ID] != key {
		t.Errorf("the account wears %q, want %q", users.avatars[user.ID], key)
	}

	read := send(s, http.MethodGet, "/api/v1/users/"+user.ID.String()+"/avatar", "good", "")
	if read.Code != http.StatusOK || read.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("read the avatar: %d %s", read.Code, read.Header().Get("Content-Type"))
	}
	if !bytes.Equal(read.Body.Bytes(), kept) {
		t.Error("what was read back is not what was stored")
	}

	// A client that already holds this picture is told so rather than sent it
	// again: an avatar keeps one address for the life of the account.
	tag := read.Header().Get("ETag")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+user.ID.String()+"/avatar", nil)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("If-None-Match", tag)
	again := httptest.NewRecorder()
	s.routes().ServeHTTP(again, request)
	if again.Code != http.StatusNotModified {
		t.Errorf("a client holding the picture: %d, want 304", again.Code)
	}
}

// TestAvatarRefusesWhatItCannotRender checks the kind of file is decided from
// its own bytes: a text file named as a picture is not one.
func TestAvatarRefusesWhatItCannotRender(t *testing.T) {
	s, user, _, files := newAvatarServer(false)

	recorder := sendAvatar(t, s, "face.png", []byte("this is not a picture at all"))
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Errorf("a text file as an avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(files.files) != 0 {
		t.Errorf("the store holds %d objects after a refusal, want none", len(files.files))
	}

	// An account without a picture answers 404, which is what the interface
	// draws its placeholder on.
	read := send(s, http.MethodGet, "/api/v1/users/"+user.ID.String()+"/avatar", "good", "")
	if read.Code != http.StatusNotFound {
		t.Errorf("an account without a picture: %d %s", read.Code, read.Body.String())
	}
}

// TestAvatarIsRemovedWithItsBytes checks taking the picture away leaves neither
// a row pointing at it nor the object behind it.
func TestAvatarIsRemovedWithItsBytes(t *testing.T) {
	s, user, users, files := newAvatarServer(false)

	if recorder := sendAvatar(t, s, "face.png", picture(t, 120, 120)); recorder.Code != http.StatusOK {
		t.Fatalf("upload an avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodDelete, "/api/v1/me/avatar", "good", ""); recorder.Code != http.StatusOK {
		t.Fatalf("remove the avatar: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, held := users.avatars[user.ID]; held {
		t.Error("the account still wears a picture")
	}
	if len(files.files) != 0 {
		t.Errorf("the store holds %d objects, want none", len(files.files))
	}
}

// TestDeleteOwnAccount checks the account goes with its files, and that the
// last active administrator is refused instead - the service must keep one.
func TestDeleteOwnAccount(t *testing.T) {
	s, user, users, files := newAvatarServer(false)
	files.files[avatarKey(user.ID)] = []byte("bytes of a picture")

	if recorder := send(s, http.MethodDelete, "/api/v1/me", "good", ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete the account: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, gone := users.deleted[user.ID]; !gone {
		t.Error("the account was not deleted")
	}
	if len(files.files) != 0 {
		t.Errorf("the store holds %d objects, want the files gone with the account", len(files.files))
	}

	last, _, _, _ := newAvatarServer(true)
	if recorder := send(last, http.MethodDelete, "/api/v1/me", "good", ""); recorder.Code != http.StatusConflict {
		t.Errorf("the last administrator deleting itself: %d %s", recorder.Code, recorder.Body.String())
	}
}
