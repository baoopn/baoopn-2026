# Deployment

The site runs on Coolify as one Docker Compose resource
(`docker-compose.coolify.yml`) with three services:

| Service | Image | Exposure |
|---|---|---|
| `postgres` | `postgres:17-alpine`, volume `baoopn-postgres-data` | internal |
| `api` | `deploy/docker/api.Dockerfile` (Go, distroless, non-root), volume `baoopn-media` on `/data/media` (fallback media store) | internal only (no domain) |
| `web` | `deploy/docker/web.Dockerfile` (SvelteKit adapter-node, non-root) | `https://example.com` → port 3000 |

The web server is the only public service. It proxies `/api/*` and `/media/*` to
`http://api:8080` and adds the client IP/country with `INTERNAL_API_TOKEN`.
Coolify builds both images on the server from the repository, so no registry is used.

Uploaded media live in the Cloudflare R2 bucket `baoopn-portfolio`, served from
its custom domain `https://media.baoopn.com` (see *Media bucket* below). Without
the S3 variables the api keeps them on the `baoopn-media` volume.

`docker-compose.yml` is for local development: Postgres and RustFS (an
S3-compatible stand-in for R2; a one-shot `rustfs-init` creates the private
`baoopn-dev` bucket) by default, or the whole stack with
`docker compose --profile full up --build` (site on :3000).

## CI/CD (GitHub Actions)

`main` changes only through merged pull requests with green CI.

- **`ci.yml`** runs on PRs and on `main`:
  - Go: `go mod tidy` drift check, gofmt, `go vet`, `go test -race` against a
    `postgres:17-alpine` service container (`TEST_DATABASE_URL`), `go build`.
  - Web: `pnpm check`, `pnpm build`.
  - Worker: `node --test workers/inbound-email/test`.
  - `docker compose config` on both compose files.
  - Docker builds of both images (PRs only, no push).
- **`release.yml`** runs on push to `main` (or manually). It reuses CI, then asks
  Coolify to deploy the resource and waits until the deployment finishes. A
  commit on `main` that did not come from a merged PR is tested but never deployed.
  The deploy job is **skipped while `vars.COOLIFY_RESOURCE_UUID` is unset**, so
  nothing is published until you decide to go live.

Repository settings (Settings → Secrets and variables → Actions):

| Kind | Name | Value |
|---|---|---|
| Secret | `COOLIFY_TOKEN` | Coolify API token with deploy permission |
| Variable | `COOLIFY_URL` | `https://<your coolify host>` |
| Variable | `COOLIFY_RESOURCE_UUID` | UUID of the baoopn resource (set this last, when publishing) |

## Coolify setup

1. **Create the resource.** Project → Environment → *New resource* → Git
   repository `baoopn/baoopn-2026`, branch `main`, build pack *Docker Compose*,
   compose location `/docker-compose.coolify.yml`. Enable *Include Source
   Commit in Build* so the admin dashboard shows the version.
2. **Domain.** On the `web` service only, set `https://example.com` (add
   `https://www.example.com` too if you want it). Traefik terminates TLS. Leave
   the `api` service without a domain.
3. **Generated secrets.** Coolify fills these on first deploy; don't set them by hand:
   `SERVICE_USER_POSTGRES`, `SERVICE_PASSWORD_POSTGRES`,
   `SERVICE_PASSWORD_64_INTERNAL` (→ `INTERNAL_API_TOKEN`),
   `SERVICE_PASSWORD_64_SECRETS` (→ `SECRETS_KEY`; keep it stable: it salts visitor hashes),
   `SERVICE_URL_WEB`.
4. **Variables to set by hand** (Environment Variables tab):

   | Variable | Value |
   |---|---|
   | `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY` | production instance of the Clerk app "baoopn" |
   | `ADMIN_ALLOWED_EMAILS` | your admin email(s); `public_metadata.role = admin` also works |
   | `SMTP_PASSWORD` | **the useSend API key** for `mail.example.com` (see below) |
   | `INBOUND_MAIL_DOMAIN` | `reply.example.com` (enables reply threading) |
   | `INBOUND_WEBHOOK_SECRET` | the value in `.secrets/inbound.env` (already set on the worker) |
   | `GITHUB_TOKEN` | optional fine-grained token (public repos, read) |
   | `TURNSTILE_SECRET_KEY`, `PUBLIC_TURNSTILE_SITE_KEY` | optional contact-form captcha |
   | `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `MEDIA_PUBLIC_URL` | R2 media bucket (see below) |

   Everything else (`SMTP_HOST`, `SMTP_PORT`, `MAIL_FROM`, `NOTIFY_EMAIL`,
   `SPOTIFY_API_URL`, …) has a production default in the compose file and can be
   overridden the same way. Every variable is described in `.env.example`.
5. **Deploy** once from Coolify to check it, then copy the resource UUID into
   the `COOLIFY_RESOURCE_UUID` repository variable when you want merges to deploy.

### Where the useSend key goes

Mail is off until a key is set: the contact form still stores messages, and
replies/test emails return `mail_disabled`.

1. In useSend, verify the domain `mail.example.com` and create an API key
   (Settings → API keys).
2. In Coolify, set `SMTP_PASSWORD` on the resource to that key and redeploy.
   (e.g. `SMTP_HOST=smtp.example.com`, `SMTP_PORT=2587`, `SMTP_USER=<useSend SMTP user>`,
   `MAIL_FROM=Jane Doe <jane@mail.example.com>`).
3. In the admin dashboard, Settings → *Send test email*.

## Media bucket (Cloudflare R2)

Production media are in the R2 bucket `baoopn-portfolio`, with public access
through the custom domain `media.baoopn.com` (R2 → bucket → Settings → Custom
Domains). The api uses its own R2 API token, scoped to that bucket only
(R2 → Manage API tokens → *Object Read & Write*, applied to `baoopn-portfolio`
only). Its values live only in Coolify's environment and `.secrets/`.

| Variable | Value |
|---|---|
| `S3_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` |
| `S3_REGION` | `auto` (default) |
| `S3_BUCKET` | `baoopn-portfolio` |
| `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` | the bucket-scoped token's S3 credentials |
| `S3_FORCE_PATH_STYLE` | `false` (default; R2 uses virtual-hosted URLs) |
| `MEDIA_PUBLIC_URL` | `https://media.baoopn.com` (no trailing slash) |

