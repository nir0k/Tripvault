package backup

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// The refusals a restore reports, each its own error so that the API can say
// which of them happened rather than passing a sentence through.
var (
	// ErrEncrypted reports an archive that needs a passphrase none was given for.
	ErrEncrypted = errors.New("this archive is encrypted")
	// ErrNotAnArchive reports a file that is not one of this service's archives.
	ErrNotAnArchive = errors.New("this file is not a Tripvault archive")
	// ErrArchiveTooNew reports an archive written by a later version of
	// Tripvault, whose schema this build does not have the migrations for.
	// Restoring it would mean loading rows into tables that do not exist yet.
	ErrArchiveTooNew = errors.New("this archive was written by a newer version of Tripvault")
	// ErrArchiveDamaged reports an archive whose files do not match what it
	// records about them.
	ErrArchiveDamaged = errors.New("this archive is damaged")
)

// ageHeader is what every age file begins with, encrypted with a passphrase or
// with a key.
const ageHeader = "age-encryption.org/"

// FileWriter stores the bytes of the files an archive carries.
type FileWriter interface {
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
}

// DatabaseStage is a restored database kept apart from the live schema until
// every table and file has been prepared successfully.
type DatabaseStage interface {
	// LoadTables replaces the staged tables' contents from an archive's dump.
	LoadTables(ctx context.Context, tables []string, open func(table string) (io.ReadCloser, error)) error
	Upgrade(ctx context.Context) error
	Activate(ctx context.Context) error
	Rollback(ctx context.Context) error
	Finish(ctx context.Context) error
	Close(ctx context.Context)
}

// DatabaseStager prepares a database schema without changing the live one.
type DatabaseStager interface {
	// Latest is the newest version this build has a migration for.
	Latest() int64
	BeginRestore(ctx context.Context, version int64, tables []string) (DatabaseStage, error)
}

// FileStage is a media tree kept beside the live tree until activation.
type FileStage interface {
	FileWriter
	Activate(ctx context.Context) error
	Rollback(ctx context.Context) error
	Finish(ctx context.Context) error
	Close(ctx context.Context)
}

// FileStager prepares a new media tree without changing files currently served.
type FileStager interface {
	BeginRestore(ctx context.Context) (FileStage, error)
}

// RestoreResult says what a restore put back.
type RestoreResult struct {
	SchemaVersion int64
	Tables        int
	Trips         int
	Files         int
}

// Restore stages reported while an archive is being restored.
const (
	RestoreStageValidating = "validating"
	RestoreStageStaging    = "staging"
	RestoreStageCutover    = "cutover"
)

// Plain - returns a reader over an archive's unencrypted bytes.
//
// The age header is recognised before anything tries to gunzip the archive, so
// an archive that needs a passphrase says exactly that rather than something
// about a corrupt gzip stream.
//
// Arguments:
//   - r: the archive as stored.
//   - encryption: the passphrase to open it with, or the zero value for none.
//
// Returns:
//   - a reader over the plain archive.
//   - ErrEncrypted when it needs a passphrase and none was given.
//   - an error when the passphrase does not open it.
func Plain(r io.Reader, encryption Encryption) (io.Reader, error) {
	// Peeked rather than read, so the bytes are still there for whichever reader
	// turns out to want them.
	sniff := bufio.NewReaderSize(r, len(ageHeader)+1)
	header, err := sniff.Peek(len(ageHeader))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the archive: %w", err)
	}
	if string(header) == ageHeader && !encryption.Enabled() {
		return nil, ErrEncrypted
	}
	return encryption.Decrypt(sniff)
}

