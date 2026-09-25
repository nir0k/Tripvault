-- +goose Up

-- A cable car, a gondola or a chairlift is a way of travelling of its own: it
-- hangs on a straight cable, so its legs are drawn as a line, not routed.
ALTER TABLE days DROP CONSTRAINT days_default_mode_check;
ALTER TABLE days ADD CONSTRAINT days_default_mode_check
    CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));
ALTER TABLE legs DROP CONSTRAINT legs_mode_check;
ALTER TABLE legs ADD CONSTRAINT legs_mode_check
    CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));

-- A journey booked ahead that takes the trip from one town to another: a
-- flight, a train, a bus, a ferry, a transfer to the hotel. It belongs to the
-- whole document, like a stay, and shows in the day it departs on (and the
-- day it arrives on, when that is another) without taking part in the day's
-- schedule: the legs between places are what the schedule is made of.
CREATE TABLE transfers (
    id                  uuid PRIMARY KEY,
    document_id         uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    kind                text NOT NULL DEFAULT 'flight'
                        CHECK (kind IN ('flight', 'train', 'bus', 'ferry', 'transfer', 'other')),
    -- The carrier or the number, such as "Icelandair FI 204"; may be empty.
    name                text NOT NULL DEFAULT '',
    from_name           text NOT NULL,
    from_address        text NOT NULL DEFAULT '',
    from_lat            double precision CHECK (from_lat BETWEEN -90 AND 90),
    from_lng            double precision CHECK (from_lng BETWEEN -180 AND 180),
    to_name             text NOT NULL,
    to_address          text NOT NULL DEFAULT '',
    to_lat              double precision CHECK (to_lat BETWEEN -90 AND 90),
    to_lng              double precision CHECK (to_lng BETWEEN -180 AND 180),
    -- Local dates and times at each end, with no zone attached. A null
    -- arrival date means the day of departure.
    departure_date      date NOT NULL,
    departure_time      time,
    arrival_date        date,
    arrival_time        time,
    booking_ref         text NOT NULL DEFAULT '',
    url                 text NOT NULL DEFAULT '',
    notes_md            text NOT NULL DEFAULT '',
    planned_cost_amount numeric(14, 2) CHECK (planned_cost_amount >= 0),
    cost_per_person     boolean NOT NULL DEFAULT false,
    -- Only a report fills the actual amount; in a plan it stays empty.
    actual_cost_amount  numeric(14, 2) CHECK (actual_cost_amount >= 0),
    source_transfer_id  uuid REFERENCES transfers (id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((from_lat IS NULL) = (from_lng IS NULL)),
    CHECK ((to_lat IS NULL) = (to_lng IS NULL)),
    -- A flight west over the date line may land the day before it left.
    CHECK (arrival_date IS NULL OR arrival_date >= departure_date - 1)
);

CREATE INDEX transfers_document_id_idx ON transfers (document_id);

-- A transfer's words are translated in a report like a stay's.
ALTER TABLE translations ADD COLUMN transfer_id uuid REFERENCES transfers (id) ON DELETE CASCADE;
ALTER TABLE translations DROP CONSTRAINT translations_check;
ALTER TABLE translations ADD CONSTRAINT translations_check
    CHECK (num_nonnulls(document_id, day_id, stay_id, item_id, leg_id, transfer_id) <= 1);
DROP INDEX translations_target_key;
CREATE UNIQUE INDEX translations_target_key
    ON translations ((COALESCE(document_id, day_id, stay_id, item_id, leg_id, transfer_id, trip_id)), field, lang);

-- +goose Down

DROP INDEX translations_target_key;
DELETE FROM translations WHERE transfer_id IS NOT NULL;
ALTER TABLE translations DROP CONSTRAINT translations_check;
ALTER TABLE translations ADD CONSTRAINT translations_check
    CHECK (num_nonnulls(document_id, day_id, stay_id, item_id, leg_id) <= 1);
ALTER TABLE translations DROP COLUMN transfer_id;
CREATE UNIQUE INDEX translations_target_key
    ON translations ((COALESCE(document_id, day_id, stay_id, item_id, leg_id, trip_id)), field, lang);
DROP TABLE transfers;

-- The older schema has no cable car; its legs become "other", which is drawn
-- the same straight way, and a day that defaulted to it defaults to nothing.
UPDATE legs SET mode = 'other' WHERE mode = 'cable_car';
UPDATE days SET default_mode = NULL WHERE default_mode = 'cable_car';
ALTER TABLE legs DROP CONSTRAINT legs_mode_check;
ALTER TABLE legs ADD CONSTRAINT legs_mode_check
    CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'other'));
ALTER TABLE days DROP CONSTRAINT days_default_mode_check;
ALTER TABLE days ADD CONSTRAINT days_default_mode_check
    CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'other'));
