package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// BackupRepository stores where the service is copied to and what has been
// copied, and reads the database in the shape an archive is built from.
type BackupRepository struct {
	pool *pgxpool.Pool
}

// NewBackupRepository - creates the backup repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewBackupRepository(pool *pgxpool.Pool) *BackupRepository {
	return &BackupRepository{pool: pool}
}

// backupConfigColumns keeps every query and scan in this file in step.
const backupConfigColumns = `id, destination_type, destination_params, sealed_secrets,
	encrypted, schedule_cron, retain_count, retain_days, enabled,
	created_at, updated_at, deleted_at`

// scanBackupConfig reads one row in the order defined by backupConfigColumns,
// followed by whatever extra columns the query appended.
func scanBackupConfig(row pgx.Row, extra ...any) (domain.BackupConfig, error) {
	var config domain.BackupConfig
	var params, secrets []byte

	fields := []any{&config.ID, &config.DestinationType, &params, &secrets,
		&config.Encrypted, &config.ScheduleCron, &config.RetainCount,
		&config.RetainDays, &config.Enabled, &config.CreatedAt, &config.UpdatedAt, &config.DeletedAt}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return domain.BackupConfig{}, err
	}

	if len(params) > 0 {
		if err := json.Unmarshal(params, &config.DestinationParams); err != nil {
			return domain.BackupConfig{}, fmt.Errorf("read destination parameters: %w", err)
		}
	}
	if config.DestinationParams == nil {
		config.DestinationParams = map[string]string{}
	}
	// Each value is sealed bytes, which JSON carries as base64.
	if len(secrets) > 0 {
		if err := json.Unmarshal(secrets, &config.SealedSecrets); err != nil {
			return domain.BackupConfig{}, fmt.Errorf("read sealed credentials: %w", err)
		}
	}
	return config, nil
}

// encodeBackupConfig turns the two free-form parts of a configuration into the
// JSON its columns hold. An empty set is written as an empty object, never as
// JSON null, so every row has the same shape.
func encodeBackupConfig(config domain.BackupConfig) (params, secrets []byte, err error) {
	destination := config.DestinationParams
	if destination == nil {
		destination = map[string]string{}
	}
	if params, err = json.Marshal(destination); err != nil {
		return nil, nil, fmt.Errorf("encode destination parameters: %w", err)
	}

	sealed := config.SealedSecrets
	if sealed == nil {
		sealed = map[string][]byte{}
	}
	if secrets, err = json.Marshal(sealed); err != nil {
		return nil, nil, fmt.Errorf("encode sealed credentials: %w", err)
	}
	return params, secrets, nil
}

// CreateConfig - stores a new backup configuration.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - config: the configuration to store, already validated.
//
// Returns:
//   - the stored configuration.
//   - an error if the statement fails.
func (r *BackupRepository) CreateConfig(ctx context.Context, config domain.BackupConfig) (
	domain.BackupConfig, error) {
	params, secrets, err := encodeBackupConfig(config)
	if err != nil {
		return domain.BackupConfig{}, err
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO backup_configs (id, destination_type, destination_params,
			sealed_secrets, encrypted, schedule_cron, retain_count, retain_days, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 RETURNING `+backupConfigColumns,
		config.ID, config.DestinationType, params, secrets, config.Encrypted,
		config.ScheduleCron, config.RetainCount, config.RetainDays, config.Enabled)

	created, err := scanBackupConfig(row)
	if err != nil {
		return domain.BackupConfig{}, fmt.Errorf("create backup configuration: %w", err)
	}
	return created, nil
}

// ListConfigs - returns the instance's backup configurations, oldest first.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the configurations.
//   - an error if the query fails.
func (r *BackupRepository) ListConfigs(ctx context.Context) ([]domain.BackupConfig, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+backupConfigColumns+` FROM backup_configs
		 WHERE deleted_at IS NULL
		 ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list backup configurations: %w", err)
	}
	defer rows.Close()

	configs := make([]domain.BackupConfig, 0)
	for rows.Next() {
		config, err := scanBackupConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("scan backup configuration: %w", err)
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read backup configurations: %w", err)
	}
	return configs, nil
}

// GetConfig - loads one configuration.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the configuration's identifier.
//
// Returns:
//   - the configuration.
//   - domain.ErrNotFound when no live configuration with that identifier exists.
func (r *BackupRepository) GetConfig(ctx context.Context, id uuid.UUID) (
	domain.BackupConfig, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+backupConfigColumns+` FROM backup_configs
		 WHERE id = $1 AND deleted_at IS NULL`, id)

	config, err := scanBackupConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BackupConfig{}, fmt.Errorf("get backup configuration: %w", err)
	}
	return config, nil
}

