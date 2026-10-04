-- +goose Up

-- Colour themes an administrator uploads for everybody on the instance. A
-- theme carries a light palette, a dark one or both, each a map of the
-- interface's colour names to colour values, checked by the build before it
-- is stored. A theme with one palette is shown in it whatever the person's
-- light or dark preference.
CREATE TABLE instance_themes (
    id         uuid PRIMARY KEY,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    light      jsonb,
    dark       jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(light, dark) >= 1)
);

CREATE UNIQUE INDEX instance_themes_name_key ON instance_themes (lower(name));

-- The theme a person reads the interface in, beside the light, dark or auto
-- preference in users.theme. No theme means the built-in palettes, and so
-- does a theme the administrator deleted.
ALTER TABLE users ADD COLUMN theme_id uuid REFERENCES instance_themes (id) ON DELETE SET NULL;

CREATE INDEX users_theme_id_idx ON users (theme_id);

-- +goose Down

DROP INDEX users_theme_id_idx;
ALTER TABLE users DROP COLUMN theme_id;
DROP TABLE instance_themes;
