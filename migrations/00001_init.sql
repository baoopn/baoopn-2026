-- +goose Up

-- Key/value settings edited from the admin dashboard (profile, mail, site).
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Projects: one row per GitHub repo (github_id set) or a hand-made entry.
-- The sync refreshes the gh_* columns; the presentation fields belong to the
-- admin and are never overwritten.
CREATE TABLE projects (
    id             bigserial PRIMARY KEY,
    slug           text NOT NULL UNIQUE,
    github_id      bigint UNIQUE,
    repo_full_name text NOT NULL DEFAULT '',
    repo_url       text NOT NULL DEFAULT '',
    gh_homepage    text NOT NULL DEFAULT '',
    gh_description text NOT NULL DEFAULT '',
    gh_language    text NOT NULL DEFAULT '',
    gh_languages   jsonb NOT NULL DEFAULT '{}',
    gh_topics      text[] NOT NULL DEFAULT '{}',
    gh_stars       integer NOT NULL DEFAULT 0,
    gh_forks       integer NOT NULL DEFAULT 0,
    gh_archived    boolean NOT NULL DEFAULT false,
    gh_fork        boolean NOT NULL DEFAULT false,
    gh_private     boolean NOT NULL DEFAULT false,
    gh_pushed_at   timestamptz,
    gh_created_at  timestamptz,
    readme_html    text NOT NULL DEFAULT '',
    synced_at      timestamptz,
    title          text NOT NULL,
    summary        text NOT NULL DEFAULT '',     -- overrides gh_description when set
    body_md        text NOT NULL DEFAULT '',     -- case study; README shown when empty
    body_html      text NOT NULL DEFAULT '',
    tech           text[] NOT NULL DEFAULT '{}', -- overrides topics/language when set
    period         text NOT NULL DEFAULT '',
    cover_url      text NOT NULL DEFAULT '',
    demo_url       text NOT NULL DEFAULT '',     -- overrides gh_homepage when set
    post_slug      text NOT NULL DEFAULT '',     -- related blog post
    visible        boolean NOT NULL DEFAULT false,
    featured       boolean NOT NULL DEFAULT false,
    sort_order     integer NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX projects_public_idx ON projects (visible, featured DESC, sort_order, gh_pushed_at DESC NULLS LAST);

-- Blog posts: Markdown source, HTML rendered by the API on save.
CREATE TABLE posts (
    id              bigserial PRIMARY KEY,
    slug            text NOT NULL UNIQUE,
    title           text NOT NULL,
    excerpt         text NOT NULL DEFAULT '',
    content_md      text NOT NULL DEFAULT '',
    content_html    text NOT NULL DEFAULT '',
    toc             jsonb NOT NULL DEFAULT '[]',
    cover_url       text NOT NULL DEFAULT '',
    tags            text[] NOT NULL DEFAULT '{}',
    series          text NOT NULL DEFAULT '',
    series_title    text NOT NULL DEFAULT '',
    series_order    integer NOT NULL DEFAULT 0,
    status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    published_at    timestamptz,                 -- in the future = scheduled
    reading_minutes integer NOT NULL DEFAULT 1,
    legacy_path     text UNIQUE,                 -- old v3 URL, redirected here
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX posts_public_idx ON posts (status, published_at DESC);
CREATE INDEX posts_series_idx ON posts (series, series_order) WHERE series <> '';

-- Uploaded media; files live in MEDIA_DIR and are served at /media/<filename>.
CREATE TABLE media (
    id           bigserial PRIMARY KEY,
    filename     text NOT NULL UNIQUE,
    original     text NOT NULL DEFAULT '',
    content_type text NOT NULL,
    size_bytes   bigint NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Contact threads: one per contact-form submission, continued by email.
CREATE TABLE threads (
    id              bigserial PRIMARY KEY,
    reply_token     text NOT NULL UNIQUE,         -- 32 hex: reply+<token>@INBOUND_MAIL_DOMAIN
    name            text NOT NULL,
    email           text NOT NULL,
    subject         text NOT NULL,
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'archived', 'spam')),
    read_at         timestamptz,                  -- NULL = unread
    last_message_at timestamptz NOT NULL DEFAULT now(),
    last_direction  text NOT NULL DEFAULT 'inbound',
    last_preview    text NOT NULL DEFAULT '',
    message_count   integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX threads_inbox_idx ON threads (status, last_message_at DESC, id DESC);
CREATE INDEX threads_unread_idx ON threads (id) WHERE read_at IS NULL AND status = 'open';

CREATE TABLE thread_messages (
    id                  bigserial PRIMARY KEY,
    thread_id           bigint NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    direction           text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    source              text NOT NULL CHECK (source IN ('form', 'email', 'admin')),
    body                text NOT NULL,
    full_body           text NOT NULL DEFAULT '', -- set when quoted text was stripped
    status              text NOT NULL DEFAULT 'received'
                        CHECK (status IN ('received', 'sending', 'sent', 'failed')),
    error               text NOT NULL DEFAULT '',
    rfc_message_id      text,
    from_address        text NOT NULL DEFAULT '',
    other_sender        boolean NOT NULL DEFAULT false,
    attachments_dropped integer NOT NULL DEFAULT 0,
    request_id          text,                     -- admin-side idempotency key
    created_at          timestamptz NOT NULL DEFAULT now(),
    sent_at             timestamptz
);
CREATE INDEX thread_messages_thread_idx ON thread_messages (thread_id, created_at, id);
CREATE UNIQUE INDEX thread_messages_rfc_idx ON thread_messages (rfc_message_id) WHERE rfc_message_id IS NOT NULL;
CREATE UNIQUE INDEX thread_messages_request_idx ON thread_messages (thread_id, request_id) WHERE request_id IS NOT NULL;

-- Inbound mail that matched no thread or was filtered out (debugging aid).
CREATE TABLE inbound_rejects (
    id         bigserial PRIMARY KEY,
    reason     text NOT NULL,
    recipient  text NOT NULL DEFAULT '',
    sender     text NOT NULL DEFAULT '',
    subject    text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Cookieless page analytics in hourly UTC buckets.
CREATE TABLE page_view_stats (
    bucket     timestamptz NOT NULL,
    path       text NOT NULL,
    device     text NOT NULL,
    browser    text NOT NULL,
    country    text NOT NULL,
    referrer   text NOT NULL,
    views      bigint NOT NULL DEFAULT 0,
    visible_ms bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket, path, device, browser, country, referrer)
);
CREATE INDEX page_view_stats_bucket_idx ON page_view_stats (bucket DESC);

-- Daily unique visitors per path: truncated sha256(salt|day|ip|ua), never a raw IP.
-- path '*' counts site-wide visitors.
CREATE TABLE page_visitors (
    day     date NOT NULL,
    path    text NOT NULL,
    visitor text NOT NULL,
    PRIMARY KEY (day, path, visitor)
);

-- +goose Down
DROP TABLE page_visitors, page_view_stats, inbound_rejects, thread_messages,
    threads, media, posts, projects, settings;
