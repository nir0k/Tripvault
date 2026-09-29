-- +goose Up

-- A plan is closed by hand: completed once it was travelled, cancelled when it
-- will not be. An open plan keeps NULL and reads its status from its dates. A
-- report is never closed, since writing one already says the trip was made.
ALTER TABLE trips
    ADD COLUMN state text CHECK (state IN ('completed', 'cancelled')),
    ADD CONSTRAINT trips_state_plan_check CHECK (state IS NULL OR kind = 'plan');

-- A car park is a place of its own: where the car is left before a walk or a
-- sight, with a cost that goes to transport.
ALTER TABLE items DROP CONSTRAINT items_category_check;
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('sight', 'nature', 'museum', 'food', 'shopping', 'activity',
                        'transport', 'parking', 'other'));

-- A person's own tags. They are a word list of the person, not of a trip: two
-- members of one trip tag it each in their own way and neither sees the other's.
-- The names are unique for their owner whatever the case. The colour is a key
-- of the interface's palette rather than a value, so a tag reads alike in the
-- light and the dark theme.
CREATE TABLE tags (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    color      text NOT NULL CHECK (color IN ('red', 'orange', 'amber', 'green', 'teal', 'sky', 'blue',
                                              'violet', 'pink', 'gray', 'yellow', 'emerald',
                                              'indigo', 'fuchsia', 'lime', 'cyan', 'purple',
                                              'rose', 'brown')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX tags_user_name_key ON tags (user_id, lower(name));

-- Which trips a tag is put on. The owner is the tag's; a trip the person has
-- lost access to keeps the row, but it is never read.
CREATE TABLE trip_tags (
    tag_id  uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    trip_id uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    PRIMARY KEY (tag_id, trip_id)
);
CREATE INDEX trip_tags_trip_idx ON trip_tags (trip_id);

-- Buses, trains, metros and trams, and ferries are ways of travelling of their
-- own, beside public transport in general. A bus follows roads; the others go
-- where no routing service in use knows the way, and are drawn straight.
ALTER TABLE days DROP CONSTRAINT days_default_mode_check;
ALTER TABLE days ADD CONSTRAINT days_default_mode_check
    CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'bus', 'train', 'tram', 'ferry', 'flight',
                            'cable_car', 'other'));
ALTER TABLE legs DROP CONSTRAINT legs_mode_check;
ALTER TABLE legs ADD CONSTRAINT legs_mode_check
    CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'bus', 'train', 'tram', 'ferry', 'flight', 'cable_car',
                    'other'));

-- A journey with changes on the way - a walk to the station, a train, a bus -
-- is a leg made of segments. A leg without changes has none and keeps its own
-- row as it always did; a leg gets segments once it has a change.
--
-- What was paid is kept by ticket, not by segment: one ticket may cover one
-- part, several, or the whole journey, and the parts say which.
CREATE TABLE leg_tickets (
    id                  uuid PRIMARY KEY,
    leg_id              uuid NOT NULL REFERENCES legs (id) ON DELETE CASCADE,
    position            integer NOT NULL CHECK (position >= 0),
    name                text NOT NULL DEFAULT '',
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    -- Only a report fills the actual amount; in a plan it stays empty.
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    UNIQUE (leg_id, position)
);

-- Each segment is calculated like a leg of its own, between where the one
-- before it ended and its stop - the change where the next begins. The last
-- segment ends at the leg's end and has no stop.
CREATE TABLE leg_segments (
    id                uuid PRIMARY KEY,
    leg_id            uuid NOT NULL REFERENCES legs (id) ON DELETE CASCADE,
    position          integer NOT NULL CHECK (position >= 0),
    mode              text NOT NULL CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'bus', 'train', 'tram',
                                                    'ferry', 'flight', 'cable_car', 'other')),
    distance_m        integer CHECK (distance_m >= 0),
    duration_s        integer CHECK (duration_s >= 0),
    geometry          text,
    calc_source       text NOT NULL DEFAULT 'pending'
                      CHECK (calc_source IN ('pending', 'provider', 'straight_line', 'estimate',
                                             'missing_coordinates')),
    calc_error        text,
    calc_input        text NOT NULL DEFAULT '',
    calculated_at     timestamptz,
    manual_distance_m integer CHECK (manual_distance_m >= 0),
    manual_duration_s integer CHECK (manual_duration_s >= 0),
    ticket_id         uuid REFERENCES leg_tickets (id) ON DELETE SET NULL,
    stop_name         text NOT NULL DEFAULT '',
    stop_lat          double precision CHECK (stop_lat BETWEEN -90 AND 90),
    stop_lng          double precision CHECK (stop_lng BETWEEN -180 AND 180),
    wait_minutes      integer NOT NULL DEFAULT 0 CHECK (wait_minutes BETWEEN 0 AND 1440),
    UNIQUE (leg_id, position)
);
CREATE INDEX leg_segments_ticket_idx ON leg_segments (ticket_id);

