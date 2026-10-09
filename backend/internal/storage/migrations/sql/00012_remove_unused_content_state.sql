-- +goose Up

-- Reports are independent snapshots; only the trip-level source link is used.
ALTER TABLE documents DROP COLUMN source_document_id;
ALTER TABLE days DROP COLUMN source_day_id;
ALTER TABLE stays DROP COLUMN source_stay_id;
ALTER TABLE items DROP COLUMN source_item_id;
ALTER TABLE expenses DROP COLUMN source_expense_id;
ALTER TABLE transfers DROP COLUMN source_transfer_id;

-- A photograph is ready when accepted. Gallery order comes from capture time.
ALTER TABLE media DROP COLUMN status;
ALTER TABLE media_links DROP COLUMN position;

-- Older duplicate-day writes used the plan-only database default in reports.
UPDATE items i SET status = 'visited'
FROM documents d
WHERE i.document_id = d.id AND d.kind = 'report'
  AND i.kind IN ('place', 'activity') AND i.status = 'planned';

-- +goose Down

-- Removed provenance and ordering values cannot be reconstructed. Restore the
-- previous schema with neutral defaults so the preceding build can run.
ALTER TABLE documents ADD COLUMN source_document_id uuid REFERENCES documents (id) ON DELETE SET NULL;
ALTER TABLE days ADD COLUMN source_day_id uuid REFERENCES days (id) ON DELETE SET NULL;
ALTER TABLE stays ADD COLUMN source_stay_id uuid REFERENCES stays (id) ON DELETE SET NULL;
ALTER TABLE items ADD COLUMN source_item_id uuid REFERENCES items (id) ON DELETE SET NULL;
ALTER TABLE expenses ADD COLUMN source_expense_id uuid;
ALTER TABLE transfers ADD COLUMN source_transfer_id uuid REFERENCES transfers (id) ON DELETE SET NULL;
ALTER TABLE media ADD COLUMN status text NOT NULL DEFAULT 'ready' CHECK (status = 'ready');
ALTER TABLE media_links ADD COLUMN position integer NOT NULL DEFAULT 0 CHECK (position >= 0);
