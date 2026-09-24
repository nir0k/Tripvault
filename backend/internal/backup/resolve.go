package backup

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// ErrSecretsUnavailable reports a configuration holding sealed credentials on an
// instance with nothing to open them with.
var ErrSecretsUnavailable = errors.New(
	"the stored credentials cannot be opened: TRIPVAULT_SECRETS_KEY is not set")

// SecretOpener opens a credential sealed with the instance's secrets key.
type SecretOpener interface {
	Open(sealed []byte) (string, error)
}

// HostKeyPinner records the host key an SFTP destination learned on its first
// connection, so that every later connection is held to it.
type HostKeyPinner interface {
	PinHostKey(ctx context.Context, configID uuid.UUID, key string) error
}

// Resolver turns a configuration into the destination it names.
//
// It exists because a destination is per configuration rather than per service:
// an instance can be copied to a disk and to a server at the same time, and the
// runner has to be told which it is working with each time rather than being
// built around one.
type Resolver struct {
	// local is built once at start-up so that a missing or unwritable directory
	// fails then rather than at three in the morning. Every other destination is
	// opened when it is used: an SSH connection held open for a schedule that
	// fires nightly would be a connection that is dead when it is needed.
	local Destination
	// secrets opens the credentials a configuration holds sealed, or is nil when
	// the instance has no secrets key.
	secrets SecretOpener
	// pins records a host key learned on first use.
	pins HostKeyPinner
}

// NewResolver - builds the resolver around the local destination.
//
// Arguments:
//   - local: the directory local backups are written to.
//   - secrets: what opens sealed credentials, or nil when there is no key.
//   - pins: where a host key learned on first use is recorded.
//
// Returns:
//   - a resolver that can open any destination a configuration names.
func NewResolver(local Destination, secrets SecretOpener, pins HostKeyPinner) *Resolver {
	return &Resolver{local: local, secrets: secrets, pins: pins}
}

// Resolve - opens the destination a configuration names.
//
// Arguments:
//   - ctx: context the destination's requests run under.
//   - config: the configuration whose destination to open.
//
// Returns:
//   - the destination, and a function to close it that the caller must call.
//   - an error when the destination cannot be reached or the configuration does
//     not describe one this build implements.
func (r *Resolver) Resolve(ctx context.Context, config domain.BackupConfig) (Destination, func() error, error) {
	switch config.DestinationType {
	case domain.BackupDestinationLocal:
		return r.local, func() error { return nil }, nil

	case domain.BackupDestinationSFTP:
		secrets, err := r.openSecrets(config.SealedSecrets)
		if err != nil {
			return nil, nil, err
		}
		destination, err := DialSFTP(config.DestinationParams, secrets)
		if err != nil {
			return nil, nil, err
		}
		// A key learned on this connection is recorded before anything is
		// written. If it cannot be, the run fails: carrying on would leave the
		// next run to trust whatever answers, which is not what was agreed.
		if learned := destination.LearnedHostKey(); learned != "" {
			if r.pins == nil {
				_ = destination.Close()
				return nil, nil, errors.New("the server's host key could not be recorded")
			}
			if err := r.pins.PinHostKey(ctx, config.ID, learned); err != nil {
				_ = destination.Close()
				return nil, nil, fmt.Errorf("record the server's host key: %w", err)
			}
		}
		return destination, destination.Close, nil

	default:
		return nil, nil, fmt.Errorf("backups to %s are not implemented", config.DestinationType)
	}
}

// Encryption - resolves what a configuration asks for into a usable passphrase.
//
// Arguments:
//   - config: the configuration whose archives are to be written or read.
//
// Returns:
//   - the encryption to wrap the archive with; the zero value when the
//     configuration writes in the clear.
//   - an error when the passphrase is sealed and cannot be opened.
func (r *Resolver) Encryption(config domain.BackupConfig) (Encryption, error) {
	if !config.Encrypted {
		return Encryption{}, nil
	}

	sealed, ok := config.SealedSecrets[domain.BackupSecretPassphrase]
	if !ok || len(sealed) == 0 {
		return Encryption{}, errors.New("this backup is encrypted but holds no passphrase")
	}
	if r.secrets == nil {
		return Encryption{}, ErrSecretsUnavailable
	}

	passphrase, err := r.secrets.Open(sealed)
	if err != nil {
		return Encryption{}, fmt.Errorf("open the archive passphrase: %w", err)
	}
	return Encryption{Passphrase: passphrase}, nil
}

// openSecrets opens every credential a configuration holds sealed.
//
// The opened values live only as long as the connection they are used for; they
// are never logged and never handed back to a caller.
func (r *Resolver) openSecrets(sealed map[string][]byte) (map[string]string, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	if r.secrets == nil {
		return nil, ErrSecretsUnavailable
	}

	opened := make(map[string]string, len(sealed))
	for name, value := range sealed {
		plain, err := r.secrets.Open(value)
		if err != nil {
			return nil, fmt.Errorf("open the stored %s: %w", name, err)
		}
		opened[name] = plain
	}
	return opened, nil
}
