package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// An archive never passes through the browser: backups are written to this
// server's disk or to an SFTP server, and a restore reads one back from the same
// places. Taking a copy away, or bringing one from elsewhere, is done on the
// host, where `--restore <archive>` also puts an instance back without the web
// interface.

// restoreConfirmWord is the value a restore's confirm field must carry. A
// restore replaces every account, trip and photograph on the instance and
// cannot be undone, so the word is the consequence rather than "yes": whoever
// types it has read what it means.
const restoreConfirmWord = "replace-everything"

// restoreResponse reports what a restore put back.
type restoreResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Stage         string `json:"stage"`
	SchemaVersion int64  `json:"schema_version"`
	Tables        int    `json:"tables"`
	Trips         int    `json:"trips"`
	Files         int    `json:"files"`
	ErrorCode     string `json:"error_code"`
	ErrorMessage  string `json:"error_message"`
}

// restoreJob is one background whole-instance replacement.
type restoreJob struct {
	ID            uuid.UUID
	Status        string
	Stage         string
	SchemaVersion int64
	Tables        int
	Trips         int
	Files         int
	ErrorCode     string
	ErrorMessage  string
	RequestedBy   string
	StartedAt     time.Time
}

// Restore run states returned by the asynchronous API.
const (
	restoreQueued  = "queued"
	restoreRunning = "running"
	restoreSuccess = "success"
	restoreFailed  = "failed"
)

// errRestoreRunning reports that whole-instance restores are serialized.
var errRestoreRunning = errors.New("a restore is already running")

// archiveResponse is one archive a destination holds.
type archiveResponse struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	WrittenAt time.Time `json:"written_at"`
	// Encrypted is read from the name, so the interface knows to ask for a
	// passphrase before anything is opened.
	Encrypted bool `json:"encrypted"`
}

// handleListArchives lists the archives a restore can be made from.
//
// Without config_id it is this server's own backup directory, which is what a
// fresh instance whose volume holds archives has to offer. With one it is the
// destination of that configuration - an SFTP server, typically - and a
// withdrawn configuration still names where its archives went.
//
// The list is read from the destination rather than from the history, because
// the history is exactly what a fresh instance does not have yet.
func (s *Server) handleListArchives(w http.ResponseWriter, r *http.Request) {
	config, ok := s.archiveSource(w, r, r.URL.Query().Get("config_id"))
	if !ok {
		return
	}
	archives, err := s.backupService.Archives(r.Context(), config)
	if err != nil {
		s.writeDestinationError(w, r, err)
		return
	}
	items := make([]archiveResponse, 0, len(archives))
	for _, archive := range archives {
		items = append(items, archiveResponse{
			Name:      archive.Name,
			SizeBytes: archive.SizeBytes,
			WrittenAt: archive.WrittenAt,
			Encrypted: backup.ArchiveEncrypted(archive.Name),
		})
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[archiveResponse]{Items: items})
}

// restoreRequest names the archive to put the service back from.
type restoreRequest struct {
	// ConfigID names the destination; absent means this server's own disk.
	ConfigID string `json:"config_id"`
	// Archive is the file's name as the listing gave it.
	Archive string `json:"archive"`
	// Passphrase opens an encrypted archive. Left empty, the configuration's
	// own is used; it is typed only for an archive another instance wrote.
	Passphrase string `json:"passphrase"`
	Confirm    string `json:"confirm"`
}