// Restore - puts the whole service back from an archive.
//
// The archive is unpacked to a directory first, and every file checked against
// its recorded size and checksum, before anything on the instance is touched. An
// archive read years later off media that may have rotted must not be allowed to
// half-replace a working instance: either all of it is good, or none of it is
// used.
//
// Then a replacement schema is built at the archive version, loaded in one
// transaction and migrated to this build while a replacement media generation
// is written beside the live one, and both are switched over. A failed switch
// of the files rolls the database back as well.
//
// Arguments:
//   - ctx: context bounding the restore.
//   - archive: the archive's bytes, already decrypted.
//   - workDir: a directory to unpack into, which is emptied afterwards.
//   - database: what builds and switches the replacement schema.
//   - files: what builds and switches the replacement media.
//   - progress: told each stage as it begins, or nil.
//
// Returns:
//   - what was put back.
//   - ErrNotAnArchive, ErrArchiveTooNew, ErrArchiveDamaged, or an error from the
//     database or the media store.
func Restore(ctx context.Context, archive io.Reader, workDir string,
	database DatabaseStager, files FileStager, progress func(stage string)) (RestoreResult, error) {
	report := func(stage string) {
		if progress != nil {
			progress(stage)
		}
	}
	report(RestoreStageValidating)
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return RestoreResult{}, fmt.Errorf("make room to unpack the archive: %w", err)
	}
	unpacked, err := os.MkdirTemp(workDir, "restore-")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("make room to unpack the archive: %w", err)
	}
	defer func() { _ = os.RemoveAll(unpacked) }()

	contents, err := unpack(archive, unpacked)
	if err != nil {
		return RestoreResult{}, err
	}
	manifest, metadata := contents.Manifest, contents.Metadata
	if err := validateArchive(contents); err != nil {
		return RestoreResult{}, err
	}
	if manifest.Format != archiveFormat {
		if manifest.Format > archiveFormat {
			return RestoreResult{}, fmt.Errorf("%w: it is in format %d, this build reads through %d",
				ErrArchiveTooNew, manifest.Format, archiveFormat)
		}
		if manifest.Format < oldestArchiveFormat {
			return RestoreResult{}, fmt.Errorf("%w: unsupported archive format %d",
				ErrNotAnArchive, manifest.Format)
		}
	}
	if manifest.SchemaVersion > database.Latest() {
		return RestoreResult{}, fmt.Errorf("%w: it holds schema %d, this build knows %d",
			ErrArchiveTooNew, manifest.SchemaVersion, database.Latest())
	}
	if err := verifyTables(unpacked, manifest, metadata); err != nil {
		return RestoreResult{}, err
	}
	if err := verifyFiles(unpacked, metadata); err != nil {
		return RestoreResult{}, err
	}
	var stagedFileBytes int64
	for _, file := range metadata.Files {
		if file.SizeBytes > int64(^uint64(0)>>1)-stagedFileBytes {
			return RestoreResult{}, fmt.Errorf("%w: its file size overflows", ErrArchiveDamaged)
		}
		stagedFileBytes += file.SizeBytes
	}
	if err := enoughUnpackSpace(unpacked, stagedFileBytes); err != nil {
		return RestoreResult{}, err
	}
	report(RestoreStageStaging)
	return restoreStaged(ctx, unpacked, manifest, metadata, database, files, report)
}

