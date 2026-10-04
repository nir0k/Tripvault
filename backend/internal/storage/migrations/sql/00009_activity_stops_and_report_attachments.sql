-- +goose Up

-- The stops along the line of an activity: a café halfway up a hike, a rest
-- by a lake, a spring to fill the bottles at. A stop is part of the activity,
-- not a place of the day: the day's legs and schedule run between its places,
-- and a stop of the trail is not one of them. It hangs on the activity's track
-- - an activity without a line has nowhere to put a stop - and goes with it:
-- removing the line, or turning the activity into a place, removes its stops.
-- A new line keeps them, moved onto it.
--
-- The point lies on the line, distance_m from its start, and grades_to are the
-- metres of the line up to it at each slope, as tracks.grades are for the
-- whole line, so the browser times the way to it at whatever speed is chosen.
-- A stop costs like a place: the budget, the day's total and the members'
-- debts count it in the activity's day.
CREATE TABLE activity_stops (
    id                  uuid PRIMARY KEY,
    document_id         uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    item_id             uuid NOT NULL REFERENCES tracks (item_id) ON DELETE CASCADE,
    kind                text NOT NULL
                        CHECK (kind IN ('food', 'shop', 'rest', 'viewpoint', 'water', 'shelter', 'hut', 'summit',
                                        'cave', 'swim', 'other')),
    name                text NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
    note_md             text NOT NULL DEFAULT '' CHECK (char_length(note_md) <= 20000),
    lat                 double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng                 double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    distance_m          integer NOT NULL CHECK (distance_m >= 0),
    grades_to           integer[] CHECK (cardinality(grades_to) = 101),
    -- Report only: when the stop was reached.
    actual_time         time,
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    cost_per_person     boolean NOT NULL DEFAULT false,
    cost_category       text NOT NULL DEFAULT 'food'
                        CHECK (cost_category IN ('accommodation', 'transport', 'car_rental', 'fuel', 'tolls', 'food',
                                                 'groceries', 'sightseeing', 'activities', 'shopping', 'other')),
    cost_note           text NOT NULL DEFAULT '',
    paid_by             uuid REFERENCES users (id) ON DELETE SET NULL,
    cost_split          text NOT NULL DEFAULT 'none' CHECK (cost_split IN ('none', 'everyone', 'individuals')),
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX activity_stops_document_id_idx ON activity_stops (document_id);
CREATE INDEX activity_stops_item_id_idx ON activity_stops (item_id);

-- The members the cost of a stop is shared among, as item_cost_shares are a
-- place's.
CREATE TABLE stop_cost_shares (
    stop_id  uuid NOT NULL REFERENCES activity_stops (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    position integer NOT NULL,
    amount   numeric(14, 2) CHECK (amount >= 0),
    PRIMARY KEY (stop_id, user_id)
);

CREATE INDEX stop_cost_shares_user_idx ON stop_cost_shares (user_id);

-- A stop's name and note are translated in a report like a place's words.
ALTER TABLE translations ADD COLUMN stop_id uuid REFERENCES activity_stops (id) ON DELETE CASCADE;
ALTER TABLE translations DROP CONSTRAINT translations_check;
ALTER TABLE translations ADD CONSTRAINT translations_check
    CHECK (num_nonnulls(document_id, day_id, stay_id, item_id, leg_id, transfer_id, stop_id) <= 1);
DROP INDEX translations_target_key;
CREATE UNIQUE INDEX translations_target_key
    ON translations ((COALESCE(document_id, day_id, stay_id, item_id, leg_id, transfer_id, stop_id, trip_id)),
                     field, lang);

-- A report's places carry no files: a report tells how the trip went, and
-- its tickets, bookings and receipts belong to the plan. The files reports
-- already held are removed with this change, and the way down cannot bring
-- them back.
DELETE FROM item_attachments a USING documents d WHERE a.document_id = d.id AND d.kind = 'report';

-- Where a person lives, and the circle kept out of what their trips show to
-- a reader from outside them: a read-only link and every PDF. home_* is the
-- point they chose, read back only by them; zone_* is the centre of the circle
-- actually hidden, moved off the home to a random point within half the
-- radius, so the ends of the lines trimmed at its edge do not point at the
-- door. All five are set together or not at all.
ALTER TABLE users
    ADD COLUMN home_lat      double precision CHECK (home_lat BETWEEN -90 AND 90),
    ADD COLUMN home_lng      double precision CHECK (home_lng BETWEEN -180 AND 180),
    ADD COLUMN home_radius_m integer CHECK (home_radius_m BETWEEN 200 AND 2000),
    ADD COLUMN zone_lat      double precision CHECK (zone_lat BETWEEN -90 AND 90),
    ADD COLUMN zone_lng      double precision CHECK (zone_lng BETWEEN -180 AND 180),
    ADD CONSTRAINT users_home_check
        CHECK (num_nulls(home_lat, home_lng, home_radius_m, zone_lat, zone_lng) IN (0, 5));

-- +goose Down

ALTER TABLE users
    DROP CONSTRAINT users_home_check,
    DROP COLUMN zone_lng,
    DROP COLUMN zone_lat,
    DROP COLUMN home_radius_m,
    DROP COLUMN home_lng,
    DROP COLUMN home_lat;

DROP INDEX translations_target_key;
DELETE FROM translations WHERE stop_id IS NOT NULL;
ALTER TABLE translations DROP CONSTRAINT translations_check;
ALTER TABLE translations ADD CONSTRAINT translations_check
    CHECK (num_nonnulls(document_id, day_id, stay_id, item_id, leg_id, transfer_id) <= 1);
ALTER TABLE translations DROP COLUMN stop_id;
CREATE UNIQUE INDEX translations_target_key
    ON translations ((COALESCE(document_id, day_id, stay_id, item_id, leg_id, transfer_id, trip_id)), field, lang);
DROP TABLE stop_cost_shares;
DROP TABLE activity_stops;
