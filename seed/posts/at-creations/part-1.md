AT Creations is a shop in Vancouver that sells crafts made by hand in Vietnam: wooden models, crochet, pottery and pop-up cards. It also runs workshops where you paint your own plaster figurine. The website is a product catalogue in two languages, English and Vietnamese. There is no cart and no login for shoppers. People browse, search, read about a product, and then contact the store.

Behind the catalogue there is a staff dashboard. Staff add products, upload photos, set prices and stock, and manage the team. One API serves both sides.

I built an earlier version of this system and wrote about it here before. This post replaces that one. I rewrote the whole thing, and this series explains what I built and why.

## The old version

The first version was a set of apps I called `atstore`. The API was Express and TypeScript, backed by MongoDB, with images in a Cloudflare R2 bucket. It worked, and the store used it. But when I went back to it with fresh eyes, I found problems that were hard to patch one by one.

I wrote them all down in `docs/SECURITY.md`, in a table of old behaviour, the risk, and what the new system does instead. A few of the worst rows:

- The guest order endpoint accepted `price`, `discount`, `total` and `status` from the request body. Anyone could buy at $0 or mark an order as completed.
- A `REGISTER_SECRET` let anyone who knew it sign up with any role, including admin. It was compared with `===`.
- Update endpoints passed raw request bodies to `findByIdAndUpdate`. That is mass assignment: a client could set fields like `role` or `apiKeys`, or slip in Mongo operators.
- Search and filters built regexes from query params without escaping them. That opens the door to regex injection and ReDoS (a regex that takes forever to run).
- Order access codes were stored and compared in plain text.
- Stock was checked and then decremented in two steps, with no transaction, so two orders could oversell the same item.
- Redis listened on all interfaces with protected mode off and a default password.
- Admin pages were protected only in the browser.

There were smaller oddities too. The old service generated Gemini embeddings for every product but never queried them. Product lists sent those 768-number vectors to the browser anyway.

None of this was exotic. It was the kind of debt that piles up when you ship fast and learn as you go. But it touched money, roles and customer data, so I decided a clean rewrite was safer than a long list of patches. I could also fix the deploy pipeline, which built on the server over SSH with no pinned host key.

## What I built instead

The new system is one monorepo with three apps and one shared package:

| App | Path | Stack |
|---|---|---|
| API | `apps/api` | Go, chi, MongoDB 8, Redis 8, Clerk, Cloudflare R2 |
| Storefront | `apps/storefront` | SvelteKit 2, Svelte 5, Tailwind 4, Paraglide |
| Admin | `apps/admin` | SvelteKit 2, Svelte 5, svelte-clerk, TanStack Query |
| API types | `packages/api-types` | Generated from `openapi.yaml` |

The pieces fit together like this:

```
browser ──TLS──▶ proxy ──▶ storefront (SvelteKit SSR) ──http──▶ api (Go) ──▶ MongoDB 8
                       ──▶ admin (SvelteKit + Clerk)                │
                       ──▶ api                                      ├──▶ Redis 8 (cache, limits)
                                                                    ├──▶ Clerk (staff sign-in)
                                                                    └──▶ R2 (images) ──▶ CDN
```

The **API** is the single source of truth. Public catalogue routes live under `/v1/...`. They take no credentials and can be cached. Admin routes live under `/v1/admin/...` and need a Clerk session token plus the right permission.

The **storefront** renders every page on the server. It calls the API over the internal network, so each page ships with real titles, descriptions and links for search engines.

The **admin** is a single-page app. Clerk handles sign-in, and the browser calls the API with a short-lived token.

**MongoDB** runs self-hosted as a single-node replica set. A replica set is what MongoDB needs for transactions, even with one node. **Redis** holds only short-lived data: cached responses, rate-limit counters and locks. Nothing in it is the only copy of anything.

There is also a small fourth service, an **image worker**. It is a separate Go binary in the same module that turns uploaded photos into WebP. I cover it in part 3.

## Why Go and SvelteKit

I chose Go for the API because the job is mostly I/O and rules: read JSON, check it, talk to MongoDB, write JSON. Go makes that plain. The standard library covers HTTP well, the binary is static, and the container image can be distroless (no shell, no package manager). Goroutines also made the background parts easy: an upload queue, scheduled jobs and the image worker.

I kept SvelteKit for both frontends. The storefront needs server rendering for SEO, and the admin needs a fast client app. SvelteKit does both, and using it twice means one set of habits, one lint setup and one build tool.

## The repo layout

```
apps/
  api/            Go API (cmd/api), image worker (cmd/imgworker), migration (cmd/migrate)
    openapi.yaml  the HTTP contract
    proto/        the gRPC contract for the image worker
  storefront/     public catalogue, SSR, en/vi
  admin/          staff dashboard
packages/
  api-types/      TypeScript types generated from openapi.yaml
deploy/           production compose stack, backups, scripts
docker/dev/       local dev stack helpers
docs/             architecture, security model, migration notes
```

Locally, one command starts everything in Docker with live reload: MongoDB, Redis, a local S3 server (RustFS) standing in for R2, the API, the storefront and the admin.

```bash
pnpm dev:stack        # MongoDB, Redis, RustFS (S3), API, storefront, admin
pnpm dev:stack:down   # stop
```

The API rebuilds when a Go file changes. The two SvelteKit apps use Vite's hot reload.

## What this series covers

- **Part 2:** the Go API. Routing with chi, the MongoDB data model, search that works in both languages, Redis caching, and OpenAPI as the contract.
- **Part 3:** auth and security. Clerk for staff, roles and permissions, and the image upload pipeline to R2.
- **Part 4:** the storefront. Server rendering, bilingual routes with Paraglide, search pages and images.
- **Part 5:** the admin dashboard with TanStack Query, and how I moved the data over from the old system.
- **Part 6:** deploying, CI, and what I learned.

![The AT Creations storefront home page in English](screenshot:storefront-home-en)
