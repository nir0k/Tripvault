package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The lines of the activities of a plan or a report. Each of them holds at
// most one, so an import replaces whatever was there: that is what the unique
// key is for, and what makes importing the same file twice harmless.
//
// The uploaded file is kept in the same row, compressed, and read only by
// TrackFile: the document query lists trackColumns, which leave it out, so a
// report with a dozen long recordings reads no faster or slower for them.

var trackColumns = `t.id, t.document_id, t.item_id, t.original_name, t.format, t.geometry, t.distance_m,
	t.point_count, t.ascent_m, t.descent_m, t.grades, t.speed_kmh, t.started_at, t.ended_at, t.file_size, t.created_at`

// scanTrack reads one row in the order of trackColumns.
func scanTrack(row pgx.Row) (domain.Track, error) {
	var t domain.Track
	err := row.Scan(&t.ID, &t.DocumentID, &t.ItemID, &t.OriginalName, &t.Format, &t.Geometry,
		&t.DistanceM, &t.PointCount, &t.AscentM, &t.DescentM, &t.Grades, &t.SpeedKmh, &t.StartedAt, &t.EndedAt,
		&t.FileSize, &t.CreatedAt)
	return t, err
}

// SaveTrack - stores the line of an activity, replacing
// the one it had. The journeys to and from the place leave and reach its
// recording's ends, so a new line sends them back to be calculated. A line
// replaced keeps the speed its time was worked out at: the activity is the
// same, and so is its pace.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - track: the parsed track with its ID, document and place set.
//   - file: the uploaded file, stored compressed for downloading.
//   - quota: the most file data its trip may keep; zero means no limit.
//
// Returns:
//   - the track as stored.
//   - domain.ErrNotFound when the place is not part of the document.
//   - domain.ErrMediaQuota when the replacement does not fit the trip.
func (r *DocumentRepository) SaveTrack(ctx context.Context, track domain.Track, file []byte,
	quota int64) (domain.Track, error) {
	compressed, err := compress(file)
	if err != nil {
		return domain.Track{}, err
	}
	track.FileSize = int64(len(file))
	var saved domain.Track
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		document, _, err := lockDocument(ctx, tx, track.DocumentID)
		if err != nil {
			return err
		}
		if quota > 0 {
			used, err := tripUsedBytes(ctx, tx, document.TripID)
			if err != nil {
				return err
			}
			var previous int64
			if err := tx.QueryRow(ctx,
				`SELECT coalesce((SELECT file_size FROM tracks WHERE item_id = $1), 0)`, track.ItemID).
				Scan(&previous); err != nil {
				return fmt.Errorf("measure replaced track: %w", err)
			}
			if used-previous+track.FileSize > quota {
				return domain.ErrMediaQuota
			}
		}
		saved, err = oneRow(scanTrack, tx.QueryRow(ctx,
			`INSERT INTO tracks AS t (id, document_id, item_id, original_name, format, geometry, distance_m,
			                          point_count, ascent_m, descent_m, file_gz, started_at, ended_at, climb_version,
			                          grades, speed_kmh, file_size)
			 SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
			 WHERE EXISTS (SELECT 1 FROM items WHERE id = $3 AND document_id = $2 AND kind <> 'stay_anchor')
			 ON CONFLICT (item_id) DO UPDATE
			   SET original_name = excluded.original_name, format = excluded.format, geometry = excluded.geometry,
			       distance_m = excluded.distance_m, point_count = excluded.point_count,
			       ascent_m = excluded.ascent_m, descent_m = excluded.descent_m, file_gz = excluded.file_gz,
			       started_at = excluded.started_at, ended_at = excluded.ended_at,
			       climb_version = excluded.climb_version, grades = excluded.grades,
			       file_size = excluded.file_size, created_at = now()
			 RETURNING `+trackColumns,
			track.ID, track.DocumentID, track.ItemID, track.OriginalName, track.Format, track.Geometry,
			track.DistanceM, track.PointCount, track.AscentM, track.DescentM, compressed,
			track.StartedAt, track.EndedAt, track.ClimbVersion, track.Grades, track.SpeedKmh, track.FileSize), "save track")
		if err != nil {
			return err
		}
		return syncLegs(ctx, tx, document.TripID)
	})
	return saved, err
}

