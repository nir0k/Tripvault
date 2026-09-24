package backup

import (
	"fmt"
	"io"

	"filippo.io/age"
)

// Encryption is what a configuration asks for, resolved into something usable.
//
// The passphrase reaches this from the configuration's sealed secrets, opened
// with the instance's key just before the archive is written, or from the form a
// restore was asked for in. It is never held anywhere in the clear.
type Encryption struct {
	// Passphrase is empty when the archive is to be written in the clear.
	Passphrase string
}

// Enabled - reports whether an archive written with this will be encrypted.
//
// Returns:
//   - true when a passphrase was resolved.
func (e Encryption) Enabled() bool {
	return e.Passphrase != ""
}

// Wrap - returns a writer that encrypts everything written to it.
//
// The scheme is age with a passphrase, which is scrypt over ChaCha20-Poly1305.
// It is used rather than anything written here because encryption is the one
// part of a backup where being clever is indistinguishable from being wrong, and
// because an archive locked with age can be opened years from now by the age
// command line without this service being involved at all.
//
// The returned writer must be closed for the archive to be complete: the final
// authentication tag is written on close, and an archive missing it will not
// decrypt.
//
// Arguments:
//   - w: where the encrypted bytes go.
//
// Returns:
//   - a writer to write the plain archive to, and which must be closed; w
//     itself, wrapped so that closing it is harmless, when no passphrase is set.
//   - an error if the recipient cannot be built.
func (e Encryption) Wrap(w io.Writer) (io.WriteCloser, error) {
	if !e.Enabled() {
		return nopWriteCloser{w}, nil
	}

	recipient, err := age.NewScryptRecipient(e.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("prepare encryption: %w", err)
	}

	encrypted, err := age.Encrypt(w, recipient)
	if err != nil {
		return nil, fmt.Errorf("start encryption: %w", err)
	}
	return encrypted, nil
}

// Decrypt - returns a reader over the plain contents of an archive.
//
// Arguments:
//   - r: the archive as stored.
//
// Returns:
//   - a reader over the decrypted bytes, or r itself when no passphrase is set.
//   - an error when the passphrase does not open the archive.
func (e Encryption) Decrypt(r io.Reader) (io.Reader, error) {
	if !e.Enabled() {
		return r, nil
	}

	identity, err := age.NewScryptIdentity(e.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("prepare decryption: %w", err)
	}

	plain, err := age.Decrypt(r, identity)
	if err != nil {
		return nil, fmt.Errorf("the passphrase does not open this archive: %w", err)
	}
	return plain, nil
}

// nopWriteCloser lets an unencrypted archive use the same code path as an
// encrypted one, so the caller never has to ask which it is holding.
type nopWriteCloser struct {
	io.Writer
}

// Close does nothing: the underlying writer belongs to the caller.
func (nopWriteCloser) Close() error { return nil }
