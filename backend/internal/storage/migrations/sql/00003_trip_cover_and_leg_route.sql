-- +goose Up

-- The part of the cover picture a trip is shown by, as fractions of the
-- picture's width and height. The file itself is never cut: the whole
-- photograph stays in the gallery, and only the frame is kept here. All four
-- are null together, which means the middle of the picture.
ALTER TABLE trips
    ADD COLUMN cover_crop_x double precision,
    ADD COLUMN cover_crop_y double precision,
    ADD COLUMN cover_crop_w double precision,
    ADD COLUMN cover_crop_h double precision,
    ADD CONSTRAINT trips_cover_crop_check CHECK (
        (cover_crop_x IS NULL AND cover_crop_y IS NULL AND cover_crop_w IS NULL AND cover_crop_h IS NULL)
        OR (cover_crop_x >= 0 AND cover_crop_y >= 0 AND cover_crop_w > 0 AND cover_crop_h > 0
            AND cover_crop_x + cover_crop_w <= 1.000001 AND cover_crop_y + cover_crop_h <= 1.000001)
    );

-- How a road leg asks to be routed. The preference is what the route is
-- optimised for; the via points are positions the route passes through, in
-- order, as [{"lat": .., "lng": ..}], most often copied from a route drawn in
-- another map. A pinned leg keeps the route somebody chose among the
-- provider's alternatives until its input changes or it is recalculated on
-- purpose.
ALTER TABLE legs
    ADD COLUMN route_preference text NOT NULL DEFAULT 'fastest'
        CHECK (route_preference IN ('fastest', 'shortest')),
    ADD COLUMN via jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(via) = 'array' AND jsonb_array_length(via) <= 25),
    ADD COLUMN route_pinned boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE legs
    DROP COLUMN route_preference,
    DROP COLUMN via,
    DROP COLUMN route_pinned;

ALTER TABLE trips
    DROP CONSTRAINT trips_cover_crop_check,
    DROP COLUMN cover_crop_x,
    DROP COLUMN cover_crop_y,
    DROP COLUMN cover_crop_w,
    DROP COLUMN cover_crop_h;