-- The way a track's climb was measured. The tracks imported so far were
-- measured the first way; the service measures them again from their files
-- when it starts, and marks them with the version it used.
ALTER TABLE tracks ADD COLUMN climb_version integer NOT NULL DEFAULT 1;

-- How long a plan's line takes to walk is worked out by its reader from the
-- metres it runs at each slope, measured with the climb, and a speed on the
-- flat: the line's own when it names one, the plan's otherwise.
ALTER TABLE tracks
    ADD COLUMN grades integer[] CHECK (cardinality(grades) = 101),
    ADD COLUMN speed_kmh double precision CHECK (speed_kmh BETWEEN 1 AND 12);
ALTER TABLE trips
    ADD COLUMN track_speed_kmh double precision NOT NULL DEFAULT 4.7 CHECK (track_speed_kmh BETWEEN 1 AND 12);

-- Whether a read-only link lets its reader download the pictures, one by one
-- or as an archive. Off unless its owner allowed it: a link made to show a
-- report does not hand out the files by that alone.
ALTER TABLE share_links ADD COLUMN allow_download boolean NOT NULL DEFAULT false;

-- A report copied from a plan keeps knowing it after the plan is deleted,
-- which empties source_trip_id: a place added to it is marked as not planned
-- only then, since a report written from scratch had no plan to depart from.
ALTER TABLE trips ADD COLUMN from_plan boolean NOT NULL DEFAULT false;
UPDATE trips SET from_plan = true WHERE source_trip_id IS NOT NULL;

