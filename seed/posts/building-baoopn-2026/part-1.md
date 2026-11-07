baoopn.com is my portfolio and blog. This series is about the 2026 version: what I built, how the parts fit, and the choices I made along the way. This first part covers why I rebuilt it and the overall shape. The later parts each take one piece: the admin dashboard, the contact inbox, the analytics, and how it ships.

![The home page of baoopn.com in light mode](screenshot:public-home)

## Why rebuild

The last version (v3) was a front-end app. The blog posts lived inside it as Markdown files, split into numbered pages under a series. That worked, but every change to a post was a change to the code, followed by a deploy. I had no way to see which posts people read. A contact form had nowhere to put a message except my mailbox.

I wanted four things this time:

- An admin dashboard where I write and publish posts, manage projects and upload images.
- A contact form that turns into an email thread I can answer from the dashboard.
- Page analytics that do not need cookies or a consent banner.
- A deploy path where merging a pull request is the release.

That list needs a real backend and a database. So the rebuild is two apps, not one.

## The shape of it

| Piece | What it is |
|---|---|
| API | Go, with the chi router and pgx for Postgres |
| Web | SvelteKit 3 and Svelte 5, server-side rendered on Node, Tailwind 4 |
| Database | PostgreSQL 17 |
| Inbound mail | A Cloudflare Email Worker |
| Hosting | One Docker Compose resource on Coolify |

The Go API holds all the data and rules. The SvelteKit app renders the public pages on the server and serves the admin dashboard, which runs in the browser.

### One public service

The web server is the one service on the internet. The API has no domain. It sits on the internal Docker network, and the web server proxies `/api/*` and `/media/*` to it.

I chose this for two reasons. The browser talks to one origin, so there is no CORS setup and no second TLS certificate. And the API can trust what the proxy tells it, because nothing else can reach it.

That trust needs care. The proxy forwards the client IP and country, and the API uses them for rate limits and analytics. A visitor could send those same headers. So the proxy first deletes any copy the client sent, then adds its own:

```ts
const HOP_HEADERS = ['connection', 'keep-alive', 'transfer-encoding', 'upgrade', 'host'];
const SPOOFABLE = [
	'x-client-ip',
	'x-client-country',
	'x-internal-token',
	'cf-connecting-ip',
	'cf-ipcountry'
];

/** Copies request headers, dropping hop-by-hop and spoofable ones, then adds ours. */
function forwardHeaders(from: Headers, request: Request, ip: string): Headers {
	const headers = new Headers(from);
	for (const k of [...HOP_HEADERS, ...SPOOFABLE]) headers.delete(k);
	for (const [k, v] of clientHeaders(request, ip)) headers.set(k, v);
	return headers;
}
```

The proxy also adds a shared secret, `X-Internal-Token`. On the Go side, a middleware compares it in constant time. If it does not match, the API drops the country header and ignores the client IP. Cloudflare's own IP and country headers count only when I turn on a flag, and that flag needs a firewall that admits Cloudflare alone. Without it, anyone who found the origin could fake them.

Server-side page loads take a shorter path. A SvelteKit `handleFetch` hook sends their `/api/` calls to the Go API, with the same header rules, so a page render does not loop back through the public proxy.

### The API

The API is a set of modules. Each one mounts its routes on up to three routers:

```go
// Module mounts a feature's routes. Any method may be a no-op.
type Module interface {
	Public(r chi.Router) // /api/v1
	Admin(r chi.Router)  // /api/admin (authenticated)
	Hooks(r chi.Router)  // /api/hooks (signed webhooks)
}
```

Public routes serve the site: profile, projects, posts, tags, the contact form and the analytics beacon. Admin routes sit behind Clerk auth. Hooks are for signed webhooks, which today means inbound email. Every response uses one envelope: `{ "data": ..., "meta": ... }` on success and `{ "error": { "code", "message" } }` on failure. The TypeScript types in the web app are the source of truth for the shapes.

There is also a small module that shows what I am listening to. It reads from a separate Spotify tracker I run, with short caches in front of it.

### One binary

The API builds to a single static binary. It embeds three things:

- The SQL migrations, run with goose on start.
- The v3 blog posts and their images, imported once on first start and guarded by a flag in the settings table.
- Time zone data, through `import _ "time/tzdata"`. The runtime image has no zoneinfo, and the analytics reports load IANA zones.

```go
// Package migrations embeds the goose SQL migrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

A fresh database plus the binary gives a working site with my old posts in it. Old v3 URLs had the form `/blog/<series>/<page>`. A route in the web app asks the API which new post a legacy path maps to and answers with a 301, so old links keep working.

### Background jobs and shutdown

Two jobs run next to the HTTP server: the analytics recorder, which flushes counts to Postgres every minute, and the GitHub sync. They run on their own context, not the signal context. When the process gets SIGTERM, the HTTP server shuts down first, so beacons in flight still land. Then the contact module waits for mail it is still sending. Then the jobs stop, and the recorder does one last flush. The database pool closes last.

```go
// Background jobs run on their own context so that they outlive the
// HTTP server's shutdown (in-flight beacons are still recorded).
jobCtx, cancelJobs := context.WithCancel(context.Background())
defer cancelJobs()
var wg sync.WaitGroup
wg.Go(func() { mods.rec.Run(jobCtx) })
if cfg.GitHubSyncInterval > 0 {
	wg.Go(func() { mods.syncer.Run(jobCtx, cfg.GitHubSyncInterval) })
}
```

### Config

All config is environment variables. The API reads them in one package. The web app uses SvelteKit 3's explicit env modules. Every variable has an entry in `.env.example` and in the compose files. Features turn off when their config is missing: no Clerk key means no admin API, no SMTP key means messages are stored but not mailed. That made it easy to bring the site up one piece at a time.

## What comes next

Part 2 covers the admin dashboard: how Go checks Clerk tokens, the Markdown editor, and the GitHub sync that fills the projects page.