// restoreStaged builds the replacement database and media tree away from the
// live instance, then switches both over. A failed switch rolls back the part
// already activated before returning the error.
func restoreStaged(ctx context.Context, dir string, manifest Manifest, metadata Metadata,
	database DatabaseStager, files FileStager, report func(stage string)) (RestoreResult, error) {
	dbStage, err := database.BeginRestore(ctx, manifest.SchemaVersion, manifest.Tables)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("prepare the restore database: %w", err)
	}
	defer dbStage.Close(context.WithoutCancel(ctx))

	open := func(table string) (io.ReadCloser, error) {
		file, err := os.Open(filepath.Join(dir, DatabaseDirectory, table+".csv"))
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: table %s is missing", ErrArchiveDamaged, table)
		}
		return file, err
	}
	if err := dbStage.LoadTables(ctx, manifest.Tables, open); err != nil {
		return RestoreResult{}, err
	}
	if err := dbStage.Upgrade(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("bring the staged schema up to date: %w", err)
	}

	fileStage, err := files.BeginRestore(ctx)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("prepare the restore media: %w", err)
	}
	defer fileStage.Close(context.WithoutCancel(ctx))
	for _, file := range metadata.Files {
		if err := restoreFile(ctx, dir, file, fileStage); err != nil {
			return RestoreResult{}, err
		}
	}

	report(RestoreStageCutover)
	if err := dbStage.Activate(ctx); err != nil {
		return RestoreResult{}, fmt.Errorf("activate restored database: %w", err)
	}
	if err := fileStage.Activate(ctx); err != nil {
		fileRollbackErr := fileStage.Rollback(context.WithoutCancel(ctx))
		databaseRollbackErr := dbStage.Rollback(context.WithoutCancel(ctx))
		return RestoreResult{}, errors.Join(fmt.Errorf("activate restored media: %w", err),
			fileRollbackErr, databaseRollbackErr)
	}

	// Activation is complete. Cleanup failures leave only a recoverable previous
	// generation and must not describe the successfully restored instance as
	// failed.
	_ = dbStage.Finish(context.WithoutCancel(ctx))
	_ = fileStage.Finish(context.WithoutCancel(ctx))
	return RestoreResult{
		SchemaVersion: manifest.SchemaVersion,
		Tables:        len(manifest.Tables),
		Trips:         manifest.Trips,
		Files:         len(metadata.Files),
	}, nil
}

// verifyTables checks the CSV payloads of format 2 against their metadata.
// Format 1 had no table checksums and is covered by the structural checks.
func verifyTables(dir string, manifest Manifest, metadata Metadata) error {
	if manifest.Format < 2 {
		return nil
	}
	for _, table := range metadata.Tables {
		name := path.Join(DatabaseDirectory, table.Name+".csv")
		if err := verifyStoredEntry(filepath.Join(dir, filepath.FromSlash(name)), name,
			table.SizeBytes, table.ChecksumSHA); err != nil {
			return err
		}
	}
	return nil
}

// unpackedArchive is the control data and regular entry catalogue read while
// an archive is expanded. The catalogue lets validation distinguish an empty
// table from a table whose CSV silently disappeared.
type unpackedArchive struct {
	Manifest Manifest
	Metadata Metadata
	Entries  map[string]int64
}

// maxControlEntryBytes bounds manifest and metadata JSON. They describe the
// archive rather than carry its data, so a control entry larger than this is
// malformed even when the archive itself is legitimately very large.
const maxControlEntryBytes = 16 << 20

// unpackReserveBytes is left free while staging an archive, so validation does
// not consume the final blocks the database and operating system need to report
// the failure cleanly.
const unpackReserveBytes = 64 << 20

// restoreFile writes one unpacked file into the store under its own key.
func restoreFile(ctx context.Context, dir string, file FileMetadata, files FileWriter) error {
	source, err := os.Open(filepath.Join(dir, MediaDirectory, filepath.FromSlash(file.Name)))
	if err != nil {
		return fmt.Errorf("read %s from the archive: %w", file.Name, err)
	}
	defer func() { _ = source.Close() }()

	if _, err := files.Put(ctx, file.Name, source); err != nil {
		return fmt.Errorf("store %s: %w", file.Name, err)
	}
	return nil
}

