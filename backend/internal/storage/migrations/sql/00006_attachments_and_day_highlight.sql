-- +goose Up

-- The files attached to a place or an activity: a ticket, a booking, a
-- timetable. They are documents and pictures only, recognised from their own
-- bytes, and are kept in the row like the file of a track rather than in the
-- media store: they are not pictures of the trip, must never reach a gallery or
-- a cover, and in the row they leave with the place, the day, the document or
-- the trip that owned them and travel in every backup without the archive
-- knowing about them. Only the download reads the file; the document query
-- never does. A read-only link never sees them, and a report copied from a plan
-- does not take them along.
CREATE TABLE item_attachments (
    id            uuid PRIMARY KEY,
    document_id   uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    item_id       uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    -- The name the file was uploaded under, reduced to its last part.
    original_name text NOT NULL CHECK (char_length(original_name) BETWEEN 1 AND 255),
    -- A line on what the file is, such as "Return tickets, both of us".
    description   text NOT NULL DEFAULT '' CHECK (char_length(description) <= 150),
    -- What the bytes were recognised as, which is what the download declares.
    mime          text NOT NULL,
    size          bigint NOT NULL CHECK (size > 0),
    -- SHA-256 of the bytes, so the same file is not attached to a place twice.
    checksum      bytea NOT NULL,
    file          bytea NOT NULL,
    -- Who attached it; forgotten when that account is deleted, like the
    -- uploader of a photograph.
    uploaded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (item_id, checksum)
);

CREATE INDEX item_attachments_document_id_idx ON item_attachments (document_id);

-- The moment a day of a report is remembered by, in a line: "Watching whales
-- breach several times out at sea." The report's PDF sets it apart on the
-- day's opening page. Only a report has one; a plan's days leave it empty, and
-- copying a plan into a report has nothing to copy.
ALTER TABLE days
    ADD COLUMN highlight text NOT NULL DEFAULT '' CHECK (char_length(highlight) <= 200);

-- +goose Down

ALTER TABLE days DROP COLUMN highlight;
DROP TABLE item_attachments;
