package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// ErrAlreadyRunning reports a configuration that is already being carried out.
// Two runs of one configuration at once would race each other's retention and
// write the same instance to the same place twice.
var ErrAlreadyRunning = errors.New("this backup is already running")

// ErrRestoring reports a backup refused because the service is being restored:
// an archive taken while the database is swapped would hold neither instance.
var ErrRestoring = errors.New("the service is being restored")

// ErrBackupRunning reports a restore refused because a backup is still being
// written; replacing the database under it would spoil the archive.
var ErrBackupRunning = errors.New("a backup is running")

// The stages a run reports while it works, in the order it passes through them.
const (
	StageConnecting = "connecting"
	StageReading    = "reading"
	StageArchiving  = "archiving"
	StageRotating   = "rotating"
)

// Progress is how far a run in flight has got.
//
// The file and byte counts are of the uploaded media, which is where nearly all
// of the time goes; the tables themselves are a few hundred kilobytes of text.
type Progress struct {
	Stage      string
	FilesDone  int
	FilesTotal int
	BytesDone  int64
	BytesTotal int64
}

// RunRecorder writes the history of what was attempted, and is what retention
// consults to find out which archives a configuration is holding.
type RunRecorder interface {
	StartRun(ctx context.Context, run domain.BackupRun) (domain.BackupRun, error)
	FinishRun(ctx context.Context, id uuid.UUID, status string, size *int64,
		artifact, message string) error
	HeldArtifacts(ctx context.Context, configID uuid.UUID, retain int) ([]domain.BackupRun, error)
	ExpiredArtifacts(ctx context.Context, configID uuid.UUID, before time.Time) ([]domain.BackupRun, error)
	MarkRotated(ctx context.Context, id uuid.UUID) error
}

// Runner carries out one configuration.
type Runner struct {
	source       SnapshotSource
	files        FileOpener
	destinations *Resolver
	runs         RunRecorder
	logger       *slog.Logger
	version      string

	// active holds the runs in flight, by configuration. It is what refuses a
	// second run of a configuration that is still working, and what a caller
	// asks for progress. Nothing in it outlives the process: a run interrupted
	// by a restart is visible in the history as one that never finished.
	mu     sync.Mutex
	active map[uuid.UUID]*tracker
	// restoring refuses every backup while a restore replaces the service.
	restoring bool
}

// tracker is one run in flight and how far it has got.
type tracker struct {
	mu       sync.Mutex
	runID    uuid.UUID
	progress Progress
}

// update changes the progress under the tracker's lock.
func (t *tracker) update(change func(*Progress)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	change(&t.progress)
}

// NewRunner - builds the runner.
//
// Arguments:
//   - source: reads the database a backup is made of.
//   - files: the store the media bytes come from.
//   - destinations: opens the destination each configuration names, and the
//     passphrase it locks its archives with.
//   - runs: where the history is recorded.
//   - logger: where failures are reported.
//   - version: the product version stamped into each manifest.
//
// Returns:
//   - a runner ready to carry out configurations.
func NewRunner(source SnapshotSource, files FileOpener, destinations *Resolver,
	runs RunRecorder, logger *slog.Logger, version string) *Runner {
	return &Runner{
		source:       source,
		files:        files,
		destinations: destinations,
		runs:         runs,
		logger:       logger,
		version:      version,
		active:       make(map[uuid.UUID]*tracker),
	}
}

// Run - carries out one configuration and records what happened.
//
// A failure is recorded and returned rather than only returned: a backup that
// silently stopped working is the one discovered on the day it was needed, so
// every attempt leaves a row whether it worked or not.
//
// Arguments:
//   - ctx: context bounding the whole run.
//   - config: the configuration to carry out.
//
// Returns:
//   - the finished run, including its status and the archive it wrote.
//   - ErrAlreadyRunning, with no row written, when the configuration is still
//     being carried out by an earlier run, and ErrRestoring while the service
//     is being restored.
//   - an error if the archive could not be produced or stored; the run is
//     already recorded as failed by then.
func (r *Runner) Run(ctx context.Context, config domain.BackupConfig) (domain.BackupRun, error) {
	track, err := r.claim(config.ID)
	if err != nil {
		return domain.BackupRun{}, err
	}
	defer r.release(config.ID)

	run, err := r.begin(ctx, config, track)
	if err != nil {
		return domain.BackupRun{}, err
	}
	return r.carry(ctx, config, run, track)
}

