package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
	"github.com/nir0k/tripvault/backend/internal/secrets"
)

// testInstant is the moment every archive in these tests records as its own.
var testInstant = time.Date(2026, 6, 20, 3, 15, 0, 0, time.UTC)

// fakeSource stands in for the database.
type fakeSource struct {
	schema int64
	tables []string
	rows   map[string]string
	trips  int
	media  []domain.Media
	// additional are the stored files outside the media catalogue: avatars.
	additional []FileMetadata
	// failTable is the table whose dump fails, for the case where the database
	// goes away half way through.
	failTable string
}

// SchemaVersion reports the migration the fake database is at.
func (s *fakeSource) SchemaVersion(context.Context) (int64, error) { return s.schema, nil }

// Tables lists the tables to dump.
func (s *fakeSource) Tables(context.Context) ([]string, error) { return s.tables, nil }

// DumpTable writes the fake rows of one table.
func (s *fakeSource) DumpTable(_ context.Context, table string, w io.Writer) error {
	if table == s.failTable {
		return fmt.Errorf("read %s: the connection went away", table)
	}
	_, err := io.WriteString(w, s.rows[table])
	return err
}

// TripCount reports how many trips the fake database holds.
func (s *fakeSource) TripCount(context.Context) (int, error) { return s.trips, nil }

// AllMedia lists every stored file.
func (s *fakeSource) AllMedia(context.Context) ([]domain.Media, error) { return s.media, nil }

// AdditionalFiles lists the fake account avatars.
func (s *fakeSource) AdditionalFiles(context.Context) ([]FileMetadata, error) {
	return s.additional, nil
}

// WithSnapshot hands the fake itself out: it holds still on its own.
func (s *fakeSource) WithSnapshot(_ context.Context, use func(Source) error) error {
	return use(s)
}

// fakeFiles stands in for the media store.
type fakeFiles map[string][]byte

// Open returns the bytes stored under a key.
func (f fakeFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := f[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// sampleArchive builds a source and a store holding two photographs.
func sampleArchive() (*fakeSource, fakeFiles) {
	first := []byte("the bytes of a photograph")
	second := []byte("the bytes of another")
	firstSum, secondSum := sha256.Sum256(first), sha256.Sum256(second)

	source := &fakeSource{
		schema: 1,
		tables: []string{"users", "trips", "media"},
		rows: map[string]string{
			"users": "id,email\n1,traveller@example.com\n",
			"trips": "id,title\n1,Iceland\n",
			"media": "id,storage_key\n1,2026/06/one.jpg\n",
		},
		trips: 1,
		media: []domain.Media{
			{
				ID: uuid.New(), StorageKey: "2026/06/one.jpg", OriginalName: "one.jpg",
				MIME: "image/jpeg", Size: int64(len(first)), Checksum: firstSum[:],
				CreatedAt: testInstant,
			},
			{
				ID: uuid.New(), StorageKey: "2026/06/two.jpg", OriginalName: "two.jpg",
				MIME: "image/jpeg", Size: int64(len(second)), Checksum: secondSum[:],
				CreatedAt: testInstant,
			},
		},
	}
	return source, fakeFiles{"2026/06/one.jpg": first, "2026/06/two.jpg": second}
}

// readArchive unpacks a gzipped tar into its entries.
func readArchive(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()

	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the archive is not gzipped: %v", err)
	}
	entries := map[string][]byte{}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s out of the archive: %v", header.Name, err)
		}
		entries[header.Name] = body
	}
}

// TestBuildHoldsTablesAndFiles checks an archive carries every table and every
// photograph, and says in its manifest what it holds.
func TestBuildHoldsTablesAndFiles(t *testing.T) {
	source, files := sampleArchive()

	var raw bytes.Buffer
	manifest, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false)
	if err != nil {
		t.Fatalf("Build() returned an unexpected error: %v", err)
	}
	if manifest.Format != archiveFormat || manifest.SchemaVersion != 1 ||
		manifest.Trips != 1 || manifest.Files != 2 || manifest.ProductVersion != "0.0.1" {
		t.Errorf("unexpected manifest: %+v", manifest)
	}
	if manifest.Encrypted {
		t.Error("the manifest claims encryption the caller did not ask for")
	}

	entries := readArchive(t, raw.Bytes())
	for _, want := range []string{
		ManifestName, MetadataName,
		"database/users.csv", "database/trips.csv", "database/media.csv",
		"media/2026/06/one.jpg", "media/2026/06/two.jpg",
	} {
		if _, ok := entries[want]; !ok {
			t.Errorf("the archive has no %s", want)
		}
	}
	if got := string(entries["database/trips.csv"]); got != source.rows["trips"] {
		t.Errorf("the dump of trips is %q", got)
	}
	if got := string(entries["media/2026/06/one.jpg"]); got != "the bytes of a photograph" {
		t.Errorf("the photograph came out as %q", got)
	}

	// The manifest inside the archive must say the same as the one handed back,
	// because a restore reads the one inside.
	var stored Manifest
	if err := json.Unmarshal(entries[ManifestName], &stored); err != nil {
		t.Fatalf("the manifest is not readable JSON: %v", err)
	}
	if stored.SchemaVersion != manifest.SchemaVersion || len(stored.Tables) != len(manifest.Tables) {
		t.Errorf("the stored manifest differs: %+v", stored)
	}

	var metadata Metadata
	if err := json.Unmarshal(entries[MetadataName], &metadata); err != nil {
		t.Fatalf("the metadata is not readable JSON: %v", err)
	}
	if len(metadata.Files) != 2 {
		t.Fatalf("the metadata lists %d files, want 2", len(metadata.Files))
	}
	sum := sha256.Sum256([]byte("the bytes of a photograph"))
	if metadata.Files[0].ChecksumSHA != fmt.Sprintf("%x", sum) {
		t.Errorf("the recorded checksum is %s", metadata.Files[0].ChecksumSHA)
	}
}

