package postgres

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/backup"
	"github.com/nir0k/tripvault/backend/internal/domain"
)

// backupSnapshot exposes every backup read through one repeatable-read
// transaction. The transaction remains open only while Build writes the
// archive, so all tables and the media catalogue describe one instant.
type backupSnapshot struct {
	tx pgx.Tx
}

// WithSnapshot - runs all archive reads against one PostgreSQL snapshot.
//
// Arguments:
//   - ctx: context bounding the snapshot transaction.
//   - use: archive builder callback receiving the snapshot-backed source.
//
// Returns:
//   - an error from the transaction or callback.
func (r *BackupRepository) WithSnapshot(ctx context.Context, use func(backup.Source) error) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(tx pgx.Tx) error {
		return use(&backupSnapshot{tx: tx})
	})
}

// gooseTable holds the schema version and is written by the migrations
// themselves, so an archive records the version in its manifest instead and the
// table is left out of the dump.
const gooseTable = "goose_db_version"

// cacheTables hold what the service fetched from elsewhere and can fetch again.
// They are left out of an archive, which would otherwise carry megabytes of map
// tiles nobody asked to keep.
var cacheTables = []string{"tile_cache"}

// SchemaVersion reports the migration version visible in the snapshot.
func (s *backupSnapshot) SchemaVersion(ctx context.Context) (int64, error) {
	return schemaVersion(ctx, s.tx)
}

// schemaVersion reads the applied migration version in the snapshot.
func schemaVersion(ctx context.Context, q pgx.Tx) (int64, error) {
	var version int64
	err := q.QueryRow(ctx,
		`SELECT coalesce(max(version_id), 0) FROM `+gooseTable+` WHERE is_applied`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// Tables lists the tables visible in the snapshot.
func (s *backupSnapshot) Tables(ctx context.Context) ([]string, error) {
	return backupTables(ctx, s.tx)
}

// backupTables orders the application tables in the snapshot.
func backupTables(ctx context.Context, q pgx.Tx) ([]string, error) {
	names, err := tableNames(ctx, q)
	if err != nil {
		return nil, err
	}
	dependencies, err := tableDependencies(ctx, q, names)
	if err != nil {
		return nil, err
	}

	// Kahn's algorithm, taking the alphabetically first table whenever several
	// are ready, so two dumps of the same schema list their tables alike.
	ordered := make([]string, 0, len(names))
	placed := make(map[string]bool, len(names))
	for len(ordered) < len(names) {
		progressed := false
		for _, name := range names {
			if placed[name] {
				continue
			}
			ready := true
			for _, parent := range dependencies[name] {
				if !placed[parent] {
					ready = false
					break
				}
			}
			if ready {
				ordered = append(ordered, name)
				placed[name] = true
				progressed = true
			}
		}
		if !progressed {
			return nil, fmt.Errorf("these tables refer to each other in a circle: %s",
				strings.Join(remainingTables(names, placed), ", "))
		}
	}
	return ordered, nil
}

// remainingTables names the tables an ordering could not place.
func remainingTables(names []string, placed map[string]bool) []string {
	var left []string
	for _, name := range names {
		if !placed[name] {
			left = append(left, name)
		}
	}
	return left
}

// tableNames lists the ordinary tables of the public schema, alphabetically,
// leaving out the schema version and the caches.
func tableNames(ctx context.Context, q pgx.Tx) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT c.relname
		 FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> ALL($1)
		 ORDER BY c.relname`, append([]string{gooseTable}, cacheTables...))
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// tableDependencies maps each table to the tables its foreign keys point at.
//
// A key onto the table itself is left out: it orders rows within one table,
// which a single COPY loads at once anyway. A deferrable key is left out too,
// and that is what lets two tables refer to each other: a restore defers those
// for the length of its transaction, so they place no requirement on the order
// the tables are loaded in.
func tableDependencies(ctx context.Context, q pgx.Tx, names []string) (map[string][]string, error) {
	rows, err := q.Query(ctx,
		`SELECT DISTINCT child.relname, parent.relname
		 FROM pg_constraint c
		 JOIN pg_class child ON child.oid = c.conrelid
		 JOIN pg_class parent ON parent.oid = c.confrelid
		 JOIN pg_namespace n ON n.oid = child.relnamespace
		 WHERE c.contype = 'f' AND NOT c.condeferrable AND n.nspname = 'public'
		   AND child.relname <> parent.relname
		   AND child.relname = ANY($1) AND parent.relname = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("list table dependencies: %w", err)
	}
	defer rows.Close()

	dependencies := make(map[string][]string)
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, fmt.Errorf("scan table dependency: %w", err)
		}
		dependencies[child] = append(dependencies[child], parent)
	}
	return dependencies, rows.Err()
}

// DumpTable writes one table as seen by the snapshot transaction.
func (s *backupSnapshot) DumpTable(ctx context.Context, table string, w io.Writer) error {
	_, err := s.tx.Conn().PgConn().CopyTo(ctx, w,
		fmt.Sprintf(`COPY %s TO STDOUT (FORMAT csv, HEADER true)`, pgx.Identifier{table}.Sanitize()))
	if err != nil {
		return fmt.Errorf("dump %s: %w", table, err)
	}
	return nil
}

// TripCount reports the number of trips visible in the snapshot.
func (s *backupSnapshot) TripCount(ctx context.Context) (int, error) {
	return tripCount(ctx, s.tx)
}

// tripCount counts trips in the snapshot.
func tripCount(ctx context.Context, q pgx.Tx) (int, error) {
	var count int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM trips`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count trips: %w", err)
	}
	return count, nil
}

// AllMedia lists the media catalogue visible in the snapshot.
func (s *backupSnapshot) AllMedia(ctx context.Context) ([]domain.Media, error) {
	return allMedia(ctx, s.tx)
}

// AdditionalFiles lists account avatars visible in the snapshot.
func (s *backupSnapshot) AdditionalFiles(ctx context.Context) ([]backup.FileMetadata, error) {
	return additionalFiles(ctx, s.tx)
}

// additionalFiles reads every avatar key in the snapshot. The archive builder
// measures the bytes because users do not store an avatar size or checksum.
func additionalFiles(ctx context.Context, q pgx.Tx) ([]backup.FileMetadata, error) {
	rows, err := q.Query(ctx,
		`SELECT avatar_key, avatar_updated_at FROM users WHERE avatar_key <> '' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list account avatars: %w", err)
	}
	defer rows.Close()

	files := make([]backup.FileMetadata, 0)
	for rows.Next() {
		var file backup.FileMetadata
		var updated *time.Time
		if err := rows.Scan(&file.Name, &updated); err != nil {
			return nil, fmt.Errorf("scan account avatar: %w", err)
		}
		file.OriginalName = "avatar.jpg"
		file.MIME = "image/jpeg"
		if updated != nil {
			file.UploadedAt = updated.UTC()
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

// allMedia reads every media row in the snapshot.
func allMedia(ctx context.Context, q pgx.Tx) ([]domain.Media, error) {
	rows, err := q.Query(ctx,
		`SELECT `+mediaColumns+` FROM media m ORDER BY m.created_at, m.id`)
	if err != nil {
		return nil, fmt.Errorf("list every file: %w", err)
	}
	defer rows.Close()

	files := make([]domain.Media, 0)
	for rows.Next() {
		file, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, file)
	}
	return files, rows.Err()
}
