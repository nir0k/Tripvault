package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// The files attached to the places of a plan or a report. The bytes are kept
// in the same row and read only by AttachmentFile: attachmentColumns leave them
// out, so listing a document's attachments costs no more than listing their
// names. Deleting the place, its day, its document or its trip deletes them by
// the keys of the table.

var attachmentColumns = `a.id, a.document_id, a.item_id, a.original_name, a.description, a.mime, a.size, a.checksum,
	a.uploaded_by, a.created_at`

// scanAttachment reads one row in the order of attachmentColumns.
func scanAttachment(row pgx.Row) (domain.Attachment, error) {
	var a domain.Attachment
	err := row.Scan(&a.ID, &a.DocumentID, &a.ItemID, &a.OriginalName, &a.Description, &a.MIME, &a.Size, &a.Checksum,
		&a.UploadedBy, &a.CreatedAt)
	return a, err
}

// CreateAttachment - stores a file attached to a place.
//
// The trip is locked the way an upload of a picture locks it, so the two kinds
// of upload are counted against the trip's allowance one after the other and
// cannot both fit where only one of them does. The place's own limit and a
// second copy of one file are checked under the same lock.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - attachment: the file's description, with its ID, document, place and
//     uploader set.
//   - file: the bytes.
//   - quota: the most a trip's files may take up in bytes; zero means no limit.
//
// Returns:
//   - domain.ErrNotFound when the place is not a place of the document.
//   - domain.ErrAttachmentLimit when the place carries as many files as it may.
//   - domain.ErrAttachmentDuplicate when the place carries this file already.
//   - domain.ErrMediaQuota when the file does not fit the trip's allowance.
func (r *DocumentRepository) CreateAttachment(ctx context.Context, attachment domain.Attachment, file []byte,
	quota int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var tripID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT t.id FROM trips t
			 JOIN documents d ON d.trip_id = t.id
			 JOIN items i ON i.document_id = d.id
			 WHERE d.id = $1 AND i.id = $2 AND i.kind <> 'stay_anchor'
			 FOR NO KEY UPDATE OF t`, attachment.DocumentID, attachment.ItemID).Scan(&tripID); err != nil {
			_, err = one(tripID, err, "lock trip")
			return err
		}
		var count int
		var duplicate bool
		if err := tx.QueryRow(ctx,
			`SELECT count(*), coalesce(bool_or(checksum = $2), false) FROM item_attachments WHERE item_id = $1`,
			attachment.ItemID, attachment.Checksum).Scan(&count, &duplicate); err != nil {
			return fmt.Errorf("count attachments: %w", err)
		}
		if duplicate {
			return domain.ErrAttachmentDuplicate
		}
		if count >= domain.MaxItemAttachments {
			return domain.ErrAttachmentLimit
		}
		if quota > 0 {
			used, err := tripUsedBytes(ctx, tx, tripID)
			if err != nil {
				return err
			}
			if used+attachment.Size > quota {
				return domain.ErrMediaQuota
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO item_attachments (id, document_id, item_id, original_name, mime, size, checksum, file,
			                               uploaded_by, description)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			attachment.ID, attachment.DocumentID, attachment.ItemID, attachment.OriginalName, attachment.MIME,
			attachment.Size, attachment.Checksum, file, attachment.UploadedBy, attachment.Description); err != nil {
			if isUniqueViolation(err) {
				return domain.ErrAttachmentDuplicate
			}
			return fmt.Errorf("create attachment: %w", err)
		}
		return nil
	})
}

// Attachments - lists the files attached to the places of a document, without
// their bytes, oldest first.
//
// Arguments:
//   - ctx: context bounding the query.
//   - documentID: the plan or report.
//
// Returns:
//   - the attachments.
//   - an error if the query fails.
func (r *DocumentRepository) Attachments(ctx context.Context, documentID uuid.UUID) ([]domain.Attachment, error) {
	return collect(ctx, r.pool, scanAttachment,
		`SELECT `+attachmentColumns+` FROM item_attachments a WHERE a.document_id = $1 ORDER BY a.created_at, a.id`,
		documentID)
}

// Attachment - reads one attachment without its bytes.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the attachment.
//
// Returns:
//   - the attachment, which names the document to check access against.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) Attachment(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	return oneRow(scanAttachment, r.pool.QueryRow(ctx,
		`SELECT `+attachmentColumns+` FROM item_attachments a WHERE a.id = $1`, id), "get attachment")
}

// AttachmentFile - reads the bytes of an attachment with its name and type.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the attachment.
//
// Returns:
//   - the file.
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) AttachmentFile(ctx context.Context, id uuid.UUID) (domain.AttachmentFile, error) {
	var file domain.AttachmentFile
	err := r.pool.QueryRow(ctx, `SELECT original_name, mime, file FROM item_attachments WHERE id = $1`, id).
		Scan(&file.Name, &file.MIME, &file.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, domain.ErrNotFound
	}
	if err != nil {
		return file, fmt.Errorf("get attachment file: %w", err)
	}
	return file, nil
}

// SetAttachmentDescription - changes the line describing an attachment.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the attachment.
//   - description: the validated description, empty for none.
//
// Returns:
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) SetAttachmentDescription(ctx context.Context, id uuid.UUID, description string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE item_attachments SET description = $2 WHERE id = $1`, id, description)
	if err != nil {
		return fmt.Errorf("set attachment description: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteAttachment - removes a file from its place.
//
// Arguments:
//   - ctx: context bounding the statement.
//   - id: the attachment.
//
// Returns:
//   - domain.ErrNotFound when it does not exist.
func (r *DocumentRepository) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM item_attachments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