// TestBuildIncludesAccountAvatars checks whole-instance files do not stop at
// the trip media catalogue.
func TestBuildIncludesAccountAvatars(t *testing.T) {
	source, files := sampleArchive()
	avatar := []byte("avatar bytes")
	files["avatars/account.jpg"] = avatar
	source.additional = []FileMetadata{{
		Name: "avatars/account.jpg", OriginalName: "avatar.jpg", MIME: "image/jpeg",
		UploadedAt: testInstant,
	}}
	var raw bytes.Buffer
	manifest, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false)
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	if manifest.Files != 3 {
		t.Errorf("manifest files = %d, want 3", manifest.Files)
	}
	entries := readArchive(t, raw.Bytes())
	if got := entries["media/avatars/account.jpg"]; !bytes.Equal(got, avatar) {
		t.Errorf("avatar came out as %q", got)
	}
}

// TestBuildRefusesAFileThatDoesNotMatchItsRow checks a photograph the disk has
// lost, or one whose bytes no longer match, fails the backup instead of being
// quietly left out.
func TestBuildRefusesAFileThatDoesNotMatchItsRow(t *testing.T) {
	source, files := sampleArchive()
	files["2026/06/two.jpg"] = []byte("shorter than the row says")

	var raw bytes.Buffer
	if _, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false); err == nil {
		t.Error("an archive was written with a file that does not match its row")
	}

	delete(files, "2026/06/two.jpg")
	raw.Reset()
	if _, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false); err == nil {
		t.Error("an archive was written with a file the store has lost")
	}
}

// TestBuildReportsATableItCouldNotRead checks a database that goes away half way
// through fails the run rather than producing an archive missing a table.
func TestBuildReportsATableItCouldNotRead(t *testing.T) {
	source, files := sampleArchive()
	source.failTable = "trips"

	var raw bytes.Buffer
	if _, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false); err == nil {
		t.Error("an archive was written without a table it could not read")
	}
}

// TestArchiveName checks the name carries the instant to the millisecond and
// says whether the archive is encrypted.
func TestArchiveName(t *testing.T) {
	name := ArchiveName(testInstant, false)
	if name != "tripvault_20260620T031500.000Z.tar.gz" {
		t.Errorf("name = %q", name)
	}
	if encrypted := ArchiveName(testInstant, true); encrypted != name+".age" {
		t.Errorf("encrypted name = %q", encrypted)
	}
	// Two backups a second apart - which is what pressing the button twice
	// gives - must not produce one name.
	if ArchiveName(testInstant.Add(time.Second), false) == name {
		t.Error("two archives a second apart share a name")
	}
}

// TestEncryptionRoundTrip checks an archive written with a passphrase comes back
// with it and with nothing else.
func TestEncryptionRoundTrip(t *testing.T) {
	const plain = "the archive's bytes"
	encryption := Encryption{Passphrase: "correct horse battery staple"}

	var sealed bytes.Buffer
	writer, err := encryption.Wrap(&sealed)
	if err != nil {
		t.Fatalf("Wrap() returned an unexpected error: %v", err)
	}
	if _, err := io.WriteString(writer, plain); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the encrypted writer: %v", err)
	}
	if bytes.Contains(sealed.Bytes(), []byte(plain)) {
		t.Fatal("the encrypted archive contains the plain bytes")
	}

	opened, err := encryption.Decrypt(bytes.NewReader(sealed.Bytes()))
	if err != nil {
		t.Fatalf("Decrypt() returned an unexpected error: %v", err)
	}
	got, _ := io.ReadAll(opened)
	if string(got) != plain {
		t.Errorf("the archive came back as %q", got)
	}

	wrong := Encryption{Passphrase: "something else"}
	if _, err := wrong.Decrypt(bytes.NewReader(sealed.Bytes())); err == nil {
		t.Error("the wrong passphrase opened the archive")
	}
}

// TestEncryptionOffPassesTheBytesThrough checks a configuration that writes in
// the clear uses the same code path without wrapping anything.
func TestEncryptionOffPassesTheBytesThrough(t *testing.T) {
	var out bytes.Buffer
	encryption := Encryption{}
	if encryption.Enabled() {
		t.Fatal("encryption with no passphrase reports itself enabled")
	}

	writer, err := encryption.Wrap(&out)
	if err != nil {
		t.Fatalf("Wrap() returned an unexpected error: %v", err)
	}
	if _, err := io.WriteString(writer, "plain"); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Closing must not close the caller's writer, which the caller still owns.
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if out.String() != "plain" {
		t.Errorf("out = %q", out.String())
	}

	reader, err := encryption.Decrypt(strings.NewReader("plain"))
	if err != nil {
		t.Fatalf("Decrypt() returned an unexpected error: %v", err)
	}
	if got, _ := io.ReadAll(reader); string(got) != "plain" {
		t.Errorf("read back %q", got)
	}
}