// Start - begins one configuration and returns while it is still working.
//
// The run's row is written before this returns, so the caller has something to
// show and to follow; the work then carries on under work, which outlives the
// request that asked for it. How far it has got is available from Progress until
// it finishes, and the history says how it ended.
//
// Arguments:
//   - ctx: context bounding the start, which is only the writing of the row.
//   - work: context bounding the backup itself; cancelling it abandons the run.
//   - config: the configuration to carry out.
//
// Returns:
//   - the run as it was recorded, still running.
//   - ErrAlreadyRunning when the configuration is still being carried out, and
//     ErrRestoring while the service is being restored.
//   - an error if the start could not be recorded.
func (r *Runner) Start(ctx, work context.Context, config domain.BackupConfig) (domain.BackupRun, error) {
	track, err := r.claim(config.ID)
	if err != nil {
		return domain.BackupRun{}, err
	}

	run, err := r.begin(ctx, config, track)
	if err != nil {
		r.release(config.ID)
		return domain.BackupRun{}, err
	}

	go func() {
		defer r.release(config.ID)
		// The outcome is recorded against the run and logged by carry; there is
		// nobody left waiting to be told.
		_, _ = r.carry(work, config, run, track)
	}()
	return run, nil
}

// Progress - reports how far a run in flight has got.
//
// Arguments:
//   - runID: the run to ask about.
//
// Returns:
//   - the progress, and true while the run is working in this process; false
//     once it has finished, or when it never ran here.
func (r *Runner) Progress(runID uuid.UUID) (Progress, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, track := range r.active {
		track.mu.Lock()
		matches := track.runID == runID
		progress := track.progress
		track.mu.Unlock()
		if matches {
			return progress, true
		}
	}
	return Progress{}, false
}

// HoldForRestore - keeps backups away while the service is being restored.
//
// Returns:
//   - a function that lets backups run again, to be called once the restore
//     has finished, whatever its outcome.
//   - ErrBackupRunning when a backup is being written now; the restore waits
//     for nothing and is refused instead.
func (r *Runner) HoldForRestore() (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.active) > 0 {
		return nil, ErrBackupRunning
	}
	r.restoring = true
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.restoring = false
	}, nil
}

// claim marks a configuration as running, refusing one that already is and any
// at all while the service is being restored.
func (r *Runner) claim(configID uuid.UUID) (*tracker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.restoring {
		return nil, ErrRestoring
	}
	if _, running := r.active[configID]; running {
		return nil, ErrAlreadyRunning
	}
	track := &tracker{progress: Progress{Stage: StageConnecting}}
	r.active[configID] = track
	return track, nil
}

// release marks a configuration as no longer running.
func (r *Runner) release(configID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, configID)
}

// begin writes the run's row before any work is done, so a run interrupted by a
// restart is visible as one that never finished rather than absent.
func (r *Runner) begin(ctx context.Context, config domain.BackupConfig,
	track *tracker) (domain.BackupRun, error) {
	run, err := r.runs.StartRun(ctx, domain.BackupRun{
		ID:        uuid.Must(uuid.NewV7()),
		ConfigID:  config.ID,
		StartedAt: time.Now().UTC(),
		Status:    domain.BackupRunning,
	})
	if err != nil {
		return domain.BackupRun{}, fmt.Errorf("record the start of a backup: %w", err)
	}

	track.mu.Lock()
	track.runID = run.ID
	track.mu.Unlock()
	return run, nil
}

// carry does the work of a run whose row is already written, and records how it
// ended.
func (r *Runner) carry(ctx context.Context, config domain.BackupConfig, run domain.BackupRun,
	track *tracker) (domain.BackupRun, error) {
	// The destination is opened once and used for both the write and the
	// retention that follows it, so a backup to a server is one connection
	// rather than two. A destination that cannot be reached at all is a failed
	// run like any other: it leaves a row saying why.
	destination, closeDestination, err := r.destinations.Resolve(ctx, config)
	if err != nil {
		return r.failRun(ctx, run, config, err)
	}
	defer func() { _ = closeDestination() }()

	name, size, err := r.perform(ctx, config, destination, run.StartedAt, track)
	if err != nil {
		return r.failRun(ctx, run, config, err)
	}

	if err := r.runs.FinishRun(ctx, run.ID, domain.BackupSuccess, &size, name, ""); err != nil {
		return run, fmt.Errorf("record the end of a backup: %w", err)
	}

	// Retention only ever happens after a success. Making room first would mean
	// deleting a good copy for one that then failed to be written.
	track.update(func(p *Progress) { p.Stage = StageRotating })
	removed, err := r.rotate(ctx, config, destination)
	if err != nil {
		// The archive is written and recorded; a directory with one archive too
		// many in it is not a reason to call the backup failed.
		r.logger.Warn("old archives could not be rotated",
			slog.String("backup_config_id", config.ID.String()),
			slog.Any("error", err))
	}

	r.logger.Info("took a backup",
		slog.String("backup_config_id", config.ID.String()),
		slog.String("artifact", name),
		slog.Int64("size_bytes", size),
		slog.Int("rotated_out", removed))

	run.Status = domain.BackupSuccess
	run.Artifact = name
	run.SizeBytes = &size
	return run, nil
}

