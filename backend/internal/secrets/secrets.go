// Package secrets seals the credentials this service has to keep in its
// database: the password or private key an SFTP backup destination needs, and
// the passphrase archives are locked with.
//
// They are typed into the site by whoever sets the backup up, which is where
// that decision belongs - making them edit the server's environment first is
// what the form exists to spare them. Sealed with a key that lives outside the
// database, a dump of these tables on its own opens nothing.
package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// ErrNoKey reports that sealing was asked for on an instance with no key
// configured. It is its own error so that the caller can say which variable to
// set rather than reporting a generic failure.
var ErrNoKey = errors.New("this server has no secrets key, so credentials cannot be stored")

// Box seals and opens secrets with one age X25519 key.
//
// X25519 rather than a passphrase: an age passphrase goes through scrypt, which
// is deliberately slow and would be paid on every backup that opens a
// credential. An X25519 key already has full entropy, so there is nothing for a
// slow function to protect.
type Box struct {
	identity  *age.X25519Identity
	recipient *age.X25519Recipient
}

// NewBox - builds a box from the key in the environment.
//
// Arguments:
//   - key: an age secret key, "AGE-SECRET-KEY-1..."; an empty string means the
//     instance has none.
//
// Returns:
//   - the box, or nil when no key is configured.
//   - an error when a key is configured but cannot be read, which the caller
//     should treat as a start-up failure: a malformed key discovered at the
//     first backup is a backup that fails at three in the morning.
func NewBox(key string) (*Box, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}

	identity, err := age.ParseX25519Identity(key)
	if err != nil {
		return nil, fmt.Errorf("the secrets key is not an age secret key: %w", err)
	}
	return &Box{identity: identity, recipient: identity.Recipient()}, nil
}

// GenerateKey - mints a new secrets key.
//
// Returns:
//   - the key in the form the variable takes.
//   - an error if the system's random source is unavailable.
func GenerateKey() (string, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("generate a secrets key: %w", err)
	}
	return identity.String(), nil
}

// Seal - encrypts a secret for storage.
//
// Arguments:
//   - plaintext: the secret.
//
// Returns:
//   - the sealed bytes, which are safe to store.
//   - ErrNoKey when the box is nil.
func (b *Box) Seal(plaintext string) ([]byte, error) {
	if b == nil {
		return nil, ErrNoKey
	}

	var out bytes.Buffer
	writer, err := age.Encrypt(&out, b.recipient)
	if err != nil {
		return nil, fmt.Errorf("seal a secret: %w", err)
	}
	if _, err := io.WriteString(writer, plaintext); err != nil {
		return nil, fmt.Errorf("seal a secret: %w", err)
	}
	// The final authentication tag is written on close; without it the sealed
	// value would not open.
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("seal a secret: %w", err)
	}
	return out.Bytes(), nil
}

// Open - decrypts a stored secret.
//
// Arguments:
//   - sealed: bytes produced by Seal.
//
// Returns:
//   - the secret.
//   - ErrNoKey when the box is nil, or an error when the bytes were sealed with
//     a different key - which is what a changed TRIPVAULT_SECRETS_KEY looks like.
func (b *Box) Open(sealed []byte) (string, error) {
	if b == nil {
		return "", ErrNoKey
	}

	reader, err := age.Decrypt(bytes.NewReader(sealed), b.identity)
	if err != nil {
		return "", fmt.Errorf("open a stored secret (was the secrets key changed?): %w", err)
	}
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("open a stored secret: %w", err)
	}
	return string(plaintext), nil
}

// KeyFromFile - reads the instance's secrets key, making one if there is none.
//
// The key is not something an operator should have to decide about: it seals
// credentials typed into the site, and asking somebody to generate one and put
// it in the environment before they can set up a backup is asking them to do the
// service's bookkeeping. So it lives in a file on a volume, made on the first
// start and read on every one after.
//
// It is deliberately not in the database: the archives the key's credentials
// produce contain that database, and a key stored inside them would unlock
// exactly what it is meant to protect.
//
// Arguments:
//   - path: where the key is kept.
//
// Returns:
//   - the key.
//   - whether it was made just now, which the caller logs.
//   - an error if the file cannot be read, made or written.
func KeyFromFile(path string) (string, bool, error) {
	stored, err := os.ReadFile(path)
	switch {
	case err == nil:
		if key := strings.TrimSpace(string(stored)); key != "" {
			return key, false, nil
		}
		// An empty file is a key that was never written, not a key of no
		// characters, so it is replaced rather than returned.
	case !errors.Is(err, os.ErrNotExist):
		return "", false, fmt.Errorf("read the secrets key: %w", err)
	}

	key, err := GenerateKey()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, fmt.Errorf("make room for the secrets key: %w", err)
	}
	// 0600: the key opens every credential the service has stored.
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", false, fmt.Errorf("write the secrets key: %w", err)
	}
	return key, true, nil
}
