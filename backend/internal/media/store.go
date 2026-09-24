// Package media keeps the files a trip carries: where their bytes live, what
// kind of file each one is, and what a browser is shown in place of a full
// photograph. Only the local filesystem is implemented, which is all a
// self-hosted service needs; the interface stays an abstraction so another
// backend can be added without touching the handlers.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/backup"
)

// ErrNotFound reports a key the store has nothing under.
var ErrNotFound = errors.New("media object not found")

// Store is where the bytes of uploaded files live.
type Store interface {
	// Put writes r under key and returns how many bytes were stored.
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	// Open returns a reader over a stored object, or ErrNotFound.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes an object; removing one that is not there is not an error.
	Delete(ctx context.Context, key string) error
}

// LocalStore keeps files on a filesystem path, which in a container is a
// mounted volume.
type LocalStore struct {
	root      string
	container string
}

// localRestoreStage keeps incoming and previous media generations on the same
// filesystem as the live root, so activation can use atomic renames.
type localRestoreStage struct {
	live      *LocalStore
	incoming  *LocalStore
	previous  string
	activated bool
}

// currentGenerationLink names the symlink switched atomically during restore.
const currentGenerationLink = ".current"

// NewLocalStore - opens a filesystem-backed store, creating the directory when
// it is not there yet.
//
// Arguments:
//   - root: absolute path of the directory that holds the files.
//
// Returns:
//   - a store ready to use.
//   - an error when the directory cannot be created or written to.
func NewLocalStore(root string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("media root must not be empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create media root %q: %w", root, err)
	}
	if err := ensureMediaGeneration(root); err != nil {
		return nil, err
	}
	return &LocalStore{root: filepath.Join(root, currentGenerationLink), container: root}, nil
}

// ensureMediaGeneration moves a legacy flat store into a generation once and
// publishes it through the current symlink. Re-running after an interrupted
// migration completes the same directory rather than losing moved entries.
func ensureMediaGeneration(root string) error {
	current := filepath.Join(root, currentGenerationLink)
	if info, err := os.Stat(current); err == nil && info.IsDir() {
		target, err := os.Readlink(current)
		if err != nil || !validGenerationTarget(target) {
			return errors.New("current media generation link is invalid")
		}
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("open current media generation: %w", err)
	}
	if _, err := os.Lstat(current); err == nil {
		return errors.New("current media generation is not a directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current media generation: %w", err)
	}

	generationName := ".generation-initial"
	generation := filepath.Join(root, generationName)
	if err := os.MkdirAll(generation, 0o750); err != nil {
		return fmt.Errorf("create initial media generation: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("list legacy media: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == generationName || name == ".restore" || name == currentGenerationLink ||
			strings.HasPrefix(name, ".generation-") || strings.HasPrefix(name, ".current-") {
			continue
		}
		if err := os.Rename(filepath.Join(root, name), filepath.Join(generation, name)); err != nil {
			return fmt.Errorf("move legacy media %s: %w", name, err)
		}
	}
	if err := replaceGenerationLink(root, generationName); err != nil {
		return fmt.Errorf("publish initial media generation: %w", err)
	}
	return nil
}

// replaceGenerationLink atomically points the store at one complete generation.
func replaceGenerationLink(root, target string) error {
	if !validGenerationTarget(target) {
		return errors.New("invalid media generation target")
	}
	temporary := filepath.Join(root, ".current-"+uuid.NewString())
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary) }()
	if err := os.Rename(temporary, filepath.Join(root, currentGenerationLink)); err != nil {
		return err
	}
	return syncDirectory(root)
}

// syncDirectory makes an atomic rename durable before a related database
// transaction is allowed to commit.
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

// validGenerationTarget accepts only a generation directory beside the link.
func validGenerationTarget(target string) bool {
	return target == filepath.Base(target) && strings.HasPrefix(target, ".generation-")
}

