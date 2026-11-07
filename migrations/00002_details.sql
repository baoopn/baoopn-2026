-- +goose Up

-- GitHub sync: the default branch resolves README links; details_pushed_at is
-- gh_pushed_at as of the last README/languages fetch (NULL = never fetched).
ALTER TABLE projects
    ADD COLUMN gh_default_branch text NOT NULL DEFAULT '',
    ADD COLUMN details_pushed_at timestamptz;

-- Content hash so identical uploads (and re-run seed imports) reuse one file.
ALTER TABLE media ADD COLUMN sha256 text NOT NULL DEFAULT '';
CREATE INDEX media_sha256_idx ON media (sha256) WHERE sha256 <> '';

CREATE INDEX posts_tags_idx ON posts USING gin (tags);

-- Per-path view lookups (/blog/<slug>, /projects/<slug>).
CREATE INDEX page_view_stats_path_idx ON page_view_stats (path, bucket);

-- Site-wide unique visitors per UTC hour, for hourly/tz-day visitor series and
-- exact range totals. Same truncated daily hash as page_visitors.
CREATE TABLE page_visitor_hours (
    hour    timestamptz NOT NULL,
    visitor text NOT NULL,
    PRIMARY KEY (hour, visitor)
);

-- +goose Down
DROP TABLE page_visitor_hours;
DROP INDEX page_view_stats_path_idx, posts_tags_idx, media_sha256_idx;
ALTER TABLE media DROP COLUMN sha256;
ALTER TABLE projects DROP COLUMN details_pushed_at, DROP COLUMN gh_default_branch;
