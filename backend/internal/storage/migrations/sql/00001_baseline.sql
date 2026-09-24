-- +goose Up

-- Accounts. Nobody signs up: an administrator creates every account, and the
-- first administrator comes from the environment on the first start.
CREATE TABLE users (
    id                   uuid PRIMARY KEY,
    email                text NOT NULL,
    display_name         text NOT NULL,
    password_hash        text NOT NULL,
    is_admin             boolean NOT NULL DEFAULT false,
    is_active            boolean NOT NULL DEFAULT true,
    -- Set whenever an administrator chose the password, cleared when the owner
    -- picks their own.
    must_change_password boolean NOT NULL DEFAULT false,
    -- NULL follows the browser. Not constrained to a list: adding a language
    -- must not need a schema change.
    locale               text,
    theme                text NOT NULL DEFAULT 'auto'
                         CHECK (theme IN ('light', 'dark', 'auto')),
    -- How far a distance is shown in. Distances are stored in metres whatever
    -- this says; two people on one trip may read it in different units.
    units                text NOT NULL DEFAULT 'km'
                         CHECK (units IN ('km', 'mi')),
    default_currency     text NOT NULL DEFAULT 'EUR'
                         CHECK (default_currency ~ '^[A-Z]{3}$'),
    -- How a date and a clock are written for this reader. Like the units, these
    -- change nothing in the database: two people on one trip may read the same
    -- day as 21/3 and 3/21.
    date_format          text NOT NULL DEFAULT 'dmy'
                         CHECK (date_format IN ('dmy', 'mdy')),
    time_format          text NOT NULL DEFAULT 'h24'
                         CHECK (time_format IN ('h24', 'h12')),
    -- The picture the account is shown by, in the media store, and when it was
    -- last changed - which is what makes a browser fetch the new one.
    avatar_key           text NOT NULL DEFAULT '',
    avatar_updated_at    timestamptz,
    last_login_at        timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- Addresses are stored lowercased; the index on lower() keeps a differently
-- cased duplicate out even if something writes one directly.
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- One row per signed-in device. A refresh replaces token_hash in place, so the
-- row - and its id, which the session list shows - lasts as long as the session.
CREATE TABLE refresh_tokens (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL,
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);

CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

-- A trip holds one document: a plan of a journey still to come, or a report of
-- one made. A report is a record of its own rather than a part of the plan: it
-- has its own people, links, files and budget, and outlives the plan it may
-- have been copied from. The owner lives only here; trip_members holds editors
-- and viewers.
CREATE TABLE trips (
    id            uuid PRIMARY KEY,
    owner_id      uuid NOT NULL REFERENCES users (id),
    kind          text NOT NULL CHECK (kind IN ('plan', 'report')),
    -- The plan a report was copied from, kept only to point back at it. The
    -- copy needs nothing from it, so deleting the plan merely forgets the link.
    source_trip_id uuid REFERENCES trips (id) ON DELETE SET NULL,
    title         text NOT NULL,
    summary       text NOT NULL DEFAULT '',
    -- A trip is planned over a period: the days of a plan take their dates from
    -- it, so both ends are required.
    start_date    date NOT NULL,
    end_date      date NOT NULL,
    -- IANA name; the trip's "today" (and so its status) is read in this zone.
    timezone      text NOT NULL DEFAULT 'UTC',
    currency      text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    travelers     integer NOT NULL DEFAULT 1 CHECK (travelers BETWEEN 1 AND 100),
    budget_amount numeric(14, 2) CHECK (budget_amount >= 0),
    -- The languages a report is written in: the first is the one its own
    -- columns hold, the rest are the ones it is translated into. A plan is
    -- written in one language and names none.
    languages     text[] NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date),
    CHECK ((kind = 'report') = (cardinality(languages) > 0))
);

CREATE INDEX trips_owner_id_idx ON trips (owner_id);
CREATE INDEX trips_source_trip_id_idx ON trips (source_trip_id);

CREATE TABLE trip_members (
    trip_id    uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (trip_id, user_id)
);

CREATE INDEX trip_members_user_id_idx ON trip_members (user_id);