// TestLocalDestinationKeepsArchives covers the whole life of an archive on disk:
// written, listed, read back and removed.
func TestLocalDestinationKeepsArchives(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups")
	destination, err := NewLocalDestination(root)
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}

	name := ArchiveName(testInstant, false)
	written, err := destination.Store(context.Background(), name, strings.NewReader("archive"))
	if err != nil || written != 7 {
		t.Fatalf("Store() returned %d, %v", written, err)
	}

	info, err := os.Stat(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("the archive is not on the disk: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the archive is mode %o, want 600", mode)
	}

	held, err := destination.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned an unexpected error: %v", err)
	}
	if len(held) != 1 || held[0].Name != name || held[0].SizeBytes != 7 {
		t.Fatalf("List() returned %+v", held)
	}

	reader, err := destination.Open(context.Background(), name)
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "archive" {
		t.Errorf("the archive came back as %q", body)
	}

	if err := destination.Remove(context.Background(), name); err != nil {
		t.Fatalf("Remove() returned an unexpected error: %v", err)
	}
	// Removing an archive that has already gone is how rotation finds a
	// directory somebody tidied by hand, and is not a failure.
	if err := destination.Remove(context.Background(), name); err != nil {
		t.Errorf("removing an archive twice returned %v", err)
	}
	if _, err := destination.Open(context.Background(), name); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Open() after Remove() returned %v, want ErrNotFound", err)
	}
}

// TestLocalDestinationNeverReplacesAnArchive checks the one thing a backup must
// not do: destroy a copy that is already there.
func TestLocalDestinationNeverReplacesAnArchive(t *testing.T) {
	destination, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}

	name := ArchiveName(testInstant, false)
	if _, err := destination.Store(context.Background(), name, strings.NewReader("the good one")); err != nil {
		t.Fatalf("Store() returned an unexpected error: %v", err)
	}
	if _, err := destination.Store(context.Background(), name, strings.NewReader("the new one")); err == nil {
		t.Fatal("a second archive replaced the first")
	}

	reader, _ := destination.Open(context.Background(), name)
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "the good one" {
		t.Errorf("the archive on the disk is now %q", body)
	}
}

// TestLocalDestinationRefusesNamesThatAreNotArchives checks a name out of a
// database row cannot walk out of the backup directory, and that a listing
// ignores whatever else is lying about.
func TestLocalDestinationRefusesNamesThatAreNotArchives(t *testing.T) {
	root := t.TempDir()
	destination, err := NewLocalDestination(root)
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}

	for _, name := range []string{"", "../escape.tar.gz", "nested/one.tar.gz", ".hidden"} {
		if _, err := destination.Store(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Errorf("Store(%q) was accepted", name)
		}
		if _, err := destination.Open(context.Background(), name); err == nil {
			t.Errorf("Open(%q) was accepted", name)
		}
	}

	// A partial file from an interrupted run, and anything else somebody left
	// in the directory, are not archives this service holds.
	for _, name := range []string{".partial-123", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	held, err := destination.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned an unexpected error: %v", err)
	}
	if len(held) != 0 {
		t.Errorf("List() returned %+v, want nothing", held)
	}
}

// TestLocalDestinationListsNewestFirst checks rotation is offered the archives in
// the order it reasons about them.
func TestLocalDestinationListsNewestFirst(t *testing.T) {
	destination, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}

	var names []string
	for day := 1; day <= 3; day++ {
		name := ArchiveName(testInstant.AddDate(0, 0, day), false)
		names = append(names, name)
		if _, err := destination.Store(context.Background(), name, strings.NewReader("x")); err != nil {
			t.Fatalf("Store() returned an unexpected error: %v", err)
		}
	}

	held, err := destination.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned an unexpected error: %v", err)
	}
	if len(held) != 3 || held[0].Name != names[2] || held[2].Name != names[0] {
		t.Errorf("List() returned %+v, want newest first", held)
	}
}

// TestResolverOpensTheLocalDestination checks a configuration naming the disk is
// given the destination that was checked at start-up, and that an unimplemented
// one is refused rather than silently doing nothing.
func TestResolverOpensTheLocalDestination(t *testing.T) {
	local, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}
	resolver := NewResolver(local, nil, nil)

	destination, closer, err := resolver.Resolve(context.Background(),
		domain.BackupConfig{DestinationType: domain.BackupDestinationLocal})
	if err != nil {
		t.Fatalf("Resolve() returned an unexpected error: %v", err)
	}
	if destination != Destination(local) {
		t.Error("Resolve() returned a destination other than the one built at start-up")
	}
	if err := closer(); err != nil {
		t.Errorf("closing the local destination returned %v", err)
	}

	if _, _, err := resolver.Resolve(context.Background(),
		domain.BackupConfig{DestinationType: "dropbox"}); err == nil {
		t.Error("a destination this build does not implement was resolved")
	}
}

// TestResolverOpensTheArchivePassphrase checks the passphrase is unsealed only
// when it is needed, and that an instance which has lost its key says so instead
// of writing an archive in the clear.
func TestResolverOpensTheArchivePassphrase(t *testing.T) {
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() returned an unexpected error: %v", err)
	}
	box, err := secrets.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox() returned an unexpected error: %v", err)
	}
	sealed, err := box.Seal("correct horse battery staple")
	if err != nil {
		t.Fatalf("Seal() returned an unexpected error: %v", err)
	}

	config := domain.BackupConfig{
		DestinationType: domain.BackupDestinationLocal,
		Encrypted:       true,
		SealedSecrets:   map[string][]byte{domain.BackupSecretPassphrase: sealed},
	}

	encryption, err := NewResolver(nil, box, nil).Encryption(config)
	if err != nil {
		t.Fatalf("Encryption() returned an unexpected error: %v", err)
	}
	if encryption.Passphrase != "correct horse battery staple" {
		t.Errorf("the passphrase came back as %q", encryption.Passphrase)
	}

	// A configuration that writes in the clear needs no key at all.
	plain, err := NewResolver(nil, nil, nil).Encryption(domain.BackupConfig{})
	if err != nil || plain.Enabled() {
		t.Errorf("an unencrypted configuration resolved to %+v, %v", plain, err)
	}

	// The key is gone, which is what an instance restored without its secrets
	// file looks like. Writing the archive unencrypted instead would be worse
	// than failing.
	if _, err := NewResolver(nil, nil, nil).Encryption(config); !errors.Is(err, ErrSecretsUnavailable) {
		t.Errorf("Encryption() without a key returned %v, want ErrSecretsUnavailable", err)
	}
}

