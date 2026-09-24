package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// fakeBackups keeps configurations and runs in memory.
type fakeBackups struct {
	configs map[uuid.UUID]domain.BackupConfig
	order   []uuid.UUID
	runs    map[uuid.UUID]domain.BackupRun
}

// newFakeBackups builds an empty store.
func newFakeBackups() *fakeBackups {
	return &fakeBackups{
		configs: map[uuid.UUID]domain.BackupConfig{},
		runs:    map[uuid.UUID]domain.BackupRun{},
	}
}

// CreateConfig stores a configuration.
func (f *fakeBackups) CreateConfig(_ context.Context, config domain.BackupConfig) (
	domain.BackupConfig, error) {
	config.CreatedAt, config.UpdatedAt = time.Now(), time.Now()
	f.configs[config.ID] = config
	f.order = append(f.order, config.ID)
	return config, nil
}

// ListConfigs returns the live configurations, oldest first.
func (f *fakeBackups) ListConfigs(context.Context) ([]domain.BackupConfig, error) {
	configs := make([]domain.BackupConfig, 0, len(f.order))
	for _, id := range f.order {
		if config, ok := f.configs[id]; ok && config.DeletedAt == nil {
			configs = append(configs, config)
		}
	}
	return configs, nil
}

// GetConfig loads a live configuration.
func (f *fakeBackups) GetConfig(_ context.Context, id uuid.UUID) (domain.BackupConfig, error) {
	config, ok := f.configs[id]
	if !ok || config.DeletedAt != nil {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	return config, nil
}

// GetConfigForRun loads a configuration whether or not it was withdrawn.
func (f *fakeBackups) GetConfigForRun(_ context.Context, id uuid.UUID) (domain.BackupConfig, error) {
	config, ok := f.configs[id]
	if !ok {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	return config, nil
}

// UpdateConfig replaces a live configuration's settings.
func (f *fakeBackups) UpdateConfig(_ context.Context, config domain.BackupConfig) (
	domain.BackupConfig, error) {
	existing, ok := f.configs[config.ID]
	if !ok || existing.DeletedAt != nil {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	config.CreatedAt, config.UpdatedAt = existing.CreatedAt, time.Now()
	f.configs[config.ID] = config
	return config, nil
}

// DeleteConfig withdraws a configuration.
func (f *fakeBackups) DeleteConfig(_ context.Context, id uuid.UUID) error {
	config, ok := f.configs[id]
	if !ok || config.DeletedAt != nil {
		return domain.ErrNotFound
	}
	now := time.Now()
	config.DeletedAt = &now
	f.configs[id] = config
	return nil
}

// ListRuns returns one page of the history.
func (f *fakeBackups) ListRuns(_ context.Context, limit, offset int) ([]domain.BackupRun, int64, error) {
	all := make([]domain.BackupRun, 0, len(f.runs))
	for _, run := range f.runs {
		all = append(all, run)
	}
	total := int64(len(all))
	if offset >= len(all) {
		return nil, total, nil
	}
	end := min(offset+limit, len(all))
	return all[offset:end], total, nil
}

// GetRun loads one run.
func (f *fakeBackups) GetRun(_ context.Context, id uuid.UUID) (domain.BackupRun, error) {
	run, ok := f.runs[id]
	if !ok {
		return domain.BackupRun{}, domain.ErrNotFound
	}
	return run, nil
}

// fakeBackupService starts runs and hands archives back without touching a disk.
type fakeBackupService struct {
	store *fakeBackups
	// busy makes Start report a configuration that is already running.
	busy bool
	// writing makes a restore find a backup being written.
	writing bool
	// held counts the restores holding backups away right now; a restore
	// releases its hold from its own goroutine.
	held atomic.Int32
	// archives holds what each destination has, by archive name: the key is
	// the configuration's identifier, or empty for this server's disk.
	archives map[string]map[string]string
	// opening, when set, is waited on before an archive is opened, which
	// holds a restore in the middle of its work for as long as a test needs.
	opening chan struct{}
}

// Start records a run against the configuration and leaves it running.
func (f *fakeBackupService) Start(_ context.Context, config domain.BackupConfig) (domain.BackupRun, error) {
	if f.busy {
		return domain.BackupRun{}, backup.ErrAlreadyRunning
	}
	run := domain.BackupRun{
		ID: uuid.New(), ConfigID: config.ID, StartedAt: time.Now(), Status: domain.BackupRunning,
	}
	f.store.runs[run.ID] = run
	return run, nil
}

// Progress reports a run half way through its files.
func (f *fakeBackupService) Progress(uuid.UUID) (backup.Progress, bool) {
	return backup.Progress{Stage: backup.StageArchiving, FilesDone: 1, FilesTotal: 2,
		BytesDone: 10, BytesTotal: 20}, true
}

// destinationKey names the archives map entry a configuration reads from.
func destinationKey(config *domain.BackupConfig) string {
	if config == nil {
		return ""
	}
	return config.ID.String()
}

// Archives lists what a destination holds, newest first.
func (f *fakeBackupService) Archives(_ context.Context, config *domain.BackupConfig) ([]backup.Artifact, error) {
	held := f.archives[destinationKey(config)]
	names := make([]string, 0, len(held))
	for name := range held {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	artifacts := make([]backup.Artifact, 0, len(names))
	for _, name := range names {
		artifacts = append(artifacts, backup.Artifact{Name: name, SizeBytes: int64(len(held[name])),
			WrittenAt: testArchiveTime})
	}
	return artifacts, nil
}

// Open returns an archive a destination holds.
func (f *fakeBackupService) Open(_ context.Context, config *domain.BackupConfig, name string) (
	io.ReadCloser, error) {
	if f.opening != nil {
		<-f.opening
	}
	body, ok := f.archives[destinationKey(config)][name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

// HoldForRestore refuses while a backup is being written, and otherwise counts
// the hold until it is released.
func (f *fakeBackupService) HoldForRestore() (func(), error) {
	if f.writing {
		return nil, backup.ErrBackupRunning
	}
	f.held.Add(1)
	return func() { f.held.Add(-1) }, nil
}

// testArchiveTime is when every archive in these tests was written.
var testArchiveTime = time.Date(2026, 6, 20, 3, 15, 0, 0, time.UTC)

// fakeSealer seals by prefixing, which is enough to tell a sealed value from a
// plain one without bringing a key into these tests.
type fakeSealer struct{}

// Seal prefixes the value.
func (fakeSealer) Seal(plaintext string) ([]byte, error) { return []byte("sealed:" + plaintext), nil }

// Open removes the prefix.
func (fakeSealer) Open(sealed []byte) (string, error) {
	return strings.TrimPrefix(string(sealed), "sealed:"), nil
}

// newBackupServer builds a server whose token "good" signs in as an
// administrator, with the backup endpoints available.
func newBackupServer(t *testing.T, sealer SecretSealer) (*Server, *fakeBackups, *fakeBackupService) {
	t.Helper()

	store := newFakeBackups()
	service := &fakeBackupService{store: store, archives: map[string]map[string]string{"": {}}}
	admin := domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}

	server := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:           &fakeAuth{user: admin},
		Users:          fakeUsers{},
		Backups:        store,
		BackupService:  service,
		Sealer:         sealer,
		RestoreWorkDir: t.TempDir(),
	})
	return server, store, service
}

// createConfig posts a configuration and returns the answer.
func createConfig(t *testing.T, s *Server, body string) backupConfigResponse {
	t.Helper()
	recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs", "good", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create a configuration: %d %s", recorder.Code, recorder.Body.String())
	}
	var created backupConfigResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("response %q is not a configuration: %v", recorder.Body.String(), err)
	}
	return created
}