-- Read-only links to a trip, handed out by its owner. Only the SHA-256 of the
-- token is stored: a lost link cannot be recovered, only replaced. Revoking one
-- takes effect at once, since every request looks the row up again.
CREATE TABLE share_links (
    id                    uuid PRIMARY KEY,
    trip_id               uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    label                 text NOT NULL DEFAULT '',
    token_hash            bytea NOT NULL UNIQUE,
    include_private_media boolean NOT NULL DEFAULT false,
    expires_at            timestamptz,
    revoked_at            timestamptz,
    created_by            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    last_used_at          timestamptz,
    use_count             bigint NOT NULL DEFAULT 0,
    created_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX share_links_trip_id_idx ON share_links (trip_id);

-- The files of a trip: the pictures a plan is prepared with, or the
-- photographs a report was taken with - each trip has a gallery of its own. The bytes live in the media store on disk; this table is their
-- catalogue, and nothing is served without a row here.
CREATE TABLE media (
    id            uuid PRIMARY KEY,
    trip_id       uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    -- Path inside the media store, assigned on upload and never reused.
    storage_key   text NOT NULL UNIQUE,
    original_name text NOT NULL DEFAULT '',
    mime          text NOT NULL,
    size          bigint NOT NULL CHECK (size > 0),
    -- SHA-256 of the bytes, so the same photograph uploaded twice can be told
    -- apart from two that only look alike.
    checksum      bytea NOT NULL,
    width         integer CHECK (width > 0),
    height        integer CHECK (height > 0),
    -- When and where the camera took it, read from EXIF at upload.
    taken_at      timestamptz,
    lat           double precision CHECK (lat BETWEEN -90 AND 90),
    lng           double precision CHECK (lng BETWEEN -180 AND 180),
    -- A private file stays out of a read-only link unless that link says
    -- otherwise.
    is_private    boolean NOT NULL DEFAULT false,
    -- Everything the current scope accepts is ready the moment it is stored;
    -- the other two states belong to the formats a later stage converts.
    status        text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'processing', 'failed')),
    -- Who uploaded the file. It is forgotten when that account is deleted: the
    -- file belongs to the trip, not to the person who happened to add it, and a
    -- guest's photographs must not disappear from somebody else's trip.
    uploaded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lng IS NULL))
);

CREATE INDEX media_trip_id_idx ON media (trip_id, created_at DESC);

-- The picture a trip is shown by. It is added here rather than with the table
-- itself, because a trip exists before any of its files do.
--
-- This is the one key that points back the way it came - a trip names a file,
-- and every file names a trip - so the two tables refer to each other in a
-- circle and no loading order satisfies both. It is therefore deferrable: it
-- behaves exactly as any other key would, and a restore, which loads the whole
-- database in one transaction, defers it for the length of that transaction.
ALTER TABLE trips ADD COLUMN cover_media_id uuid REFERENCES media (id) ON DELETE SET NULL
    DEFERRABLE INITIALLY IMMEDIATE;

-- The document of a trip, of the trip's own kind: exactly one per trip.
CREATE TABLE documents (
    id                 uuid PRIMARY KEY,
    trip_id            uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    kind               text NOT NULL CHECK (kind IN ('plan', 'report')),
    -- The plan a report was copied from; the plan may be deleted later.
    source_document_id uuid REFERENCES documents (id) ON DELETE SET NULL,
    intro_md           text NOT NULL DEFAULT '',
    summary_md         text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (trip_id)
);

-- A day of a document. On a trip with dates, a day's date is the trip start
-- plus its position; on a trip without dates it has none.
CREATE TABLE days (
    id             uuid PRIMARY KEY,
    document_id    uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position       integer NOT NULL CHECK (position >= 0),
    date           date,
    title          text NOT NULL DEFAULT '',
    notes_md       text NOT NULL DEFAULT '',
    start_time     time NOT NULL DEFAULT '09:00',
    -- NULL inherits from the trip.
    default_mode   text CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'other')),
    timezone       text,
    -- The stay marks are placed automatically; each can be switched off, for
    -- example when the day starts at an airport.
    morning_anchor boolean NOT NULL DEFAULT true,
    evening_anchor boolean NOT NULL DEFAULT true,
    -- The night after this day is deliberately spent without a stay.
    no_overnight   boolean NOT NULL DEFAULT false,
    cover_media_id uuid REFERENCES media (id) ON DELETE SET NULL,
    source_day_id  uuid REFERENCES days (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    -- Deferred so a reorder can renumber the days in one statement.
    UNIQUE (document_id, position) DEFERRABLE INITIALLY DEFERRED
);

