package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakeThemes keeps themes in memory.
type fakeThemes struct {
	themes []domain.InstanceTheme
}

// List returns the stored themes.
func (f *fakeThemes) List(context.Context) ([]domain.InstanceTheme, error) {
	return f.themes, nil
}

// Create stores a theme.
func (f *fakeThemes) Create(_ context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error) {
	f.themes = append(f.themes, theme)
	return theme, nil
}

// Replace overwrites a stored theme.
func (f *fakeThemes) Replace(_ context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error) {
	for i := range f.themes {
		if f.themes[i].ID == theme.ID {
			f.themes[i] = theme
			return theme, nil
		}
	}
	return domain.InstanceTheme{}, domain.ErrNotFound
}

// Delete removes a stored theme.
func (f *fakeThemes) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.themes {
		if f.themes[i].ID == id {
			f.themes = append(f.themes[:i], f.themes[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

// themeFileBody writes a theme file with a dark palette of one colour.
func themeFileBody(name, color string) string {
	colors := make([]string, 0, len(domain.ThemeColorNames))
	for _, key := range domain.ThemeColorNames {
		colors = append(colors, `"`+key+`":"`+color+`"`)
	}
	return `{"format":1,"name":"` + name + `","dark":{` + strings.Join(colors, ",") + `}}`
}

// newThemeServer builds a server whose reader is user, over the given themes.
func newThemeServer(user domain.User, themes *fakeThemes) *Server {
	return newHandlerServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:   &fakeAuth{user: user},
		Users:  fakeUsers{},
		Themes: themes,
	})
}

// TestThemesAreManagedByTheAdministrator checks an administrator uploads,
// replaces and deletes a theme, a bad file is refused, and everybody reads
// the list.
func TestThemesAreManagedByTheAdministrator(t *testing.T) {
	themes := &fakeThemes{}
	admin := newThemeServer(domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}, themes)

	created := send(admin, http.MethodPost, "/api/v1/admin/themes", "good", themeFileBody(" Night ", "#112233"))
	if created.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", created.Code, created.Body.String())
	}
	var theme themeResponse
	if err := json.Unmarshal(created.Body.Bytes(), &theme); err != nil {
		t.Fatalf("response %s: %v", created.Body.String(), err)
	}
	if theme.Name != "Night" || theme.Light != nil || theme.Dark["primary"] != "#112233" {
		t.Errorf("stored %+v", theme)
	}

	bad := send(admin, http.MethodPost, "/api/v1/admin/themes", "good", themeFileBody("Bad", "red"))
	if bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("a colour name: %d %s", bad.Code, bad.Body.String())
	}

	replaced := send(admin, http.MethodPut, "/api/v1/admin/themes/"+theme.ID, "good", themeFileBody("Night", "#445566"))
	if replaced.Code != http.StatusOK || themes.themes[0].Dark["primary"] != "#445566" {
		t.Errorf("replace: %d %s", replaced.Code, replaced.Body.String())
	}

	reader := newThemeServer(domain.User{ID: uuid.New(), IsActive: true}, themes)
	listed := send(reader, http.MethodGet, "/api/v1/themes", "good", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"Night"`) {
		t.Errorf("list: %d %s", listed.Code, listed.Body.String())
	}
	if refused := send(reader, http.MethodPost, "/api/v1/admin/themes", "good", themeFileBody("x", "#fff")); refused.Code != http.StatusForbidden {
		t.Errorf("upload by a member: %d", refused.Code)
	}

	if deleted := send(admin, http.MethodDelete, "/api/v1/admin/themes/"+theme.ID, "good", ""); deleted.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	if missing := send(admin, http.MethodDelete, "/api/v1/admin/themes/"+theme.ID, "good", ""); missing.Code != http.StatusNotFound {
		t.Errorf("delete again: %d", missing.Code)
	}
}
