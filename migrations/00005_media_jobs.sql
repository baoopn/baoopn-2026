-- +goose Up

-- Raster uploads are converted to WebP by the media worker (cmd/mediaworker)
-- through a job queue. A media row is 'processing' until its job finishes,
-- then 'ready' (converted = true for a WebP, false for a published original)
-- or 'failed' with a reason in error.
ALTER TABLE media
    ADD COLUMN status text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'processing', 'failed')),
    ADD COLUMN error text NOT NULL DEFAULT '',
    ADD COLUMN converted boolean NOT NULL DEFAULT false,
    -- sha256 of the uploaded original (uncropped uploads only), so the same
    -- original uploaded again is deduplicated although sha256 is the WebP's.
    ADD COLUMN original_sha256 text NOT NULL DEFAULT '';
CREATE INDEX media_original_sha256_idx ON media (original_sha256) WHERE original_sha256 <> '';

-- phase 'convert' jobs are leased by workers (SELECT ... FOR UPDATE SKIP
-- LOCKED); phase 'fallback' jobs publish the stripped original and are run by
-- the API itself. The original lives in MEDIA_DIR/originals/ (never in the
-- public bucket) until the job is done, or for 24 hours after it failed.
CREATE TABLE media_jobs (
    id                    bigserial PRIMARY KEY,
    media_id              bigint NOT NULL UNIQUE REFERENCES media (id) ON DELETE CASCADE,
    phase                 text NOT NULL DEFAULT 'convert' CHECK (phase IN ('convert', 'fallback')),
    status                text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    original_content_type text NOT NULL,
    original_size         bigint NOT NULL,
    orientation           int NOT NULL DEFAULT 1,
    crop_x                int,
    crop_y                int,
    crop_w                int,
    crop_h                int,
    attempts              int NOT NULL DEFAULT 0,
    max_attempts          int NOT NULL DEFAULT 5,
    next_attempt_at       timestamptz NOT NULL DEFAULT now(),
    lease_until           timestamptz,
    lease_token           text NOT NULL DEFAULT '',
    worker_id             text NOT NULL DEFAULT '',
    last_error            text NOT NULL DEFAULT '',
    failed_at             timestamptz,
    original_deleted      boolean NOT NULL DEFAULT false,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_jobs_queued_idx ON media_jobs (phase, next_attempt_at, id) WHERE status = 'queued';
CREATE INDEX media_jobs_running_idx ON media_jobs (lease_until) WHERE status = 'running';
CREATE INDEX media_jobs_failed_idx ON media_jobs (failed_at) WHERE status = 'failed' AND NOT original_deleted;

-- +goose Down
DROP TABLE media_jobs;
DROP INDEX media_original_sha256_idx;
ALTER TABLE media DROP COLUMN status, DROP COLUMN error, DROP COLUMN converted, DROP COLUMN original_sha256;