-- A place to sleep. It belongs to the whole document and covers every night
-- from the check-in date up to the night before the check-out date.
CREATE TABLE stays (
    id                  uuid PRIMARY KEY,
    document_id         uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    name                text NOT NULL,
    kind                text NOT NULL DEFAULT 'hotel'
                        CHECK (kind IN ('hotel', 'apartment', 'hostel', 'camping', 'friends', 'other')),
    address             text NOT NULL DEFAULT '',
    lat                 double precision CHECK (lat BETWEEN -90 AND 90),
    lng                 double precision CHECK (lng BETWEEN -180 AND 180),
    -- Local dates and times at the stay, with no zone attached.
    check_in_date       date NOT NULL,
    check_in_time       time,
    check_out_date      date NOT NULL,
    check_out_time      time,
    booking_ref         text NOT NULL DEFAULT '',
    url                 text NOT NULL DEFAULT '',
    contacts            text NOT NULL DEFAULT '',
    notes_md            text NOT NULL DEFAULT '',
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    -- Only a report fills the actual amount; in a plan it stays empty.
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    source_stay_id      uuid REFERENCES stays (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lng IS NULL)),
    CHECK (check_out_date > check_in_date)
);

CREATE INDEX stays_document_id_idx ON stays (document_id);

-- An element of a document: a place, or the morning or evening mark of a stay
-- inside a day. A place without a day is "unassigned" and exists only in a
-- plan. Marks carry no data of their own: they point at their stay.
CREATE TABLE items (
    id                  uuid PRIMARY KEY,
    document_id         uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    day_id              uuid REFERENCES days (id) ON DELETE CASCADE,
    -- Orders places inside a day or inside the unassigned list; marks sit
    -- before and after the places whatever their position.
    position            integer NOT NULL DEFAULT 0,
    -- A place or an activity is where the trip goes; a stay mark follows the
    -- stays. An activity names what kind it is.
    kind                text NOT NULL CHECK (kind IN ('place', 'activity', 'stay_anchor')),
    anchor              text CHECK (anchor IN ('morning', 'evening')),
    stay_id             uuid REFERENCES stays (id) ON DELETE CASCADE,
    name                text NOT NULL DEFAULT '',
    category            text NOT NULL DEFAULT 'other'
                        CHECK (category IN ('sight', 'nature', 'museum', 'food', 'shopping', 'activity',
                                            'transport', 'other')),
    activity_type       text CHECK (activity_type IN ('hike', 'walk', 'bike', 'run', 'canyoning', 'climbing',
                                                      'via_ferrata', 'kayak', 'swim', 'ski', 'tour', 'other')),
    lat                 double precision CHECK (lat BETWEEN -90 AND 90),
    lng                 double precision CHECK (lng BETWEEN -180 AND 180),
    address             text NOT NULL DEFAULT '',
    osm_ref             text,
    description_md      text NOT NULL DEFAULT '',
    url                 text NOT NULL DEFAULT '',
    desired_time        time,
    visit_minutes       integer NOT NULL DEFAULT 0 CHECK (visit_minutes BETWEEN 0 AND 1440),
    is_optional         boolean NOT NULL DEFAULT false,
    booking_ref         text NOT NULL DEFAULT '',
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    cost_per_person     boolean NOT NULL DEFAULT false,
    cost_category       text NOT NULL DEFAULT 'other'
                        CHECK (cost_category IN ('accommodation', 'transport', 'food', 'activities',
                                                 'shopping', 'other')),
    -- Report only. A plan keeps status 'planned' and leaves the rest empty; a
    -- place of a report is 'visited' until marked 'skipped', and 'unplanned'
    -- marks one added straight into the report.
    status              text NOT NULL DEFAULT 'planned'
                        CHECK (status IN ('planned', 'visited', 'skipped', 'unplanned')),
    story_md            text NOT NULL DEFAULT '',
    actual_time         time,
    -- When the place was left or the activity finished; it may be earlier
    -- than actual_time for an activity that ran past midnight.
    actual_end_time     time,
    rating              smallint CHECK (rating BETWEEN 1 AND 5),
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    cover_media_id      uuid REFERENCES media (id) ON DELETE SET NULL,
    source_item_id      uuid REFERENCES items (id) ON DELETE SET NULL,
    -- How hard an activity is, from 1 (very easy) to 5 (extreme). A place
    -- has none.
    difficulty          smallint CHECK (difficulty BETWEEN 1 AND 5),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL) = (lng IS NULL)),
    CHECK ((kind <> 'stay_anchor') = (anchor IS NULL AND stay_id IS NULL)),
    CHECK ((kind = 'activity') = (activity_type IS NOT NULL)),
    CHECK (kind <> 'stay_anchor' OR day_id IS NOT NULL),
    CHECK (kind = 'activity' OR difficulty IS NULL)
);