// fakeRuns records the history the way the database would, well enough for
// retention to reason about it.
type fakeRuns struct {
	started  []domain.BackupRun
	finished map[uuid.UUID]domain.BackupRun
	rotated  []uuid.UUID
	// failStart makes recording the start of a run fail, which is what a
	// database that has gone away looks like.
	failStart bool
}

// newFakeRuns builds an empty history.
func newFakeRuns() *fakeRuns {
	return &fakeRuns{finished: map[uuid.UUID]domain.BackupRun{}}
}

// StartRun records that a backup has begun.
func (f *fakeRuns) StartRun(_ context.Context, run domain.BackupRun) (domain.BackupRun, error) {
	if f.failStart {
		return domain.BackupRun{}, errors.New("the database is not there")
	}
	run.Status = domain.BackupRunning
	f.started = append(f.started, run)
	return run, nil
}

// FinishRun records how a backup ended.
func (f *fakeRuns) FinishRun(_ context.Context, id uuid.UUID, status string, size *int64,
	artifact, message string) error {
	finished := time.Now().UTC()
	f.finished[id] = domain.BackupRun{
		ID: id, Status: status, SizeBytes: size, Artifact: artifact,
		ErrorMessage: message, FinishedAt: &finished,
	}
	return nil
}

// HeldArtifacts returns the archives beyond the newest the configuration keeps.
func (f *fakeRuns) HeldArtifacts(_ context.Context, configID uuid.UUID, retain int) (
	[]domain.BackupRun, error) {
	held := f.heldFor(configID)
	if len(held) <= retain {
		return nil, nil
	}
	return held[retain:], nil
}

// ExpiredArtifacts returns the archives taken before an instant.
func (f *fakeRuns) ExpiredArtifacts(_ context.Context, configID uuid.UUID, before time.Time) (
	[]domain.BackupRun, error) {
	var stale []domain.BackupRun
	for _, run := range f.heldFor(configID) {
		if run.StartedAt.Before(before) {
			stale = append(stale, run)
		}
	}
	return stale, nil
}

// heldFor lists the archives one configuration still holds, newest first.
func (f *fakeRuns) heldFor(configID uuid.UUID) []domain.BackupRun {
	var held []domain.BackupRun
	for i := len(f.started) - 1; i >= 0; i-- {
		run := f.started[i]
		if run.ConfigID != configID {
			continue
		}
		outcome, ok := f.finished[run.ID]
		if !ok || !outcome.ArchiveHeld() {
			continue
		}
		run.Status, run.Artifact = outcome.Status, outcome.Artifact
		if !slicesContains(f.rotated, run.ID) {
			held = append(held, run)
		}
	}
	return held
}

// MarkRotated records that a run's archive has been removed.
func (f *fakeRuns) MarkRotated(_ context.Context, id uuid.UUID) error {
	f.rotated = append(f.rotated, id)
	return nil
}

// slicesContains reports whether a list holds an identifier.
func slicesContains(list []uuid.UUID, want uuid.UUID) bool {
	for _, id := range list {
		if id == want {
			return true
		}
	}
	return false
}

// newTestRunner builds a runner writing to a fresh directory.
func newTestRunner(t *testing.T, runs RunRecorder) (*Runner, *LocalDestination) {
	t.Helper()

	source, files := sampleArchive()
	local, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRunner(source, files, NewResolver(local, nil, nil), runs, logger, "0.0.1"), local
}

// TestRunnerWritesAnArchiveAndRecordsIt covers a whole successful run: the
// archive lands at its destination and the history says what it wrote.
func TestRunnerWritesAnArchiveAndRecordsIt(t *testing.T) {
	history := newFakeRuns()
	runner, local := newTestRunner(t, history)
	config := domain.BackupConfig{ID: uuid.New(), DestinationType: domain.BackupDestinationLocal}

	run, err := runner.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned an unexpected error: %v", err)
	}
	if run.Status != domain.BackupSuccess || run.Artifact == "" || run.SizeBytes == nil {
		t.Fatalf("the run finished as %+v", run)
	}

	held, err := local.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned an unexpected error: %v", err)
	}
	if len(held) != 1 || held[0].Name != run.Artifact {
		t.Fatalf("the destination holds %+v", held)
	}

	// The row is written before the work and closed after it, so an interrupted
	// run shows as one that never finished.
	if len(history.started) != 1 || history.started[0].Status != domain.BackupRunning {
		t.Fatalf("the history recorded %+v", history.started)
	}
	outcome := history.finished[run.ID]
	if outcome.Status != domain.BackupSuccess || outcome.Artifact != run.Artifact {
		t.Errorf("the run was recorded as %+v", outcome)
	}

	// What was written must be an archive, not merely a file of the right name.
	reader, err := local.Open(context.Background(), run.Artifact)
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	defer func() { _ = reader.Close() }()
	body, _ := io.ReadAll(reader)
	if _, ok := readArchive(t, body)[ManifestName]; !ok {
		t.Error("what was stored is not an archive")
	}
}

