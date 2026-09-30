-- +goose Up

-- How PostgreSQL keeps the files held in rows. A value of more than about two
-- kilobytes is compressed with pglz before it is moved out of the row, which
-- is wasted work on bytes that are compressed already: a track's file is
-- gzipped before it is stored, and a map tile is a PNG. Those two are moved
-- out of the row as they are. An attachment may be a PDF, a picture or a plain
-- text timetable, so it is still compressed, but with lz4, which gives up on
-- incompressible bytes far sooner and costs a fraction of the time. Values
-- already stored keep the way they were written; new ones follow these.
ALTER TABLE tracks ALTER COLUMN file_gz SET STORAGE EXTERNAL;
ALTER TABLE tile_cache ALTER COLUMN body SET STORAGE EXTERNAL;
ALTER TABLE item_attachments ALTER COLUMN file SET COMPRESSION lz4;

-- A session remembers the token it exchanged last and when, so a refresh token
-- presented again after its session moved on is recognised as a copy: whoever
-- holds it is not the client that refreshed, and the session is ended rather
-- than left to whichever of the two refreshes next.
ALTER TABLE refresh_tokens ADD COLUMN previous_token_hash bytea;
ALTER TABLE refresh_tokens ADD COLUMN rotated_at timestamptz;

CREATE INDEX refresh_tokens_previous_token_hash_idx ON refresh_tokens (previous_token_hash);

-- +goose Down

DROP INDEX refresh_tokens_previous_token_hash_idx;
ALTER TABLE refresh_tokens DROP COLUMN rotated_at;
ALTER TABLE refresh_tokens DROP COLUMN previous_token_hash;

ALTER TABLE item_attachments ALTER COLUMN file SET COMPRESSION default;
ALTER TABLE tile_cache ALTER COLUMN body SET STORAGE EXTENDED;
ALTER TABLE tracks ALTER COLUMN file_gz SET STORAGE EXTENDED;
