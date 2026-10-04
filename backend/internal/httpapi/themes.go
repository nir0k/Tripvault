package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// ThemeStore is the persistence of the instance's colour themes.
type ThemeStore interface {
	List(ctx context.Context) ([]domain.InstanceTheme, error)
	Create(ctx context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error)
	Replace(ctx context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// themeResponse is a theme with its palettes; an absent palette is null.
type themeResponse struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Light     domain.ThemePalette `json:"light"`
	Dark      domain.ThemePalette `json:"dark"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// newThemeResponse maps a theme onto the wire.
func newThemeResponse(theme domain.InstanceTheme) themeResponse {
	return themeResponse{ID: theme.ID.String(), Name: theme.Name, Light: theme.Light, Dark: theme.Dark,
		CreatedAt: theme.CreatedAt, UpdatedAt: theme.UpdatedAt}
}

// handleListThemes lists the instance's themes with their palettes, for
// everybody signed in to choose from and for the administrator to manage.
func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	themes, err := s.themes.List(r.Context())
	if err != nil {
		s.internalError(w, r, "list themes", err)
		return
	}
	items := make([]themeResponse, 0, len(themes))
	for _, theme := range themes {
		items = append(items, newThemeResponse(theme))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[themeResponse]{Items: items})
}

// decodeThemeFile reads a theme file from the body and checks it, writing the
// refusal itself.
func (s *Server) decodeThemeFile(w http.ResponseWriter, r *http.Request) (domain.InstanceTheme, bool) {
	var file domain.ThemeFile
	if !s.decodeJSON(w, r, &file) {
		return domain.InstanceTheme{}, false
	}
	theme, err := domain.ParseThemeFile(file)
	if err != nil {
		s.writeDomainError(w, r, "validate theme", err)
		return domain.InstanceTheme{}, false
	}
	return theme, true
}

// handleCreateTheme adds a theme from an uploaded theme file. A name another
// theme has, in any case, answers 409 already_exists.
func (s *Server) handleCreateTheme(w http.ResponseWriter, r *http.Request) {
	theme, ok := s.decodeThemeFile(w, r)
	if !ok {
		return
	}
	theme.ID = uuid.Must(uuid.NewV7())
	stored, err := s.themes.Create(r.Context(), theme)
	if err != nil {
		s.writeDomainError(w, r, "create theme", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newThemeResponse(stored))
}

// handleReplaceTheme puts an uploaded theme file in place of a theme, so the
// people reading the interface in it see the new colours without choosing
// again.
func (s *Server) handleReplaceTheme(w http.ResponseWriter, r *http.Request) {
	themeID, ok := s.pathUUID(w, r, "themeID")
	if !ok {
		return
	}
	theme, ok := s.decodeThemeFile(w, r)
	if !ok {
		return
	}
	theme.ID = themeID
	stored, err := s.themes.Replace(r.Context(), theme)
	if err != nil {
		s.writeDomainError(w, r, "replace theme", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newThemeResponse(stored))
}

// handleDeleteTheme removes a theme; the people reading the interface in it
// go back to the built-in one.
func (s *Server) handleDeleteTheme(w http.ResponseWriter, r *http.Request) {
	themeID, ok := s.pathUUID(w, r, "themeID")
	if !ok {
		return
	}
	if err := s.themes.Delete(r.Context(), themeID); err != nil {
		s.writeDomainError(w, r, "delete theme", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