The bucket is used only when the endpoint, bucket and both keys are set; a partial
set logs a warning and keeps the volume. With it on:

- Uploads keep the same validation, size limit, sha256 dedupe and filenames, and are
  stored with `Cache-Control: public, max-age=31536000, immutable`; SVGs also get
  `Content-Disposition: attachment`, so a script in an SVG opened directly on
  `media.baoopn.com` (a sibling of the site) never runs.
- Media URLs become `https://media.baoopn.com/<file>`, and `/media/<file>` answers
  `301` there, so links already in posts and the profile keep working. Without
  `MEDIA_PUBLIC_URL` the api streams `/media/<file>` from the bucket instead (the
  default in development).
- Deleting a media item removes the object too.
- On every start, a background job (15-minute timeout) copies each file in the
  media table that is on the volume but not in the bucket (`HeadObject` check),
  logging `media migration done` with counts. It never blocks or fails the boot and
  keeps the local files; once it reports `uploaded=0 failed=0` the volume is only a
  backup.

No bucket CORS rule is needed: images are loaded with plain `<img>` tags and
uploads go through the api. Add one (GET from `https://baoopn.com`) only if the
site ever fetches media with `fetch()` or `crossorigin`.

## DNS (Cloudflare, your zone)

| Record | Name | Value | Notes |
|---|---|---|---|
| A (and/or AAAA) | `example.com` | the Coolify server's IP | proxied (orange cloud) is fine; the web server reads `CF-Connecting-IP` / `CF-IPCountry` |
| CNAME or A | `www` | `example.com` / server IP | optional; add the domain in Coolify too |
| MX | `reply` | Cloudflare Email Routing (`route1/2/3.mx.cloudflare.net`) | added by Email Routing when you enable it for the subdomain |
| TXT | `reply` | `v=spf1 include:_spf.mx.cloudflare.net ~all` | added by Email Routing |
| (useSend) | `mail` | the SPF/DKIM/MX records useSend shows for `mail.example.com` | sending domain |

Cloudflare SSL mode should be *Full (strict)*: Traefik serves a Let's Encrypt
certificate. If you proxy through Cloudflare, Let's Encrypt's HTTP challenge
still works through the proxy.

## Inbound email (Cloudflare Email Worker)

Visitor replies go to `reply+<token>@reply.example.com`. Cloudflare Email
Routing hands them to the worker in `workers/inbound-email`, which signs the
raw message and POSTs it to `https://example.com/api/hooks/inbound-email`.

1. Cloudflare dashboard → example.com → Email → Email Routing → enable it for
   the `reply.example.com` subdomain (this adds the MX/TXT records above).
2. Deploy the worker (done 2026-11-07 through the API; redo with wrangler if needed):
   ```sh
   cd workers/inbound-email
   npx wrangler login
   npx wrangler deploy
   npx wrangler secret put INBOUND_WEBHOOK_SECRET   # value from .secrets/inbound.env
   ```
   The API's `INBOUND_WEBHOOK_SECRET` (Coolify env) must match it exactly.
3. Email Routing → Settings → enable *Subaddressing*, then Routes → custom address
   `reply@reply.example.com` → *Send to a Worker* → `baoopn-inbound-email`
   (subaddressing makes `reply+<token>@reply.example.com` match it). Do **not** use
   the zone catch-all, and never touch the root domain's MX (your existing mailbox provider).
4. Set `INBOUND_MAIL_DOMAIN=reply.example.com` and `INBOUND_WEBHOOK_SECRET` on the
   Coolify resource and redeploy. While staging, point the worker's `API_URL` var at
   the staging host (`https://preview.example.com/api/hooks/inbound-email`).

The worker rejects unknown addresses and oversized messages permanently, and
retries transient API failures three times before letting Cloudflare report a
temporary failure.

## Operations

- **Logs:** Coolify → resource → service → Logs (`LOG_FORMAT=json`).
- **Health:** the api image probes `/healthz` with `api healthcheck`; the web
  image fetches a static asset.
- **Backups:** enable Coolify's scheduled backups for the `postgres` service. Media
  are in R2 (`baoopn-portfolio`); the `baoopn-media` volume keeps the files from
  before the migration and is the store when R2 is not configured.
- **Rollback:** redeploy an older commit from Coolify's deployment list.

## Branch protection

`main` only changes through pull requests with green CI (squash or rebase merge).
While the repository is private on a free plan GitHub cannot enforce rulesets, so
the rule is enforced by `.githooks/pre-push` (run `git config core.hooksPath
.githooks` once per clone) and by the deploy gate in `release.yml`, which never
deploys a commit that did not come from a merged PR.

When the repository becomes public (or the account has Pro), apply the saved ruleset:

```sh
gh api -X POST repos/<owner>/<repo>/rulesets --input .github/rulesets/main.json
```

Before making it public, run `sh scripts/check-public.sh` (CI runs it on every PR).
