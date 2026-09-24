// Package backup builds, encrypts, stores and rotates copies of the whole
// service.
//
// An archive holds two things, because a trip is two things. The table dump is
// the service put back exactly as it was: accounts, trips, plans, reports and
// the history of what was copied. The uploaded files sit beside it, each
// verified against the checksum its row records, because a database that
// remembers a photograph the disk has lost is not a backup of anything.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The names inside an archive. They are part of its format: a restore looks for
// exactly these, and anything else in the tar is ignored rather than guessed at.
const (
	// ManifestName describes the archive well enough to decide whether to open it.
	ManifestName = "manifest.json"
	// MetadataName lists the uploaded files with the checksums they are verified
	// against.
	MetadataName = "metadata.json"
	// DatabaseDirectory holds one CSV per table, which is what a restore reads
	// to put the service back.
	DatabaseDirectory = "database"
	// MediaDirectory holds the uploaded files, each under its storage key.
	MediaDirectory = "media"
)

// archiveFormat is the version of the layout, not of the product.
//
// A restore refuses a number it does not know rather than reading an archive on
// the assumption that the shape has not changed. The product version travels
// beside it for the reader's benefit; it is not what the restore branches on.
const archiveFormat = 2

// oldestArchiveFormat is the oldest layout this build can restore. Format 1
// did not checksum table dumps, but its required entries can still be checked
// structurally before it is allowed to replace an instance.
const oldestArchiveFormat = 1

// ArchivePrefix is what every archive this service writes is named with, which
// is how a destination's listing tells them from anything else in the directory.
const ArchivePrefix = "tripvault_"

// archiveInstant is how the moment is written into an archive's name, to the
// millisecond: two backups a second apart - which is what pressing the button
// twice gives - must not produce one name.
const archiveInstant = "20060102T150405.000Z"

// Manifest describes the archive well enough to decide whether to open it.
type Manifest struct {
	Format         int       `json:"format"`
	ProductVersion string    `json:"product_version"`
	CreatedAt      time.Time `json:"created_at"`
	Encrypted      bool      `json:"encrypted"`

	// SchemaVersion is the migration the dump was taken at, and Tables the
	// tables it holds, parents first. A restore puts the schema back to that
	// version before loading a single row.
	SchemaVersion int64    `json:"schema_version"`
	Tables        []string `json:"tables"`

	Trips int `json:"trips"`
	Files int `json:"files"`
}

