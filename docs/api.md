# API contract

The TypeScript types in `web/src/lib/api/types.ts` are the source of truth for shapes.
Envelopes: `{ "data": …, "meta"?: PageMeta }` on success, `{ "error": { "code", "message" } }`
on failure. The browser always calls the same origin (`/api/...`); the SvelteKit server proxies
to the Go API (`web/src/hooks.server.ts`) and adds `X-Client-IP`, `X-Client-Country` and
`X-Internal-Token`.

## Public — `/api/v1`

| Method | Path | Query / body | Response `data` |
|---|---|---|---|
| GET | `/profile` | – | `Profile` |
| GET | `/projects` | `featured=1` (optional) | `ProjectSummary[]` (visible only, ordered) |
| GET | `/projects/{slug}` | – | `ProjectDetail` (404 if hidden) |
| GET | `/posts` | `category` (`tech` or `life`; none = all, anything else 400), `tag`, `q` (title, excerpt and part text), `page` (1+), `limit` (1–50, default 10) | `PostSummary[]` + `meta` (one entry per post) |
| GET | `/posts/{slug}` | `part` (1+, default 1) | `PostDetail` with the requested part (published and `published_at <= now` only; 404 if the part does not exist) |
| GET | `/tags` | `category` (`tech` or `life`; none = all) | `TagCount[]` (visible posts of that category) |
| POST | `/contact` | `ContactInput` | `201 { ok: true }`; 422 `validation` with message; 429 `rate_limited` (5/hour/IP) |
| POST | `/events` | `BeaconEvent` (text/plain or JSON, max 2 KB) | `202` empty |
| GET | `/listening/now-playing` | – | `NowPlaying \| null` (cached 5 s; served from the stream hub's snapshot while it is live). Polling fallback for the stream below |
| GET | `/listening/now-playing/stream` | – | Server-Sent Events, see below |
| GET | `/listening/recent` | `limit` (1–20, default 6) | `RecentPlay[]` (cached 60 s) |
| GET | `/listening/top-artists` | `range` (`7d`,`30d`,`90d`), `limit` (1–20) | `TopArtist[]` (cached 5 min) |
| GET | `/brand` | – | `Brand` (current URL, type and size of every brand asset; `Cache-Control: public, max-age=60`; the defaults when the database is unreachable) |
| GET | `/brand/{slot}` | – | `302` to the slot's current file (`Cache-Control: public, max-age=300`); 404 `not_found` for an unknown slot |
| GET | `/listening/top-playlists` | `limit` (1–24, default 6: older entries per period) | `TopPlaylists` (newest first per period; only the newest entry of each period carries `tracks`; cached 5 min; empty periods when `SPOTIFY_API_URL` is unset) |

### Now-playing stream — `GET /api/v1/listening/now-playing/stream`

`text/event-stream` (`Cache-Control: no-store`, `X-Accel-Buffering: no`). Not an
envelope: each event's `data` is a bare `NowPlaying` JSON object or `null`.

```
retry: 5000

event: now-playing
data: {"is_playing":true,"progress_ms":81234,"fetched_at":"…","track":{…}}

: keep-alive
```

- The current snapshot is sent right after connecting, then one `now-playing`
  event per change; `: keep-alive` comments every 25 s. While the upstream is
  reconnecting, a new client gets no stale snapshot: its first event is the one the
  reconnect brings.
- The API keeps **one** upstream connection to spotify-tracker
  (`SPOTIFY_API_URL` + `/api/v1/now-playing/stream`) for all clients, opened on
  the first subscriber and closed 60 s after the last one leaves. It reconnects with
  exponential backoff and jitter (1 s → 30 s) and after 70 s without any data.
- A slow client only misses intermediate updates; it always gets the latest.
- At most 500 concurrent streams: above that `503 too_many_streams` with `Retry-After`.
  At most 8 per client IP (IPv6 /64): above that `429 too_many_streams` with
  `Retry-After`. Either way the site falls back to polling.
- Mounted under `/api/v1` with the public rate limit but without the 20 s request
  timeout. The handler clears the server's 2 min read deadline and gives each write
  its own 30 s deadline, so a client that stops reading is dropped.
- `SPOTIFY_API_URL` unset: one `now-playing` event with `null`, then keep-alives only.
- The site (`web/src/lib/site/listening.svelte.ts`) uses one `EventSource` per page.
  It falls back to polling `/listening/now-playing` every 15 s when EventSource is
  missing, when the browser gives up on the stream (e.g. a 429/503), after 3 errors
  within a minute, or with no event 15 s after opening, and it
  retries the stream every 5 min. The stream closes after the tab has been hidden for 60 s.

## Admin — `/api/admin` (Bearer Clerk session token)

`GET /config` is unauthenticated (`AdminConfig`). Everything else needs
`Authorization: Bearer <clerk session token>`; 401 `unauthorized`, 403 `forbidden`.

| Method | Path | Body | Response `data` |
|---|---|---|---|
| GET | `/me` | – | `AdminUser` |
| GET | `/overview` | – | `Overview` |
| GET | `/profile` | – | `Profile` |
| PUT | `/profile` | `Profile` (read-only fields ignored) | `Profile` |
| POST | `/profile/github` | – | `Partial<Profile>` from the GitHub user (not saved) |
| GET | `/projects` | – | `AdminProject[]` (all, incl. hidden) |
| POST | `/projects` | `{ title, slug? }` (manual project) | `AdminProject` |
| GET | `/projects/{id}` | – | `AdminProject` |
| PATCH | `/projects/{id}` | `ProjectPatch` (`logo_url` follows the `cover_url` rule: http(s) URL or /path, `""` clears it. `logo_bg` is `tile` or `ink`; other values 422) | `AdminProject` |
| DELETE | `/projects/{id}` | – | `204` (a synced repo comes back hidden on next sync unless archived) |
| POST | `/projects/sync` | – | `SyncResult` (pull repos of `GITHUB_USER`; new ones are hidden) |
| POST | `/projects/reorder` | `{ ids: number[] }` | `204` (sort_order = index) |
| GET | `/posts` | `state`, `category`, `q` | `AdminPost[]` |
| POST | `/posts` | `PostInput` (`category` missing or empty = `tech`; other values 422) | `AdminPost` (409 `slug_taken`) |
| GET | `/posts/{id}` | – | `AdminPost` |
| PUT | `/posts/{id}` | `PostInput` | `AdminPost` |
| DELETE | `/posts/{id}` | – | `204` |
| POST | `/posts/preview` | `{ markdown }` | `PreviewResult` |
| GET | `/media` | `page`, `limit` | `Media[]` + `meta` |
| POST | `/media` | multipart `file` (≤ MAX_UPLOAD_MB) plus optional `crop_x`, `crop_y`, `crop_w`, `crop_h` — see *Media uploads* | `201 Media` (ready) or `202 Media` (`processing`, or `failed`) |
| GET | `/media/{id}` | – | `Media` (poll a `processing` upload; 404 `not_found`) |
| POST | `/media/{id}/retry` | – | `202 Media` (`processing`); 409 `conflict` unless `failed`; 410 `gone` once the original was cleaned up (upload again) |
| DELETE | `/media/{id}` | – | `204` |
| GET | `/media/orphans` | – | `OrphanPlan` (dry run: what a cleanup would delete now, and why the rest is kept) |
| POST | `/media/orphans` | `{ keys?: string[] }` (≤ 500 candidate keys; empty or absent = every eligible candidate except `local_copy`) | `OrphanResult`; 400 `bad_request` for a bad key; 409 `disabled` (`MEDIA_ORPHAN_GRACE=0`) or `busy` (a run is in progress) |
| PUT | `/media/orphans/auto` | `{ enabled }` | `OrphanPlan` (daily cleanup on or off) |
| GET | `/threads` | `status` (default `open`), `q` | `ThreadSummary[]` |
| GET | `/threads/unread-count` | – | `{ count }` |
| GET | `/threads/{id}` | – | `ThreadDetail` |
| POST | `/threads/{id}/read` · `/unread` | – | `204` |
| PATCH | `/threads/{id}` | `{ status }` | `ThreadSummary` |
| DELETE | `/threads/{id}` | – | `204` |
| POST | `/threads/{id}/messages` | `{ body, request_id }` (1–10 000 chars) | `ThreadMessage` (`status` sent or failed) |
| POST | `/threads/{id}/messages/{mid}/retry` | – | `ThreadMessage` |
| GET | `/inbox/events` | – | Server-Sent Events, see *Inbox event stream* |
| GET | `/analytics` | `range` (`24h`,`7d`,`30d`,`90d`), `tz` (IANA) | `AnalyticsReport` |
| GET | `/settings` | – | `SiteSettings` |
| PUT | `/settings` | `SiteSettings` | `SiteSettings` |
| POST | `/settings/test-email` | – | `{ sent_to }` |
| GET | `/brand` | – | `AdminBrandAsset[]` (slot order) |
| PUT | `/brand/{slot}` | multipart `file` (request ≤ 1 MiB) — see *Brand assets* | `AdminBrandAsset` (`custom: true`; the same bytes as the current file change nothing) |
| DELETE | `/brand/{slot}` | – | `AdminBrandAsset` (`custom: false`, back to the default; idempotent); 404 `not_found` for an unknown slot |
| GET | `/backups` | – | `BackupStatus` (see *Backups*) |
| PUT | `/backups/settings` | `BackupSettingsInput` (a missing field is `null`) | `BackupStatus`; 422 `validation` names the field and its range |
| DELETE | `/backups/settings` | – | `BackupStatus` (the four overrides are cleared; `paused` stays) |
| PUT | `/backups/pause` | `{ paused }` | `BackupStatus` (sets or clears `paused_at`) |
| POST | `/backups/run` | – | `202 BackupRun` (`status: "queued"`); 409 `busy`, `agent_offline` or `not_configured` |

### Inbox event stream — `GET /api/admin/inbox/events`

`text/event-stream` (`Cache-Control: no-store`, `X-Accel-Buffering: no`) for the
admin inbox, so lists, the unread badge and an open thread update live. Auth is the
usual `Authorization: Bearer <clerk session token>` header, checked once when the
stream opens; the token never goes in the URL, so the client reads the stream with
`fetch`, not `EventSource`. 401/403 and 503 `admin_disabled` as for every admin route.

```
retry: 5000

event: ready
data: {}

event: message.created
data: {"thread_id":12,"message_id":40,"direction":"outbound","request_id":"r-abc123"}

: keep-alive
```

| event | data | when |
|---|---|---|
| `ready` | `{}` | the stream opened |
| `thread.created` | `{ thread_id }` | a contact form was stored |
| `message.created` | `{ thread_id, message_id, direction, request_id? }` | an inbound email was stored (`inbound`), or an admin reply was inserted (`outbound`, with its `request_id`) |
| `message.updated` | `{ thread_id, message_id, status }` | a retry started (`sending`), or a reply or retry recorded `sent` or `failed` |
| `thread.updated` | `{ thread_id, change, status? }` | marked `read` / `unread` (only when that changed it), or `change: "status"` with the new `status` |
| `thread.deleted` | `{ thread_id }` | the thread was deleted |

- Events carry ids and kinds only, never bodies, names or email addresses: the
  client refetches what changed. Unknown events are ignored.
- No `id:` and no replay. The client refetches everything on each `ready`.
- `: keep-alive` every 25 s without events (below Cloudflare's 100 s idle limit).
- The server ends each stream after 15 min; the client reconnects with a fresh
  token. On shutdown every stream ends at once, and new ones get
  `503 shutting_down` with `Retry-After: 10`.
- At most 16 open streams: above that `503 too_many_streams` with `Retry-After: 10`.
  A client that stops reading (32 events buffered) is dropped and reconnects.
- Mounted behind `AdminAuth` without the 60 s admin request timeout; deadlines as
  for the now-playing stream (read deadline cleared, 30 s per write).
- Events are published in-process: correct while one API instance runs. More
  instances would need Postgres `LISTEN/NOTIFY` feeding each instance's hub.

### Brand assets

- **Slots** (fixed order), with what each accepts (type sniffed from the bytes, never the name):

  | slot | type | rule | max |
  |---|---|---|---|
  | `favicon_ico` | ICO | 1–16 images, each inside the file; must contain 16×16, 32×32 and 48×48 | 256 KiB |
  | `favicon_svg` | SVG | square viewBox | 256 KiB |
  | `apple_touch_icon` | PNG | exactly 180×180 | 512 KiB |
  | `wordmark_primary` | SVG | viewBox | 256 KiB |
  | `monogram_primary` | SVG | square viewBox | 256 KiB |
  | `monogram_inverse` | SVG | square viewBox | 256 KiB |
  | `email_logo` | PNG | 240–1200 px wide, 60–600 px tall, 1.5:1 to 4:1 (shown 48 px tall in emails) | 256 KiB |

- **Errors:** 400 `bad_request` (not multipart, no `file` field), 404 `not_found` (unknown
  slot), 413 `too_large`, 415 `unsupported_type` (e.g. "favicon_ico needs an ICO file"),
  422 `validation` with the reason (e.g. "apple_touch_icon must be 180×180 (got 192×192)"),
  503 `storage_unavailable`.
- **SVG rules** (rejected, never rewritten): well-formed XML with an `<svg>` root in the SVG
  namespace and a valid viewBox; every element in the SVG namespace or an editor/metadata one
  (Inkscape, Sodipodi, RDF, Creative Commons, Dublin Core); no DOCTYPE/ENTITY and no processing
  instruction other than `<?xml …?>`; no `script`, `foreignObject`, `iframe`, `embed`,
  `object`, `audio`, `video`, `canvas`, `handler` or `listener` elements; no `on*` and no
  `xml:base` attributes; `href`/`xlink:href` only `#id` or
  `data:image/(png|jpeg|webp|gif);base64,`; no `attributeName` of `href`/`xlink:href`
  (animations cannot change links); no `javascript:`, `@import`, or `url(`/`image(`/`image-set(`
  other than `#id` in any attribute or `<style>` (the whole `<style>` text is checked at once,
  across comments and CDATA, and it may hold only text, no elements); no backslash (CSS escape) in
  `<style>` or in any attribute without a namespace prefix.
  Editor attributes (Inkscape, Sodipodi) are fine.
- **Storage:** files are stored byte for byte (never converted, no `media` row) under
  `brand/<stem>.<16 hex of sha256>.<ext>` in the bucket (or `MEDIA_DIR/brand/`), so each
  version has its own immutable URL (`MEDIA_PUBLIC_URL/brand/…`, else `/media/brand/…`).
  The defaults are embedded in the API (`internal/brand/defaults/`) and uploaded on startup
  when missing; an override whose file is gone falls back to the default (only when every
  default was found or uploaded, so an unreachable or read-only bucket cannot wipe the overrides). Uploading a
  slot's default bytes is a reset. The `brand`
  setting holds only overrides and replaced files. A replaced file is deleted 7 days later;
  a current or default file never is. The orphan cleanup ignores `brand/`.
- The web app reads `GET /api/v1/brand` (cached 60 s) and redirects `/favicon.ico`,
  `/favicon.svg` and `/apple-touch-icon.png` to the current files.
- **Emails** show `email_logo` (PNG, since Gmail shows no SVG) in the header, 48 px tall and
  linked to the site; the URL is made absolute at send time (`MEDIA_PUBLIC_URL/brand/…` as is,
  else `PUBLIC_URL/media/brand/…`), so a replaced logo applies to the next email. When it has
  no absolute http(s) URL, the emails show the text badge instead. The logo is a remote image
  shared by all recipients, so its URL carries no per-recipient data, but fetching it can still
  reveal that a mail was opened.

### Backups

The backup sidecar (`deploy/backup`, see `docs/backup.md`) writes a heartbeat to
`backup_agent` every 30 s and one row per run to `backup_runs`. The API only reads these
tables. It never talks to the bucket. It writes the `backup` settings document and queues
manual runs. No secret value reaches the API: `secrets` only says which ones are set.

- **Health** (`health`, the first rule that matches wins):

  | code | when |
  |---|---|
  | `not_reporting` | no heartbeat row yet (sidecar not deployed, or an older version) |
  | `offline` | the last heartbeat is 3 min old or more |
  | `not_configured` | the sidecar is idle because backups are not set up (`agent.idle_reason` says why) |
  | `paused` | scheduled backups are paused (the sidecar says so, or `settings.paused` is on) |
  | `ok` | the last success is at most `max_age_hours` old (26 when unknown) |
  | `warning` | as `ok`, but the newest finished run failed |
  | `waiting` | no success yet, and less than `retry_minutes` + 60 min have passed since the first run was due (or none is planned yet) |
  | `stale` | anything else: no success within `max_age_hours` |

- **Settings:** each of `backup_time`, `keep_days`, `keep_min` and `max_age_hours` has
  `env` (the sidecar's value from its environment), `override` (the admin's value) and
  `value` (the override if set, else env). `source` is `override` or `env`.
  `null` means "use the env value". The rules:

  | field | allowed |
  |---|---|
  | `backup_time` | `HH:MM`, 24-hour UTC, `00:00` to `23:59` (`""` counts as `null`) |
  | `keep_days` | 0 to 3650 (0 keeps every backup) |
  | `keep_min` | 0 to 1000 |
  | `max_age_hours` | 25 to 720 |

  A stored value outside these rules counts as no override (the sidecar logs a warning and
  uses its env value). `pending` is true until the sidecar has applied the latest change; it
  checks every 30 s. Pausing stops scheduled and startup backups only: Back up now still works.
- **Back up now:** `POST /backups/run` adds a queued run. The sidecar starts it within 30 s.
  At most one run is queued at a time: 409 `busy` while a run is queued or running. 409
  `agent_offline` when there is no heartbeat in the last 3 min, and 409 `not_configured` when
  the sidecar is idle. A queued run that is not picked up within 10 min fails with
  "Not picked up by the backup service within 10 minutes".
- **Runs:** `runs` lists the newest 20. `last_run` is the newest finished run, `active_run` the
  queued or running one. The sidecar keeps the newest 200 finished runs. `last_success_at` is
  the latest of the sidecar's last success and the newest `ok` run.

### Media uploads

- **Accepted:** JPEG, PNG, GIF, WebP, HEIC/HEIF, BMP, TIFF, AVIF, SVG, PDF (sniffed from the
  bytes, not the name). Anything else: 415 `unsupported_type`; a raster image whose header
  cannot be read: 415 `unsupported_type`. Over `MAX_UPLOAD_MB`, or more than
  `MEDIA_MAX_PIXELS` pixels (checked from the header before decoding): 413 `too_large`.
  Private originals store not writable: 503 `storage_unavailable`.
- **Stored as is:** SVG, PDF, AVIF and animated GIF/WebP (`status: ready`, `converted: false`).
- **Converted:** every other raster image is converted to WebP by the media worker
  (`cmd/mediaworker`) through a queue:
  - EXIF orientation applied, metadata stripped, longest side ≤ `MEDIA_MAX_DIMENSION`
    (never upscaled), lossy WebP at `MEDIA_WEBP_QUALITY`.
  - The new `Media` has its final `url` (`<random>.webp`) and `status: processing`. The
    upload waits up to `MEDIA_UPLOAD_WAIT` (10 s) for the conversion: `201` when it is
    `ready`, otherwise `202`. Then poll `GET /media/{id}`.
  - The original is kept privately on the API's volume until the job finishes (never in the
    public bucket).
- **Crop:** optional form fields `crop_x`, `crop_y`, `crop_w`, `crop_h`.
  - Integers in original image pixels, after EXIF orientation; all four or none, in any order
    relative to `file`.
  - The image is cropped before it is resized.
  - Out-of-range values: 400 `bad_request`. For HEIC, whose header may not give the oriented
    size, the bounds are checked by the worker, and a bad crop fails the conversion.
  - Ignored for files stored as is.
- **Fallbacks** (`Media.error` explains, `converted: false`):
  - The worker cannot convert the file (permanent error, or `MEDIA_JOB_MAX_ATTEMPTS` retries
    with backoff used up), or no worker took the job within `MEDIA_FALLBACK_AFTER`:
    - JPEG, PNG, GIF and WebP originals are published with their metadata stripped (GPS,
      EXIF other than the orientation, XMP, IPTC, text chunks). `status: ready`, e.g.
      `error: "WebP conversion failed: …; saved the original"`. The URL then ends in the
      original's extension; stored references to the `.webp` URL are updated.
    - HEIC, HEIF, TIFF and BMP originals give `status: failed` (`"converter unavailable"`,
      or `"WebP conversion failed: …"`).
  - Storage (R2) refuses the file: the job is retried with backoff (`status` stays
    `processing`); after the last attempt it is `failed` with `"storage unavailable"`.
  - A failed upload keeps its original for `MEDIA_FAILED_RETENTION` (24 h) so
    `POST /media/{id}/retry` can queue it again; after that the retry answers 410 `gone`.
- **Dedupe:** the same uncropped original uploaded again returns the existing `Media`;
  `sha256` is computed on the stored bytes.
- **Orphan cleanup** (Settings → Media storage):
  - A file is in use when its 24-hex stem appears anywhere in `posts`, `post_parts`, `projects`,
    `settings` or `thread_messages` (every column, every key; drafts and sent replies included;
    percent- and JSON-escaped links count too). Matching the stem, not the URL,
    keeps files whose extension changed in a fallback and links in any form (`/media/<file>`,
    `MEDIA_PUBLIC_URL/<file>`, absolute site URLs).
  - Unused files are deleted only after `MEDIA_ORPHAN_GRACE` (7 days) unused. The clock is
    `touched_at`: set on upload, and bumped when content naming the file is saved or deleted
    (old and new text) and when an upload is deduplicated to it. `0` turns deletion off.
  - Candidates (`key` = `<kind>:<name>`): `row` (a media row and its files), `object` (a
    bucket file with no row), `local` (a `MEDIA_DIR` file with no row), `local_copy` (the
    local copy of a bucket file; only deleted when its key is sent), `original` and `temp`
    (upload leftovers older than 24 h). Processing uploads and failed ones whose original is
    kept for a retry are never candidates. Other bucket keys and files are left alone and
    counted as `foreign`; with more than 10,000 bucket keys no bucket file is deleted.
  - Each run re-checks every candidate under a lock that content saves share, deletes at
    most `MEDIA_ORPHAN_MAX_PER_PASS` (50) and logs each deletion. The last run's counts are
    kept in the `media.orphans` setting.
  - Local drafts live in the browser, so the server does not see them. "Clean up now" leaves
    out files the browser's own drafts name; the daily run cannot, so save a draft post to keep
    its images.
  - The daily run is off until turned on (`PUT /media/orphans/auto`). It deletes nothing when
    it would delete more than max(10, half) of the media rows, or more than max(10, half) of
    the stored files (bucket root objects plus `MEDIA_DIR` files), or any file while the media
    table is empty, or when it finds no references while 3 or more files are in use.
  - The bucket and `MEDIA_DIR` must belong to this database only. Never point another
    deployment (staging, a preview, a fresh database) at them: its cleanup would see every
    file as unused.
- **URLs:** with `MEDIA_PUBLIC_URL` set, media URLs are `MEDIA_PUBLIC_URL/<file>`. On startup,
  stored `/media/<file>` links (post parts, covers, project bodies, the profile) are rewritten
  to it once. `/media/<file>` still 301-redirects for old external links.

## Hooks — `/api/hooks`

`POST /inbound-email` — called by the Cloudflare Email Worker with the raw RFC 822 message as
the body. Headers: `X-Envelope-To`, `X-Envelope-From`, `X-Timestamp` (unix seconds),
`X-Signature` = hex(HMAC-SHA256(`INBOUND_WEBHOOK_SECRET`, timestamp + "." + X-Envelope-To + "\n" + body)). The
timestamp must be within 5 minutes. Responses: `204` handled or dropped, `401` bad signature,
`503` retryable.
