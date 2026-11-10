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
| GET | `/brand` | – | `Brand` (current URL, type and size of every brand asset; `Cache-Control: public, max-age=60`, `Vary: Origin`; CORS for `BRAND_CORS_ORIGINS`; the defaults when the database is unreachable) |
| GET | `/brand/{slot}` | – | `302` to the slot's current file (`Cache-Control: public, max-age=300`, `Vary: Origin`; CORS for `BRAND_CORS_ORIGINS`); 404 `not_found` for an unknown slot |
| OPTIONS | `/brand`, `/brand/{slot}` | – | `204` CORS preflight (headers only for a listed origin) |
| GET | `/brand/listening` | – | `ListeningBrand` (the same for the listening sub-brand; `Cache-Control: public, max-age=60`, `Vary: Origin`; CORS for `BRAND_CORS_ORIGINS`) |
| GET | `/brand/listening/{slot}` | – | `302` to the listening slot's current file (`Cache-Control: public, max-age=300`, `Vary: Origin`; CORS for `BRAND_CORS_ORIGINS`); 404 `not_found` for an unknown slot |
| OPTIONS | `/brand/listening`, `/brand/listening/{slot}` | – | `204` CORS preflight (headers only for a listed origin) |
| GET | `/credit` | `?logo=wordmark\|mark&color=color\|mono` (both optional) | `Credit` (the footer credit for client sites; `logo` picks the variant instead of the stored default, `color` overrides that variant's style for this request; `Cache-Control: public, max-age=300`, `Vary: Origin`; CORS for listed origins only, see *Credit*) |
| OPTIONS | `/credit` | – | `204` CORS preflight (headers only for a listed origin) |
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

## Admin — `/api/admin` (Bearer Clerk session token or user API key)

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
| GET | `/brand/listening` | – | `AdminBrandAsset[]` of the listening sub-brand (slot order) |
| PUT | `/brand/listening/{slot}` | multipart `file`, as for `/brand/{slot}` | `AdminBrandAsset` |
| DELETE | `/brand/listening/{slot}` | – | `AdminBrandAsset` (back to the default; idempotent) |
| GET | `/credit` | – | `AdminCredit` (see *Credit*) |
| PUT | `/credit` | `CreditInput` | `AdminCredit`; 422 `validation` with a message that starts with the field name |
| GET | `/backups` | – | `BackupStatus` (see *Backups*) |
| PUT | `/backups/settings` | `BackupSettingsInput` (a missing field is `null`) | `BackupStatus`; 422 `validation` names the field and its range |
| DELETE | `/backups/settings` | – | `BackupStatus` (the four overrides are cleared; `paused` stays) |
| PUT | `/backups/pause` | `{ paused }` | `BackupStatus` (sets or clears `paused_at`) |
| POST | `/backups/run` | – | `202 BackupRun` (`status: "queued"`); 409 `busy`, `agent_offline` or `not_configured` |

### Calling the admin API with an API key

Scripts can call the admin API with a Clerk user API key instead of a session token.

- Create the key in `/admin/account`, on the API keys tab. Only keys that belong to an admin work.
- Send it as `Authorization: Bearer <key>`. The API sees the `ak_` prefix and checks the key with Clerk.
- The API returns 401 `unauthorized` for an invalid, revoked or expired key, 403 `forbidden`
  for a user who is not an admin, and 502 `auth_unavailable` when it cannot reach Clerk.
- The API caches a checked key for up to 60 s, so a revoked key can keep working for up to 60 s.
- The same limit of 600 requests per minute per IP applies.

```
curl -H "Authorization: Bearer $ADMIN_USER_API_KEY" https://baoopn.com/api/admin/me
```

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

- **Brands:** the API keeps two brands. `baoopn` is the site itself: routes `/brand`, settings
  key `brand`, files under `brand/`. `listening` is the sub-brand of listening.baoopn.com:
  routes `/brand/listening` (public and admin, same verbs, errors and cache headers as
  `baoopn`), settings key `brand_listening`, files under `brand/listening/`. Each brand's
  store only reads, keeps and deletes its own keys, so a reset or prune of one brand never
  touches the other. A slot name outside the brand's table is 404 `not_found` (for example
  `monogram_primary` under `/brand/listening`). `GET /api/v1/brand/listening/` (no slot) and
  `PUT /api/admin/brand/listening` are 404 too. The public routes send CORS headers to the origins
  in `BRAND_CORS_ORIGINS` (default `https://listening.baoopn.com`), with the same exact-match
  rules as *Credit*. The admin routes never send them. The compose files turn an empty value
  into the default; to send no CORS headers, set `BRAND_CORS_ORIGINS` empty in the API's own
  environment, outside compose. The `/{slot}` `302` works for `<img>` and `<link>`, but a
  `fetch()` that follows it also needs CORS from the media host (media.baoopn.com already
  sends it).