// TestRunnerEncryptsWhenAsked checks an encrypted configuration writes an
// archive that only its passphrase opens, and names it so.
func TestRunnerEncryptsWhenAsked(t *testing.T) {
	key, _ := secrets.GenerateKey()
	box, err := secrets.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox() returned an unexpected error: %v", err)
	}
	sealed, err := box.Seal("correct horse battery staple")
	if err != nil {
		t.Fatalf("Seal() returned an unexpected error: %v", err)
	}

	source, files := sampleArchive()
	local, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination() returned an unexpected error: %v", err)
	}
	history := newFakeRuns()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(source, files, NewResolver(local, box, nil), history, logger, "0.0.1")

	config := domain.BackupConfig{
		ID:              uuid.New(),
		DestinationType: domain.BackupDestinationLocal,
		Encrypted:       true,
		SealedSecrets:   map[string][]byte{domain.BackupSecretPassphrase: sealed},
	}

	run, err := runner.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run() returned an unexpected error: %v", err)
	}
	if !strings.HasSuffix(run.Artifact, ".age") {
		t.Errorf("an encrypted archive was named %q", run.Artifact)
	}

	reader, err := local.Open(context.Background(), run.Artifact)
	if err != nil {
		t.Fatalf("Open() returned an unexpected error: %v", err)
	}
	defer func() { _ = reader.Close() }()
	stored, _ := io.ReadAll(reader)

	if _, err := gzip.NewReader(bytes.NewReader(stored)); err == nil {
		t.Error("the stored archive is readable without the passphrase")
	}
	plain, err := Encryption{Passphrase: "correct horse battery staple"}.Decrypt(bytes.NewReader(stored))
	if err != nil {
		t.Fatalf("the passphrase did not open the archive: %v", err)
	}
	opened, _ := io.ReadAll(plain)
	if _, ok := readArchive(t, opened)[ManifestName]; !ok {
		t.Error("what the passphrase opened is not an archive")
	}
}

// TestRunnerRecordsAFailure checks a run that could not be carried out leaves a
// row saying why, rather than nothing at all.
func TestRunnerRecordsAFailure(t *testing.T) {
	history := newFakeRuns()
	runner, _ := newTestRunner(t, history)
	// An encrypted configuration on an instance with no secrets key cannot
	// write an archive, and must not write one in the clear instead.
	config := domain.BackupConfig{
		ID: uuid.New(), DestinationType: domain.BackupDestinationLocal, Encrypted: true,
		SealedSecrets: map[string][]byte{domain.BackupSecretPassphrase: []byte("sealed")},
	}

	run, err := runner.Run(context.Background(), config)
	if err == nil {
		t.Fatal("a backup that could not be encrypted was reported as done")
	}
	if run.Status != domain.BackupFailed || run.ErrorMessage == "" {
		t.Fatalf("the run finished as %+v", run)
	}
	if outcome := history.finished[history.started[0].ID]; outcome.Status != domain.BackupFailed ||
		outcome.ErrorMessage == "" {
		t.Errorf("the failure was recorded as %+v", outcome)
	}
}

// TestRunnerRefusesASecondRunOfTheSameConfiguration checks two runs of one
// configuration cannot race each other's retention, and that no row is written
// for the one that was refused.
func TestRunnerRefusesASecondRunOfTheSameConfiguration(t *testing.T) {
	history := newFakeRuns()
	runner, _ := newTestRunner(t, history)
	config := domain.BackupConfig{ID: uuid.New(), DestinationType: domain.BackupDestinationLocal}

	if _, err := runner.claim(config.ID); err != nil {
		t.Fatalf("claim() returned an unexpected error: %v", err)
	}
	if _, err := runner.Run(context.Background(), config); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Run() on a busy configuration returned %v, want ErrAlreadyRunning", err)
	}
	if len(history.started) != 0 {
		t.Errorf("a refused run left %d rows behind", len(history.started))
	}

	// Another configuration is not blocked by it.
	runner.release(config.ID)
	if _, err := runner.Run(context.Background(), config); err != nil {
		t.Errorf("Run() after the first finished returned %v", err)
	}
}

// TestRunnerAndRestoreKeepOutOfEachOthersWay checks a restore is refused while
// a backup is being written, and every backup while a restore is held.
func TestRunnerAndRestoreKeepOutOfEachOthersWay(t *testing.T) {
	history := newFakeRuns()
	runner, _ := newTestRunner(t, history)
	config := domain.BackupConfig{ID: uuid.New(), DestinationType: domain.BackupDestinationLocal}

	if _, err := runner.claim(config.ID); err != nil {
		t.Fatalf("claim() returned an unexpected error: %v", err)
	}
	if _, err := runner.HoldForRestore(); !errors.Is(err, ErrBackupRunning) {
		t.Errorf("HoldForRestore() during a backup returned %v, want ErrBackupRunning", err)
	}
	runner.release(config.ID)

	resume, err := runner.HoldForRestore()
	if err != nil {
		t.Fatalf("HoldForRestore() returned an unexpected error: %v", err)
	}
	if _, err := runner.Run(context.Background(), config); !errors.Is(err, ErrRestoring) {
		t.Errorf("Run() during a restore returned %v, want ErrRestoring", err)
	}
	if len(history.started) != 0 {
		t.Errorf("a refused run left %d rows behind", len(history.started))
	}
	resume()
	if _, err := runner.Run(context.Background(), config); err != nil {
		t.Errorf("Run() after the restore returned %v", err)
	}
}

