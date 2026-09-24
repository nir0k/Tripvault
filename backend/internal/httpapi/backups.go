package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// SecretSealer seals a credential before it is stored and opens it again when it
// is used. It is nil on an instance with no secrets key.
type SecretSealer interface {
	Seal(plaintext string) ([]byte, error)
	Open(sealed []byte) (string, error)
}

// BackupStore is the persistence the backup screens depend on.
//
// Nothing here is scoped to a trip: a backup copies the whole service, and who
// may configure one is decided by the administrator check on the route.
type BackupStore interface {
	CreateConfig(ctx context.Context, config domain.BackupConfig) (domain.BackupConfig, error)
	ListConfigs(ctx context.Context) ([]domain.BackupConfig, error)
	GetConfig(ctx context.Context, id uuid.UUID) (domain.BackupConfig, error)
	GetConfigForRun(ctx context.Context, id uuid.UUID) (domain.BackupConfig, error)
	UpdateConfig(ctx context.Context, config domain.BackupConfig) (domain.BackupConfig, error)
	DeleteConfig(ctx context.Context, id uuid.UUID) error
	ListRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, int64, error)
	GetRun(ctx context.Context, id uuid.UUID) (domain.BackupRun, error)
}

// BackupService starts a backup on request, reports how far a running one has
// got, and reads archives back for a restore.
//
// Reading takes the configuration as well as the name because a destination
// belongs to a configuration: an archive written to a server is not in the local
// directory, and the run alone does not say where it went. A nil configuration
// is this server's own backup directory.
type BackupService interface {
	Start(ctx context.Context, config domain.BackupConfig) (domain.BackupRun, error)
	Progress(runID uuid.UUID) (backup.Progress, bool)
	// Archives lists what a destination holds, newest first.
	Archives(ctx context.Context, config *domain.BackupConfig) ([]backup.Artifact, error)
	// Open reads one archive from a destination.
	Open(ctx context.Context, config *domain.BackupConfig, name string) (io.ReadCloser, error)
	// HoldForRestore refuses backups until the returned function is called, and
	// fails with backup.ErrBackupRunning while one is being written.
	HoldForRestore() (func(), error)
}

// backupConfigRequest is the body of a create or an update.
type backupConfigRequest struct {
	DestinationType   string            `json:"destination_type"`
	DestinationParams map[string]string `json:"destination_params"`
	// Secrets carries credentials typed into the form. A name that is absent
	// leaves what is stored alone, so a form that never showed a password need
	// not send it back; an empty value removes it.
	Secrets      map[string]string `json:"secrets"`
	Encrypted    bool              `json:"encrypted"`
	ScheduleCron string            `json:"schedule_cron"`
	RetainCount  *int              `json:"retain_count"`
	RetainDays   *int              `json:"retain_days"`
	Enabled      *bool             `json:"enabled"`
}

// backupConfigResponse is the public view of a configuration.
//
// Stored credentials - the SFTP password or key, and the passphrase archives are
// locked with - are listed by name only. Their values are sealed in the database
// and never leave the server.
type backupConfigResponse struct {
	ID                string            `json:"id"`
	DestinationType   string            `json:"destination_type"`
	DestinationParams map[string]string `json:"destination_params"`
	StoredSecrets     []string          `json:"stored_secrets"`
	Encrypted         bool              `json:"encrypted"`
	ScheduleCron      string            `json:"schedule_cron"`
	RetainCount       int               `json:"retain_count"`
	RetainDays        int               `json:"retain_days"`
	Enabled           bool              `json:"enabled"`
	// NextRunAt is when the schedule next comes due, so a reader can see that
	// the schedule they chose means what they thought it meant.
	NextRunAt *string `json:"next_run_at"`
}

// newBackupConfigResponse maps a configuration onto its wire form.
func newBackupConfigResponse(config domain.BackupConfig) backupConfigResponse {
	response := backupConfigResponse{
		ID:                config.ID.String(),
		DestinationType:   config.DestinationType,
		DestinationParams: config.DestinationParams,
		StoredSecrets:     config.SecretNames(),
		Encrypted:         config.Encrypted,
		ScheduleCron:      config.ScheduleCron,
		RetainCount:       config.RetainCount,
		RetainDays:        config.RetainDays,
		Enabled:           config.Enabled,
	}
	if response.DestinationParams == nil {
		response.DestinationParams = map[string]string{}
	}

	if config.Enabled && config.ScheduleCron != "" {
		if schedule, err := backup.ParseSchedule(config.ScheduleCron); err == nil {
			next := schedule.Next(time.Now()).UTC().Format(time.RFC3339)
			response.NextRunAt = &next
		}
	}
	return response
}