// GetConfigForRun - loads a configuration whether or not it has been withdrawn.
//
// It exists for one caller: reading an archive back. A withdrawn configuration
// is not offered anywhere and cannot be run, but the archives it wrote are still
// where it put them, and this row is the only record of where that was.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the configuration's identifier.
//
// Returns:
//   - the configuration.
//   - domain.ErrNotFound when no configuration with that identifier exists.
func (r *BackupRepository) GetConfigForRun(ctx context.Context, id uuid.UUID) (
	domain.BackupConfig, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+backupConfigColumns+` FROM backup_configs WHERE id = $1`, id)

	config, err := scanBackupConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BackupConfig{}, fmt.Errorf("get backup configuration: %w", err)
	}
	return config, nil
}

// UpdateConfig - replaces a configuration's settings.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - config: the configuration with its new values; ID identifies it.
//
// Returns:
//   - the stored configuration.
//   - domain.ErrNotFound when no live configuration with that identifier exists.
func (r *BackupRepository) UpdateConfig(ctx context.Context, config domain.BackupConfig) (
	domain.BackupConfig, error) {
	params, secrets, err := encodeBackupConfig(config)
	if err != nil {
		return domain.BackupConfig{}, err
	}

	row := r.pool.QueryRow(ctx,
		`UPDATE backup_configs SET
			destination_type = $2, destination_params = $3, sealed_secrets = $4,
			encrypted = $5, schedule_cron = $6, retain_count = $7,
			retain_days = $8, enabled = $9, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING `+backupConfigColumns,
		config.ID, config.DestinationType, params, secrets, config.Encrypted,
		config.ScheduleCron, config.RetainCount, config.RetainDays, config.Enabled)

	updated, err := scanBackupConfig(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BackupConfig{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BackupConfig{}, fmt.Errorf("update backup configuration: %w", err)
	}
	return updated, nil
}

// DeleteConfig - retires a configuration.
//
// The row is marked rather than removed, and the runs it produced are the reason
// why: they are the record of what was copied and when, and the archives are
// still at the destination. Removing the instruction should not make the copies
// it already made unreachable.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the configuration to retire.
//
// Returns:
//   - domain.ErrNotFound when no live configuration with that identifier exists.
func (r *BackupRepository) DeleteConfig(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE backup_configs SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete backup configuration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DueConfigs - returns every scheduled configuration with the instant it last ran.
//
// Arguments:
//   - ctx: context bounding the query.
//
// Returns:
//   - the configurations the scheduler has to consider.
//   - an error if the query fails.
func (r *BackupRepository) DueConfigs(ctx context.Context) ([]backup.ScheduledConfig, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+backupConfigColumns+`,
		        (SELECT max(started_at) FROM backup_runs
		         WHERE backup_config_id = backup_configs.id)
		 FROM backup_configs
		 WHERE deleted_at IS NULL AND enabled AND schedule_cron <> ''
		 ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list scheduled backups: %w", err)
	}
	defer rows.Close()

	scheduled := make([]backup.ScheduledConfig, 0)
	for rows.Next() {
		var lastStartedAt *time.Time
		config, err := scanBackupConfig(rows, &lastStartedAt)
		if err != nil {
			return nil, fmt.Errorf("scan scheduled backup: %w", err)
		}

		entry := backup.ScheduledConfig{Config: config}
		if lastStartedAt != nil {
			entry.LastStartedAt = *lastStartedAt
		}
		scheduled = append(scheduled, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read scheduled backups: %w", err)
	}
	return scheduled, nil
}

// backupRunColumns keeps every run query and scan in step.
const backupRunColumns = `id, backup_config_id, started_at, finished_at,
	status, size_bytes, artifact, rotated_at, error_message`

// scanBackupRun reads one row in the order defined by backupRunColumns.
func scanBackupRun(row pgx.Row) (domain.BackupRun, error) {
	var run domain.BackupRun
	err := row.Scan(&run.ID, &run.ConfigID, &run.StartedAt, &run.FinishedAt,
		&run.Status, &run.SizeBytes, &run.Artifact, &run.RotatedAt, &run.ErrorMessage)
	return run, err
}

// StartRun - records that a backup has begun.
//
// The row is written before the work rather than after it, so a run interrupted
// by a restart is visible as one that never finished instead of not being
// visible at all.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - run: the run to record.
//
// Returns:
//   - the stored run.
//   - an error if the statement fails.
func (r *BackupRepository) StartRun(ctx context.Context, run domain.BackupRun) (domain.BackupRun, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO backup_runs (id, backup_config_id, started_at, status)
		 VALUES ($1,$2,$3,$4)
		 RETURNING `+backupRunColumns,
		run.ID, run.ConfigID, run.StartedAt, domain.BackupRunning)

	created, err := scanBackupRun(row)
	if err != nil {
		return domain.BackupRun{}, fmt.Errorf("record backup start: %w", err)
	}
	return created, nil
}

// FinishRun - records how a backup ended.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the run to close.
//   - status: success or failed.
//   - size: how large the archive was, or nil when there is none.
//   - artifact: what the run wrote, or empty when it wrote nothing.
//   - message: why it failed, or empty when it did not.
//
// Returns:
//   - domain.ErrNotFound when no run with that identifier exists.
func (r *BackupRepository) FinishRun(ctx context.Context, id uuid.UUID, status string,
	size *int64, artifact, message string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE backup_runs
		 SET finished_at = now(), status = $2, size_bytes = $3, artifact = $4, error_message = $5
		 WHERE id = $1`, id, status, size, artifact, message)
	if err != nil {
		return fmt.Errorf("record backup end: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListRuns - returns the instance's backup history, most recent first.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - limit: page size.
//   - offset: how many runs to skip.
//
// Returns:
//   - the page of runs and the total.
//   - an error if a query fails.
func (r *BackupRepository) ListRuns(ctx context.Context, limit, offset int) (
	[]domain.BackupRun, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM backup_runs`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count backup runs: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs
		 ORDER BY started_at DESC, id DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list backup runs: %w", err)
	}
	defer rows.Close()

	runs := make([]domain.BackupRun, 0, limit)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan backup run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read backup runs: %w", err)
	}
	return runs, total, nil
}