- **Admin fields:** each `AdminBrandAsset` has a `group` (`favicons`, `logos`, `icons`,
  `social` or `email`) that the admin page uses for its sections, and a `kind` of `icon`,
  `logo` or `image` (`image` is the social share image). The list is in table order, which
  is also group order.
- **Keys:** `media.BrandKeyRe` accepts `brand/<stem>.<16 hex>.<ext>` and
  `brand/listening/<stem>.<16 hex>.<ext>`. No other subfolder, case or `..` is accepted.

#### baoopn slots

- **Slots** (fixed order), with what each accepts (type sniffed from the bytes, never the name):

  | slot | type | rule | max |
  |---|---|---|---|
  | `favicon_ico` | ICO | 1–16 images, each inside the file; must contain 16×16, 32×32 and 48×48 | 256 KiB |
  | `favicon_svg` | SVG | square viewBox | 256 KiB |
  | `apple_touch_icon` | PNG | exactly 180×180 | 512 KiB |
  | `wordmark_primary` | SVG | viewBox | 256 KiB |
  | `wordmark_inverse` | SVG | viewBox (shown on dark backgrounds) | 256 KiB |
  | `wordmark_navy` | SVG | viewBox (navy glyph, shown on light backgrounds) | 256 KiB |
  | `wordmark_cream` | SVG | viewBox (cream glyph, shown on dark backgrounds) | 256 KiB |
  | `monogram_primary` | SVG | square viewBox | 256 KiB |
  | `monogram_inverse` | SVG | square viewBox | 256 KiB |
  | `monogram_navy` | SVG | viewBox (navy glyph, shown on light backgrounds) | 256 KiB |
  | `monogram_cream` | SVG | viewBox (cream glyph, shown on dark backgrounds) | 256 KiB |
  | `email_logo` | PNG | 240–1200 px wide, 60–600 px tall, 1.5:1 to 4:1 (shown 48 px tall in emails) | 256 KiB |

#### listening slots

- **Slots** (fixed order). The defaults are the files of the listening design kit, copied
  byte for byte into `internal/brand/defaults/listening/`:

  | slot | type | rule | max |
  |---|---|---|---|
  | `favicon_ico` | ICO | must contain 16×16, 32×32 and 48×48 | 256 KiB |
  | `favicon_svg` | SVG | square viewBox | 256 KiB |
  | `apple_touch_icon` | PNG | exactly 180×180 | 512 KiB |
  | `favicon_16` | PNG | exactly 16×16 | 512 KiB |
  | `favicon_32` | PNG | exactly 32×32 | 512 KiB |
  | `favicon_48` | PNG | exactly 48×48 | 512 KiB |
  | `icon_192` | PNG | exactly 192×192 | 512 KiB |
  | `icon_512` | PNG | exactly 512×512 | 512 KiB |
  | `icon_maskable_512` | PNG | exactly 512×512; keep the mark inside the central 80% circle | 512 KiB |
  | `mark_primary` | SVG | square viewBox | 256 KiB |
  | `mark_inverse` | SVG | square viewBox (shown on dark backgrounds) | 256 KiB |
  | `mark_navy` | SVG | viewBox (navy glyph, shown on light backgrounds) | 256 KiB |
  | `mark_cream` | SVG | viewBox (cream glyph, shown on dark backgrounds) | 256 KiB |
  | `wordmark_primary` | SVG | viewBox | 256 KiB |
  | `wordmark_inverse` | SVG | viewBox (shown on dark backgrounds) | 256 KiB |
  | `wordmark_navy` | SVG | viewBox (navy glyph, shown on light backgrounds) | 256 KiB |
  | `wordmark_cream` | SVG | viewBox (cream glyph, shown on dark backgrounds) | 256 KiB |
  | `icon_<name>_navy` | SVG | square viewBox (shown on light backgrounds) | 256 KiB |
  | `icon_<name>_cream` | SVG | square viewBox (shown on dark backgrounds) | 256 KiB |
  | `og_image` | PNG | exactly 1200×630 | 512 KiB |
  | `email_logo` | PNG | 240–1200 px wide, 60–600 px tall, 1.5:1 to 4:1 (shown 160 px wide in emails) | 512 KiB |

  `<name>` is one of `headphones`, `now_playing` (animated), `now_playing_static` (the same
  bars without motion, for reduced motion), `paused`, `playlist` and `track`: 12 UI icon
  slots, 31 slots in all.
