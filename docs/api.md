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
| GET | `/analytics` | `range` (`24h`,`7d`,`30d`,`90d`), `tz` (IANA) | `AnalyticsReport` |
| GET | `/settings` | – | `SiteSettings` |
| PUT | `/settings` | `SiteSettings` | `SiteSettings` |
| POST | `/settings/test-email` | – | `{ sent_to }` |
| GET | `/brand` | – | `AdminBrandAsset[]` (slot order) |
| PUT | `/brand/{slot}` | multipart `file` (request ≤ 1 MiB) — see *Brand assets* | `AdminBrandAsset` (`custom: true`; the same bytes as the current file change nothing) |
| DELETE | `/brand/{slot}` | – | `AdminBrandAsset` (`custom: false`, back to the default; idempotent); 404 `not_found` for an unknown slot |

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

- **Errors:** 400 `bad_request` (not multipart, no `file` field), 404 `not_found` (unknown
  slot), 413 `too_large`, 415 `unsupported_type` (e.g. "favicon_ico needs an ICO file"),
  422 `validation` with the reason (e.g. "apple_touch_icon must be 180×180 (got 192×192)"),
  503 `storage_unavailable`.
- **SVG rules** (rejected, never rewritten): well-formed XML with an `<svg>` root and a valid
  viewBox; no DOCTYPE/ENTITY and no processing instruction other than `<?xml …?>`; no
  `script`, `foreignObject`, `iframe`, `embed`, `object`, `audio`, `video`, `canvas`,
  `handler` or `listener` elements; no `on*` attributes; `href`/`xlink:href` only `#id` or
  `data:image/(png|jpeg|webp|gif);base64,`; no `javascript:`, `@import` or `url(` other than
  `url(#id)` in any attribute or `<style>`. Editor attributes (Inkscape, Sodipodi) are fine.
- **Storage:** files are stored byte for byte (never converted, no `media` row) under
  `brand/<stem>.<16 hex of sha256>.<ext>` in the bucket (or `MEDIA_DIR/brand/`), so each
  version has its own immutable URL (`MEDIA_PUBLIC_URL/brand/…`, else `/media/brand/…`).
  The defaults are embedded in the API (`internal/brand/defaults/`) and uploaded on startup
  when missing; an override whose file is gone falls back to the default. The `brand`
  setting holds only overrides and replaced files. A replaced file is deleted 7 days later;
  a current or default file never is. The orphan cleanup ignores `brand/`.
- The web app reads `GET /api/v1/brand` (cached 60 s) and redirects `/favicon.ico`,
  `/favicon.svg` and `/apple-touch-icon.png` to the current files.

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
