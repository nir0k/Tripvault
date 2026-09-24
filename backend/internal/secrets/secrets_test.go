package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSealedSecretsOpenWithTheirKeyOnly covers both halves at once: that what is
// stored is not the secret, and that it comes back.
func TestSealedSecretsOpenWithTheirKeyOnly(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned an unexpected error: %v", err)
	}
	if !strings.HasPrefix(key, "AGE-SECRET-KEY-1") {
		t.Fatalf("the key is not in the form the variable takes: %q", key)
	}

	box, err := NewBox(key)
	if err != nil {
		t.Fatalf("NewBox() returned an unexpected error: %v", err)
	}

	const password = "sftp-password-nobody-should-read"
	sealed, err := box.Seal(password)
	if err != nil {
		t.Fatalf("Seal() returned an unexpected error: %v", err)
	}
	if bytes.Contains(sealed, []byte(password)) {
		t.Fatal("the sealed value contains the secret")
	}

	opened, err := box.Open(sealed)
	if err != nil || opened != password {
		t.Fatalf("Open() returned %q, %v", opened, err)
	}

	// A different key is what a changed TRIPVAULT_SECRETS_KEY looks like, and it
	// must fail rather than return something.
	otherKey, _ := GenerateKey()
	other, _ := NewBox(otherKey)
	if _, err := other.Open(sealed); err == nil {
		t.Error("a secret opened with a key it was not sealed with")
	}
}

// TestNoKeyMeansNoBox covers the instance that has simply not configured one:
// that is a supported state, and sealing then reports it rather than storing a
// credential in the clear.
func TestNoKeyMeansNoBox(t *testing.T) {
	box, err := NewBox("  ")
	if err != nil || box != nil {
		t.Fatalf("NewBox(empty) returned %v, %v; want nil, nil", box, err)
	}
	if _, err := box.Seal("x"); !errors.Is(err, ErrNoKey) {
		t.Errorf("Seal() on a nil box returned %v, want ErrNoKey", err)
	}
	if _, err := box.Open([]byte("x")); !errors.Is(err, ErrNoKey) {
		t.Errorf("Open() on a nil box returned %v, want ErrNoKey", err)
	}
}

// TestMalformedKeyIsRefused checks a typo in the variable stops the service at
// start-up instead of at the first backup.
func TestMalformedKeyIsRefused(t *testing.T) {
	if _, err := NewBox("AGE-SECRET-KEY-1-not-really"); err == nil {
		t.Error("a malformed key was accepted")
	}
}

// TestKeyFromFileMakesOneThenReusesIt checks the first start writes a key that
// every later start reads back, and that it is not left readable to others.
func TestKeyFromFileMakesOneThenReusesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "secrets.key")

	key, created, err := KeyFromFile(path)
	if err != nil || !created {
		t.Fatalf("KeyFromFile() on a fresh instance returned created=%v, %v", created, err)
	}
	if _, err := NewBox(key); err != nil {
		t.Fatalf("the key that was written is not usable: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the key file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the key file is mode %o, want 600", mode)
	}

	again, created, err := KeyFromFile(path)
	if err != nil || created {
		t.Fatalf("KeyFromFile() on a second start returned created=%v, %v", created, err)
	}
	if again != key {
		t.Error("the second start read a different key")
	}
}

// TestKeyFromFileReplacesAnEmptyFile checks a file left behind by a write that
// never finished is treated as no key rather than as a key of no characters.
func TestKeyFromFileReplacesAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write the empty key file: %v", err)
	}

	key, created, err := KeyFromFile(path)
	if err != nil || !created {
		t.Fatalf("KeyFromFile() returned created=%v, %v", created, err)
	}
	if !strings.HasPrefix(key, "AGE-SECRET-KEY-1") {
		t.Errorf("key = %q, want a freshly made one", key)
	}
}
