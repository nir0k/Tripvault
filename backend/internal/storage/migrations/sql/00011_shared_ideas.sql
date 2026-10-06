-- +goose Up

-- A person's list of ideas may be shared: its owner lets other accounts read
-- it, or edit it - add, change and delete ideas - as editors. The list is the
-- owner's ideas as a whole, so a member sees every one of them, and an idea an
-- editor adds belongs to the owner's list and goes with the owner's account.
CREATE TABLE idea_members (
    owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, user_id),
    CHECK (owner_id <> user_id)
);

CREATE INDEX idea_members_user_id_idx ON idea_members (user_id);

-- An invitation to somebody's list of ideas is addressed to one email, like a
-- trip's: an existing account with that address accepts it, or it creates one.
-- Only the owner invites, so the owner is the one who sent it.
CREATE TABLE idea_invitations (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email       text NOT NULL,
    role        text NOT NULL CHECK (role IN ('editor', 'viewer')),
    token_hash  bytea NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    accepted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((accepted_at IS NULL) = (accepted_by IS NULL)),
    CHECK (num_nonnulls(accepted_at, revoked_at) <= 1)
);

CREATE UNIQUE INDEX idea_invitations_open_email_key ON idea_invitations (owner_id, lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- Who wrote an idea and who changed it last. A deleted account leaves its
-- ideas in the lists of others, and only the name of who wrote them is lost.
ALTER TABLE ideas ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE ideas ADD COLUMN updated_by uuid REFERENCES users (id) ON DELETE SET NULL;
UPDATE ideas SET created_by = owner_id, updated_by = owner_id;

CREATE INDEX ideas_created_by_idx ON ideas (created_by);
CREATE INDEX ideas_updated_by_idx ON ideas (updated_by);

-- What happened to the ideas of a list, and who did it. The idea's title is
-- kept in the row, so a deleted idea is still named in its list's history.
CREATE TABLE idea_changes (
    id         uuid PRIMARY KEY,
    owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    idea_id    uuid REFERENCES ideas (id) ON DELETE SET NULL,
    idea_title text NOT NULL,
    user_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    action     text NOT NULL
               CHECK (action IN ('created', 'updated', 'deleted', 'photo_added', 'photo_removed')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idea_changes_owner_idx ON idea_changes (owner_id, created_at DESC);
CREATE INDEX idea_changes_idea_idx ON idea_changes (idea_id, created_at DESC);
CREATE INDEX idea_changes_user_idx ON idea_changes (user_id);

-- +goose Down

DROP TABLE idea_changes;
DROP INDEX ideas_updated_by_idx;
DROP INDEX ideas_created_by_idx;
ALTER TABLE ideas DROP COLUMN updated_by;
ALTER TABLE ideas DROP COLUMN created_by;
DROP TABLE idea_invitations;
DROP TABLE idea_members;
