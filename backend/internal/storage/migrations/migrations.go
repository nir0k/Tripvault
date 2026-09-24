// Package migrations embeds the SQL schema migrations and applies them with
// goose. Migrations run in-process at start-up, so a freshly built image and an
// empty database converge without a separate step.
package migrations

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/nir0k/tripvault/backend/internal/backup"
)

//go:embed sql/*.sql
var migrationFS embed.FS

// migrationDir is the directory inside migrationFS holding the scripts.
const migrationDir = "sql"

// Apply - brings the database schema up to the newest embedded migration.
//
// Arguments:
//   - ctx: context bounding the run.
//   - pool: an established pool pointing at the target database.
//   - logger: destination for goose progress output.
//
// Returns:
//   - an error if a migration cannot be applied.
func Apply(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	return withGoose(pool, logger, func(db *sql.DB) error {
		if err := goose.UpContext(ctx, db, migrationDir); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		return nil
	})
}

// Version - reports the migration version currently applied.
//
// Arguments:
//   - ctx: context bounding the query.
//   - pool: an established pool pointing at the target database.
//
// Returns:
//   - the applied version, or 0 when no migration has run.
//   - an error if the version cannot be read.
func Version(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var version int64
	err := withGoose(pool, nil, func(db *sql.DB) error {
		var err error
		version, err = goose.GetDBVersionContext(ctx, db)
		if err != nil {
			return fmt.Errorf("read migration version: %w", err)
		}
		return nil
	})
	return version, err
}

// withGoose runs one goose operation against a short-lived database/sql handle
// on the pool. goose needs the standard library driver while the service talks
// to PostgreSQL through pgx natively; closing the handle leaves the pool open.
func withGoose(pool *pgxpool.Pool, logger *slog.Logger, run func(*sql.DB) error) error {
	goose.SetBaseFS(migrationFS)
	if logger != nil {
		goose.SetLogger(gooseLogger{logger: logger})
	} else {
		goose.SetLogger(goose.NopLogger())
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer func() { _ = db.Close() }()
	return run(db)
}

// gooseLogger adapts goose's printf-style logger to slog, so its output joins
// the logfmt stream instead of going to stderr.
type gooseLogger struct {
	logger *slog.Logger
}

// Printf forwards goose progress messages at info level.
func (g gooseLogger) Printf(format string, v ...any) {
	g.logger.Info(fmt.Sprintf(format, v...))
}

// Fatalf forwards goose failures at error level. It must not exit the process:
// the caller decides what a failed migration means.
func (g gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Error(fmt.Sprintf(format, v...))
}

// UpTo - brings the schema to one particular migration.
//
// Arguments:
//   - ctx: context bounding the run.
//   - pool: an established pool pointing at the target database.
//   - logger: destination for goose progress output.
//   - version: the migration to stop at.
//
// Returns:
//   - an error if the migrations cannot be applied.
func UpTo(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, version int64) error {
	return withGoose(pool, logger, func(db *sql.DB) error {
		if err := goose.UpToContext(ctx, db, migrationDir, version); err != nil {
			return fmt.Errorf("build the schema of the archive: %w", err)
		}
		return nil
	})
}

// Latest - the newest migration this build ships.
//
// A restore refuses an archive whose schema is newer than this: loading rows
// into tables the build does not know how to create would be guesswork.
//
// Returns:
//   - the highest embedded migration version.
//   - an error if the embedded migrations cannot be read.
func Latest() (int64, error) {
	entries, err := migrationFS.ReadDir(migrationDir)
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}

	var latest int64
	for _, entry := range entries {
		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil {
			continue
		}
		latest = max(latest, version)
	}
	return latest, nil
}

// Migrator - prepares and atomically activates schemas for a restore.
//
// The replacement is built at the archive version, loaded and upgraded under
// an isolated search path before public is renamed during cutover. The legacy
// Reset and UpTo methods remain for callers that do not support staging.
type Migrator struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	// restoreMu serialises schema swaps. A second whole-instance restore must
	// not prepare against a public schema that the first is about to replace.
	restoreMu sync.Mutex
}