CREATE INDEX items_document_id_idx ON items (document_id);
CREATE INDEX items_day_id_idx ON items (day_id);
CREATE UNIQUE INDEX items_day_anchor_key ON items (day_id, anchor) WHERE anchor IS NOT NULL;

-- The journey between two neighbouring elements of a day. Legs carry no order
-- of their own: they follow the elements, and the service replaces them when
-- the elements stop being neighbours.
CREATE TABLE legs (
    id                  uuid PRIMARY KEY,
    document_id         uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    day_id              uuid NOT NULL REFERENCES days (id) ON DELETE CASCADE,
    from_item_id        uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    to_item_id          uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    mode                text NOT NULL CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'other')),
    -- The calculated values. calc_input records the mode and coordinates they
    -- were calculated for; a leg whose input changed goes back to pending.
    distance_m          integer CHECK (distance_m >= 0),
    duration_s          integer CHECK (duration_s >= 0),
    geometry            text,
    calc_source         text NOT NULL DEFAULT 'pending'
                        CHECK (calc_source IN ('pending', 'provider', 'straight_line', 'estimate',
                                               'missing_coordinates')),
    calc_error          text,
    calc_input          text NOT NULL DEFAULT '',
    calculated_at       timestamptz,
    -- Values a person typed in; they win over the calculated ones and survive
    -- recalculation until cleared.
    manual_distance_m   integer CHECK (manual_distance_m >= 0),
    manual_duration_s   integer CHECK (manual_duration_s >= 0),
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    -- Only a report fills the actual amount; in a plan it stays empty.
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    note                text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (from_item_id, to_item_id)
);

CREATE INDEX legs_document_id_idx ON legs (document_id);
CREATE INDEX legs_day_id_idx ON legs (day_id);

