-- +goose Up

-- Instance settings belong to the service rather than to one account. The row
-- is created lazily by the running build so an upgraded installation can seed
-- the trip quota from its existing environment setting once.
CREATE TABLE instance_settings (
    id                     smallint PRIMARY KEY CHECK (id = 1),
    media_trip_quota_bytes bigint NOT NULL CHECK (media_trip_quota_bytes >= 0),
    -- Mail credentials and the public address belong to the deployment, while
    -- the decision to send mail belongs to an administrator and survives
    -- restarts.
    mail_enabled           boolean NOT NULL DEFAULT false,
    -- Self-registration is the administrator's decision too. It is open only
    -- while mail is delivered, because an account it makes is confirmed by email.
    self_registration      boolean NOT NULL DEFAULT false,
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- The source size makes a track contribute predictably to the application
-- quota even though its bytes are gzip-compressed in PostgreSQL. An old row's
-- original size is unavailable without inflating it, so upgrades initialise it
-- conservatively from the bytes already stored.
ALTER TABLE tracks ADD COLUMN file_size bigint NOT NULL DEFAULT 0 CHECK (file_size >= 0);
UPDATE tracks SET file_size = octet_length(file_gz);

-- Ordinary notifications may be silenced by their recipient. Security mail,
-- password recovery and invitations ignore this preference.
ALTER TABLE users ADD COLUMN email_notifications boolean NOT NULL DEFAULT true;

-- An account that registered itself stays inactive until its address is
-- confirmed. This is when it first registered; it is cleared on confirmation,
-- and an account still carrying it five days later is deleted.
ALTER TABLE users ADD COLUMN email_unverified_since timestamptz;

CREATE INDEX users_email_unverified_idx ON users (email_unverified_since)
    WHERE email_unverified_since IS NOT NULL;

-- Messages are committed before they are delivered. A worker claims due rows,
-- retries transient failures and leaves a durable account of the last result.
CREATE TABLE mail_outbox (
    id              uuid PRIMARY KEY,
    recipient       text NOT NULL,
    subject         text NOT NULL,
    text_body       text NOT NULL,
    html_body       text NOT NULL DEFAULT '',
    attempts        integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    claimed_at      timestamptz,
    sent_at         timestamptz,
    failed_at       timestamptz,
    discard_after   timestamptz,
    last_error      text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(sent_at, failed_at) <= 1)
);

CREATE INDEX mail_outbox_due_idx ON mail_outbox (next_attempt_at, created_at)
    WHERE sent_at IS NULL AND failed_at IS NULL;

-- Credential records keep only a digest. The pending outbox message carries
-- the delivery link until it is sent, fails permanently or expires, then the
-- worker redacts its recipient and bodies.
CREATE TABLE password_reset_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id, created_at DESC);

-- The pending confirmation of a self-registered account: one at a time, so a
-- new message replaces the link and code of the one before. Both are kept as
-- digests; the code is short, so its wrong guesses are counted.
CREATE TABLE email_verifications (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    code_hash  bytea NOT NULL,
    attempts   integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    expires_at timestamptz NOT NULL,
    sent_at    timestamptz NOT NULL
);

-- An administrator may invite an account without creating a password they
-- know. Accepting the invitation creates the account on this closed instance.
CREATE TABLE user_invitations (
    id           uuid PRIMARY KEY,
    email        text NOT NULL,
    display_name text NOT NULL,
    is_admin     boolean NOT NULL DEFAULT false,
    token_hash   bytea NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    revoked_at   timestamptz,
    created_by   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(accepted_at, revoked_at) <= 1)
);

CREATE UNIQUE INDEX user_invitations_open_email_key ON user_invitations (lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- A trip invitation is addressed to one email. It may be accepted by an
-- existing account with that address or create one before adding membership.
CREATE TABLE trip_invitations (
    id          uuid PRIMARY KEY,
    trip_id     uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    email       text NOT NULL,
    role        text NOT NULL CHECK (role IN ('editor', 'viewer')),
    token_hash  bytea NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    accepted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz,
    created_by  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((accepted_at IS NULL) = (accepted_by IS NULL)),
    CHECK (num_nonnulls(accepted_at, revoked_at) <= 1)
);

CREATE UNIQUE INDEX trip_invitations_open_email_key ON trip_invitations (trip_id, lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- +goose Down

DROP TABLE trip_invitations;
DROP TABLE user_invitations;
DROP TABLE email_verifications;
DROP TABLE password_reset_tokens;
DROP TABLE mail_outbox;
DROP INDEX users_email_unverified_idx;
ALTER TABLE users DROP COLUMN email_unverified_since;
ALTER TABLE users DROP COLUMN email_notifications;
ALTER TABLE tracks DROP COLUMN file_size;
DROP TABLE instance_settings;