// restoreStage is a migrated schema and its pool before activation, then the
// name of the previous public schema until cleanup or rollback.
type restoreStage struct {
	migrator *Migrator
	name     string
	previous string
	pool     *pgxpool.Pool
	active   bool
	closed   sync.Once
}

// NewMigrator - builds the migrator a restore uses.
//
// Arguments:
//   - pool: an established pool pointing at the target database.
//   - logger: destination for goose progress output.
//
// Returns:
//   - a migrator bound to that pool.
func NewMigrator(pool *pgxpool.Pool, logger *slog.Logger) *Migrator {
	return &Migrator{pool: pool, logger: logger}
}

// Latest - the newest migration this build ships.
//
// Returns:
//   - the highest embedded migration version, or 0 if they cannot be read, which
//     makes a restore refuse every archive rather than accept a wrong one.
func (m *Migrator) Latest() int64 {
	latest, err := Latest()
	if err != nil {
		m.logger.Error("the embedded migrations could not be read", slog.Any("error", err))
		return 0
	}
	return latest
}

// BeginRestore - creates and migrates an isolated schema for an archive.
//
// Arguments:
//   - ctx: context bounding schema creation and migrations.
//   - version: migration version recorded by the archive.
//   - tables: table names recorded by the archive.
//
// Returns:
//   - a stage that loads tables without changing public.
//   - an error if the staging schema cannot be prepared.
func (m *Migrator) BeginRestore(ctx context.Context, version int64, tables []string) (backup.DatabaseStage, error) {
	m.restoreMu.Lock()
	stage := &restoreStage{
		migrator: m,
		name:     "tripvault_restore_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		previous: "tripvault_previous_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
	}
	if _, err := m.pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{stage.name}.Sanitize()); err != nil {
		m.restoreMu.Unlock()
		return nil, fmt.Errorf("create restore schema: %w", err)
	}

	poolConfig := m.pool.Config().Copy()
	params := make(map[string]string, len(poolConfig.ConnConfig.RuntimeParams)+1)
	for key, value := range poolConfig.ConnConfig.RuntimeParams {
		params[key] = value
	}
	params["search_path"] = stage.name
	poolConfig.ConnConfig.RuntimeParams = params
	var err error
	stage.pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		stage.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("create restore schema pool: %w", err)
	}
	if err := stage.pool.Ping(ctx); err != nil {
		stage.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("open restore schema: %w", err)
	}
	if err := UpTo(ctx, stage.pool, m.logger, version); err != nil {
		stage.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	if err := validateRestoreTables(ctx, stage.pool, tables); err != nil {
		stage.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return stage, nil
}

// validateRestoreTables checks the manifest contains every application table
// created by the archive's schema version and no invented table.
func validateRestoreTables(ctx context.Context, pool *pgxpool.Pool, recorded []string) error {
	rows, err := pool.Query(ctx,
		`SELECT c.relname
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = current_schema() AND c.relkind = 'r'
		   AND c.relname <> ALL($1)
		 ORDER BY c.relname`, []string{"goose_db_version", "tile_cache"})
	if err != nil {
		return fmt.Errorf("list staged restore tables: %w", err)
	}
	defer rows.Close()
	want := make([]string, 0, len(recorded))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan staged restore table: %w", err)
		}
		want = append(want, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list staged restore tables: %w", err)
	}
	got := append([]string(nil), recorded...)
	sort.Strings(got)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		return fmt.Errorf("%w: table list does not match schema %v", backup.ErrArchiveDamaged, want)
	}
	return nil
}

// LoadTables loads CSV rows into the isolated schema.
func (s *restoreStage) LoadTables(ctx context.Context, tables []string,
	open func(table string) (io.ReadCloser, error)) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
			return fmt.Errorf("defer restore constraints: %w", err)
		}
		for _, table := range tables {
			if err := loadRestoreTable(ctx, tx, table, open); err != nil {
				return err
			}
		}
		return nil
	})
}