// unpack writes an archive's entries into a directory and reads its manifest.
//
// Entry names are checked rather than trusted: a tar can name "../etc", and an
// archive is exactly the kind of file that arrives from somewhere else.
func unpack(archive io.Reader, dir string) (unpackedArchive, error) {
	compressed, err := gzip.NewReader(archive)
	if err != nil {
		return unpackedArchive{}, fmt.Errorf("%w: %s", ErrNotAnArchive, err)
	}
	defer func() { _ = compressed.Close() }()

	var (
		contents     = unpackedArchive{Entries: make(map[string]int64)}
		seenManifest bool
		seenMetadata bool
		totalSize    int64
	)
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return unpackedArchive{}, fmt.Errorf("%w: %s", ErrNotAnArchive, err)
		}
		if header.Typeflag != tar.TypeReg {
			return unpackedArchive{}, fmt.Errorf("%w: it contains a non-regular entry", ErrArchiveDamaged)
		}

		name := path.Clean(header.Name)
		if !safeArchivePath(name) || name != header.Name || header.Size < 0 {
			return unpackedArchive{}, fmt.Errorf("%w: it names %q", ErrNotAnArchive, header.Name)
		}
		if _, duplicate := contents.Entries[name]; duplicate ||
			(name == ManifestName && seenManifest) || (name == MetadataName && seenMetadata) {
			return unpackedArchive{}, fmt.Errorf("%w: it contains %s more than once", ErrArchiveDamaged, name)
		}
		if header.Size > maxControlEntryBytes && (name == ManifestName || name == MetadataName) {
			return unpackedArchive{}, fmt.Errorf("%w: %s is too large", ErrArchiveDamaged, name)
		}
		if header.Size > int64(^uint64(0)>>1)-totalSize {
			return unpackedArchive{}, fmt.Errorf("%w: its expanded size overflows", ErrArchiveDamaged)
		}
		totalSize += header.Size
		if err := enoughUnpackSpace(dir, header.Size); err != nil {
			return unpackedArchive{}, err
		}

		switch name {
		case ManifestName:
			if err := json.NewDecoder(reader).Decode(&contents.Manifest); err != nil {
				return unpackedArchive{}, fmt.Errorf("%w: %s", ErrNotAnArchive, err)
			}
			seenManifest = true
			continue
		case MetadataName:
			if err := json.NewDecoder(reader).Decode(&contents.Metadata); err != nil {
				return unpackedArchive{}, fmt.Errorf("%w: %s", ErrNotAnArchive, err)
			}
			seenMetadata = true
			continue
		}

		if err := writeUnpacked(reader, filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return unpackedArchive{}, err
		}
		contents.Entries[name] = header.Size
	}

	if !seenManifest {
		return unpackedArchive{}, fmt.Errorf("%w: it has no manifest", ErrNotAnArchive)
	}
	if !seenMetadata {
		return unpackedArchive{}, fmt.Errorf("%w: it has no metadata", ErrArchiveDamaged)
	}
	return contents, nil
}

// safeArchivePath reports whether a slash-separated archive name stays below
// the unpack directory.
func safeArchivePath(name string) bool {
	return name != "" && name != "." && !path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../")
}

// enoughUnpackSpace checks bytes still to be staged against the filesystem.
// Tar entry sizes are enforced by the reader, so checking them before writing
// prevents a highly compressed archive from filling it.
func enoughUnpackSpace(dir string, requiredBytes int64) error {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(dir, &stats); err != nil {
		return fmt.Errorf("measure room to unpack the archive: %w", err)
	}
	available := stats.Bavail * uint64(stats.Bsize)
	required := uint64(requiredBytes) + unpackReserveBytes
	if required > available {
		return fmt.Errorf("%w: it expands to more data than the restore disk can hold", ErrArchiveDamaged)
	}
	return nil
}

var tableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validateArchive checks the archive describes exactly the payload it carries.
// It runs before migrations or repositories are touched.
func validateArchive(contents unpackedArchive) error {
	manifest, metadata := contents.Manifest, contents.Metadata
	if manifest.Format < oldestArchiveFormat || manifest.Format > archiveFormat {
		return nil // Restore maps unsupported formats to their public errors.
	}
	if manifest.SchemaVersion < 1 || manifest.Trips < 0 || manifest.Files < 0 {
		return fmt.Errorf("%w: its manifest contains invalid counts", ErrArchiveDamaged)
	}
	if len(metadata.Files) != manifest.Files {
		return fmt.Errorf("%w: it records %d files but describes %d",
			ErrArchiveDamaged, manifest.Files, len(metadata.Files))
	}

	expected := make(map[string]bool, len(manifest.Tables)+len(metadata.Files))
	seenTables := make(map[string]bool, len(manifest.Tables))
	for _, table := range manifest.Tables {
		if !tableNamePattern.MatchString(table) || seenTables[table] {
			return fmt.Errorf("%w: invalid or duplicate table %q", ErrArchiveDamaged, table)
		}
		seenTables[table] = true
		name := path.Join(DatabaseDirectory, table+".csv")
		if _, found := contents.Entries[name]; !found {
			return fmt.Errorf("%w: %s is missing", ErrArchiveDamaged, name)
		}
		expected[name] = true
	}

	if manifest.Format >= 2 {
		if len(metadata.Tables) != len(manifest.Tables) {
			return fmt.Errorf("%w: its table metadata is incomplete", ErrArchiveDamaged)
		}
		for index, table := range metadata.Tables {
			if table.Name != manifest.Tables[index] {
				return fmt.Errorf("%w: table metadata does not match the manifest", ErrArchiveDamaged)
			}
			name := path.Join(DatabaseDirectory, table.Name+".csv")
			if err := verifyEntry(contents, name, table.SizeBytes, table.ChecksumSHA); err != nil {
				return err
			}
		}
	}

	seenFiles := make(map[string]bool, len(metadata.Files))
	for _, file := range metadata.Files {
		if !safeArchivePath(file.Name) || path.Clean(file.Name) != file.Name || seenFiles[file.Name] {
			return fmt.Errorf("%w: invalid or duplicate media key %q", ErrArchiveDamaged, file.Name)
		}
		seenFiles[file.Name] = true
		name := path.Join(MediaDirectory, file.Name)
		if err := verifyEntry(contents, name, file.SizeBytes, file.ChecksumSHA); err != nil {
			return err
		}
		expected[name] = true
	}
	for name := range contents.Entries {
		if !expected[name] {
			return fmt.Errorf("%w: it contains unexpected entry %s", ErrArchiveDamaged, name)
		}
	}
	return nil
}

// verifyEntry checks an entry's declared size and checksum metadata are usable.
// The bytes themselves are checked by verifyFiles and verifyTables afterwards.
func verifyEntry(contents unpackedArchive, name string, size int64, checksum string) error {
	actual, found := contents.Entries[name]
	if !found || size < 0 || actual != size {
		return fmt.Errorf("%w: %s does not match its recorded size", ErrArchiveDamaged, name)
	}
	decoded, err := hex.DecodeString(checksum)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%w: %s has an invalid checksum", ErrArchiveDamaged, name)
	}
	return nil
}

// writeUnpacked writes one entry to disk, making the directories it needs.
func writeUnpacked(r io.Reader, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("unpack the archive: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("unpack the archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("unpack the archive: %w", err)
	}
	return nil
}

// verifyFiles checks every file against the size and checksum recorded for it.
//
// A photograph that comes back subtly wrong is worse than one that does not come
// back at all: the first is found years later, when nobody remembers what it
// should have looked like.
func verifyFiles(dir string, metadata Metadata) error {
	for _, file := range metadata.Files {
		target := filepath.Join(dir, MediaDirectory, filepath.FromSlash(file.Name))
		if err := verifyStoredEntry(target, file.Name, file.SizeBytes, file.ChecksumSHA); err != nil {
			return err
		}
	}
	return nil
}

// verifyStoredEntry hashes one staged payload and compares it with its metadata.
func verifyStoredEntry(target, name string, size int64, checksum string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("%w: %s is missing from it", ErrArchiveDamaged, name)
	}
	if info.Size() != size {
		return fmt.Errorf("%w: %s is %d bytes, the archive records %d",
			ErrArchiveDamaged, name, info.Size(), size)
	}

	opened, err := os.Open(target)
	if err != nil {
		return fmt.Errorf("read %s from the archive: %w", name, err)
	}
	hasher := sha256.New()
	_, err = io.Copy(hasher, opened)
	_ = opened.Close()
	if err != nil {
		return fmt.Errorf("read %s from the archive: %w", name, err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != checksum {
		return fmt.Errorf("%w: %s does not match its checksum", ErrArchiveDamaged, name)
	}
	return nil
}