// FileMetadata is what a table dump does not say about an uploaded file: whether
// the bytes that came back are the bytes that went in.
//
// The row in the media table carries the same checksum, but the dump is loaded
// only after every file has been verified, and reading it out of the CSV to do
// that would mean parsing the dump to decide whether to trust it.
type FileMetadata struct {
	// Name is the file's storage key, which is where it sits inside the
	// archive's media directory and where a restore puts it back.
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name"`
	MIME         string    `json:"mime"`
	SizeBytes    int64     `json:"size_bytes"`
	ChecksumSHA  string    `json:"checksum_sha256"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

// TableMetadata records the bytes of one table dump. Format 2 added this so a
// damaged CSV is found before a restore changes the database.
type TableMetadata struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	ChecksumSHA string `json:"checksum_sha256"`
}

// Metadata is the part of the archive the tables are not read for.
type Metadata struct {
	Tables []TableMetadata `json:"tables,omitempty"`
	Files  []FileMetadata  `json:"files"`
}

// Source supplies everything an archive is built from.
type Source interface {
	// SchemaVersion is the migration the database is at.
	SchemaVersion(ctx context.Context) (int64, error)
	// Tables lists the tables to dump, parents before children, which is the
	// order a restore can load them in.
	Tables(ctx context.Context) ([]string, error)
	// DumpTable writes one table as CSV.
	DumpTable(ctx context.Context, table string, w io.Writer) error
	// TripCount is recorded in the manifest, so a listing can say what an
	// archive holds without opening the dump.
	TripCount(ctx context.Context) (int, error)
	// AllMedia lists every stored file of every trip.
	AllMedia(ctx context.Context) ([]domain.Media, error)
	// AdditionalFiles lists stored objects referenced outside the media
	// catalogue, such as account avatars. Their size and checksum are measured
	// from the bytes written to the archive.
	AdditionalFiles(ctx context.Context) ([]FileMetadata, error)
}

// SnapshotSource hands out a Source that reads the whole database through one
// snapshot, so that an archive never holds a row pointing at another row
// written after the copy began.
type SnapshotSource interface {
	WithSnapshot(ctx context.Context, use func(Source) error) error
}

// FileOpener reads the bytes of a stored file.
type FileOpener interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// ArchiveName - names the archive of the service taken at one moment.
//
// Arguments:
//   - at: when the copy was taken.
//   - encrypted: whether the archive is encrypted, which adds an extension.
//
// Returns:
//   - the filename, without any directory.
func ArchiveName(at time.Time, encrypted bool) string {
	name := ArchivePrefix + at.UTC().Format(archiveInstant) + ".tar.gz"
	if encrypted {
		return name + encryptedSuffix
	}
	return name
}

// encryptedSuffix ends the name of every archive ArchiveName writes encrypted.
const encryptedSuffix = ".age"

// ArchiveEncrypted - reports whether an archive's name says it is encrypted.
//
// It is what a listing shows before anything is opened. Restoring does not rely
// on it: the archive's own first bytes decide that.
//
// Arguments:
//   - name: the archive's filename.
//
// Returns:
//   - true for a name ArchiveName gives an encrypted archive.
func ArchiveEncrypted(name string) bool {
	return strings.HasSuffix(name, encryptedSuffix)
}

// MediaPath - where one file sits inside an archive.
//
// It is the storage key under a directory of its own, so a restore knows where
// each file belongs without having to work it out from the rows it is loading.
//
// Arguments:
//   - item: the media record.
//
// Returns:
//   - the path inside the archive.
func MediaPath(item domain.Media) string {
	return path.Join(MediaDirectory, item.StorageKey)
}

// Build - writes the whole service as a gzipped tar to w.
//
// A file that cannot be read fails the backup rather than being skipped. Half an
// archive that looks like a whole one is the failure mode worth avoiding here:
// the point of a backup is that it can be trusted years later, and a silently
// missing photograph is not something anybody would notice in time.
//
// Arguments:
//   - ctx: context bounding the reads.
//   - w: where the archive is written.
//   - source: the database to copy, read through one snapshot by the caller.
//   - files: the store the media bytes are read from.
//   - productVersion: stamped into the manifest for a reader.
//   - at: the instant the archive records as its own.
//   - encrypted: what the manifest says about encryption; the caller wraps w.
//
// Returns:
//   - the manifest that was written, which the caller records against the run.
//   - an error if any part could not be read or written.
func Build(ctx context.Context, w io.Writer, source Source, files FileOpener,
	productVersion string, at time.Time, encrypted bool) (Manifest, error) {
	schema, err := source.SchemaVersion(ctx)
	if err != nil {
		return Manifest{}, err
	}
	tables, err := source.Tables(ctx)
	if err != nil {
		return Manifest{}, err
	}
	trips, err := source.TripCount(ctx)
	if err != nil {
		return Manifest{}, err
	}
	media, err := source.AllMedia(ctx)
	if err != nil {
		return Manifest{}, err
	}
	additional, err := source.AdditionalFiles(ctx)
	if err != nil {
		return Manifest{}, err
	}

	manifest := Manifest{
		Format:         archiveFormat,
		ProductVersion: productVersion,
		CreatedAt:      at.UTC(),
		Encrypted:      encrypted,
		SchemaVersion:  schema,
		Tables:         tables,
		Trips:          trips,
		Files:          len(media) + len(additional),
	}

	metadata := Metadata{
		Tables: make([]TableMetadata, 0, len(tables)),
		Files:  make([]FileMetadata, 0, len(media)+len(additional)),
	}

	compressed := gzip.NewWriter(w)
	archive := tar.NewWriter(compressed)

	// Each table is spooled to a temporary file because tar needs its length in
	// the header. Only one dump is staged at a time, so a large database is never
	// held in memory and does not require room for every table twice.
	for _, table := range tables {
		tableMetadata, err := writeTableEntry(ctx, archive, table, at, source)
		if err != nil {
			return Manifest{}, err
		}
		metadata.Tables = append(metadata.Tables, tableMetadata)
	}

	for _, item := range media {
		file, err := writeMediaEntry(ctx, archive, item, at, files)
		if err != nil {
			return Manifest{}, err
		}
		metadata.Files = append(metadata.Files, file)
	}
	for _, item := range additional {
		file, err := writeAdditionalFileEntry(ctx, archive, item, at, files)
		if err != nil {
			return Manifest{}, err
		}
		metadata.Files = append(metadata.Files, file)
	}

	// Metadata follows the payload in format 2 because its checksums are made
	// from the bytes actually written, not trusted from database rows. A tar
	// reader does not require control entries to come first.
	if err := writeJSONEntry(archive, MetadataName, metadata, at); err != nil {
		return Manifest{}, err
	}
	if err := writeJSONEntry(archive, ManifestName, manifest, at); err != nil {
		return Manifest{}, err
	}

	if err := archive.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close the archive: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return Manifest{}, fmt.Errorf("finish compression: %w", err)
	}
	return manifest, nil
}

// writeTableEntry spools and checksums one table before adding it to the tar.
func writeTableEntry(ctx context.Context, archive *tar.Writer, table string,
	at time.Time, source Source) (TableMetadata, error) {
	temporary, err := os.CreateTemp("", "tripvault-backup-table-*")
	if err != nil {
		return TableMetadata{}, fmt.Errorf("stage %s: %w", table, err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()

	hasher := sha256.New()
	if err := source.DumpTable(ctx, table, io.MultiWriter(temporary, hasher)); err != nil {
		return TableMetadata{}, err
	}
	info, err := temporary.Stat()
	if err != nil {
		return TableMetadata{}, fmt.Errorf("measure %s: %w", table, err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return TableMetadata{}, fmt.Errorf("rewind %s: %w", table, err)
	}
	name := path.Join(DatabaseDirectory, table+".csv")
	if err := writeEntry(archive, name, info.Size(), at, func(to io.Writer) error {
		_, err := io.Copy(to, temporary)
		return err
	}); err != nil {
		return TableMetadata{}, err
	}
	return TableMetadata{
		Name: table, SizeBytes: info.Size(), ChecksumSHA: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// tarMode is the permission recorded for every entry: readable and writable by
// the owner alone. An archive of somebody's trips is not a world-readable file.
const tarMode = 0o600

// writeEntry writes one file into the tar with a known size.
func writeEntry(archive *tar.Writer, name string, size int64, at time.Time,
	body func(io.Writer) error) error {
	header := &tar.Header{
		Name:    name,
		Mode:    tarMode,
		Size:    size,
		ModTime: at.UTC(),
		Format:  tar.FormatPAX,
	}
	if err := archive.WriteHeader(header); err != nil {
		return fmt.Errorf("write the %s header: %w", name, err)
	}
	if err := body(archive); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// writeJSONEntry writes a value as indented JSON, which costs a little space and
// makes an archive something a person can look inside years later.
func writeJSONEntry(archive *tar.Writer, name string, value any, at time.Time) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	encoded = append(encoded, '\n')
	return writeEntry(archive, name, int64(len(encoded)), at, func(to io.Writer) error {
		_, err := to.Write(encoded)
		return err
	})
}

// writeMediaEntry copies one stored file into the archive.
//
// tar needs the length in the header before the body, and the recorded size is
// what the upload actually wrote, so a file whose bytes no longer match its row
// is a real inconsistency and is reported rather than padded over.
func writeMediaEntry(ctx context.Context, archive *tar.Writer, item domain.Media,
	at time.Time, files FileOpener) (FileMetadata, error) {
	reader, err := files.Open(ctx, item.StorageKey)
	if err != nil {
		return FileMetadata{}, fmt.Errorf("read %s for the archive: %w", item.OriginalName, err)
	}
	defer func() { _ = reader.Close() }()

	hasher := sha256.New()
	err = writeEntry(archive, MediaPath(item), item.Size, at, func(to io.Writer) error {
		written, err := io.Copy(io.MultiWriter(to, hasher), reader)
		if err != nil {
			return err
		}
		if written != item.Size {
			return fmt.Errorf("expected %d bytes, read %d", item.Size, written)
		}
		return nil
	})
	if err != nil {
		return FileMetadata{}, err
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if checksum != fmt.Sprintf("%x", item.Checksum) {
		return FileMetadata{}, fmt.Errorf("%s does not match its recorded checksum", item.OriginalName)
	}
	return FileMetadata{
		Name:         item.StorageKey,
		OriginalName: item.OriginalName,
		MIME:         item.MIME,
		SizeBytes:    item.Size,
		ChecksumSHA:  checksum,
		UploadedAt:   item.CreatedAt.UTC(),
	}, nil
}

// writeAdditionalFileEntry stages an object whose database row does not carry
// a byte count, then records the count and checksum measured from its contents.
func writeAdditionalFileEntry(ctx context.Context, archive *tar.Writer, item FileMetadata,
	at time.Time, files FileOpener) (FileMetadata, error) {
	reader, err := files.Open(ctx, item.Name)
	if err != nil {
		return FileMetadata{}, fmt.Errorf("read %s for the archive: %w", item.Name, err)
	}
	defer func() { _ = reader.Close() }()

	temporary, err := os.CreateTemp("", "tripvault-backup-file-*")
	if err != nil {
		return FileMetadata{}, fmt.Errorf("stage %s: %w", item.Name, err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(temporary, hasher), reader)
	if err != nil {
		return FileMetadata{}, fmt.Errorf("read %s for the archive: %w", item.Name, err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return FileMetadata{}, fmt.Errorf("rewind %s: %w", item.Name, err)
	}
	if err := writeEntry(archive, path.Join(MediaDirectory, item.Name), size, at,
		func(to io.Writer) error {
			_, err := io.Copy(to, temporary)
			return err
		}); err != nil {
		return FileMetadata{}, err
	}
	item.SizeBytes = size
	item.ChecksumSHA = hex.EncodeToString(hasher.Sum(nil))
	return item, nil
}
