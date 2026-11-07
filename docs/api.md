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
| GET | `/posts` | `tag`, `q` (title, excerpt and part text), `page` (1+), `limit` (1–50, default 10) | `PostSummary[]` + `meta` (one entry per post) |
| GET | `/posts/{slug}` | `part` (1+, default 1) | `PostDetail` with the requested part (published and `published_at <= now` only; 404 if the part does not exist) |
| GET | `/tags` | – | `TagCount[]` |
| POST | `/contact` | `ContactInput` | `201 { ok: true }`; 422 `validation` with message; 429 `rate_limited` (5/hour/IP) |
| POST | `/events` | `BeaconEvent` (text/plain or JSON, max 2 KB) | `202` empty |
| GET | `/listening/now-playing` | – | `NowPlaying \| null` (cached 5 s) |
| GET | `/listening/recent` | `limit` (1–20, default 6) | `RecentPlay[]` (cached 60 s) |
| GET | `/listening/top-artists` | `range` (`7d`,`30d`,`90d`), `limit` (1–20) | `TopArtist[]` (cached 5 min) |

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
| GET | `/posts` | `state`, `q` | `AdminPost[]` |
| POST | `/posts` | `PostInput` | `AdminPost` (409 `slug_taken`) |
| GET | `/posts/{id}` | – | `AdminPost` |
| PUT | `/posts/{id}` | `PostInput` | `AdminPost` |
| DELETE | `/posts/{id}` | – | `204` |
| POST | `/posts/preview` | `{ markdown }` | `PreviewResult` |
| GET | `/media` | `page`, `limit` | `Media[]` + `meta` |
| POST | `/media` | multipart `file` (image/*, pdf; ≤ MAX_UPLOAD_MB) | `Media` |
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

## Hooks — `/api/hooks`

`POST /inbound-email` — called by the Cloudflare Email Worker with the raw RFC 822 message as
the body. Headers: `X-Envelope-To`, `X-Envelope-From`, `X-Timestamp` (unix seconds),
`X-Signature` = hex(HMAC-SHA256(`INBOUND_WEBHOOK_SECRET`, timestamp + "." + X-Envelope-To + "\n" + body)). The
timestamp must be within 5 minutes. Responses: `204` handled or dropped, `401` bad signature,
`503` retryable.