// handleRestore puts the whole service back from an archive a destination holds.
//
// Everything is replaced: accounts, trips, plans, reports and the files. That
// includes the sessions, so whoever asked for this is signed out by their own
// request unless their account is in the archive as well - which is why the
// interface says so before it asks. The work runs in the background; the answer
// is the run to follow at /restore-runs/{id}.
//
// It is deliberately allowed on an instance that is not empty. A restore is what
// somebody reaches for after a mistake, and refusing unless the database is
// blank would make the feature useless exactly when it is needed; the archive is
// verified in full before anything is touched, and the confirmation is what
// stands in for the refusal.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var body restoreRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	if body.Confirm != restoreConfirmWord {
		s.writeError(w, r, http.StatusUnprocessableEntity, "confirmation_required",
			"Restoring replaces everything on this server. Send confirm="+restoreConfirmWord+" to go ahead.")
		return
	}
	config, ok := s.archiveSource(w, r, body.ConfigID)
	if !ok {
		return
	}

	// The name is checked against what the destination holds before the
	// request is answered, so a mistyped or rotated archive is refused here
	// rather than reported as a failed run a moment later.
	archives, err := s.backupService.Archives(r.Context(), config)
	if err != nil {
		s.writeDestinationError(w, r, err)
		return
	}
	held := false
	for _, archive := range archives {
		held = held || archive.Name == body.Archive
	}
	if !held {
		s.writeError(w, r, http.StatusNotFound, "archive_not_found",
			"The destination does not hold that archive")
		return
	}

	passphrase, ok := s.restorePassphrase(w, r, config, body.Passphrase)
	if !ok {
		return
	}
	if passphrase == "" && backup.ArchiveEncrypted(body.Archive) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "passphrase_required",
			"This archive is encrypted. Enter the passphrase it was locked with.")
		return
	}

	requestedBy := principalFrom(r.Context()).user.ID.String()
	job, err := s.startRestore(func(ctx context.Context) (io.ReadCloser, error) {
		return s.backupService.Open(ctx, config, body.Archive)
	}, passphrase, requestedBy)
	switch {
	case errors.Is(err, errRestoreRunning):
		s.writeError(w, r, http.StatusConflict, "restore_running", "A restore is already running")
		return
	case errors.Is(err, backup.ErrBackupRunning):
		s.writeError(w, r, http.StatusConflict, "backup_in_progress",
			"A backup is being written. Restore once it has finished.")
		return
	case err != nil:
		s.internalError(w, r, "start restore", err)
		return
	}
	writeJSON(w, s.logger, http.StatusAccepted, newRestoreResponse(job))
}

// archiveSource resolves the configuration a request names as the place its
// archives are, writing the error itself.
//
// Arguments:
//   - w: the response, written only on failure.
//   - r: the request.
//   - raw: the configuration's identifier, or empty for this server's disk.
//
// Returns:
//   - the configuration, or nil for this server's disk.
//   - false when an error response has been written instead.
func (s *Server) archiveSource(w http.ResponseWriter, r *http.Request, raw string) (*domain.BackupConfig, bool) {
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Resource not found")
		return nil, false
	}
	// A withdrawn configuration is not offered anywhere, but its archives are
	// still where it put them and its row is the only record of where that was.
	config, err := s.backups.GetConfigForRun(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "load backup configuration", err)
		return nil, false
	}
	return &config, true
}

// restorePassphrase decides what opens the archive: the one typed, or else the
// configuration's own. It writes the error itself.
//
// Arguments:
//   - w: the response, written only on failure.
//   - r: the request.
//   - config: the configuration the archive belongs to, or nil for the disk.
//   - typed: the passphrase sent with the request, possibly empty.
//
// Returns:
//   - the passphrase, empty when there is none to use.
//   - false when an error response has been written instead.
func (s *Server) restorePassphrase(w http.ResponseWriter, r *http.Request, config *domain.BackupConfig,
	typed string) (string, bool) {
	if typed != "" || config == nil || !config.Encrypted {
		return typed, true
	}
	sealed := config.SealedSecrets[domain.BackupSecretPassphrase]
	if len(sealed) == 0 {
		return "", true
	}
	if s.sealer == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "secrets_unavailable",
			"The archive passphrase cannot be opened on this server")
		return "", false
	}
	passphrase, err := s.sealer.Open(sealed)
	if err != nil {
		s.internalError(w, r, "open archive passphrase", err)
		return "", false
	}
	return passphrase, true
}

// writeDestinationError answers a destination that could not be read.
func (s *Server) writeDestinationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, backup.ErrSecretsUnavailable) {
		s.writeError(w, r, http.StatusServiceUnavailable, "secrets_unavailable",
			"The stored credentials cannot be opened on this server")
		return
	}
	s.logger.Warn("backup destination could not be read",
		slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("error", err))
	s.writeError(w, r, http.StatusBadGateway, "destination_unavailable",
		"The backup destination could not be reached")
}