// failRun records why a backup did not happen and hands the failure back.
//
// Every attempt leaves a row, failures included: a backup that silently stopped
// working is the one discovered on the day it was needed.
func (r *Runner) failRun(ctx context.Context, run domain.BackupRun,
	config domain.BackupConfig, cause error) (domain.BackupRun, error) {
	r.logger.Error("a backup failed",
		slog.String("backup_config_id", config.ID.String()),
		slog.Any("error", cause))

	// The reason is stored so the interface can show it without the operator
	// having to go and read the log. It is written under a context of its own: a
	// run abandoned because its context was cancelled still has to say so.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := r.runs.FinishRun(record, run.ID, domain.BackupFailed, nil, "", cause.Error()); err != nil {
		r.logger.Error("the failure of a backup could not be recorded",
			slog.String("backup_run_id", run.ID.String()),
			slog.Any("error", err))
	}

	run.Status = domain.BackupFailed
	run.ErrorMessage = cause.Error()
	return run, cause
}

// perform builds, encrypts and stores one archive of the whole service.
func (r *Runner) perform(ctx context.Context, config domain.BackupConfig,
	destination Destination, at time.Time, track *tracker) (string, int64, error) {
	encryption, err := r.destinations.Encryption(config)
	if err != nil {
		return "", 0, err
	}

	name := ArchiveName(at, encryption.Enabled())
	files := progressFiles{inner: r.files, track: track}

	// The archive is built into the destination as it is produced rather than
	// into memory first: an instance with a few thousand photographs on it is a
	// large thing to hold twice, and the local destination writes to a temporary
	// name anyway, so nothing incomplete is ever visible under the real one.
	reader, writer := io.Pipe()
	go func() {
		encrypted, err := encryption.Wrap(writer)
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		err = r.source.WithSnapshot(ctx, func(snapshot Source) error {
			if err := countFiles(ctx, snapshot, track); err != nil {
				return err
			}
			_, err := Build(ctx, encrypted, snapshot, files, r.version, at, encryption.Enabled())
			return err
		})
		if err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		// Closing the encrypted writer writes the final authentication tag,
		// without which the archive will not decrypt.
		if err := encrypted.Close(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		_ = writer.Close()
	}()

	size, err := destination.Store(ctx, name, reader)
	// Closing tells the goroutine to stop if the destination gave up early,
	// which would otherwise leave it blocked on a write nobody is reading.
	_ = reader.CloseWithError(err)
	if err != nil {
		return "", 0, err
	}
	return name, size, nil
}

// countFiles gives the progress something to measure against. The files are
// the part of a backup that takes any time at all, and they are counted in the
// same snapshot the archive is written from, so the total is the one reached.
func countFiles(ctx context.Context, snapshot Source, track *tracker) error {
	track.update(func(p *Progress) { p.Stage = StageReading })
	media, err := snapshot.AllMedia(ctx)
	if err != nil {
		return fmt.Errorf("read the files: %w", err)
	}
	var total int64
	for _, item := range media {
		total += item.Size
	}
	track.update(func(p *Progress) {
		p.Stage = StageArchiving
		p.FilesTotal = len(media)
		p.BytesTotal = total
	})
	return nil
}

// rotate removes the archives the configuration no longer keeps.
//
// A configuration keeps by count, by age, or everything. The archive this run
// has just written is never among the removed: it is the newest by count, and
// younger than any age.
//
// Each removal is recorded, so a later rotation does not keep trying to delete
// files that are already gone, and the history says an old entry's archive is no
// longer held rather than pointing at something that is not there.
func (r *Runner) rotate(ctx context.Context, config domain.BackupConfig,
	destination Destination) (int, error) {
	var stale []domain.BackupRun
	var err error
	switch {
	case config.RetainCount > 0:
		stale, err = r.runs.HeldArtifacts(ctx, config.ID, config.RetainCount)
	case config.RetainDays > 0:
		cutoff := time.Now().AddDate(0, 0, -config.RetainDays)
		stale, err = r.runs.ExpiredArtifacts(ctx, config.ID, cutoff)
	default:
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	var removed int
	for _, run := range stale {
		if err := destination.Remove(ctx, run.Artifact); err != nil {
			return removed, err
		}
		if err := r.runs.MarkRotated(ctx, run.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// progressFiles reads the media store on the runner's behalf and counts what it
// reads, which is how a run knows how far through the files it is.
type progressFiles struct {
	inner FileOpener
	track *tracker
}

// Open opens one stored file and counts its bytes as they are read.
func (p progressFiles) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := p.inner.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	return &progressReader{ReadCloser: reader, track: p.track}, nil
}

// progressReader passes reads through and adds them to the run's progress.
type progressReader struct {
	io.ReadCloser
	track *tracker
}

// Read reads and counts.
func (p *progressReader) Read(buffer []byte) (int, error) {
	n, err := p.ReadCloser.Read(buffer)
	if n > 0 {
		p.track.update(func(progress *Progress) { progress.BytesDone += int64(n) })
	}
	return n, err
}

// Close counts the file as done and closes it.
func (p *progressReader) Close() error {
	p.track.update(func(progress *Progress) { progress.FilesDone++ })
	return p.ReadCloser.Close()
}