// TestRunnerKeepsOnlyWhatTheConfigurationAsksFor checks retention removes the
// archives beyond the count kept, never the one just written.
func TestRunnerKeepsOnlyWhatTheConfigurationAsksFor(t *testing.T) {
	history := newFakeRuns()
	runner, local := newTestRunner(t, history)
	config := domain.BackupConfig{
		ID: uuid.New(), DestinationType: domain.BackupDestinationLocal, RetainCount: 2,
	}

	var names []string
	for range 4 {
		run, err := runner.Run(context.Background(), config)
		if err != nil {
			t.Fatalf("Run() returned an unexpected error: %v", err)
		}
		names = append(names, run.Artifact)
		// The name carries the instant to the millisecond, so runs in the same
		// millisecond would collide with each other rather than rotate.
		time.Sleep(2 * time.Millisecond)
	}

	held, err := local.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned an unexpected error: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("the destination holds %d archives, want 2", len(held))
	}
	if held[0].Name != names[3] || held[1].Name != names[2] {
		t.Errorf("the archives kept are %s and %s", held[0].Name, held[1].Name)
	}
	if len(history.rotated) != 2 {
		t.Errorf("%d archives were recorded as rotated, want 2", len(history.rotated))
	}
}

// TestParseSchedule checks the expressions a crontab accepts, and that the
// second field a different parser would take is refused rather than shifting
// every other field along.
func TestParseSchedule(t *testing.T) {
	for _, expression := range []string{"0 3 * * *", "@daily", "*/15 * * * *", "  0 3 * * *  "} {
		if _, err := ParseSchedule(expression); err != nil {
			t.Errorf("ParseSchedule(%q) returned %v", expression, err)
		}
	}
	for _, expression := range []string{"", "   ", "every night", "0 3 * * * *", "99 * * * *"} {
		if _, err := ParseSchedule(expression); err == nil {
			t.Errorf("ParseSchedule(%q) was accepted", expression)
		}
	}
}

// TestSchedulerRunsWhatIsDue checks the three cases the scheduler decides
// between: never run, run recently, and run long enough ago.
func TestSchedulerRunsWhatIsDue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	scheduler := NewScheduler(nil, nil, logger, time.Minute)
	now := time.Date(2026, 6, 20, 3, 30, 0, 0, time.UTC)

	cases := map[string]struct {
		scheduled ScheduledConfig
		want      bool
	}{
		"never run": {ScheduledConfig{Config: domain.BackupConfig{ScheduleCron: "0 3 * * *"}}, true},
		"ran this morning": {ScheduledConfig{
			Config:        domain.BackupConfig{ScheduleCron: "0 3 * * *"},
			LastStartedAt: now.Add(-30 * time.Minute),
		}, false},
		// The service was down over the scheduled hour, so the backup is taken
		// when it comes back rather than skipped for the day.
		"ran yesterday": {ScheduledConfig{
			Config:        domain.BackupConfig{ScheduleCron: "0 3 * * *"},
			LastStartedAt: now.AddDate(0, 0, -1),
		}, true},
		"unreadable schedule": {ScheduledConfig{
			Config: domain.BackupConfig{ScheduleCron: "every night"},
		}, false},
	}

	for name, test := range cases {
		if got := scheduler.isDue(test.scheduled, now); got != test.want {
			t.Errorf("%s: due = %v, want %v", name, got, test.want)
		}
	}
}

// fakeSchedule hands the scheduler a fixed set of configurations.
type fakeSchedule []ScheduledConfig

// DueConfigs returns the configurations the scheduler has to consider.
func (f fakeSchedule) DueConfigs(context.Context) ([]ScheduledConfig, error) {
	return f, nil
}

// TestSchedulerPassRunsOnlyTheDueConfiguration checks a pass carries out the
// configuration that has come due and leaves the other alone.
func TestSchedulerPassRunsOnlyTheDueConfiguration(t *testing.T) {
	history := newFakeRuns()
	runner, _ := newTestRunner(t, history)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	due := domain.BackupConfig{ID: uuid.New(), DestinationType: domain.BackupDestinationLocal,
		ScheduleCron: "0 3 * * *"}
	notDue := domain.BackupConfig{ID: uuid.New(), DestinationType: domain.BackupDestinationLocal,
		ScheduleCron: "0 3 * * *"}

	now := time.Date(2026, 6, 20, 3, 30, 0, 0, time.UTC)
	store := fakeSchedule{
		{Config: due},
		{Config: notDue, LastStartedAt: now.Add(-30 * time.Minute)},
	}

	NewScheduler(store, runner, logger, time.Minute).pass(context.Background(), now)

	if len(history.started) != 1 || history.started[0].ConfigID != due.ID {
		t.Errorf("the pass ran %+v", history.started)
	}
}

// restoreLog records, in order, what a restore asked of the database and the
// media store, which is how a test sees that nothing live moved too early.
type restoreLog struct {
	steps []string
}

// add appends one step.
func (l *restoreLog) add(step string) { l.steps = append(l.steps, step) }

// fakeDatabase builds the replacement schema a restore loads into.
type fakeDatabase struct {
	log    *restoreLog
	latest int64
	stage  *fakeDatabaseStage
}

// Latest is the newest migration this build knows.
func (d *fakeDatabase) Latest() int64 { return d.latest }

// BeginRestore opens a staged schema at the archive's version.
func (d *fakeDatabase) BeginRestore(_ context.Context, version int64, _ []string) (DatabaseStage, error) {
	d.log.add(fmt.Sprintf("database: begin at %d", version))
	d.stage = &fakeDatabaseStage{log: d.log}
	return d.stage, nil
}

// fakeDatabaseStage records the tables a restore loaded and what was in them.
type fakeDatabaseStage struct {
	log    *restoreLog
	loaded map[string]string
	order  []string
}

