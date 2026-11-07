MUBEN is a fashion brand with a Japanese feel. The name comes from 無辺のキュレイテッド, which reads as "boundless curated". The look is black and white, with lots of empty space, thin wide type and outline buttons. My job was to build the brand's website and a small CMS so the team can run it without me.

## The brief

The launch is about content, not sales. The site has to publish a journal and show the brand. A shop and a pop-up store program will come later, so for now those pages say "coming soon".

Four things shaped every choice:

- **Two languages from day one.** Every piece of text the team can edit has an English and a Vietnamese version.
- **Good SEO for the journal.** Posts must show up in search, in both languages.
- **The team edits content in the browser.** No Markdown, no Git, no deploys to fix a typo.
- **One small server.** Traffic is low, and the budget is too. I did not want to run a cluster for a brand site.

![The MUBEN home page in English, with the logo, the outline buttons and the journal preview](screenshot:public-home)

## The pages

The public site has six routes per language:

| Page | What it does |
|---|---|
| Home | Hero with a background image the team can change, plus the three latest posts |
| About | The brand story, edited in the admin |
| Blogs | A paged list and a detail page per post |
| Contact | A form that stores messages and can send an email alert |
| Pop Up, Shop | "Coming soon" pages with a slow ring animation |

Every URL starts with the language: `/en/blogs/some-post` and `/vi/blogs/some-post`. A visitor who lands on `/` gets a redirect based on a cookie, then the `Accept-Language` header, then English.

The admin lives at `/_admin`. It has an About editor, a Home hero editor, a blog CMS, a contact inbox and an SMTP settings page.

## The architecture

It is one repo with two apps and a few support files:

```
web/       SvelteKit app: the public site and the admin portal
server/    Go + Fiber REST API under /api/v1
backup/    A small sidecar that copies the database to object storage each day
docs/      Plan, API contract, deploy and backup guides
```

At run time there are four containers on one host:

1. **web**: SvelteKit on the Node adapter. It renders public pages on the server and serves the admin as a client-side app.
2. **server**: the Go API. It holds the database in-process as a SQLite file on a Docker volume.
3. **redis**: a cache for public API reads. The API works without it.
4. **backup**: a shell script in an Alpine image that snapshots the SQLite file.

Media lives in an S3-compatible bucket (Cloudflare R2 in production). The browser uploads images straight to the bucket with a presigned URL, so image bytes never pass through the API.

Sign-in for the admin uses Clerk. The API checks the Clerk session token and then checks the account's email against an allowlist.

## Why these parts

**SvelteKit** gives me server-rendered HTML for SEO and a client app for the admin in the same codebase. The admin layout sets `ssr = false`, and the public routes render on the server. One app, two modes.

**Go with Fiber** builds to one static binary. With the pure-Go SQLite driver, it builds with `CGO_ENABLED=0` and runs as a non-root user in a small Alpine image.

**SQLite** was not my first pick. The first plan used hosted Postgres. Partway in, I switched to an embedded SQLite file. The site is small, reads go through a cache, and writes come from one or two editors. A separate database service added cost and moving parts for no gain. I cover this in Part 4.

**Server rendering with a cache, not a static build.** The team edits content at run time. A static build would go stale the moment someone hits Publish. So pages render on each request, the API caches its JSON in Redis, and SvelteKit sends cache headers for the CDN.

## The request path

Here is what happens when someone opens a post:

1. SvelteKit runs the page's `load` function on the server.
2. It calls `GET /api/v1/blogs/:slug?locale=vi` on the Go API.
3. The API checks Redis. On a hit, it returns the cached JSON as is.
4. On a miss, it reads the post from SQLite, picks the Vietnamese fields, stores the JSON in Redis for five minutes, and returns it.
5. SvelteKit renders the full HTML and sets `s-maxage=60` so the CDN can hold it for a minute.

When an editor publishes or edits a post, the API deletes the matching Redis keys. The next request gets fresh data.

The API resolves the language itself. The public client asks for `?locale=en` or `?locale=vi` and gets flat fields like `title` and `contentHtml`. The admin gets both sides, `titleEn` and `titleVi`. This keeps the public pages simple: they never pick fields.

## What the rest of the series covers

- **Part 2:** the public site. Design tokens, the motion system, SEO and how pages stay fast.
- **Part 3:** the admin CMS. The bilingual content model, the Tiptap editor, image uploads and auth.
- **Part 4:** the Go API with embedded SQLite. Migrations, caching, backups and deploys.
- **Part 5:** what I learned and what I would change.
