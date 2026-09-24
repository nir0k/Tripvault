package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// Artifact is one archive sitting at a destination.
type Artifact struct {
	Name      string
	SizeBytes int64
	WrittenAt time.Time
}

// Destination is somewhere archives are written and kept.
//
// Storing is a stream rather than a path because a remote destination has no
// path to hand back: the SFTP destination implements this interface without
// changing it.
type Destination interface {
	// Store writes an archive and returns how many bytes it took.
	Store(ctx context.Context, name string, r io.Reader) (int64, error)
	// Open reads an archive back.
	Open(ctx context.Context, name string) (io.ReadCloser, error)
	// List returns the archives held at this destination, newest first.
	List(ctx context.Context) ([]Artifact, error)
	// Remove deletes one archive. Removing one that is not there is not an error.
	Remove(ctx context.Context, name string) error
}

// LocalDestination keeps archives on a filesystem path, which in a container is
// a mounted volume.
type LocalDestination struct {
	root string
}

// NewLocalDestination - opens a directory to write archives into.
//
// The directory is created and written to at start-up rather than on the first
// backup, so a path that is missing or not writable is a start-up failure
// instead of a backup that fails at three in the morning.
//
// Arguments:
//   - root: the directory archives are kept in.
//
// Returns:
//   - the destination.
//   - an error if the directory cannot be created or written to.
func NewLocalDestination(root string) (*LocalDestination, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("the backup directory must not be empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create the backup directory %q: %w", root, err)
	}

	probe := filepath.Join(root, ".writable")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return nil, fmt.Errorf("the backup directory %q is not writable: %w", root, err)
	}
	if err := os.Remove(probe); err != nil {
		return nil, fmt.Errorf("clean up in the backup directory %q: %w", root, err)
	}

	return &LocalDestination{root: root}, nil
}

// resolve turns an archive name into a path, refusing anything that is not a
// plain filename.
//
// The names this service generates are always plain, but a name also arrives
// from a database row, and a row is not a place to take a path from on trust.
func (d *LocalDestination) resolve(name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%q is not a valid archive name", name)
	}
	return filepath.Join(d.root, name), nil
}

// Store - writes an archive to the directory.
//
// It is written under a temporary name and linked into place, so a run
// interrupted half way through leaves no partial file that a later restore might
// take for a good one.
//
// Linking rather than renaming is deliberate: a rename would silently replace an
// archive that is already there, and the one thing this must never do is destroy
// a good backup. A collision fails the run instead, loudly and with the name in
// the message.
//
// Arguments:
//   - ctx: unused by the filesystem, present for the interface.
//   - name: the archive's filename.
//   - r: the archive's bytes.
//
// Returns:
//   - the number of bytes written.
//   - an error if the file cannot be written.
func (d *LocalDestination) Store(_ context.Context, name string, r io.Reader) (int64, error) {
	path, err := d.resolve(name)
	if err != nil {
		return 0, err
	}

	partial, err := os.CreateTemp(d.root, ".partial-*")
	if err != nil {
		return 0, fmt.Errorf("create the archive: %w", err)
	}
	// Removing the temporary name is harmless once the link has happened, and is
	// what clears it away when anything below fails.
	defer func() {
		_ = partial.Close()
		_ = os.Remove(partial.Name())
	}()

	written, err := io.Copy(partial, r)
	if err != nil {
		return 0, fmt.Errorf("write the archive: %w", err)
	}
	// The bytes have to be on the disk before the name points at them, or a
	// power cut leaves a correctly named file with nothing in it.
	if err := partial.Sync(); err != nil {
		return 0, fmt.Errorf("flush the archive: %w", err)
	}
	if err := partial.Close(); err != nil {
		return 0, fmt.Errorf("close the archive: %w", err)
	}
	if err := os.Chmod(partial.Name(), 0o600); err != nil {
		return 0, fmt.Errorf("set the archive's permissions: %w", err)
	}
	if err := os.Link(partial.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, fmt.Errorf("an archive named %s is already there", name)
		}
		return 0, fmt.Errorf("put the archive in place: %w", err)
	}
	return written, nil
}

// Open - reads an archive back.
//
// Arguments:
//   - ctx: unused by the filesystem, present for the interface.
//   - name: the archive's filename.
//
// Returns:
//   - a reader the caller must close.
//   - domain.ErrNotFound when no such archive is held.
func (d *LocalDestination) Open(_ context.Context, name string) (io.ReadCloser, error) {
	path, err := d.resolve(name)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open the archive: %w", err)
	}
	return file, nil
}

// List - returns the archives held in the directory, newest first.
//
// Arguments:
//   - ctx: unused by the filesystem, present for the interface.
//
// Returns:
//   - the archives, newest first.
//   - an error if the directory cannot be read.
func (d *LocalDestination) List(_ context.Context) ([]Artifact, error) {
	entries, err := os.ReadDir(d.root)
	if err != nil {
		return nil, fmt.Errorf("read the backup directory: %w", err)
	}

	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ArchivePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// The file went away between listing and stat, which a rotation
			// running elsewhere can do. It is simply not there to report.
			continue
		}
		artifacts = append(artifacts, Artifact{
			Name:      entry.Name(),
			SizeBytes: info.Size(),
			WrittenAt: info.ModTime(),
		})
	}

	// By name rather than by modification time: the name carries the instant the
	// archive was taken, which is the order a reader means, and it does not move
	// when a file is copied about.
	sort.Slice(artifacts, func(a, b int) bool {
		return artifacts[a].Name > artifacts[b].Name
	})
	return artifacts, nil
}

// Remove - deletes one archive.
//
// Arguments:
//   - ctx: unused by the filesystem, present for the interface.
//   - name: the archive's filename.
//
// Returns:
//   - an error if the file exists and cannot be removed.
func (d *LocalDestination) Remove(_ context.Context, name string) error {
	path, err := d.resolve(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the archive: %w", err)
	}
	return nil
}