// LoadTables reads every table's CSV out of the archive.
func (s *fakeDatabaseStage) LoadTables(_ context.Context, tables []string,
	open func(table string) (io.ReadCloser, error)) error {
	s.log.add("database: load")
	s.loaded = map[string]string{}
	s.order = append([]string(nil), tables...)
	for _, table := range tables {
		file, err := open(table)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return err
		}
		s.loaded[table] = string(body)
	}
	return nil
}

// Upgrade brings the staged schema to this build.
func (s *fakeDatabaseStage) Upgrade(context.Context) error {
	s.log.add("database: upgrade")
	return nil
}

// Activate swaps the staged schema in.
func (s *fakeDatabaseStage) Activate(context.Context) error {
	s.log.add("database: activate")
	return nil
}

// Rollback puts the previous schema back.
func (s *fakeDatabaseStage) Rollback(context.Context) error {
	s.log.add("database: rollback")
	return nil
}

// Finish drops the previous schema.
func (s *fakeDatabaseStage) Finish(context.Context) error { s.log.add("database: finish"); return nil }

// Close releases the stage.
func (s *fakeDatabaseStage) Close(context.Context) { s.log.add("database: close") }

// fakeMediaStore builds the replacement media a restore writes.
type fakeMediaStore struct {
	log   *restoreLog
	files map[string][]byte
	// activateErr is what switching the files over fails with, if anything.
	activateErr error
}

// BeginRestore opens a staged media tree.
func (m *fakeMediaStore) BeginRestore(context.Context) (FileStage, error) {
	m.log.add("files: begin")
	m.files = map[string][]byte{}
	return &fakeFileStage{store: m}, nil
}

// fakeFileStage collects the files a restore puts back.
type fakeFileStage struct {
	store *fakeMediaStore
}

// Put stores one file's bytes.
func (f *fakeFileStage) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	f.store.files[key] = body
	return int64(len(body)), nil
}

// Activate switches the staged tree in, or fails as the test asks.
func (f *fakeFileStage) Activate(context.Context) error {
	f.store.log.add("files: activate")
	return f.store.activateErr
}

// Rollback puts the previous tree back.
func (f *fakeFileStage) Rollback(context.Context) error {
	f.store.log.add("files: rollback")
	return nil
}

// Finish removes the previous tree.
func (f *fakeFileStage) Finish(context.Context) error { f.store.log.add("files: finish"); return nil }

// Close releases the stage.
func (f *fakeFileStage) Close(context.Context) { f.store.log.add("files: close") }

// newRestoreFakes builds a database and a media store sharing one log.
func newRestoreFakes(latest int64) (*fakeDatabase, *fakeMediaStore, *restoreLog) {
	log := &restoreLog{}
	return &fakeDatabase{log: log, latest: latest}, &fakeMediaStore{log: log}, log
}

// buildTestArchive writes the sample instance to a buffer.
func buildTestArchive(t *testing.T) []byte {
	t.Helper()

	source, files := sampleArchive()
	var raw bytes.Buffer
	if _, err := Build(context.Background(), &raw, source, files, "0.0.1", testInstant, false); err != nil {
		t.Fatalf("Build() returned an unexpected error: %v", err)
	}
	return raw.Bytes()
}

// TestRestorePutsTheInstanceBack covers the whole round trip: what Build wrote
// is what Restore loads, into a staged schema and media tree that replace the
// live ones only once both are complete.
func TestRestorePutsTheInstanceBack(t *testing.T) {
	raw := buildTestArchive(t)
	database, files, log := newRestoreFakes(1)

	var stages []string
	result, err := Restore(context.Background(), bytes.NewReader(raw), t.TempDir(), database, files,
		func(stage string) { stages = append(stages, stage) })
	if err != nil {
		t.Fatalf("Restore() returned an unexpected error: %v", err)
	}
	if result.SchemaVersion != 1 || result.Tables != 3 || result.Trips != 1 || result.Files != 2 {
		t.Errorf("the restore reports %+v", result)
	}

	// Rows are loaded into the shape they left, brought up to this build, and
	// only then does anything live move.
	want := []string{"database: begin at 1", "database: load", "database: upgrade", "files: begin",
		"database: activate", "files: activate", "database: finish", "files: finish",
		"files: close", "database: close"}
	if strings.Join(log.steps, "; ") != strings.Join(want, "; ") {
		t.Errorf("the restore went\n  %v\nwant\n  %v", log.steps, want)
	}
	if strings.Join(stages, ",") != "validating,staging,cutover" {
		t.Errorf("the stages reported were %v", stages)
	}

	if got := database.stage.loaded["trips"]; got != "id,title\n1,Iceland\n" {
		t.Errorf("trips came back as %q", got)
	}
	if len(database.stage.order) != 3 || database.stage.order[0] != "users" {
		t.Errorf("the tables were loaded as %v, want parents first", database.stage.order)
	}
	if string(files.files["2026/06/one.jpg"]) != "the bytes of a photograph" {
		t.Errorf("the photograph came back as %q", files.files["2026/06/one.jpg"])
	}
}

// TestRestoreRollsBackWhenTheFilesCannotSwitch checks a failed switch of the
// media puts the database back too, so the instance is never half restored.
func TestRestoreRollsBackWhenTheFilesCannotSwitch(t *testing.T) {
	raw := buildTestArchive(t)
	database, files, log := newRestoreFakes(1)
	files.activateErr = errors.New("the disk is full")

	_, err := Restore(context.Background(), bytes.NewReader(raw), t.TempDir(), database, files, nil)
	if err == nil || !strings.Contains(err.Error(), "the disk is full") {
		t.Fatalf("Restore() = %v, want the failed switch", err)
	}
	steps := strings.Join(log.steps, "; ")
	if !strings.Contains(steps, "files: activate; files: rollback; database: rollback") {
		t.Errorf("a failed switch was not rolled back: %v", log.steps)
	}
	if strings.Contains(steps, "finish") {
		t.Errorf("a failed restore dropped the previous generation: %v", log.steps)
	}
}

