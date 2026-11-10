<p align="center">
  <a href="https://baoopn.com">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://baoopn.com/api/v1/brand/wordmark_inverse">
      <img src="https://baoopn.com/api/v1/brand/wordmark_primary" alt="baoopn" width="200">
    </picture>
  </a>
</p>

# baoopn-2026

The source of my portfolio and blog at [baoopn.com](https://baoopn.com): a Go API and a
SvelteKit site with an admin dashboard.

## What it does

- **Portfolio.** Projects sync from my GitHub repos. I pick which ones show, feature a few, and
  link each to a blog post.
- **Blog.** Posts are written in Markdown in the admin editor, with live preview, image upload
  and scheduled publishing. A post can have up to 20 parts, and posts are split into Tech and Life.
- **Contact as email threads.** The contact form starts a thread. I reply from the dashboard, the
  visitor gets an email, and their reply lands back in the same thread.
- **Cookieless analytics.** Page views, visitors, time on page, referrers, devices and
  countries. No cookies and no stored IPs.
- **Listening.** A now-playing card and recent plays from my
  [spotify-tracker](https://github.com/baoopn/spotify-tracker) service.
- **Brand files.** The logos, icons and email logo live in the admin. I can replace or reset each
  one, and a public API serves them to other sites. It holds two brands: baoopn, and the
  listening sub-brand that listening.baoopn.com uses. See [docs/brand-kit](docs/brand-kit).
- **Footer credit.** Client sites I build fetch a "by baoopn" credit from one endpoint. I set the
  text and logo style in the admin, and only listed sites get it. See
  [docs/credit.md](docs/credit.md).
- **Backups.** An encrypted Postgres dump goes off-site once a day. The admin shows the status and recent
  runs, sets the schedule, and has a button to back up now. See [docs/backup.md](docs/backup.md).
- **SEO.** Each page has a canonical URL and structured data, and the admin is kept out of search.
- **Admin API keys.** The account page embeds Clerk's profile panel, where I can make API keys.
  Scripts can then call the admin API with a key instead of a session.

## Stack

| Part | Tech |
|---|---|
| API | Go, [chi](https://github.com/go-chi/chi), PostgreSQL ([pgx](https://github.com/jackc/pgx), [goose](https://github.com/pressly/goose) migrations), goldmark + chroma for Markdown |
| Web | SvelteKit 3, Svelte 5, Tailwind CSS 4, adapter-node (SSR) |
| Admin auth | [Clerk](https://clerk.com) session tokens, verified in Go |
| Media | S3-compatible storage: Cloudflare R2 in production, [RustFS](https://rustfs.com) in development, local disk as a fallback |
| Mail | SMTP through a self-hosted [useSend](https://usesend.com); replies come back through a Cloudflare Email Worker |
| Deploy | Docker, [Coolify](https://coolify.io), GitHub Actions |

## How it fits together

```
browser ──► web (SvelteKit SSR, :3000) ──► api (Go, :8080) ──► PostgreSQL
                │  proxies /api/* and /media/*        ├──► S3 bucket (media)
                │                                     ├──► SMTP (useSend)
                └─ the only public service            └──► GitHub API, spotify-tracker

email reply ──► Cloudflare Email Routing ──► Email Worker ──► POST /api/hooks/inbound-email (HMAC-signed)
```

The web server is the only public service. It proxies `/api` and `/media` to the API on the
internal network and forwards the visitor's IP and country with a shared token.

## Repository layout

```
cmd/api/              API entry point and wiring
internal/             feature packages (blog, projects, contact, mail, analytics, brand, credit, ...)
migrations/           goose SQL migrations, embedded in the binary
seed/                 first-run content (profile, posts and their screenshots)
web/                  SvelteKit app: public site in routes/(site), admin in routes/admin
workers/inbound-email Cloudflare Email Worker for email replies
deploy/docker/        Dockerfiles for the API and the web app
deploy/backup/        encrypted off-site Postgres backups (docs/backup.md)
docs/                 API contract (api.md), deployment, backups, credit and the brand kit
```

## Run it locally

You need Go 1.27+, Node 22+, pnpm 11 and Docker.

```sh
cp .env.example .env          # then fill in what you need (see below)
make deps                     # Postgres on :5433, RustFS (S3) on :9000 with a dev bucket
make run-api                  # API on :8080, runs migrations and seeds content
make web-dev                  # site on http://localhost:5173, admin at /admin
```

In `.env`, set `S3_SECRET_ACCESS_KEY` to any local value before `make deps`. Add Clerk development
keys to sign in to the admin, and SMTP settings to send mail. Features with empty settings
turn themselves off.

Or run the whole stack in Docker with `docker compose --profile full up --build` (site on :3000).

## Tests and checks

```sh
make pg                       # embedded Postgres on :5434 for DB tests (keep it running)
make test-db                  # Go tests against it
make lint                     # go vet + svelte-check
node --test workers/inbound-email/test
sh scripts/check-public.sh    # no machine paths, keys or secrets in tracked files
make backup-test              # backup image: backup/restore roundtrip with throwaway containers
```

CI runs all of these on every pull request, plus Docker builds of both images.

## Deploy

Merging a pull request into `main` deploys. GitHub Actions runs CI, checks that the commit came
from a merged PR, and then triggers the Coolify resource, which builds both images on the server.
Direct pushes to `main` are blocked by `.githooks/pre-push` (enable it with
`git config core.hooksPath .githooks`). See [docs/deployment.md](docs/deployment.md) for the
Coolify setup, DNS, the media bucket and the email worker.

## API

The HTTP contract is in [docs/api.md](docs/api.md), and the TypeScript types the web app uses are
in [`web/src/lib/api/types.ts`](web/src/lib/api/types.ts).

## License

The code is MIT licensed; see [LICENSE](LICENSE). The blog posts, photos and profile text under `seed/` are not: they are all rights reserved. See [seed/LICENSE.md](seed/LICENSE.md).
