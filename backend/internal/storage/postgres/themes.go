package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// ThemeRepository reads and writes the colour themes of the instance.
type ThemeRepository struct {
	pool *pgxpool.Pool
}

// NewThemeRepository - creates the theme repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewThemeRepository(pool *pgxpool.Pool) *ThemeRepository {
	return &ThemeRepository{pool: pool}
}

// themeColumns is the select list of a theme, kept in step with scanTheme.
const themeColumns = `id, name, light, dark, created_at, updated_at`

// scanTheme reads one row in the order of themeColumns. pgx decodes a JSON
// null palette into a nil map.
func scanTheme(row pgx.Row) (domain.InstanceTheme, error) {
	var theme domain.InstanceTheme
	err := row.Scan(&theme.ID, &theme.Name, &theme.Light, &theme.Dark, &theme.CreatedAt, &theme.UpdatedAt)
	return theme, err
}

// paletteArg passes a palette to a statement, an absent one as SQL NULL
// rather than as the JSON null.
func paletteArg(palette domain.ThemePalette) any {
	if palette == nil {
		return nil
	}
	return palette
}

// List - returns the themes of the instance in the order of their names.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the themes with their palettes.
//   - an error if the query fails.
func (r *ThemeRepository) List(ctx context.Context) ([]domain.InstanceTheme, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+themeColumns+` FROM instance_themes ORDER BY lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("list themes: %w", err)
	}
	themes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.InstanceTheme, error) {
		return scanTheme(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read themes: %w", err)
	}
	return themes, nil
}

// Create - stores a new theme.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - theme: the parsed theme with its identifier.
//
// Returns:
//   - the stored theme.
//   - domain.ErrAlreadyExists when another theme has that name, whatever its
//     case.
func (r *ThemeRepository) Create(ctx context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error) {
	stored, err := scanTheme(r.pool.QueryRow(ctx,
		`INSERT INTO instance_themes (id, name, light, dark) VALUES ($1, $2, $3, $4)
		 RETURNING `+themeColumns,
		theme.ID, theme.Name, paletteArg(theme.Light), paletteArg(theme.Dark)))
	switch {
	case isUniqueViolation(err):
		return domain.InstanceTheme{}, domain.ErrAlreadyExists
	case err != nil:
		return domain.InstanceTheme{}, fmt.Errorf("create theme: %w", err)
	}
	return stored, nil
}

// Replace - puts the name and palettes of a new file in place of a theme's,
// so the people reading the interface in it keep it.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - theme: the parsed theme, with the identifier of the one it replaces.
//
// Returns:
//   - the stored theme.
//   - domain.ErrNotFound when there is no such theme.
//   - domain.ErrAlreadyExists when another theme has that name.
func (r *ThemeRepository) Replace(ctx context.Context, theme domain.InstanceTheme) (domain.InstanceTheme, error) {
	stored, err := scanTheme(r.pool.QueryRow(ctx,
		`UPDATE instance_themes SET name = $2, light = $3, dark = $4, updated_at = now()
		 WHERE id = $1
		 RETURNING `+themeColumns,
		theme.ID, theme.Name, paletteArg(theme.Light), paletteArg(theme.Dark)))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.InstanceTheme{}, domain.ErrNotFound
	case isUniqueViolation(err):
		return domain.InstanceTheme{}, domain.ErrAlreadyExists
	case err != nil:
		return domain.InstanceTheme{}, fmt.Errorf("replace theme: %w", err)
	}
	return stored, nil
}

// Delete - removes a theme; the people who read the interface in it go back
// to the built-in one.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the theme.
//
// Returns:
//   - domain.ErrNotFound when there is no such theme.
//   - an error if the statement fails.
func (r *ThemeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM instance_themes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete theme: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