// backupProgressResponse is how far a run in flight has got.
type backupProgressResponse struct {
	Stage      string `json:"stage"`
	FilesDone  int    `json:"files_done"`
	FilesTotal int    `json:"files_total"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total"`
}

// backupRunResponse is the public view of one attempt.
type backupRunResponse struct {
	ID         string  `json:"id"`
	ConfigID   string  `json:"backup_config_id"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Status     string  `json:"status"`
	SizeBytes  *int64  `json:"size_bytes"`
	Artifact   string  `json:"artifact"`
	// ArchiveHeld says whether that archive is still at its destination. The
	// name stays in the history after retention removes the file, so the two are
	// not the same question and the interface needs both.
	ArchiveHeld bool `json:"archive_held"`
	// Error is what went wrong, so the interface can say so without the operator
	// having to go and read the log.
	Error string `json:"error_message"`
	// Progress is set while the run is working in this process. A run left as
	// running by a restart has none, which is how the interface tells the two
	// apart: one is worth watching, the other will never finish.
	Progress *backupProgressResponse `json:"progress"`
}

// newBackupRunResponse maps a run onto its wire form.
func newBackupRunResponse(run domain.BackupRun) backupRunResponse {
	response := backupRunResponse{
		ID:          run.ID.String(),
		ConfigID:    run.ConfigID.String(),
		StartedAt:   run.StartedAt.UTC().Format(time.RFC3339),
		Status:      run.Status,
		SizeBytes:   run.SizeBytes,
		Artifact:    run.Artifact,
		ArchiveHeld: run.ArchiveHeld(),
		Error:       run.ErrorMessage,
	}
	if run.FinishedAt != nil {
		finished := run.FinishedAt.UTC().Format(time.RFC3339)
		response.FinishedAt = &finished
	}
	return response
}

// runResponse maps a run onto its wire form, with its progress while it works.
func (s *Server) runResponse(run domain.BackupRun) backupRunResponse {
	response := newBackupRunResponse(run)
	if run.Status != domain.BackupRunning {
		return response
	}
	if progress, ok := s.backupService.Progress(run.ID); ok {
		response.Progress = &backupProgressResponse{
			Stage:      progress.Stage,
			FilesDone:  progress.FilesDone,
			FilesTotal: progress.FilesTotal,
			BytesDone:  progress.BytesDone,
			BytesTotal: progress.BytesTotal,
		}
	}
	return response
}

// defaultBackupRetainCount is what a configuration keeps when the request says
// nothing about retention: five archives, which is what the form offers first.
const defaultBackupRetainCount = 5

// maxSecretLength bounds a credential typed into the form. An RSA private key is
// a few kilobytes; nothing anybody pastes as a password comes close.
const maxSecretLength = 16 << 10

// backupRunsPageSize is how many runs one page of the history holds.
const backupRunsPageSize = 50

// errCredentialsNotStorable is the refusal of a credential on an instance that
// has no key to seal it with.
var errCredentialsNotStorable = domain.NewValidationError("secrets", "no_secrets_key",
	"passwords, keys and passphrases cannot be stored because this server has no secrets key")

// toBackupConfig reads a request body into a configuration, applying the
// defaults that make a minimal request mean something sensible.
//
// stored is what the configuration already holds sealed, so an update that does
// not repeat a password keeps it.
func (s *Server) toBackupConfig(w http.ResponseWriter, r *http.Request,
	id uuid.UUID, stored map[string][]byte) (domain.BackupConfig, bool) {
	var body backupConfigRequest
	if !s.decodeJSON(w, r, &body) {
		return domain.BackupConfig{}, false
	}

	config := domain.BackupConfig{
		ID:                id,
		DestinationType:   strings.TrimSpace(body.DestinationType),
		DestinationParams: body.DestinationParams,
		Encrypted:         body.Encrypted,
		ScheduleCron:      body.ScheduleCron,
		Enabled:           true,
	}
	if config.DestinationType == "" {
		config.DestinationType = domain.BackupDestinationLocal
	}
	// Retention is by count unless the request says otherwise. Zero for both is
	// a request to keep everything, which has to be asked for explicitly.
	if body.RetainCount == nil && body.RetainDays == nil {
		config.RetainCount = defaultBackupRetainCount
	} else {
		if body.RetainCount != nil {
			config.RetainCount = *body.RetainCount
		}
		if body.RetainDays != nil {
			config.RetainDays = *body.RetainDays
		}
	}
	if body.Enabled != nil {
		config.Enabled = *body.Enabled
	}

	// An unreadable schedule is refused here rather than logged every minute
	// afterwards by a scheduler that cannot make sense of it.
	if config.ScheduleCron != "" {
		if _, err := backup.ParseSchedule(config.ScheduleCron); err != nil {
			s.writeDomainError(w, r, "validate backup schedule", err)
			return domain.BackupConfig{}, false
		}
	}

	// A destination is checked for what can be checked without reaching it - a
	// missing host, a host key that cannot be read, a private key that cannot be
	// parsed: a configuration that is wrong should be refused while somebody is
	// looking at it rather than every night with nobody watching.
	if config.DestinationType == domain.BackupDestinationSFTP {
		if err := backup.ValidateSFTPParams(config.DestinationParams); err != nil {
			s.writeDomainError(w, r, "validate backup destination", err)
			return domain.BackupConfig{}, false
		}
	}

	sealed, err := s.mergeSecrets(config, stored, body.Secrets)
	if err != nil {
		s.writeDomainError(w, r, "seal backup credentials", err)
		return domain.BackupConfig{}, false
	}
	config.SealedSecrets = sealed

	if config.DestinationType == domain.BackupDestinationSFTP {
		if err := backup.ValidateSFTPCredentials(config.SecretNames(), body.Secrets); err != nil {
			s.writeDomainError(w, r, "validate backup credentials", err)
			return domain.BackupConfig{}, false
		}
	}

	// The configuration is checked once, at the end: whether an encrypted one has
	// a passphrase can only be answered after the credentials are merged, because
	// an update that keeps the stored passphrase does not repeat it in the body.
	config, err = config.Normalize()
	if err != nil {
		s.writeDomainError(w, r, "validate backup configuration", err)
		return domain.BackupConfig{}, false
	}
	return config, true
}

// mergeSecrets applies the credentials a request carries to those already
// stored, sealing each new one.
//
// A new private key drops a passphrase kept from before unless the request
// brings one with it: a passphrase belongs to the key it was entered for. A
// credential the configuration no longer has any use for - an SFTP password on
// one that now writes to the local disk, an archive passphrase on one that is no
// longer encrypted - is dropped rather than left lying sealed in a row nothing
// reads.
//
// Returns the credentials to store, or a *domain.ValidationError for a name that
// is not a credential, a value too long to be one, or an instance with no key to
// seal it with.
func (s *Server) mergeSecrets(config domain.BackupConfig, stored map[string][]byte,
	changes map[string]string) (map[string][]byte, error) {
	merged := make(map[string][]byte, len(stored)+len(changes))
	for name, value := range stored {
		merged[name] = value
	}

	_, passphraseGiven := changes[backup.SFTPSecretKeyPassphrase]
	if changes[backup.SFTPSecretPrivateKey] != "" && !passphraseGiven {
		delete(merged, backup.SFTPSecretKeyPassphrase)
	}

	for _, name := range sortedNames(changes) {
		value := changes[name]
		if !backup.IsSFTPSecret(name) && name != domain.BackupSecretPassphrase {
			return nil, domain.NewValidationError("secrets."+name, "unsupported",
				"is not a credential a backup takes")
		}
		if backup.IsSFTPSecret(name) && config.DestinationType != domain.BackupDestinationSFTP && value != "" {
			return nil, domain.NewValidationError("secrets."+name, "unsupported",
				"only an SFTP destination takes a password or a key")
		}
		if value == "" {
			delete(merged, name)
			continue
		}
		if len(value) > maxSecretLength {
			return nil, domain.NewValidationError("secrets."+name, "too_long", "is too long")
		}
		if s.sealer == nil {
			return nil, errCredentialsNotStorable
		}
		sealed, err := s.sealer.Seal(value)
		if err != nil {
			return nil, err
		}
		merged[name] = sealed
	}

	if config.DestinationType != domain.BackupDestinationSFTP {
		for name := range merged {
			if backup.IsSFTPSecret(name) {
				delete(merged, name)
			}
		}
	}
	if !config.Encrypted {
		delete(merged, domain.BackupSecretPassphrase)
	}
	return merged, nil
}

// sortedNames lists a map's keys in ascending order, so that a request carrying
// two bad credentials is always refused for the same one.
func sortedNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// handleListBackupConfigs returns the instance's backup configurations.
func (s *Server) handleListBackupConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := s.backups.ListConfigs(r.Context())
	if err != nil {
		s.internalError(w, r, "list backup configurations", err)
		return
	}

	items := make([]backupConfigResponse, 0, len(configs))
	for _, config := range configs {
		items = append(items, newBackupConfigResponse(config))
	}
	writeJSON(w, s.logger, http.StatusOK, listResponse[backupConfigResponse]{Items: items})
}

// handleCreateBackupConfig stores a new backup configuration.
func (s *Server) handleCreateBackupConfig(w http.ResponseWriter, r *http.Request) {
	config, ok := s.toBackupConfig(w, r, uuid.Must(uuid.NewV7()), nil)
	if !ok {
		return
	}

	created, err := s.backups.CreateConfig(r.Context(), config)
	if err != nil {
		s.internalError(w, r, "create backup configuration", err)
		return
	}
	writeJSON(w, s.logger, http.StatusCreated, newBackupConfigResponse(created))
}

// handleUpdateBackupConfig replaces a configuration's settings.
//
// Stored credentials the request does not mention are kept, so the form can
// leave a password field empty to mean "unchanged" without ever having shown it.
func (s *Server) handleUpdateBackupConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "configID")
	if !ok {
		return
	}

	existing, err := s.backups.GetConfig(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "load backup configuration", err)
		return
	}

	config, ok := s.toBackupConfig(w, r, id, existing.SealedSecrets)
	if !ok {
		return
	}

	updated, err := s.backups.UpdateConfig(r.Context(), config)
	if err != nil {
		s.writeDomainError(w, r, "update backup configuration", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, newBackupConfigResponse(updated))
}

// handleDeleteBackupConfig withdraws a configuration, leaving its archives and
// its history alone.
func (s *Server) handleDeleteBackupConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "configID")
	if !ok {
		return
	}

	if err := s.backups.DeleteConfig(r.Context(), id); err != nil {
		s.writeDomainError(w, r, "delete backup configuration", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRunBackup starts a backup now rather than waiting for the schedule.
//
// It answers as soon as the run is recorded and leaves the work going. An
// instance with a few thousand photographs on it takes minutes over a slow link,
// which is longer than a request should be held open, and whoever pressed the
// button wants to see it moving. The answer does not claim success: it is the
// run, still running, and the caller follows it at /admin/backup-runs/{runID}
// until it says how it ended.
func (s *Server) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "configID")
	if !ok {
		return
	}

	config, err := s.backups.GetConfig(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "load backup configuration", err)
		return
	}

	run, err := s.backupService.Start(r.Context(), config)
	if errors.Is(err, backup.ErrAlreadyRunning) {
		s.writeError(w, r, http.StatusConflict, "backup_running", "This backup is already running")
		return
	}
	if err != nil {
		s.internalError(w, r, "start backup", err)
		return
	}
	writeJSON(w, s.logger, http.StatusAccepted, s.runResponse(run))
}

// handleGetBackupRun returns one run, with its progress while it works.
func (s *Server) handleGetBackupRun(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathUUID(w, r, "runID")
	if !ok {
		return
	}

	run, err := s.backups.GetRun(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, r, "load backup run", err)
		return
	}
	writeJSON(w, s.logger, http.StatusOK, s.runResponse(run))
}

// handleListBackupRuns returns one page of the instance's backup history, most
// recent first. The cursor is the offset of the next page.
func (s *Server) handleListBackupRuns(w http.ResponseWriter, r *http.Request) {
	offset, ok := s.queryInt(w, r, "cursor")
	if !ok {
		return
	}
	if offset < 0 {
		s.writeDomainError(w, r, "read query",
			domain.NewValidationError("cursor", "out_of_range", "must not be negative"))
		return
	}

	runs, total, err := s.backups.ListRuns(r.Context(), backupRunsPageSize, offset)
	if err != nil {
		s.internalError(w, r, "list backup runs", err)
		return
	}

	items := make([]backupRunResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, s.runResponse(run))
	}

	next := ""
	if int64(offset+len(runs)) < total {
		next = strconv.Itoa(offset + len(runs))
	}
	writeJSON(w, s.logger, http.StatusOK, newPageResponse(items, next))
}