// GetRun - loads one run.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the run's identifier.
//
// Returns:
//   - the run.
//   - domain.ErrNotFound when no run with that identifier exists.
func (r *BackupRepository) GetRun(ctx context.Context, id uuid.UUID) (domain.BackupRun, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs WHERE id = $1`, id)

	run, err := scanBackupRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BackupRun{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.BackupRun{}, fmt.Errorf("get backup run: %w", err)
	}
	return run, nil
}

// HeldArtifacts - returns the archives one configuration has produced and still
// holds, beyond the newest it is asked to keep.
//
// Retention asks the history rather than the destination's directory. Two
// configurations can write into the same directory, so a listing there is not
// this configuration's archives, and acting on it would be one backup destroying
// another's copies.
//
// Arguments:
//   - ctx: context bounding the query.
//   - configID: the configuration whose archives to consider.
//   - retain: how many of the newest to leave alone.
//
// Returns:
//   - the runs whose archives should now be removed, newest first.
//   - an error if the query fails.
func (r *BackupRepository) HeldArtifacts(ctx context.Context, configID uuid.UUID, retain int) (
	[]domain.BackupRun, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs
		 WHERE backup_config_id = $1 AND status = 'success'
		   AND artifact <> '' AND rotated_at IS NULL
		 ORDER BY started_at DESC, id DESC
		 OFFSET $2`, configID, retain)
	if err != nil {
		return nil, fmt.Errorf("list held archives: %w", err)
	}
	defer rows.Close()

	return collectBackupRuns(rows, "held archive")
}

// ExpiredArtifacts - returns the archives one configuration has produced and
// still holds that were taken before a given instant.
//
// It is the age-based half of retention, and asks the history for the same
// reason HeldArtifacts does: the destination's directory may be shared.
//
// Arguments:
//   - ctx: context bounding the query.
//   - configID: the configuration whose archives to consider.
//   - before: archives taken before this instant are the ones to remove.
//
// Returns:
//   - the runs whose archives should now be removed, newest first.
//   - an error if the query fails.
func (r *BackupRepository) ExpiredArtifacts(ctx context.Context, configID uuid.UUID,
	before time.Time) ([]domain.BackupRun, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+backupRunColumns+` FROM backup_runs
		 WHERE backup_config_id = $1 AND status = 'success'
		   AND artifact <> '' AND rotated_at IS NULL AND started_at < $2
		 ORDER BY started_at DESC, id DESC`, configID, before)
	if err != nil {
		return nil, fmt.Errorf("list expired archives: %w", err)
	}
	defer rows.Close()

	return collectBackupRuns(rows, "expired archive")
}

// collectBackupRuns reads a rotation query's rows, naming what it was reading in
// any failure.
func collectBackupRuns(rows pgx.Rows, what string) ([]domain.BackupRun, error) {
	runs := make([]domain.BackupRun, 0)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", what, err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %ss: %w", what, err)
	}
	return runs, nil
}

// PinHostKey - records the host key an SFTP destination accepted on first use.
//
// It only fills a key that is still empty. A key somebody set in the meantime is
// the one they meant, and is left alone for the next run to be held to. A
// withdrawn configuration is updated too: restoring one of its archives is a
// connection like any other, and the key it learns is just as much the answer.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - configID: the configuration the key belongs to.
//   - key: the host key in authorized_keys form.
//
// Returns:
//   - an error if the statement fails.
func (r *BackupRepository) PinHostKey(ctx context.Context, configID uuid.UUID, key string) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE backup_configs
		 SET destination_params = jsonb_set(destination_params, '{host_key}', to_jsonb($2::text)),
		     updated_at = now()
		 WHERE id = $1 AND coalesce(destination_params->>'host_key', '') = ''`,
		configID, key); err != nil {
		return fmt.Errorf("record host key: %w", err)
	}
	return nil
}

// MarkRotated - records that a run's archive is no longer at its destination.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the run whose archive was removed.
//
// Returns:
//   - an error if the statement fails.
func (r *BackupRepository) MarkRotated(ctx context.Context, id uuid.UUID) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE backup_runs SET rotated_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("record archive rotation: %w", err)
	}
	return nil
}
