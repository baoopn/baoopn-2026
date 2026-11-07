-- +goose Up

-- Orphan media cleanup (internal/media/orphans.go). touched_at is bumped when
-- content that names the file is saved or deleted and when an upload is
-- deduplicated to the row, so the grace period counts from the last use, not
-- from the upload. Existing rows age from their creation.
ALTER TABLE media ADD COLUMN touched_at timestamptz NOT NULL DEFAULT now();
UPDATE media SET touched_at = created_at;

-- References are matched by the 24-hex stem: the fallback renames stem.webp
-- to stem.png, and the link rewrite changes the URL form, never the stem.
ALTER TABLE media ADD COLUMN stem text GENERATED ALWAYS AS (left(filename, 24)) STORED;
CREATE INDEX media_stem_idx ON media (stem);

-- +goose Down
DROP INDEX media_stem_idx;
ALTER TABLE media DROP COLUMN stem, DROP COLUMN touched_at;
