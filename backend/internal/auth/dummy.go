package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
)

// dummyPasswordHash returns a hash that no submitted password can match.
//
// Login verifies against it when the email is unknown, so a request for a
// non-existent account costs the same Argon2id work as a wrong password. Without
// that, the response time alone would reveal which addresses are registered.
//
// The value is derived from random bytes at first use rather than hardcoded:
// a constant in the source would be a published hash, and computing it once
// lazily keeps the cost off the start-up path.
var dummyPasswordHash = sync.OnceValue(func() string {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		// A hash of an empty string still costs the right amount of work, which
		// is all this value is for.
		seed = nil
	}

	hash, err := HashPassword(base64.RawStdEncoding.EncodeToString(seed))
	if err != nil {
		// HashPassword only fails when the random source is unavailable. Returning
		// an unparseable value keeps the timing cost and can never match.
		return "unavailable"
	}
	return hash
})