// Track - reads one recorded line without its file.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the track.
//
// Returns:
//   - the track, which names the document to check access against.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Track(ctx context.Context, id uuid.UUID) (domain.Track, error) {
	return oneRow(scanTrack, r.pool.QueryRow(ctx, `SELECT `+trackColumns+` FROM tracks t WHERE t.id = $1`, id),
		"get track")
}

// TrackFile - reads the file a track was imported from.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the track.
//
// Returns:
//   - the file, uncompressed.
//   - domain.ErrNotFound when the track does not exist.
func (r *DocumentRepository) TrackFile(ctx context.Context, id uuid.UUID) (domain.TrackFile, error) {
	var file domain.TrackFile
	var compressed []byte
	err := r.pool.QueryRow(ctx, `SELECT original_name, format, file_gz FROM tracks WHERE id = $1`, id).
		Scan(&file.Name, &file.Format, &compressed)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, domain.ErrNotFound
	}
	if err != nil {
		return file, fmt.Errorf("get track file: %w", err)
	}
	file.Data, err = decompress(compressed)
	return file, err
}

// StaleTracks - lists the tracks whose climb was measured an older way.
//
// Arguments:
//   - ctx: context bounding the query.
//   - version: the way the climb is measured now.
//
// Returns:
//   - the tracks to measure again.
//   - an error if the query fails.
func (r *DocumentRepository) StaleTracks(ctx context.Context, version int) ([]uuid.UUID, error) {
	return collect(ctx, r.pool, func(row pgx.Row) (uuid.UUID, error) {
		var id uuid.UUID
		return id, row.Scan(&id)
	}, `SELECT id FROM tracks WHERE climb_version < $1 ORDER BY created_at`, version)
}

// UpdateTrackClimb - stores the climb and the slopes of a track measured again
// from its file.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the track.
//   - ascent, descent: the metres gained and lost, nil when the file records
//     no heights.
//   - grades: the metres run at each slope, nil without heights.
//   - version: the way they were measured.
//
// Returns:
//   - an error if the statement fails; a track deleted meanwhile is no error.
func (r *DocumentRepository) UpdateTrackClimb(ctx context.Context, id uuid.UUID, ascent, descent *int,
	grades []int, version int) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE tracks SET ascent_m = $2, descent_m = $3, grades = $4, climb_version = $5 WHERE id = $1`,
		id, ascent, descent, grades, version); err != nil {
		return fmt.Errorf("update track climb: %w", err)
	}
	return nil
}

// SetTrackSpeed - stores the speed an activity's line is timed at.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - itemID: the activity.
//   - speed: the speed in km/h, or nil to take the plan's own.
//
// Returns:
//   - domain.ErrNotFound when the activity has no track.
func (r *DocumentRepository) SetTrackSpeed(ctx context.Context, itemID uuid.UUID, speed *float64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE tracks SET speed_kmh = $2 WHERE item_id = $1`, itemID, speed)
	if err != nil {
		return fmt.Errorf("set track speed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteTrack - removes the line of an activity. Its
// journeys go back to leaving and reaching the place's own position.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - itemID: the place or activity.
//
// Returns:
//   - domain.ErrNotFound when it had no track.
func (r *DocumentRepository) DeleteTrack(ctx context.Context, itemID uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var documentID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT document_id FROM tracks WHERE item_id = $1`, itemID).Scan(&documentID)
		if _, err := one(documentID, err, "find track"); err != nil {
			return err
		}
		document, _, err := lockDocument(ctx, tx, documentID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM tracks WHERE item_id = $1`, itemID)
		if err != nil {
			return fmt.Errorf("delete track: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return syncLegs(ctx, tx, document.TripID)
	})
}

// compress gzips a track file. A recording is text that repeats itself on
// every line, so it shrinks to a fraction of what was uploaded.
func compress(data []byte) ([]byte, error) {
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("compress track: %w", err)
	}
	if _, err := writer.Write(data); err != nil {
		return nil, fmt.Errorf("compress track: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("compress track: %w", err)
	}
	return out.Bytes(), nil
}

// decompress reads back what compress wrote.
func decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompress track: %w", err)
	}
	defer func() { _ = reader.Close() }()
	out, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decompress track: %w", err)
	}
	return out, nil
}
