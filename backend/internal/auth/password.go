// Package auth implements password hashing, session tokens and the sign-in,
// refresh and sign-out flows built on them.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Argon2id parameters. These follow the OWASP recommendation of 19 MiB of
// memory with two passes, which resists GPU cracking while staying light enough
// for a single-board computer to run a sign-in without a noticeable pause.
const (
	argonMemoryKiB  uint32 = 19456
	argonIterations uint32 = 2
	argonThreads    uint8  = 1
	argonSaltLength        = 16
	argonKeyLength  uint32 = 32
)

// hashEncoding is the PHC string format. The parameters travel with the hash,
// so they can be raised later without invalidating existing passwords.
const hashEncoding = "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"

// Bounds on a password.
//
// Eight is what OWASP asks for as a floor and is the point below which a
// passphrase is not one. The ceiling is not a security limit: argon2id will hash
// anything, and hashing a megabyte of it on every sign-in is a way to make the
// server do work for free.
const (
	minPasswordLength = 8
	maxPasswordLength = 256
)

// CheckPasswordStrength - reports whether a password may be used.
//
// It checks length and nothing else. Composition rules - a digit, a capital, a
// symbol - push people towards "Password1!" and away from long, memorable
// phrases that are actually harder to guess.
//
// Arguments:
//   - field: the name of the input field, reported back in the error.
//   - password: the plaintext password.
//
// Returns:
//   - a *domain.ValidationError when it cannot be used.
func CheckPasswordStrength(field, password string) error {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return domain.NewValidationError(field, "too_short",
			fmt.Sprintf("must be at least %d characters", minPasswordLength))
	}
	if len(password) > maxPasswordLength {
		return domain.NewValidationError(field, "too_long",
			fmt.Sprintf("must be at most %d bytes", maxPasswordLength))
	}
	return nil
}

// HashPassword - derives a storable Argon2id hash from a plaintext password.
//
// The returned string carries the algorithm, its parameters and a fresh random
// salt, so verification needs nothing but the stored value.
//
// Arguments:
//   - password: the plaintext password to hash.
//
// Returns:
//   - the encoded hash in PHC string format.
//   - an error if the system's random source is unavailable.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonThreads, argonKeyLength)

	return fmt.Sprintf(hashEncoding,
		argon2.Version, argonMemoryKiB, argonIterations, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword - checks a plaintext password against a stored hash.
//
// The comparison is constant-time, so the duration of a failed attempt does not
// reveal how much of the hash matched.
//
// Arguments:
//   - encodedHash: a hash previously produced by HashPassword.
//   - password: the plaintext password to check.
//
// Returns:
//   - true when the password matches.
//   - an error only if the stored hash cannot be parsed; a simple mismatch
//     returns false with a nil error.
func VerifyPassword(encodedHash, password string) (bool, error) {
	params, salt, want, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.iterations, params.memoryKiB, params.threads, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// hashParams are the Argon2id cost parameters read back from a stored hash.
type hashParams struct {
	memoryKiB  uint32
	iterations uint32
	threads    uint8
}

// decodeHash splits a PHC string into its parameters, salt and derived key.
func decodeHash(encodedHash string) (hashParams, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return hashParams{}, nil, nil, fmt.Errorf("password hash is not in argon2id PHC format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return hashParams{}, nil, nil, fmt.Errorf("parse password hash version: %w", err)
	}
	if version != argon2.Version {
		return hashParams{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var params hashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memoryKiB, &params.iterations, &params.threads); err != nil {
		return hashParams{}, nil, nil, fmt.Errorf("parse password hash parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return hashParams{}, nil, nil, fmt.Errorf("decode password salt: %w", err)
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return hashParams{}, nil, nil, fmt.Errorf("decode password hash: %w", err)
	}

	return params, salt, key, nil
}
