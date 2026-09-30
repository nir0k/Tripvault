package backup

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// TestCheckLocalDestination checks a writable directory passes, counts the
// archives it holds and is left exactly as it was found.
func TestCheckLocalDestination(t *testing.T) {
	root := t.TempDir()
	local, err := NewLocalDestination(root)
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}
	archive := filepath.Join(root, ArchivePrefix+"20260620T031500Z.tar.gz")
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := NewResolver(local, nil, nil).Check(context.Background(),
		domain.BackupConfig{DestinationType: domain.BackupDestinationLocal})
	if result.Err != nil || result.Step != "" {
		t.Fatalf("Check() failed at %q: %v", result.Step, result.Err)
	}
	if result.Archives != 1 {
		t.Errorf("Check() counted %d archives, want 1", result.Archives)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the check left %d entries behind, want only the archive", len(entries))
	}
}

// TestCheckLocalDestinationThatCannotBeWritten checks a directory that went
// read-only after start-up is reported at the step of writing.
func TestCheckLocalDestinationThatCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not stop root")
	}
	root := t.TempDir()
	local, err := NewLocalDestination(root)
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	result := NewResolver(local, nil, nil).Check(context.Background(),
		domain.BackupConfig{DestinationType: domain.BackupDestinationLocal})
	if result.Err == nil || result.Step != CheckStepWrite {
		t.Errorf("Check() of a read-only directory returned step %q, error %v", result.Step, result.Err)
	}
}

// TestCheckSFTPDestinationThatDoesNotAnswer checks a server nobody listens on
// is reported as a connection that failed, not as a refused account.
func TestCheckSFTPDestinationThatDoesNotAnswer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	_ = listener.Close()

	result := NewResolver(nil, nil, nil).Check(context.Background(), domain.BackupConfig{
		DestinationType:   domain.BackupDestinationSFTP,
		DestinationParams: map[string]string{"host": host, "port": port, "user": "tripvault"},
	})
	// With no credentials the check stops before dialling at all.
	if result.Step != CheckStepCredentials {
		t.Errorf("Check() without credentials failed at %q: %v", result.Step, result.Err)
	}

	_, _, step, err := dialSFTP(context.Background(), map[string]string{"host": host, "port": port, "user": "tripvault"},
		map[string]string{SFTPSecretPassword: "hunter2"})
	if err == nil || step != CheckStepConnect {
		t.Errorf("dialSFTP() to a closed port failed at %q: %v", step, err)
	}
}

// TestCheckRefusesAnUnknownDestination checks a destination this build does
// not implement fails the check rather than passing it.
func TestCheckRefusesAnUnknownDestination(t *testing.T) {
	result := NewResolver(nil, nil, nil).Check(context.Background(),
		domain.BackupConfig{DestinationType: "dropbox"})
	if result.Err == nil {
		t.Error("Check() passed a destination this build does not implement")
	}
}