// startRestore claims the single restore slot, keeps backups away, and starts
// the work after returning a run the caller can poll.
//
// Arguments:
//   - open: opens the archive, called once the service is in maintenance.
//   - passphrase: opens an encrypted archive, or empty.
//   - requestedBy: the account that asked, for the log.
//
// Returns:
//   - the run, still queued.
//   - errRestoreRunning when another restore holds the slot, or
//     backup.ErrBackupRunning when a backup is being written.
func (s *Server) startRestore(open func(context.Context) (io.ReadCloser, error), passphrase,
	requestedBy string) (restoreJob, error) {
	s.restoreJobsMu.Lock()
	defer s.restoreJobsMu.Unlock()
	if s.restoreActive {
		return restoreJob{}, errRestoreRunning
	}
	resumeBackups, err := s.backupService.HoldForRestore()
	if err != nil {
		return restoreJob{}, err
	}
	job := restoreJob{
		ID: uuid.Must(uuid.NewV7()), Status: restoreQueued, Stage: backup.RestoreStageValidating,
		RequestedBy: requestedBy, StartedAt: time.Now().UTC(),
	}
	stored := job
	s.restoreActive = true
	s.restoreJobs[job.ID] = &stored

	go s.runRestore(job.ID, open, passphrase, resumeBackups)
	return job, nil
}

// runRestore puts the service into maintenance, then opens, validates, stages
// and activates one archive.
//
// The whole restore runs in maintenance rather than only its switch: whatever
// anybody wrote meanwhile would be replaced by the archive anyway, and a
// request left running - a PDF being drawn, an upload - is cancelled rather
// than waited for, so the service is back as soon as the archive is.
func (s *Server) runRestore(id uuid.UUID, open func(context.Context) (io.ReadCloser, error),
	passphrase string, resumeBackups func()) {
	defer func() {
		resumeBackups()
		s.restoreJobsMu.Lock()
		s.restoreActive = false
		s.restoreJobsMu.Unlock()
	}()
	s.enterMaintenance()
	defer s.leaveMaintenance()
	s.updateRestoreJob(id, func(job *restoreJob) { job.Status = restoreRunning })

	archive, err := open(s.workContext)
	if err != nil {
		s.failRestoreJob(id, "archive_unreadable", "The archive could not be opened")
		s.logger.Error("restore archive could not be opened", slog.Any("error", err))
		return
	}
	defer func() { _ = archive.Close() }()
	plain, err := backup.Plain(archive, backup.Encryption{Passphrase: passphrase})
	if err != nil {
		if errors.Is(err, backup.ErrEncrypted) {
			s.failRestoreJob(id, "archive_encrypted",
				"This archive is encrypted. Enter the passphrase it was locked with.")
		} else {
			s.failRestoreJob(id, "archive_unreadable", "The archive could not be opened")
		}
		return
	}

	result, err := backup.Restore(s.workContext, plain, s.restoreWorkDir, s.migrator, s.restoreFiles,
		func(stage string) {
			s.updateRestoreJob(id, func(job *restoreJob) { job.Stage = stage })
		})
	if err != nil {
		code, message := restorePublicError(err)
		s.failRestoreJob(id, code, message)
		s.logger.Error("whole-instance restore failed", slog.Any("error", err))
		return
	}

	s.updateRestoreJob(id, func(job *restoreJob) {
		job.Status = restoreSuccess
		job.Stage = "complete"
		job.SchemaVersion = result.SchemaVersion
		job.Tables = result.Tables
		job.Trips = result.Trips
		job.Files = result.Files
	})
	// Backups leave previews out, so the restored pictures have none; the walk
	// that renders them waits for the maintenance to end.
	s.requestPreviewBackfill()
	s.logger.Warn("restored the whole service from an archive",
		slog.String("restore_id", id.String()),
		slog.String("requested_by", s.restoreJobRequestedBy(id)),
		slog.Int64("schema_version", result.SchemaVersion),
		slog.Int("tables", result.Tables),
		slog.Int("trips", result.Trips),
		slog.Int("files", result.Files))
}