- **Animated SVG:** an SVG may animate with SMIL (`<animate>`, `<set>`), as the default
  `icon_now_playing_*` does. The SVG rules below still apply, so an animation can never change
  a link. Scripts and `url(` references stay blocked.
- **Maskable icon:** the API checks only the size of `icon_maskable_512`. Nothing checks the
  safe zone, so keep the mark inside the central 80% circle yourself. The admin page shows a
  copy clipped to a circle next to the full image.
- **Caching:** `GET /api/v1/brand/listening` is cached 60 s and each `302` 300 s, so a
  client that reads the API sees a replaced file within a few minutes. The file URLs
  themselves never change and are cached for a year. listening.baoopn.com still serves its
  own copies until it reads this API. Do not add a Cloudflare cache rule for `/api/*`:
  Cloudflare ignores `Vary: Origin` and would serve one origin's CORS headers to another.

#### Both brands

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
  `brand/<stem>.<16 hex of sha256>.<ext>` in the bucket (or `MEDIA_DIR/brand/`; the
  listening brand uses `brand/listening/` and `MEDIA_DIR/brand/listening/`), so each
  version has its own immutable URL (`MEDIA_PUBLIC_URL/brand/…`, else `/media/brand/…`).
  The defaults are embedded in the API (`internal/brand/defaults/`) and uploaded on startup
  when missing; an override whose file is gone falls back to the default (only when every
  default was found or uploaded, so an unreachable or read-only bucket cannot wipe the overrides). Uploading a
  slot's default bytes is a reset. The `brand` and `brand_listening`
  settings hold only overrides and replaced files. A replaced file is deleted 7 days later;
  a current or default file never is. The orphan cleanup ignores `brand/`.
- The web app reads `GET /api/v1/brand` (cached 60 s) and redirects `/favicon.ico`,
  `/favicon.svg` and `/apple-touch-icon.png` to the current files.
- **Emails** show `email_logo` (PNG, since Gmail shows no SVG) in the header, 48 px tall and
  linked to the site; the URL is made absolute at send time (`MEDIA_PUBLIC_URL/brand/…` as is,
  else `PUBLIC_URL/media/brand/…`), so a replaced logo applies to the next email. When it has
  no absolute http(s) URL, the emails show the text badge instead. The logo is a remote image
  shared by all recipients, so its URL carries no per-recipient data, but fetching it can still
  reveal that a mail was opened.

### Credit

Client sites built by Bao show a "Designed & built by" line with the baoopn logo in their
footers. The line is a prefix, then the linked logo, then a suffix. `GET /api/v1/credit`
gives client sites the text, the link and the current logo files, so new text or a replaced
brand logo reaches every client site without a deploy there. The snippet and the setup
steps for a client site are in `docs/credit.md`.

- **Variants:** the owner sets the line once for each logo kind, `wordmark` and `mark`. Each
  variant has its own `prefix`, `suffix` and `style`. A client site picks the variant with
  `?logo=wordmark|mark`. Without it, or with any other value, the API serves the stored
  default variant (`logo`).
- **Style override:** `?color=color|mono` serves that style instead of the variant's stored
  style, for this request only. It works with or without `?logo=` (for example
  `?logo=mark&color=mono`). Both parameters need exact lowercase values; the API ignores any
  other value and never changes the stored document. `logo.style` in the answer is the style
  the API served.
- **Storage:** the `credit` settings document holds `url`, `logo` (the default variant),
  `variants` (`{"wordmark": {prefix, suffix, style}, "mark": {…}}`), `origins` and
  `updated_at`. It needs no table of its own. The API keeps the document in memory (one API
  instance) and replaces it after each save.
- **Defaults** (no document yet, or one field broken by a hand edit): url the origin of
  `PUBLIC_URL`, logo `wordmark`, both variants with prefix `Designed & built by`, an empty
  suffix and style `color`, no origins, `updated_at` null. Each field of a variant falls back
  on its own. A broken origin in a stored document is dropped. A document saved before the
  variants has a top-level `text`; the API reads it as the prefix of each missing variant
  and keeps reading the top-level `logo` as the default variant. The next save writes the
  new shape.