-- What to take on a trip, kept on the plan's trip. A category is the trip's
-- own word - no fixed list - and an item without one is listed apart. One tick
-- serves the whole group: the list is packed together. Who brings an item is a
-- member of the trip; the column is emptied when they leave it. A category's
-- colour is a key of the tags' palette and its icon a key of a fixed set, so
-- both read alike in the interface, in either theme, and in the PDF.
CREATE TABLE packing_categories (
    id         uuid PRIMARY KEY,
    trip_id    uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    color      text NOT NULL CHECK (color IN ('red', 'orange', 'amber', 'green', 'teal', 'sky', 'blue',
                                              'violet', 'pink', 'gray', 'yellow', 'emerald',
                                              'indigo', 'fuchsia', 'lime', 'cyan', 'purple',
                                              'rose', 'brown')),
    icon       text NOT NULL CHECK (icon IN ('documents', 'tickets', 'cards', 'money', 'coins', 'keys', 'work',
                                             'clothes', 'bags', 'luggage', 'glasses', 'rain', 'first_aid',
                                             'medicine', 'thermometer', 'hygiene', 'health', 'cosmetics',
                                             'electronics', 'phone', 'chargers', 'power_bank', 'headphones',
                                             'camera', 'flash_drive', 'watch', 'games', 'food', 'snacks',
                                             'drinks', 'groceries', 'books', 'music', 'drawing', 'toys', 'gifts',
                                             'kids', 'sport', 'swimming', 'beach', 'snow', 'camping', 'lamp',
                                             'compass', 'map', 'binoculars', 'hiking', 'sleep', 'nature', 'gear',
                                             'flight', 'train', 'bus', 'car', 'fuel', 'home', 'tools',
                                             'stationery', 'shopping', 'other')),
    position   integer NOT NULL CHECK (position >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (trip_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE packing_items (
    id          uuid PRIMARY KEY,
    trip_id     uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    category_id uuid REFERENCES packing_categories (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    quantity    integer NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 999),
    note        text NOT NULL DEFAULT '',
    packed      boolean NOT NULL DEFAULT false,
    bringer_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    position    integer NOT NULL CHECK (position >= 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX packing_items_trip_idx ON packing_items (trip_id);
CREATE INDEX packing_items_category_idx ON packing_items (category_id);

-- A person's ideas: somewhere they would like to go, kept apart from their
-- trips. An idea is its owner's alone and goes with their account. Its costs
-- are rough, for the whole trip; getting there is priced by each way of
-- getting there, and the whole is worked out, never stored.
CREATE TABLE ideas (
    id              uuid PRIMARY KEY,
    owner_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title           text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    countries       text[] NOT NULL DEFAULT '{}',
    months          smallint[] NOT NULL DEFAULT '{}',
    days_min        integer CHECK (days_min BETWEEN 1 AND 30),
    days_max        integer CHECK (days_max BETWEEN 1 AND 30),
    days_ideal      integer CHECK (days_ideal BETWEEN 1 AND 30),
    description_md  text NOT NULL DEFAULT '',
    currency        char(3) NOT NULL,
    cost_stay       numeric(14, 2) CHECK (cost_stay >= 0),
    cost_food       numeric(14, 2) CHECK (cost_food >= 0),
    cost_other      numeric(14, 2) CHECK (cost_other >= 0),
    visa            text NOT NULL DEFAULT 'not_needed'
                    CHECK (visa IN ('unknown', 'not_needed', 'needed', 'on_arrival')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (days_min IS NULL OR days_max IS NULL OR days_min <= days_max),
    CHECK (days_ideal IS NULL OR ((days_min IS NULL OR days_min <= days_ideal)
                                  AND (days_max IS NULL OR days_ideal <= days_max)))
);
CREATE INDEX ideas_owner_idx ON ideas (owner_id);

-- The places an idea goes to, in their order: a name, and a point when one
-- was found.
CREATE TABLE idea_places (
    idea_id  uuid NOT NULL REFERENCES ideas (id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    name     text NOT NULL DEFAULT '',
    lat      double precision CHECK (lat BETWEEN -90 AND 90),
    lng      double precision CHECK (lng BETWEEN -180 AND 180),
    PRIMARY KEY (idea_id, position),
    CHECK ((lat IS NULL) = (lng IS NULL))
);

-- The ways of getting to an idea, each an alternative to the others: one way
-- of travelling, or several mixed along one route, with its cost and time.
CREATE TABLE idea_transports (
    idea_id  uuid NOT NULL REFERENCES ideas (id) ON DELETE CASCADE,
    position integer NOT NULL CHECK (position >= 0),
    modes    text[] NOT NULL CHECK (cardinality(modes) BETWEEN 1 AND 5),
    cost     numeric(14, 2) CHECK (cost >= 0),
    minutes  integer CHECK (minutes BETWEEN 1 AND 10080),
    PRIMARY KEY (idea_id, position)
);

-- The pictures of an idea, in their order: each rendered by the server into a
-- picture to show and a preview, both kept in the media store and served only
-- to the idea's owner.
CREATE TABLE idea_photos (
    id         uuid PRIMARY KEY,
    idea_id    uuid NOT NULL REFERENCES ideas (id) ON DELETE CASCADE,
    position   integer NOT NULL CHECK (position BETWEEN 0 AND 9),
    storage_key text NOT NULL UNIQUE,
    thumb_key  text NOT NULL UNIQUE,
    width      integer NOT NULL CHECK (width > 0),
    height     integer NOT NULL CHECK (height > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (idea_id, position) DEFERRABLE INITIALLY DEFERRED
);

-- The owner's tags on an idea, from the same list as the tags on their trips.
CREATE TABLE idea_tags (
    tag_id  uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    idea_id uuid NOT NULL REFERENCES ideas (id) ON DELETE CASCADE,
    PRIMARY KEY (tag_id, idea_id)
);
CREATE INDEX idea_tags_idea_idx ON idea_tags (idea_id);

-- +goose Down

DROP TABLE idea_tags;
DROP TABLE idea_photos;
DROP TABLE idea_transports;
DROP TABLE idea_places;
DROP TABLE ideas;

DROP TABLE packing_items;
DROP TABLE packing_categories;

ALTER TABLE trips DROP COLUMN from_plan;

ALTER TABLE share_links DROP COLUMN allow_download;

ALTER TABLE trips DROP COLUMN track_speed_kmh;
ALTER TABLE tracks
    DROP COLUMN speed_kmh,
    DROP COLUMN grades;
ALTER TABLE tracks DROP COLUMN climb_version;

-- A leg with changes goes back to what its own row says; the new ways of
-- travelling fold into the ones they were nearest to.
DROP TABLE leg_segments;
DROP TABLE leg_tickets;
ALTER TABLE days DROP CONSTRAINT days_default_mode_check;
ALTER TABLE legs DROP CONSTRAINT legs_mode_check;
UPDATE days SET default_mode = CASE default_mode WHEN 'ferry' THEN 'other' ELSE 'transit' END
    WHERE default_mode IN ('bus', 'train', 'tram', 'ferry');
UPDATE legs SET mode = CASE mode WHEN 'ferry' THEN 'other' ELSE 'transit' END
    WHERE mode IN ('bus', 'train', 'tram', 'ferry');
ALTER TABLE days ADD CONSTRAINT days_default_mode_check
    CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));
ALTER TABLE legs ADD CONSTRAINT legs_mode_check
    CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));

DROP TABLE trip_tags;
DROP TABLE tags;

-- A car park folds back into transport, the category it was nearest to.
ALTER TABLE items DROP CONSTRAINT items_category_check;
UPDATE items SET category = 'transport' WHERE category = 'parking';
ALTER TABLE items ADD CONSTRAINT items_category_check
    CHECK (category IN ('sight', 'nature', 'museum', 'food', 'shopping', 'activity',
                        'transport', 'other'));

ALTER TABLE trips
    DROP CONSTRAINT trips_state_plan_check,
    DROP COLUMN state;