// loadRestoreTable copies one CSV into a table of the staging schema.
func loadRestoreTable(ctx context.Context, tx pgx.Tx, table string,
	open func(table string) (io.ReadCloser, error)) error {
	file, err := open(table)
	if err != nil {
		return fmt.Errorf("read %s from the archive: %w", table, err)
	}
	defer func() { _ = file.Close() }()

	buffered := bufio.NewReader(file)
	header, err := buffered.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read the columns of %s: %w", table, err)
	}
	columns := strings.TrimSpace(header)
	if columns == "" {
		return nil
	}
	names := make([]string, 0, 8)
	for _, column := range strings.Split(columns, ",") {
		names = append(names, pgx.Identifier{strings.Trim(strings.TrimSpace(column), `"`)}.Sanitize())
	}
	statement := fmt.Sprintf(`COPY %s (%s) FROM STDIN (FORMAT csv)`,
		pgx.Identifier{table}.Sanitize(), strings.Join(names, ", "))
	if _, err := tx.Conn().PgConn().CopyFrom(ctx, buffered, statement); err != nil {
		return fmt.Errorf("load %s: %w", table, err)
	}
	return nil
}

// Upgrade migrates the restored schema to this build before it is activated.
func (s *restoreStage) Upgrade(ctx context.Context) error {
	return Apply(ctx, s.pool, s.migrator.logger)
}

// Activate atomically swaps the staged schema with public.
func (s *restoreStage) Activate(ctx context.Context) error {
	if s.active {
		return nil
	}
	s.pool.Close()
	err := pgx.BeginFunc(ctx, s.migrator.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `ALTER SCHEMA public RENAME TO `+pgx.Identifier{s.previous}.Sanitize()); err != nil {
			return fmt.Errorf("move the current schema aside: %w", err)
		}
		if _, err := tx.Exec(ctx, `ALTER SCHEMA `+pgx.Identifier{s.name}.Sanitize()+` RENAME TO public`); err != nil {
			return fmt.Errorf("activate the restored schema: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.migrator.pool.Reset()
	s.active = true
	return nil
}

// Rollback returns the previous schema to public after a failed cutover.
func (s *restoreStage) Rollback(ctx context.Context) error {
	if !s.active {
		return nil
	}
	failed := "tripvault_failed_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	err := pgx.BeginFunc(ctx, s.migrator.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `ALTER SCHEMA public RENAME TO `+pgx.Identifier{failed}.Sanitize()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `ALTER SCHEMA `+pgx.Identifier{s.previous}.Sanitize()+` RENAME TO public`); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("roll back restored schema: %w", err)
	}
	s.migrator.pool.Reset()
	s.active = false
	s.previous = ""
	_, dropErr := s.migrator.pool.Exec(context.WithoutCancel(ctx),
		`DROP SCHEMA `+pgx.Identifier{failed}.Sanitize()+` CASCADE`)
	return dropErr
}

// Finish removes the previous schema after a successful cutover.
func (s *restoreStage) Finish(ctx context.Context) error {
	if !s.active || s.previous == "" {
		return nil
	}
	_, err := s.migrator.pool.Exec(ctx,
		`DROP SCHEMA `+pgx.Identifier{s.previous}.Sanitize()+` CASCADE`)
	if err == nil {
		s.previous = ""
	}
	return err
}

// Close drops an unactivated staging schema and releases the restore lock.
func (s *restoreStage) Close(ctx context.Context) {
	s.closed.Do(func() {
		if s.pool != nil {
			s.pool.Close()
		}
		if !s.active && s.name != "" {
			_, _ = s.migrator.pool.Exec(ctx,
				`DROP SCHEMA IF EXISTS `+pgx.Identifier{s.name}.Sanitize()+` CASCADE`)
		}
		s.migrator.restoreMu.Unlock()
	})
}