// enterMaintenance cancels every API request in flight and holds the gate, so
// that nothing but the restore touches the database until leaveMaintenance.
func (s *Server) enterMaintenance() {
	s.servingMu.Lock()
	s.stopServing()
	s.servingMu.Unlock()
	// Lock waits for the cancelled requests to return; from the moment it is
	// asked for, requireAvailable turns every new one away.
	s.restoreGate.Lock()
}

// leaveMaintenance lets requests in again, under a fresh context of their own.
func (s *Server) leaveMaintenance() {
	s.servingMu.Lock()
	s.serving, s.stopServing = context.WithCancel(context.Background())
	s.servingMu.Unlock()
	s.restoreGate.Unlock()
}

// servingContext returns the context that ends when a restore begins.
func (s *Server) servingContext() context.Context {
	s.servingMu.Lock()
	defer s.servingMu.Unlock()
	return s.serving
}

// updateRestoreJob changes one in-memory restore run under its lock.
func (s *Server) updateRestoreJob(id uuid.UUID, update func(*restoreJob)) {
	s.restoreJobsMu.Lock()
	defer s.restoreJobsMu.Unlock()
	if job := s.restoreJobs[id]; job != nil {
		update(job)
	}
}

// failRestoreJob records a public failure without exposing internal details.
func (s *Server) failRestoreJob(id uuid.UUID, code, message string) {
	s.updateRestoreJob(id, func(job *restoreJob) {
		job.Status = restoreFailed
		job.ErrorCode = code
		job.ErrorMessage = message
	})
}

// restoreJobRequestedBy reads the audit identity of one run.
func (s *Server) restoreJobRequestedBy(id uuid.UUID) string {
	s.restoreJobsMu.Lock()
	defer s.restoreJobsMu.Unlock()
	if job := s.restoreJobs[id]; job != nil {
		return job.RequestedBy
	}
	return ""
}

// restorePublicError turns a restore failure into a stable API code and message.
func restorePublicError(err error) (string, string) {
	switch {
	case errors.Is(err, backup.ErrNotAnArchive):
		return "not_an_archive", err.Error()
	case errors.Is(err, backup.ErrArchiveTooNew):
		return "archive_too_new", err.Error()
	case errors.Is(err, backup.ErrArchiveDamaged):
		return "archive_damaged", err.Error()
	case errors.Is(err, context.Canceled):
		return "restore_cancelled", "The restore stopped because the service is shutting down"
	default:
		return "restore_failed", "The archive could not be restored"
	}
}

// newRestoreResponse maps an in-memory job to its public representation.
func newRestoreResponse(job restoreJob) restoreResponse {
	return restoreResponse{
		ID: job.ID.String(), Status: job.Status, Stage: job.Stage,
		SchemaVersion: job.SchemaVersion, Tables: job.Tables, Trips: job.Trips, Files: job.Files,
		ErrorCode: job.ErrorCode, ErrorMessage: job.ErrorMessage,
	}
}

// handleGetRestoreRun reports a background restore by its unguessable ID.
func (s *Server) handleGetRestoreRun(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "restoreID")
	if !ok {
		return
	}
	s.restoreJobsMu.Lock()
	job := s.restoreJobs[id]
	if job != nil {
		copy := *job
		job = &copy
	}
	s.restoreJobsMu.Unlock()
	if job == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Restore run not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newRestoreResponse(*job))
}

// requireAvailable refuses API work while a restore replaces the service, and
// ties every request it lets through to the serving context, so that a restore
// beginning later ends the request instead of waiting for it.
func (s *Server) requireAvailable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.restoreGate.TryRLock() {
			s.writeError(w, r, http.StatusServiceUnavailable, "restore_in_progress",
				"The service is being restored from a backup; try again shortly")
			return
		}
		defer s.restoreGate.RUnlock()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(s.servingContext(), cancel)
		defer stop()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
