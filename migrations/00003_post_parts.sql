-- +goose Up

-- A post has 1–20 ordered parts; each part keeps its own Markdown source and
-- the HTML/TOC rendered from it on save. posts.reading_minutes is the total.
CREATE TABLE post_parts (
    id              bigserial PRIMARY KEY,
    post_id         bigint NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    part_no         integer NOT NULL CHECK (part_no BETWEEN 1 AND 20),
    title           text NOT NULL DEFAULT '',     -- '' = shown as "Part <no>"
    content_md      text NOT NULL DEFAULT '',
    content_html    text NOT NULL DEFAULT '',
    toc             jsonb NOT NULL DEFAULT '[]',
    reading_minutes integer NOT NULL DEFAULT 1,
    UNIQUE (post_id, part_no)
);
-- The unique index serves per-post part lookups; text search scans parts with
-- ILIKE, which a b-tree cannot help (the blog is small).

-- The old v3 posts (one page per post, linked by series) are dropped.
DELETE FROM posts WHERE legacy_path IS NOT NULL;

INSERT INTO post_parts (post_id, part_no, title, content_md, content_html, toc, reading_minutes)
SELECT id, 1, '', content_md, content_html, toc, reading_minutes FROM posts;

DROP INDEX posts_series_idx;
ALTER TABLE posts
    DROP COLUMN content_md,
    DROP COLUMN content_html,
    DROP COLUMN toc,
    DROP COLUMN series,
    DROP COLUMN series_title,
    DROP COLUMN series_order,
    DROP COLUMN legacy_path;

-- +goose Down
-- Best effort: the parts of each post are joined back into one page. Deleted
-- v3 posts are not restored.
ALTER TABLE posts
    ADD COLUMN content_md   text NOT NULL DEFAULT '',
    ADD COLUMN content_html text NOT NULL DEFAULT '',
    ADD COLUMN toc          jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN series       text NOT NULL DEFAULT '',
    ADD COLUMN series_title text NOT NULL DEFAULT '',
    ADD COLUMN series_order integer NOT NULL DEFAULT 0,
    ADD COLUMN legacy_path  text UNIQUE;

UPDATE posts p SET
    content_md = j.md,
    content_html = j.html,
    toc = j.toc
FROM (
    SELECT pp.post_id,
           string_agg(CASE WHEN pp.title <> '' THEN '## ' || pp.title || E'\n\n' ELSE '' END || pp.content_md,
                      E'\n\n' ORDER BY pp.part_no) AS md,
           string_agg(pp.content_html, E'\n' ORDER BY pp.part_no) AS html,
           coalesce((SELECT jsonb_agg(e.item ORDER BY q.part_no, e.ord)
                     FROM post_parts q, jsonb_array_elements(q.toc) WITH ORDINALITY AS e(item, ord)
                     WHERE q.post_id = pp.post_id), '[]'::jsonb) AS toc
    FROM post_parts pp
    GROUP BY pp.post_id
) j
WHERE p.id = j.post_id;

CREATE INDEX posts_series_idx ON posts (series, series_order) WHERE series <> '';
DROP TABLE post_parts;
