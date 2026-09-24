// Package bootstrap prepares a freshly installed instance by creating its first
// administrator from the environment.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/auth"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// UserStore is the account persistence seeding needs. List is there for the
// example trips, which have to find the administrator that owns them.
type UserStore interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, user domain.User) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
}

// Options carries the first administrator's details.
type Options struct {
	Email       string
	Password    string
	DisplayName string
}

// Seed - creates the first administrator on an instance with no accounts.
//
// It does nothing once any account exists, so a restart never resurrects a
// removed administrator or resets a changed password. Nobody can sign up on
// their own, so an empty instance without these settings has no way in; that is
// logged loudly rather than treated as an error, because the operator may be
// about to set them.
//
// The password is the operator's own choice, made in their environment, so the
// account is not asked to change it at the first sign-in.
//
// Arguments:
//   - ctx: context bounding the operation.
//   - users: account persistence.
//   - opts: the administrator's details.
//   - logger: destination for the outcome.
//
// Returns:
//   - an error if the instance cannot be read, the details are invalid, or the
//     account cannot be created.
func Seed(ctx context.Context, users UserStore, opts Options, logger *slog.Logger) error {
	count, err := users.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	if opts.Email == "" {
		logger.Warn("no accounts exist and no first administrator is configured",
			slog.String("hint", "set TRIPVAULT_ADMIN_EMAIL and TRIPVAULT_ADMIN_PASSWORD"))
		return nil
	}

	email, err := domain.NormalizeEmail(opts.Email)
	if err != nil {
		return fmt.Errorf("TRIPVAULT_ADMIN_EMAIL: %w", err)
	}
	name, err := domain.NormalizeDisplayName(opts.DisplayName)
	if err != nil {
		return fmt.Errorf("TRIPVAULT_ADMIN_DISPLAY_NAME: %w", err)
	}
	if err := auth.CheckPasswordStrength("password", opts.Password); err != nil {
		return fmt.Errorf("TRIPVAULT_ADMIN_PASSWORD: %w", err)
	}
	hash, err := auth.HashPassword(opts.Password)
	if err != nil {
		return err
	}

	user, err := users.Create(ctx, domain.User{
		ID:              uuid.Must(uuid.NewV7()),
		Email:           email,
		DisplayName:     name,
		PasswordHash:    hash,
		IsAdmin:         true,
		IsActive:        true,
		Theme:           domain.ThemeAuto,
		Units:           domain.UnitsKilometres,
		DefaultCurrency: domain.DefaultCurrency,
	})
	if errors.Is(err, domain.ErrAlreadyExists) {
		// Another replica seeded first; the instance has its administrator.
		return nil
	}
	if err != nil {
		return err
	}

	logger.Info("created the first administrator",
		slog.String("user_id", user.ID.String()),
		slog.String("email", user.Email))
	return nil
}