- **Logo:** the kind and the style pick the brand files:

  | kind | style | light background | dark background |
  |---|---|---|---|
  | `wordmark` | `color` | `wordmark_primary` | `wordmark_inverse` |
  | `wordmark` | `mono` | `wordmark_navy` | `wordmark_cream` |
  | `mark` | `color` | `monogram_primary` | `monogram_inverse` |
  | `mark` | `mono` | `monogram_navy` | `monogram_cream` |

  `color` is the filled tile; `mono` is the glyph only. The URLs are absolute
  (`MEDIA_PUBLIC_URL/brand/…`, else `PUBLIC_URL/media/brand/…`), and each version has its
  own URL. The width and height come from the SVG viewBox. `alt` is the host of
  `PUBLIC_URL` without `www.`.
- **Public answer** (`Credit`): `prefix`, `suffix`, `url`, `alt`, `logo` (`kind`, `style`,
  `light`, `dark`) and `updated_at`, all for the chosen variant. `text` is a deprecated copy
  of `prefix`, kept so old snippets still work.
- **Admin answer** (`AdminCredit`): `url`, `logo`, `variants`, `origins`, `updated_at`,
  `endpoint`, `logos` (all four files, as `logos[kind][style]`, for the previews) and
  `limits` (`prefix_max`, `suffix_max`, `url_max`, `origins_max`).
- **Fields** (`PUT /api/admin/credit` checks them in this order and reports the first problem):
  - `url`: an absolute `https` URL with a host and no user info, at most 500 bytes.
  - `logo`: `wordmark` or `mark`, the default variant.
  - `variants`: when present, exactly the keys `wordmark` and `mark` (else "variants must
    have wordmark and mark"). The API checks `wordmark` first, then `mark`, and in each one
    `prefix`, `suffix` and `style`. The message names the field, for example
    `variants.mark.suffix must be at most 80 characters on one line`.
    - `prefix` and `suffix`: 0 to 80 characters after trimming, on one line: no control
      characters, no line or paragraph separators (U+2028, U+2029) and no format characters
      such as bidi overrides or zero-width spaces.
    - `style`: `color` or `mono`. An empty style means `color`.
  - `text` (deprecated): read only when `variants` is absent. It follows the prefix rules
    and becomes the prefix of both variants, with an empty suffix and style `color`. With
    neither `variants` nor `text`, both variants get the default line. The admin app always
    sends `variants`.
  - `origins`: at most 50 after removing repeats (`null` means none). Each entry is a bare
    origin: `https://host[:port]`, or `http` for `localhost`, `127.0.0.1` and `[::1]` only.
    No path other than `/`, no query, no fragment, no user info, no `*` and no `%`. The host
    is dot-separated labels of letters, digits and `-` (an IDN must be in punycode,
    `xn--…`) or a bracketed IPv6 address without a zone. A port must be 1 to 65535; an
    empty port (`https://a.com:`) is rejected. The API stores each entry as lowercase
    `scheme://host[:port]`, writes the port without leading zeros, drops the default port
    (443 or 80) and keeps the first entry of any repeat.
  - Unknown fields give 400 `bad_request`.
- **CORS:** only the two `/api/v1/credit` routes and the public `/api/v1/brand` routes (with
  their own list, `BRAND_CORS_ORIGINS`) send CORS headers. The rest of the API stays
  same-origin. The API compares the request's `Origin`, lowercased, with each listed origin
  and accepts only an exact match. It never matches `null` and never sends
  `Access-Control-Allow-Credentials`.
  - `GET` always sends `Vary: Origin`. For a listed origin it also sends
    `Access-Control-Allow-Origin: <origin>`. Any other origin still gets the body, but the
    browser hides it from the page.
  - `OPTIONS` answers `204` with no body and always sends
    `Vary: Origin, Access-Control-Request-Method, Access-Control-Request-Headers`. For a
    listed origin it adds `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods: GET,
    OPTIONS`, `Access-Control-Allow-Headers: Content-Type` and `Access-Control-Max-Age: 86400`.
- **Errors:** when the database is unreachable, `GET /api/v1/credit` serves the defaults
  (with no origins) and `Cache-Control: public, max-age=60`, and the next request reads the
  database again; the admin routes answer 500. When a logo URL cannot be built, the public
  GET answers 500 `internal` with `Cache-Control: no-store` and no CORS header.
- **Cloudflare:** never add a Cache Rule for `/api/*`. Cloudflare ignores `Vary: Origin`, so
  a cached answer would carry one client's `Access-Control-Allow-Origin` to every other
  client. Cloudflare does not cache this JSON by default.

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
  "Not picked up by the backup service within 10 minutes". A running row left behind by a
  lost sidecar write does not count as active (and does not block a new run) once the online
  sidecar reports a state other than `running`; the sidecar fails such rows after 6 hours.
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