// TestBackupsAreForAdministratorsOnly checks somebody who does not administer
// the service cannot see where it is copied to, let alone take a copy.
func TestBackupsAreForAdministratorsOnly(t *testing.T) {
	store := newFakeBackups()
	member := domain.User{ID: uuid.New(), IsActive: true}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:          &fakeAuth{user: member},
		Users:         fakeUsers{},
		Backups:       store,
		BackupService: &fakeBackupService{store: store},
	})

	for _, path := range []string{"/api/v1/admin/backup-configs", "/api/v1/admin/backup-runs"} {
		recorder := send(s, http.MethodGet, path, "good", "")
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s", path, recorder.Code, recorder.Body.String())
		}
	}
	recorder := send(s, http.MethodPost, "/api/v1/admin/restore", "good", "")
	if recorder.Code != http.StatusForbidden {
		t.Errorf("restore: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestBackupsUnavailableWithoutADirectory checks an instance that started with
// no usable backup directory says so rather than failing on a missing service.
func TestBackupsUnavailableWithoutADirectory(t *testing.T) {
	admin := domain.User{ID: uuid.New(), IsActive: true, IsAdmin: true}
	s := NewServer(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		Auth:  &fakeAuth{user: admin},
		Users: fakeUsers{},
	})

	recorder := send(s, http.MethodGet, "/api/v1/admin/backup-configs", "good", "")
	if recorder.Code != http.StatusServiceUnavailable || errorCode(t, recorder) != "backups_unavailable" {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
}

// TestBackupConfigDefaults checks a configuration with only a destination means
// something sensible: kept by count, enabled, and run only on request.
func TestBackupConfigDefaults(t *testing.T) {
	s, _, _ := newBackupServer(t, fakeSealer{})

	created := createConfig(t, s, `{"destination_type":"local"}`)
	if created.RetainCount != defaultBackupRetainCount || created.RetainDays != 0 {
		t.Errorf("retention is %d by count, %d by age", created.RetainCount, created.RetainDays)
	}
	if !created.Enabled || created.ScheduleCron != "" || created.NextRunAt != nil {
		t.Errorf("the created configuration is %+v", created)
	}
	if len(created.StoredSecrets) != 0 {
		t.Errorf("it holds credentials nobody entered: %v", created.StoredSecrets)
	}
}

// TestBackupConfigSchedule checks a readable schedule is accepted and reported
// with the instant it next comes due, and an unreadable one is refused where
// somebody is looking at it.
func TestBackupConfigSchedule(t *testing.T) {
	s, _, _ := newBackupServer(t, fakeSealer{})

	created := createConfig(t, s, `{"destination_type":"local","schedule_cron":"  0 3 * * *  "}`)
	if created.ScheduleCron != "0 3 * * *" {
		t.Errorf("the schedule was stored as %q", created.ScheduleCron)
	}
	if created.NextRunAt == nil {
		t.Error("a scheduled configuration does not say when it next runs")
	}

	recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs", "good",
		`{"destination_type":"local","schedule_cron":"every night"}`)
	if recorder.Code != http.StatusUnprocessableEntity || errorCode(t, recorder) != "validation_failed" {
		t.Errorf("an unreadable schedule: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestBackupConfigRefusesImpossibleRetention checks the two ways of keeping
// archives cannot both be asked for at once.
func TestBackupConfigRefusesImpossibleRetention(t *testing.T) {
	s, _, _ := newBackupServer(t, fakeSealer{})

	recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs", "good",
		`{"destination_type":"local","retain_count":7,"retain_days":30}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
}

// TestBackupCredentialsNeverComeBack checks a credential typed into the form is
// sealed, listed by name and never returned.
func TestBackupCredentialsNeverComeBack(t *testing.T) {
	s, store, _ := newBackupServer(t, fakeSealer{})

	created := createConfig(t, s, `{
		"destination_type":"sftp",
		"destination_params":{"host":"backups.example.com","user":"tripvault"},
		"secrets":{"password":"hunter2"}
	}`)
	if len(created.StoredSecrets) != 1 || created.StoredSecrets[0] != "password" {
		t.Fatalf("the stored credentials are %v", created.StoredSecrets)
	}
	if strings.Contains(created.DestinationParams["password"], "hunter2") {
		t.Error("the password came back in the parameters")
	}

	stored := store.configs[uuid.MustParse(created.ID)]
	if string(stored.SealedSecrets["password"]) != "sealed:hunter2" {
		t.Errorf("the password was stored as %q", stored.SealedSecrets["password"])
	}

	// An update that does not repeat the password keeps it, so a form that never
	// showed it need not send it back.
	recorder := send(s, http.MethodPut, "/api/v1/admin/backup-configs/"+created.ID, "good", `{
		"destination_type":"sftp",
		"destination_params":{"host":"backups.example.com","user":"tripvault","port":"2222"}
	}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update: %d %s", recorder.Code, recorder.Body.String())
	}
	if string(store.configs[uuid.MustParse(created.ID)].SealedSecrets["password"]) != "sealed:hunter2" {
		t.Error("the stored password was lost by an update that did not mention it")
	}

	// An empty value is how the form removes one.
	recorder = send(s, http.MethodPut, "/api/v1/admin/backup-configs/"+created.ID, "good", `{
		"destination_type":"local",
		"secrets":{"password":""}
	}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove the password: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, held := store.configs[uuid.MustParse(created.ID)].SealedSecrets["password"]; held {
		t.Error("the password is still stored")
	}
}

// TestEncryptedBackupTakesItsPassphraseFromTheSameRequest checks the passphrase
// a new encrypted configuration is created with counts as stored: the rule that
// an encrypted backup needs one is about what it will hold, not about what was
// there before the request.
func TestEncryptedBackupTakesItsPassphraseFromTheSameRequest(t *testing.T) {
	s, store, _ := newBackupServer(t, fakeSealer{})

	created := createConfig(t, s, `{
		"destination_type":"local",
		"encrypted":true,
		"secrets":{"passphrase":"correct horse battery staple"}
	}`)
	if !created.Encrypted || len(created.StoredSecrets) != 1 || created.StoredSecrets[0] != "passphrase" {
		t.Fatalf("the created configuration is %+v", created)
	}

	stored := store.configs[uuid.MustParse(created.ID)]
	if string(stored.SealedSecrets["passphrase"]) != "sealed:correct horse battery staple" {
		t.Errorf("the passphrase was stored as %q", stored.SealedSecrets["passphrase"])
	}

	// Turning encryption off drops the passphrase rather than leaving it sealed
	// in a row nothing reads.
	recorder := send(s, http.MethodPut, "/api/v1/admin/backup-configs/"+created.ID, "good",
		`{"destination_type":"local","encrypted":false}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("turn encryption off: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, held := store.configs[uuid.MustParse(created.ID)].SealedSecrets["passphrase"]; held {
		t.Error("the passphrase is still stored on a configuration that writes in the clear")
	}
}

// TestBackupCredentialsNeedAKey checks an instance with nothing to seal with
// refuses a credential rather than storing it in the clear.
func TestBackupCredentialsNeedAKey(t *testing.T) {
	s, _, _ := newBackupServer(t, nil)

	recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs", "good",
		`{"destination_type":"local","encrypted":true,"secrets":{"passphrase":"open sesame"}}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if errorCode(t, recorder) != "validation_failed" {
		t.Errorf("the refusal is %s", recorder.Body.String())
	}
}

// TestBackupConfigRefusesUnusableDestinations checks what can be checked without
// reaching a server is checked while the form is open.
func TestBackupConfigRefusesUnusableDestinations(t *testing.T) {
	s, _, _ := newBackupServer(t, fakeSealer{})

	cases := map[string]string{
		"no host":       `{"destination_type":"sftp","destination_params":{"user":"tripvault"},"secrets":{"password":"x"}}`,
		"no way in":     `{"destination_type":"sftp","destination_params":{"host":"h","user":"u"}}`,
		"bad port":      `{"destination_type":"sftp","destination_params":{"host":"h","user":"u","port":"http"},"secrets":{"password":"x"}}`,
		"bad host key":  `{"destination_type":"sftp","destination_params":{"host":"h","user":"u","host_key":"nonsense"},"secrets":{"password":"x"}}`,
		"unknown kind":  `{"destination_type":"dropbox"}`,
		"encrypted":     `{"destination_type":"local","encrypted":true}`,
		"odd credentia": `{"destination_type":"local","secrets":{"api_key":"x"}}`,
	}
	for name, body := range cases {
		recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs", "good", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
}

// TestWithdrawnConfigurationKeepsItsArchives checks withdrawing an instruction
// hides it from the list without making the copies it made unreachable.
func TestWithdrawnConfigurationKeepsItsArchives(t *testing.T) {
	s, store, service := newBackupServer(t, fakeSealer{})
	created := createConfig(t, s, `{"destination_type":"local"}`)
	configID := uuid.MustParse(created.ID)

	finished := time.Now()
	run := domain.BackupRun{
		ID: uuid.New(), ConfigID: configID, StartedAt: time.Now(), FinishedAt: &finished,
		Status: domain.BackupSuccess, Artifact: "tripvault_20260620T031500.000Z.tar.gz",
	}
	store.runs[run.ID] = run
	service.archives[created.ID] = map[string]string{run.Artifact: "the archive"}

	if recorder := send(s, http.MethodDelete, "/api/v1/admin/backup-configs/"+created.ID, "good", ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("withdraw: %d %s", recorder.Code, recorder.Body.String())
	}

	recorder := send(s, http.MethodGet, "/api/v1/admin/backup-configs", "good", "")
	var listed listResponse[backupConfigResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("response %q is not a list: %v", recorder.Body.String(), err)
	}
	if len(listed.Items) != 0 {
		t.Errorf("a withdrawn configuration is still listed: %+v", listed.Items)
	}

	// Its row still says where its archives went, so they can be restored.
	if names := listArchives(t, s, created.ID); len(names) != 1 || names[0].Name != run.Artifact {
		t.Errorf("the archives of a withdrawn configuration: %+v", names)
	}

	// Running one is another matter: the instruction was withdrawn.
	if recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs/"+created.ID+"/run", "good", ""); recorder.Code != http.StatusNotFound {
		t.Errorf("run a withdrawn configuration: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestRunBackupAnswersWhileItWorks checks pressing the button answers at once
// with a run to follow, and that a second press is refused.
func TestRunBackupAnswersWhileItWorks(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	created := createConfig(t, s, `{"destination_type":"local"}`)

	recorder := send(s, http.MethodPost, "/api/v1/admin/backup-configs/"+created.ID+"/run", "good", "")
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", recorder.Code, recorder.Body.String())
	}
	var run backupRunResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &run); err != nil {
		t.Fatalf("response %q is not a run: %v", recorder.Body.String(), err)
	}
	if run.Status != domain.BackupRunning || run.Progress == nil || run.Progress.FilesTotal != 2 {
		t.Errorf("the run is %+v", run)
	}

	service.busy = true
	recorder = send(s, http.MethodPost, "/api/v1/admin/backup-configs/"+created.ID+"/run", "good", "")
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "backup_running" {
		t.Errorf("a second run: %d %s", recorder.Code, recorder.Body.String())
	}
}

// listArchives reads what a destination holds; configID empty is the disk.
func listArchives(t *testing.T, s *Server, configID string) []archiveResponse {
	t.Helper()
	path := "/api/v1/admin/archives"
	if configID != "" {
		path += "?config_id=" + configID
	}
	recorder := send(s, http.MethodGet, path, "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list archives: %d %s", recorder.Code, recorder.Body.String())
	}
	var listed listResponse[archiveResponse]
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("response %q is not a list: %v", recorder.Body.String(), err)
	}
	return listed.Items
}

// requestRestore asks for a restore of a named archive, confirmed.
func requestRestore(s *Server, request restoreRequest) *httptest.ResponseRecorder {
	if request.Confirm == "" {
		request.Confirm = restoreConfirmWord
	}
	body, _ := json.Marshal(request)
	return send(s, http.MethodPost, "/api/v1/admin/restore", "good", string(body))
}

// awaitRestore follows an accepted restore until its background validation
// reports the stable outcome.
func awaitRestore(t *testing.T, s *Server, accepted *httptest.ResponseRecorder) restoreResponse {
	t.Helper()
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("restore was not accepted: %d %s", accepted.Code, accepted.Body.String())
	}
	var run restoreResponse
	if err := json.Unmarshal(accepted.Body.Bytes(), &run); err != nil {
		t.Fatalf("restore response is invalid: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for run.Status == restoreQueued || run.Status == restoreRunning {
		if time.Now().After(deadline) {
			t.Fatal("restore did not finish")
		}
		time.Sleep(time.Millisecond)
		response := send(s, http.MethodGet, "/api/v1/restore-runs/"+run.ID, "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("read restore: %d %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
			t.Fatalf("restore status is invalid: %v", err)
		}
	}
	return run
}

// TestListArchivesReadsTheDestination checks the list comes from the disk or
// from a configuration's destination, and says which archives need a passphrase.
func TestListArchivesReadsTheDestination(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""] = map[string]string{
		"tripvault_20260620T031500.000Z.tar.gz":     "plain",
		"tripvault_20260621T031500.000Z.tar.gz.age": "sealed",
	}
	created := createConfig(t, s, `{"destination_type":"local"}`)
	service.archives[created.ID] = map[string]string{"tripvault_20260101T000000.000Z.tar.gz": "old"}

	disk := listArchives(t, s, "")
	if len(disk) != 2 || disk[0].Name != "tripvault_20260621T031500.000Z.tar.gz.age" ||
		!disk[0].Encrypted || disk[1].Encrypted || disk[1].SizeBytes != 5 {
		t.Errorf("the disk holds %+v", disk)
	}
	if named := listArchives(t, s, created.ID); len(named) != 1 || named[0].Name != "tripvault_20260101T000000.000Z.tar.gz" {
		t.Errorf("the configuration's destination holds %+v", named)
	}
	for _, id := range []string{uuid.NewString(), "not-an-id"} {
		recorder := send(s, http.MethodGet, "/api/v1/admin/archives?config_id="+id, "good", "")
		if recorder.Code != http.StatusNotFound {
			t.Errorf("an unknown configuration %q: %d %s", id, recorder.Code, recorder.Body.String())
		}
	}
}

// TestRestoreNeedsTheConsequenceTypedOut checks naming an archive alone does
// not replace the service.
func TestRestoreNeedsTheConsequenceTypedOut(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""]["tripvault_20260620T031500.000Z.tar.gz"] = "an archive"

	for _, word := range []string{"", "yes"} {
		body, _ := json.Marshal(restoreRequest{Archive: "tripvault_20260620T031500.000Z.tar.gz", Confirm: word})
		recorder := send(s, http.MethodPost, "/api/v1/admin/restore", "good", string(body))
		if recorder.Code != http.StatusUnprocessableEntity || errorCode(t, recorder) != "confirmation_required" {
			t.Errorf("confirmed with %q: %d %s", word, recorder.Code, recorder.Body.String())
		}
	}
	if service.held.Load() != 0 {
		t.Error("an unconfirmed restore held the backups")
	}
}

// TestRestoreNeedsAnArchiveTheDestinationHolds checks a name the destination
// does not hold is refused before anything starts.
func TestRestoreNeedsAnArchiveTheDestinationHolds(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""]["tripvault_20260620T031500.000Z.tar.gz"] = "an archive"

	for _, name := range []string{"tripvault_20990101T000000.000Z.tar.gz", "../../etc/passwd", ""} {
		recorder := requestRestore(s, restoreRequest{Archive: name})
		if recorder.Code != http.StatusNotFound || errorCode(t, recorder) != "archive_not_found" {
			t.Errorf("archive %q: %d %s", name, recorder.Code, recorder.Body.String())
		}
	}
	recorder := requestRestore(s, restoreRequest{ConfigID: uuid.NewString(),
		Archive: "tripvault_20260620T031500.000Z.tar.gz"})
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an unknown configuration: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestRestoreReportsAnArchiveItCannotOpen checks a file that is not an archive,
// and one that needs a passphrase, each say which it is.
func TestRestoreReportsAnArchiveItCannotOpen(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""]["tripvault_20260620T031500.000Z.tar.gz"] = "not an archive at all"
	service.archives[""]["tripvault_20260621T031500.000Z.tar.gz.age"] = "sealed"
	// Named as if in the clear, but encrypted inside: only its bytes can tell.
	service.archives[""]["tripvault_20260622T031500.000Z.tar.gz"] = "age-encryption.org/v1\n-> scrypt\n"

	run := awaitRestore(t, s, requestRestore(s, restoreRequest{Archive: "tripvault_20260620T031500.000Z.tar.gz"}))
	if run.Status != restoreFailed || run.ErrorCode != "not_an_archive" {
		t.Errorf("a file that is not an archive: %+v", run)
	}

	// An archive named encrypted is not started without something to open it.
	recorder := requestRestore(s, restoreRequest{Archive: "tripvault_20260621T031500.000Z.tar.gz.age"})
	if recorder.Code != http.StatusUnprocessableEntity || errorCode(t, recorder) != "passphrase_required" {
		t.Errorf("an encrypted archive with no passphrase: %d %s", recorder.Code, recorder.Body.String())
	}

	run = awaitRestore(t, s, requestRestore(s, restoreRequest{Archive: "tripvault_20260622T031500.000Z.tar.gz"}))
	if run.Status != restoreFailed || run.ErrorCode != "archive_encrypted" {
		t.Errorf("an encrypted archive: %+v", run)
	}
	if held := service.held.Load(); held != 0 {
		t.Errorf("finished restores still hold the backups: %d", held)
	}
}

// TestRestoreOpensTheArchiveWithItsConfigurationsPassphrase checks an encrypted
// archive of an encrypted configuration needs nothing typed, and that a typed
// passphrase is used instead when given.
func TestRestoreOpensTheArchiveWithItsConfigurationsPassphrase(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	created := createConfig(t, s,
		`{"destination_type":"local","encrypted":true,"secrets":{"passphrase":"correct horse battery staple"}}`)

	// Not an archive inside, so opening it is the whole of what can succeed:
	// the right passphrase gets as far as "not an archive", a wrong one not.
	var sealed bytes.Buffer
	writer, err := backup.Encryption{Passphrase: "correct horse battery staple"}.Wrap(&sealed)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	_, _ = io.WriteString(writer, "not an archive at all")
	_ = writer.Close()
	name := "tripvault_20260620T031500.000Z.tar.gz.age"
	service.archives[created.ID] = map[string]string{name: sealed.String()}

	run := awaitRestore(t, s, requestRestore(s, restoreRequest{ConfigID: created.ID, Archive: name}))
	if run.ErrorCode != "not_an_archive" {
		t.Errorf("with the configuration's passphrase: %+v", run)
	}
	run = awaitRestore(t, s, requestRestore(s, restoreRequest{ConfigID: created.ID, Archive: name,
		Passphrase: "something else"}))
	if run.ErrorCode != "archive_unreadable" {
		t.Errorf("with a wrong typed passphrase: %+v", run)
	}
}

// TestRestoreWaitsForNoBackup checks a restore is refused while a backup is
// being written, rather than replacing the database under it.
func TestRestoreWaitsForNoBackup(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""]["tripvault_20260620T031500.000Z.tar.gz"] = "an archive"
	service.writing = true

	recorder := requestRestore(s, restoreRequest{Archive: "tripvault_20260620T031500.000Z.tar.gz"})
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "backup_in_progress" {
		t.Errorf("%d %s", recorder.Code, recorder.Body.String())
	}
}

// TestRestoreHoldsTheServiceUntilItEnds checks the API turns requests away for
// the whole of a restore, the restore's own status excepted, and lets them in
// again once it has finished.
func TestRestoreHoldsTheServiceUntilItEnds(t *testing.T) {
	s, _, service := newBackupServer(t, fakeSealer{})
	service.archives[""]["tripvault_20260620T031500.000Z.tar.gz"] = "not an archive at all"
	service.opening = make(chan struct{})

	accepted := requestRestore(s, restoreRequest{Archive: "tripvault_20260620T031500.000Z.tar.gz"})
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("restore: %d %s", accepted.Code, accepted.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		recorder := send(s, http.MethodGet, "/api/v1/admin/backup-configs", "good", "")
		if recorder.Code == http.StatusServiceUnavailable && errorCode(t, recorder) == "restore_in_progress" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the API still answers during a restore: %d %s", recorder.Code, recorder.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
	close(service.opening)
	awaitRestore(t, s, accepted)

	if recorder := send(s, http.MethodGet, "/api/v1/admin/backup-configs", "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("after the restore: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestMaintenanceEndsRequestsInFlight checks a restore beginning cancels what is
// already running rather than waiting for it, however long it would take.
func TestMaintenanceEndsRequestsInFlight(t *testing.T) {
	s, _, _ := newBackupServer(t, fakeSealer{})
	started := make(chan struct{})
	slow := s.requireAvailable(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))

	finished := make(chan struct{})
	go func() {
		slow.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips", nil))
		close(finished)
	}()
	<-started

	entered := make(chan struct{})
	go func() {
		s.enterMaintenance()
		close(entered)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance waited for a request instead of ending it")
	}
	<-finished
	s.leaveMaintenance()
}
