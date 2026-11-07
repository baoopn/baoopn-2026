-- +goose Up

-- Posts are either tech or life; every existing post is tech.
ALTER TABLE posts ADD COLUMN category text NOT NULL DEFAULT 'tech' CHECK (category IN ('tech', 'life'));
CREATE INDEX posts_category_idx ON posts (category, status, published_at DESC);

-- +goose Down
DROP INDEX posts_category_idx;
ALTER TABLE posts DROP COLUMN category;