// TestRestoreReadsFormatOne keeps compatibility with archives written before
// table dump checksums were added.
func TestRestoreReadsFormatOne(t *testing.T) {
	raw := rewriteArchive(t, buildTestArchive(t), func(name string, body []byte) ([]byte, bool) {
		switch name {
		case ManifestName:
			var manifest Manifest
			if err := json.Unmarshal(body, &manifest); err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			manifest.Format = 1
			body, _ = json.Marshal(manifest)
		case MetadataName:
			var metadata Metadata
			if err := json.Unmarshal(body, &metadata); err != nil {
				t.Fatalf("decode metadata: %v", err)
			}
			metadata.Tables = nil
			body, _ = json.Marshal(metadata)
		}
		return body, true
	})
	database, files, _ := newRestoreFakes(1)
	if _, err := Restore(context.Background(), bytes.NewReader(raw), t.TempDir(), database, files, nil); err != nil {
		t.Fatalf("restore format 1: %v", err)
	}
}

// TestRestoreRefusesWhatItCannotTrust covers refusals that must happen
// before anything on the instance is touched.
func TestRestoreRefusesWhatItCannotTrust(t *testing.T) {
	raw := buildTestArchive(t)

	cases := map[string]struct {
		archive []byte
		latest  int64
		want    error
	}{
		"not an archive": {[]byte("a photograph, perhaps"), 1, ErrNotAnArchive},
		// The archive was written by a build with migrations this one does not
		// have, so its rows would go into tables that do not exist.
		"from a newer build": {raw, 0, ErrArchiveTooNew},
		"damaged media":      {damagedArchive(t, raw, "media/2026/06/one.jpg", false), 1, ErrArchiveDamaged},
		"damaged table":      {damagedArchive(t, raw, "database/trips.csv", false), 1, ErrArchiveDamaged},
		"missing table":      {damagedArchive(t, raw, "database/users.csv", true), 1, ErrArchiveDamaged},
	}

	for name, test := range cases {
		database, files, log := newRestoreFakes(test.latest)
		_, err := Restore(context.Background(), bytes.NewReader(test.archive), t.TempDir(), database, files, nil)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: error = %v, want %v", name, err, test.want)
		}
		if len(log.steps) != 0 {
			t.Errorf("%s: a replacement was begun before the archive was trusted: %v", name, log.steps)
		}
	}
}

// damagedArchive rewrites or removes one payload while leaving the control
// entries untouched, like corruption or an incomplete copy would.
func damagedArchive(t *testing.T, raw []byte, target string, remove bool) []byte {
	t.Helper()
	return rewriteArchive(t, raw, func(name string, body []byte) ([]byte, bool) {
		if name == target && remove {
			return nil, false
		}
		if name == target {
			// The same length, so only the checksum can tell.
			body = bytes.Repeat([]byte("x"), len(body))
		}
		return body, true
	})
}

// rewriteArchive applies one test transformation while preserving a valid tar
// and gzip envelope.
func rewriteArchive(t *testing.T, raw []byte,
	rewrite func(name string, body []byte) ([]byte, bool)) []byte {
	t.Helper()

	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}

	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	archive := tar.NewWriter(writer)
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		body, keep := rewrite(header.Name, body)
		if !keep {
			continue
		}
		header.Size = int64(len(body))
		if err := archive.WriteHeader(header); err != nil {
			t.Fatalf("write %s: %v", header.Name, err)
		}
		if _, err := archive.Write(body); err != nil {
			t.Fatalf("write %s: %v", header.Name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close the archive: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("finish compression: %v", err)
	}
	return out.Bytes()
}

// TestPlainTellsAnEncryptedArchiveApart checks the age header is recognised
// before anything tries to gunzip it, and that a passphrase opens what it locked.
func TestPlainTellsAnEncryptedArchiveApart(t *testing.T) {
	encryption := Encryption{Passphrase: "correct horse battery staple"}

	var sealed bytes.Buffer
	writer, err := encryption.Wrap(&sealed)
	if err != nil {
		t.Fatalf("Wrap() returned an unexpected error: %v", err)
	}
	if _, err := writer.Write(buildTestArchive(t)); err != nil {
		t.Fatalf("write the archive: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the encrypted writer: %v", err)
	}

	if _, err := Plain(bytes.NewReader(sealed.Bytes()), Encryption{}); !errors.Is(err, ErrEncrypted) {
		t.Errorf("Plain() without a passphrase returned %v, want ErrEncrypted", err)
	}

	plain, err := Plain(bytes.NewReader(sealed.Bytes()), encryption)
	if err != nil {
		t.Fatalf("Plain() with the passphrase returned %v", err)
	}
	database, files, _ := newRestoreFakes(1)
	if _, err := Restore(context.Background(), plain, t.TempDir(), database, files, nil); err != nil {
		t.Errorf("an opened archive did not restore: %v", err)
	}

	// An archive in the clear passes straight through, so the caller never has
	// to ask which kind it is holding.
	raw := buildTestArchive(t)
	through, err := Plain(bytes.NewReader(raw), Encryption{})
	if err != nil {
		t.Fatalf("Plain() on a plain archive returned %v", err)
	}
	if body, _ := io.ReadAll(through); !bytes.Equal(body, raw) {
		t.Error("a plain archive did not come through unchanged")
	}
}
