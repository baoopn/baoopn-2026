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
| PATCH | `/projects/{id}` | `ProjectPatch` | `AdminProject` |
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
- **URLs:** with `MEDIA_PUBLIC_URL` set, media URLs are `MEDIA_PUBLIC_URL/<file>`. On startup,
  stored `/media/<file>` links (post parts, covers, project bodies, the profile) are rewritten
  to it once. `/media/<file>` still 301-redirects for old external links.

## Hooks — `/api/hooks`

`POST /inbound-email` — called by the Cloudflare Email Worker with the raw RFC 822 message as
the body. Headers: `X-Envelope-To`, `X-Envelope-From`, `X-Timestamp` (unix seconds),
`X-Signature` = hex(HMAC-SHA256(`INBOUND_WEBHOOK_SECRET`, timestamp + "." + X-Envelope-To + "\n" + body)). The
timestamp must be within 5 minutes. Responses: `204` handled or dropped, `401` bad signature,
`503` retryable.
