-- +goose Up

-- Project brand logo, shown on the tile and banner when there is no cover.
-- '' means none, like cover_url. logo_bg picks the plate behind the logo.
ALTER TABLE projects
    ADD COLUMN logo_url text NOT NULL DEFAULT '',
    ADD COLUMN logo_bg  text NOT NULL DEFAULT 'tile' CHECK (logo_bg IN ('tile', 'ink'));

-- +goose Down
ALTER TABLE projects DROP COLUMN logo_bg, DROP COLUMN logo_url;
