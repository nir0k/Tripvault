-- +goose Up

-- A cable car, a gondola or a chairlift is a way of travelling of its own: it
-- hangs on a straight cable, so its legs are drawn as a line, not routed.
ALTER TABLE days DROP CONSTRAINT days_default_mode_check;
ALTER TABLE days ADD CONSTRAINT days_default_mode_check
    CHECK (default_mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));
ALTER TABLE legs DROP CONSTRAINT legs_mode_check;
ALTER TABLE legs ADD CONSTRAINT legs_mode_check
    CHECK (mode IN ('walk', 'car', 'bike', 'transit', 'flight', 'cable_car', 'other'));

-- +goose Down

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