// Put - writes an object to the store.
//
// The bytes go to a temporary file that is renamed into place, so an upload cut
// short never leaves half a photograph behind under a key the catalogue
// already points at.
//
// Arguments:
//   - ctx: context bounding the write.
//   - key: storage key, used as a path below the store root.
//   - r: the bytes to store.
//
// Returns:
//   - the number of bytes written.
//   - an error when the key is unusable or the file cannot be written.
func (s *LocalStore) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	path, err := s.resolve(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, fmt.Errorf("create media directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return 0, fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		// Nothing to clean up once the rename below succeeded.
		_ = os.Remove(temp.Name())
	}()

	written, err := io.Copy(temp, r)
	if err != nil {
		_ = temp.Close()
		return 0, fmt.Errorf("write media: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return 0, fmt.Errorf("flush media: %w", err)
	}
	if err := temp.Close(); err != nil {
		return 0, fmt.Errorf("close media: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.Chmod(temp.Name(), 0o640); err != nil {
		return 0, fmt.Errorf("set media permissions: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return 0, fmt.Errorf("store media: %w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return 0, fmt.Errorf("flush media directory: %w", err)
	}
	return written, nil
}

// Open - reads an object back.
//
// Arguments:
//   - ctx: context of the read; the caller closes the reader.
//   - key: the key the object was stored under.
//
// Returns:
//   - a reader over the object, which the caller closes.
//   - ErrNotFound when nothing is stored under the key, or another error.
func (s *LocalStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open media: %w", err)
	}
	return file, nil
}

// Delete - removes an object.
//
// Arguments:
//   - ctx: context of the removal.
//   - key: the key to remove.
//
// Returns:
//   - an error when the key is unusable or the file cannot be removed; a key
//     with nothing under it is not an error.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete media: %w", err)
	}
	return nil
}

// Check - reports whether the store can be written to, for the readiness probe.
//
// Arguments:
//   - ctx: context of the check.
//
// Returns:
//   - an error when the root is missing or not writable.
func (s *LocalStore) Check(_ context.Context) error {
	info, err := os.Stat(s.root)
	if err != nil {
		return fmt.Errorf("media root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("media root %q is not a directory", s.root)
	}
	probe, err := os.CreateTemp(s.root, ".probe-*")
	if err != nil {
		return fmt.Errorf("media root is not writable: %w", err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// BeginRestore - creates an isolated media tree for a restore.
//
// Arguments:
//   - ctx: context checked before staging begins.
//
// Returns:
//   - a stage that accepts restored objects and can activate them.
//   - an error when staging directories cannot be created.
func (s *LocalStore) BeginRestore(ctx context.Context) (backup.FileStage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	incomingName := ".generation-" + id
	incomingPath := filepath.Join(s.container, incomingName)
	if err := os.Mkdir(incomingPath, 0o750); err != nil {
		return nil, fmt.Errorf("create incoming media generation: %w", err)
	}
	previous, err := os.Readlink(filepath.Join(s.container, currentGenerationLink))
	if err != nil {
		_ = os.RemoveAll(incomingPath)
		return nil, fmt.Errorf("read current media generation: %w", err)
	}
	if !validGenerationTarget(previous) {
		_ = os.RemoveAll(incomingPath)
		return nil, errors.New("current media generation link is invalid")
	}
	return &localRestoreStage{
		live: s, incoming: &LocalStore{root: incomingPath, container: s.container}, previous: previous,
	}, nil
}

// Put writes one restored object into the isolated incoming tree.
func (s *localRestoreStage) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	return s.incoming.Put(ctx, key, r)
}

// Activate atomically points the store at the complete staged generation.
func (s *localRestoreStage) Activate(ctx context.Context) error {
	if s.activated {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.Base(s.incoming.root)
	if err := replaceGenerationLink(s.live.container, target); err != nil {
		current, readErr := os.Readlink(filepath.Join(s.live.container, currentGenerationLink))
		s.activated = readErr == nil && current == target
		return fmt.Errorf("activate restored media: %w", err)
	}
	s.activated = true
	return nil
}

// Rollback restores the previous visible media tree after a failed database
// activation.
func (s *localRestoreStage) Rollback(_ context.Context) error {
	if !s.activated {
		return nil
	}
	if err := replaceGenerationLink(s.live.container, s.previous); err != nil {
		return fmt.Errorf("restore previous media generation: %w", err)
	}
	s.activated = false
	return nil
}

// Finish removes the previous generation after database activation succeeds.
func (s *localRestoreStage) Finish(_ context.Context) error {
	if err := os.RemoveAll(filepath.Join(s.live.container, filepath.Base(s.previous))); err != nil {
		return fmt.Errorf("remove previous media generation: %w", err)
	}
	return nil
}

// Close removes an unactivated stage. An activated stage is left alone until
// Finish or Rollback decides which generation is safe to discard.
func (s *localRestoreStage) Close(_ context.Context) {
	if !s.activated {
		_ = os.RemoveAll(s.incoming.root)
	}
}

// resolve turns a storage key into a path below the root, refusing anything
// that would climb out of it.
func (s *LocalStore) resolve(key string) (string, error) {
	clean := filepath.Clean("/" + strings.TrimSpace(key))
	if clean == "/" {
		return "", fmt.Errorf("invalid media key %q", key)
	}
	return filepath.Join(s.root, clean), nil
}
