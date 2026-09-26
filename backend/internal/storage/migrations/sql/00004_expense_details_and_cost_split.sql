-- +goose Up

-- The budget categories a car trip needs: the hire itself, the fuel and the
-- tolls and vignettes of the roads it takes, apart from the rest of the
-- transport; groceries bought to cook apart from restaurants and bars, which
-- keep the key food; and sightseeing - tickets to a sight or a museum - apart
-- from other activities.
ALTER TABLE items DROP CONSTRAINT items_cost_category_check;
ALTER TABLE items ADD CONSTRAINT items_cost_category_check
    CHECK (cost_category IN ('accommodation', 'transport', 'car_rental', 'fuel', 'tolls', 'food',
                             'groceries', 'sightseeing', 'activities', 'shopping', 'other'));
ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check
    CHECK (category IN ('accommodation', 'transport', 'car_rental', 'fuel', 'tolls', 'food',
                        'groceries', 'sightseeing', 'activities', 'shopping', 'other'));

-- An expense's note names it in a few words; the comment says anything longer
-- as plain text, and the url links to a booking, a receipt or a shop.
ALTER TABLE expenses
    ADD COLUMN comment text NOT NULL DEFAULT '',
    ADD COLUMN url     text NOT NULL DEFAULT '';

-- Who pays for a place or an activity, and how its cost is shared among the
-- members of the trip. cost_note says in a few words what the money is for.
-- A payer whose account is deleted is forgotten, like the uploader of a file,
-- and the cost stays where it is.
ALTER TABLE items
    ADD COLUMN cost_note  text NOT NULL DEFAULT '',
    ADD COLUMN paid_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN cost_split text NOT NULL DEFAULT 'none'
                          CHECK (cost_split IN ('none', 'everyone', 'individuals'));

-- The members a split cost is shared among. Split among everyone the cost is
-- divided equally and amount stays empty; split among individuals each share
-- names its own amount. position keeps the order the shares were listed in,
-- which decides who takes the cent an equal split leaves over.
CREATE TABLE item_cost_shares (
    item_id  uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    position integer NOT NULL,
    amount   numeric(14, 2) CHECK (amount >= 0),
    PRIMARY KEY (item_id, user_id)
);

CREATE INDEX item_cost_shares_user_idx ON item_cost_shares (user_id);

-- +goose Down

DROP TABLE item_cost_shares;

ALTER TABLE items
    DROP COLUMN cost_note,
    DROP COLUMN paid_by,
    DROP COLUMN cost_split;

ALTER TABLE expenses
    DROP COLUMN comment,
    DROP COLUMN url;

-- The new categories fold back into the ones they were split from.
ALTER TABLE items DROP CONSTRAINT items_cost_category_check;
ALTER TABLE expenses DROP CONSTRAINT expenses_category_check;
UPDATE items SET cost_category = CASE cost_category
        WHEN 'groceries' THEN 'food'
        WHEN 'sightseeing' THEN 'activities'
        ELSE 'transport' END
    WHERE cost_category IN ('car_rental', 'fuel', 'tolls', 'groceries', 'sightseeing');
UPDATE expenses SET category = CASE category
        WHEN 'groceries' THEN 'food'
        WHEN 'sightseeing' THEN 'activities'
        ELSE 'transport' END
    WHERE category IN ('car_rental', 'fuel', 'tolls', 'groceries', 'sightseeing');
ALTER TABLE items ADD CONSTRAINT items_cost_category_check
    CHECK (cost_category IN ('accommodation', 'transport', 'food', 'activities', 'shopping', 'other'));
ALTER TABLE expenses ADD CONSTRAINT expenses_category_check
    CHECK (category IN ('accommodation', 'transport', 'food', 'activities', 'shopping', 'other'));