-- The line a place or an activity was really walked or driven - a hike, a walk
-- around a lake - imported from a GPX or KML file. Each carries at most one
-- track; a new import replaces the old one. A day has none of its own: its
-- journeys are the legs, and a recording of a part of it belongs to that part.
--
-- The line is stored in the same encoded polyline format legs use, already
-- thinned to what a map can draw, while the length and the climb are measured
-- over every original point. The uploaded file is kept too, compressed, so it
-- can be downloaded as it was recorded: it lives in this row rather than in the
-- media store, so a deleted place, day or trip takes it along and a backup
-- carries it without knowing about it. The document query never reads it.
CREATE TABLE tracks (
    id            uuid PRIMARY KEY,
    document_id   uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    item_id       uuid NOT NULL UNIQUE REFERENCES items (id) ON DELETE CASCADE,
    original_name text NOT NULL DEFAULT '',
    format        text NOT NULL CHECK (format IN ('gpx', 'kml')),
    geometry      text NOT NULL,
    distance_m    integer NOT NULL CHECK (distance_m >= 0),
    -- How many points the file held, before the line was thinned.
    point_count   integer NOT NULL CHECK (point_count >= 0),
    -- Height gained and lost; null when the file records no heights.
    ascent_m      integer CHECK (ascent_m >= 0),
    descent_m     integer CHECK (descent_m >= 0),
    -- The first and last moment the file records; null when its points carry
    -- no time.
    started_at    timestamptz,
    ended_at      timestamptz,
    -- The uploaded file, gzip-compressed.
    file_gz       bytea NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tracks_document_id_idx ON tracks (document_id);

-- Where a file is shown. One file may hang in several places at once, and the
-- position orders a gallery.
--
-- The specification sketches a single polymorphic target; three nullable
-- columns are used instead, so the database removes the links of a deleted day
-- or place by itself instead of leaving rows pointing at nothing. The API keeps
-- the sketch's shape, {target_type, target_id}.
CREATE TABLE media_links (
    media_id uuid NOT NULL REFERENCES media (id) ON DELETE CASCADE,
    trip_id  uuid REFERENCES trips (id) ON DELETE CASCADE,
    day_id   uuid REFERENCES days (id) ON DELETE CASCADE,
    item_id  uuid REFERENCES items (id) ON DELETE CASCADE,
    position integer NOT NULL DEFAULT 0 CHECK (position >= 0),
    -- Whether this is one of the few pictures the report shows for the day or
    -- the place; the rest of its gallery is found in the trip's own gallery. A
    -- plan has no favourites: it is read before the pictures exist.
    is_favorite boolean NOT NULL DEFAULT false,
    CHECK (num_nonnulls(trip_id, day_id, item_id) = 1)
);

CREATE UNIQUE INDEX media_links_trip_key ON media_links (trip_id, media_id) WHERE trip_id IS NOT NULL;
CREATE UNIQUE INDEX media_links_day_key ON media_links (day_id, media_id) WHERE day_id IS NOT NULL;
CREATE UNIQUE INDEX media_links_item_key ON media_links (item_id, media_id) WHERE item_id IS NOT NULL;
CREATE INDEX media_links_media_id_idx ON media_links (media_id);

-- Costs that belong to no place, stay or leg: a city tax, a souvenir, a tank of
-- fuel. An expense with no day_id belongs to the trip as a whole rather than to
-- one of its days.
CREATE TABLE expenses (
    id                uuid PRIMARY KEY,
    document_id       uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    day_id            uuid REFERENCES days (id) ON DELETE SET NULL,
    category          text NOT NULL DEFAULT 'other'
                      CHECK (category IN ('accommodation', 'transport', 'food', 'activities',
                                          'shopping', 'other')),
    planned_amount    numeric(14, 2) CHECK (planned_amount >= 0),
    actual_amount     numeric(14, 2) CHECK (actual_amount >= 0),
    spent_on          date,
    note              text NOT NULL DEFAULT '',
    source_expense_id uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX expenses_document_id_idx ON expenses (document_id);
CREATE INDEX expenses_day_id_idx ON expenses (day_id);

-- The words of a report in another of its languages. The report's own columns
-- hold the original; a row here holds one field of one element in one further
-- language, and a field without a row is read in the original.
--
-- trip_id names the report the row belongs to, whatever it translates; the
-- other keys name the element, and a row naming none of them translates the
-- trip itself. They are separate columns, as in media_links, so a deleted day
-- or place takes its translations with it.
CREATE TABLE translations (
    trip_id     uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    document_id uuid REFERENCES documents (id) ON DELETE CASCADE,
    day_id      uuid REFERENCES days (id) ON DELETE CASCADE,
    stay_id     uuid REFERENCES stays (id) ON DELETE CASCADE,
    item_id     uuid REFERENCES items (id) ON DELETE CASCADE,
    leg_id      uuid REFERENCES legs (id) ON DELETE CASCADE,
    field       text NOT NULL,
    lang        text NOT NULL,
    value       text NOT NULL CHECK (value <> ''),
    CHECK (num_nonnulls(document_id, day_id, stay_id, item_id, leg_id) <= 1)
);

CREATE UNIQUE INDEX translations_target_key
    ON translations ((COALESCE(document_id, day_id, stay_id, item_id, leg_id, trip_id)), field, lang);
CREATE INDEX translations_trip_id_idx ON translations (trip_id);

-- Routes the provider returned, shared by every trip. The key is a hash of the
-- provider, the profile and both points rounded to five decimals.
CREATE TABLE route_cache (
    key        bytea PRIMARY KEY,
    provider   text NOT NULL,
    profile    text NOT NULL,
    distance_m integer NOT NULL,
    duration_s integer NOT NULL,
    geometry   text NOT NULL,
    hits       bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX route_cache_expires_at_idx ON route_cache (expires_at);

-- Search results the geocoder returned, shared by every user. The key is a
-- hash of the kind of lookup, the text or point, the language and the area.
CREATE TABLE geocode_cache (
    key        bytea PRIMARY KEY,
    provider   text NOT NULL,
    payload    jsonb NOT NULL,
    hits       bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX geocode_cache_expires_at_idx ON geocode_cache (expires_at);

-- Map tiles fetched to draw the maps of a report's PDF, shared by every trip.
-- The key is a hash of the tile's address, so a different tile server never
-- answers from another's tiles. It is a cache, not a record: backups leave it
-- out, and a restored instance fetches its tiles again.
CREATE TABLE tile_cache (
    key        bytea PRIMARY KEY,
    body       bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX tile_cache_expires_at_idx ON tile_cache (expires_at);

-- Requests to external providers per service and hour, for the daily limits
-- and the status screen.
CREATE TABLE provider_usage (
    service         text NOT NULL CHECK (service IN ('directions', 'geocode')),
    hour            timestamptz NOT NULL,
    requests        integer NOT NULL DEFAULT 0,
    errors          integer NOT NULL DEFAULT 0,
    last_success_at timestamptz,
    last_error_at   timestamptz,
    PRIMARY KEY (service, hour)
);

-- One standing instruction to copy the whole service somewhere. It belongs to
-- the instance rather than to a trip: where the copies go, how often and how
-- long they are kept are decisions about the server, and the archive a run
-- produces holds every trip on it.
CREATE TABLE backup_configs (
    id uuid PRIMARY KEY,

    -- Where the archive goes. The local disk, or a machine over SFTP; plain FTP
    -- is absent by decision rather than omission, because it sends the password
    -- in the clear.
    destination_type text NOT NULL DEFAULT 'local',
    -- Whatever that destination needs: nothing for the local disk, a host, a
    -- directory and a pinned host key for SFTP. Never a credential.
    destination_params jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The credentials themselves, each sealed on its own with the instance's
    -- key: an SFTP password or private key, and the passphrase the archives are
    -- locked with. The names are readable so a listing can say which are set;
    -- the values open only with that key, so a dump of this table gives nothing
    -- away. A configuration can never name an environment variable to read a
    -- credential from: that would let whoever can edit one aim it at a machine
    -- of their choosing and hand it any variable the service holds.
    sealed_secrets jsonb NOT NULL DEFAULT '{}'::jsonb,

    encrypted boolean NOT NULL DEFAULT false,

    -- A five-field cron expression, or a descriptor such as @daily. Empty means
    -- the configuration runs only when somebody asks for it.
    schedule_cron text NOT NULL DEFAULT '',
    -- How many of the newest archives to keep, and how many days one is kept.
    -- At most one of them is set; both zero keeps every archive. Rotation runs
    -- only after a success, and only over this configuration's own history.
    retain_count integer NOT NULL DEFAULT 7,
    retain_days  integer NOT NULL DEFAULT 0,
    enabled      boolean NOT NULL DEFAULT true,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- Withdrawing an instruction is not a request to destroy the copies it
    -- already made, so a deleted configuration is still readable: its archives
    -- are read back through it.
    deleted_at timestamptz,

    CONSTRAINT backup_configs_destination_check CHECK (
        destination_type IN ('local', 'sftp')),
    CONSTRAINT backup_configs_retain_check CHECK (
        retain_count BETWEEN 0 AND 365 AND retain_days BETWEEN 0 AND 3650
        AND NOT (retain_count > 0 AND retain_days > 0))
);

-- One attempt to carry out a configuration.
CREATE TABLE backup_runs (
    id               uuid PRIMARY KEY,
    backup_config_id uuid NOT NULL REFERENCES backup_configs (id) ON DELETE CASCADE,

    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    -- running is written before the work starts, so a run interrupted by a
    -- restart is visible as one that never finished rather than absent.
    status text NOT NULL DEFAULT 'running',

    size_bytes bigint,
    -- What the run wrote, so a listing can offer it back.
    artifact text NOT NULL DEFAULT '',
    -- When retention removed that archive from its destination. The name is kept
    -- rather than cleared: the history is a record of what was copied and when,
    -- and losing the name would make an old entry say nothing at all.
    rotated_at    timestamptz,
    error_message text NOT NULL DEFAULT '',

    CONSTRAINT backup_runs_status_check CHECK (status IN ('running', 'success', 'failed'))
);

CREATE INDEX backup_runs_config_idx ON backup_runs (backup_config_id, started_at DESC);
-- Retention asks one question: which archives has this configuration produced
-- that are still held.
CREATE INDEX backup_runs_rotatable_idx ON backup_runs (backup_config_id, started_at DESC)
    WHERE status = 'success' AND artifact <> '' AND rotated_at IS NULL;

-- +goose Down

DROP TABLE backup_runs;
DROP TABLE backup_configs;
DROP TABLE media_links;
DROP TABLE tracks;
DROP TABLE provider_usage;
DROP TABLE geocode_cache;
DROP TABLE route_cache;
DROP TABLE expenses;
DROP TABLE legs;
DROP TABLE items;
DROP TABLE stays;
DROP TABLE days;
DROP TABLE documents;
DROP TABLE share_links;
DROP TABLE trip_members;
-- CASCADE because trips and media point at each other: a trip names its cover
-- picture and every picture names its trip, so whichever is dropped first is
-- still referenced by the other. It drops that one key, not the other table.
DROP TABLE trips CASCADE;
DROP TABLE media;
DROP TABLE refresh_tokens;
DROP TABLE users;
