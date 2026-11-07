MUBEN is a small site, but it has most of the parts of a larger one: two languages, a CMS, auth, media, caching, backups and CI. Here is what worked, what bit me, and what I would do another way.

## What worked

### Picking the small database

Moving from hosted Postgres to embedded SQLite was the best call in the project. It removed a service, a bill and a network hop. The API, the database and the migrations ship as one binary and one volume. Backups became one short shell script.

It worked because I had kept all SQL behind the `models` package from the start. The switch touched the store layer and the migrations. Handlers, the API contract and the frontend did not change.

The lesson is not "always use SQLite". It is: size the database to the real load, and keep data access behind one seam so you can change your mind.

### Rendering HTML on the server at publish time

Storing Tiptap JSON as the source and HTML as a build product kept the public side simple. The public page drops in stored HTML. The renderer is the one place that decides what HTML is allowed, and it runs in Go on the server, not in a browser I do not control.

### Bilingual fields as column pairs

Pairs like `title_en` and `title_vi` are not elegant, but they are clear. Every query, DTO and form field reads the same way. TypeScript checks that the UI catalogs match. The API resolves the language for public reads, so the public pages never pick fields.

### Failing soft

Redis down means slower pages, not errors. API down means empty lists, not a crashed site. Storage not set up means upload routes return 503, and the rest works. Each of these was a few lines, and each one turned an outage into a small degradation.

## What bit me

### SvelteKit's `fetch` and CORS

The `fetch` that SvelteKit passes to `load` follows browser CORS rules, even on the server. My API lived on another origin, so every server-side read failed. My client caught the error and returned empty data, which is what I built it to do. The result was a site that looked fine and had no posts.

Two lessons. First, server-to-server calls do not need CORS, so use the platform `fetch` for them. Second, a soft fallback can hide a real bug. "Empty" and "broken" looked the same on the page.

### Animating a tall element

Putting a blur transition on a whole article looked fine in Chrome and broke in Safari. The blurred layer was taller than Safari's texture limit, and the text went blank. Animating each block instead fixed it and looked better. The rule I took from this: keep animated layers about one screen tall.

### A token in the browser bundle

The local dev token was handy, and it was also published in the client bundle for local use. Blocking it in production was not enough on its own. A staging API behind a tunnel would have accepted it. Now the API checks for proxy headers and a local source IP before it honors the token. When a secret has to be public for dev, assume it will leak and limit where it works.

### Auth is not authorization

Clerk tells me a user is signed in. It does not tell me they should edit the site. Any account that exists in the Clerk instance, including one made through a hosted sign-up page, would have had full CMS access. The email allowlist fixed that, and it fails closed: an empty list lets no one in.

### Deploying from CI

The CI deploy job is written, but the edge in front of the deploy API answers GitHub's runners with a bot challenge. A rule to skip managed rules did not help, because the challenge is scored on the IP's reputation. From a home connection, every client passes.

So deploys run from my machine with a PowerShell script that does the same steps: trigger, wait, smoke test. The CI job stays in place behind an opt-in repo variable. I wrote down what I learned, so the next person does not spend an afternoon on the same WAF rules.

## What I would change

- **Write the docs as I go, and keep them current.** The main guide file still says "Postgres" in a few places after the switch. The code was right, and the docs drifted. I should have fixed every mention in the same change.
- **Log when fallbacks fire.** Soft failure is good for visitors, but it needs a signal for me, so "empty" and "broken" look different in the logs.
- **Plan for a third language as a table.** Column pairs are right for two languages. If MUBEN adds Japanese, I would move to a translations table instead of adding a third set of columns.
- **Continuous backups if the data grows.** A daily snapshot can lose a day of edits. For a brand journal that is fine today. Litestream is the next step if it is not.
- **More than one image size.** The browser shrinks uploads to one 2000 px WebP, which suits full-width covers. Small cards on the list page would load less with a few `srcset` widths made at upload time.

## Small rules that paid off

- A CI check that fails on em dashes keeps the copy consistent across code, docs and UI.
- A CI check that re-runs the logo script catches any hand edit to generated files.
- Pinning the version of tools that run on every session (the agent harness in my case) avoids slow first runs after an upstream release.
- A storage "test connection" button in the admin saved more time than any other small feature.

MUBEN starts as a content site. The shop and pop-up pages are placeholders for now. When those features come, the same shape should hold: bilingual columns, a Go handler behind a cache, and an admin form with an EN/VI toggle.
