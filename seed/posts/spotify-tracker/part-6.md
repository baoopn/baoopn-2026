This was a small project with one user, and I still learned a lot from it. Here is what worked, what I would change, and what I would tell myself at the start.

## What worked

### One owner for the database

Making the API the only service that touches Postgres was the best call in the project. The poller and enricher are thin. They fetch, they forward, and they report. Every rule about plays, replays and enrichment lives in one package with tests.

It also kept secrets in one place. The poller gets a short-lived Spotify access token over gRPC and nothing else. When I rotate the refresh token, or re-authorize, no worker needs a new config or a restart.

The cost is one more hop. Each snapshot goes poller to API to Postgres, not poller to Postgres. At one snapshot every 3 seconds, that cost does not matter.

### Pure functions for the rules

`Decide` takes the last state and the new snapshot and returns what to do. It does no I/O. That made the play rules easy to test and easy to reason about. When I wondered "what happens if I scrub back 5 seconds?", I could answer it by reading ten lines.

I would do this again for any rule that decides what gets written to the database.

### Two layers of dedup

Redis holds the last state, and Postgres has a unique key on `(track_id, played_at)`. Either one alone has a gap. Redis can lose data. The unique key alone cannot tell a replay from a new play. Together they cover each other, and the same unique key made the Directus import safe to rerun.

### One retry system, not two

In the enricher I ack every stream message and keep retries in Postgres. Redis streams can redeliver unacked messages, and it was tempting to use that. But then a failed track would live in two places with two retry clocks. With one `next_attempt_at` column, I can see every pending retry with one SQL query, and the dashboard can show it.

### Cache generations

Bumping one counter to drop the whole response cache sounds crude. In practice it is the right size for this app. Writes are rare (a new play every few minutes), reads are cheap to redo, and I never have to work out which keys a new play affects. The page never shows stale stats.

### A design doc before the UI

Writing the "liner notes" doc first, with a list of things to avoid, gave both apps one look. It also saved time. Many small choices (shadow or no shadow, card or rule, which colour for "pending") already had an answer. The contrast check in CI keeps the palette honest as I tweak it.

## What I would change

### Plan for the platform to move

Spotify's 2026 changes hit this project three times: audio features went away for Development Mode apps, refresh tokens got a 180-day limit, and the owner of the app now needs Premium. None of these were bugs in my code. They were changes in what the platform allows.

The parts that handled this well were the ones with a clear seam. Enrichment sits behind a small `Provider` interface, so moving to Soundcharts did not touch the rest of the system. Token handling sits in one `Manager`, so adding expiry alerts was one new package.

Next time I would treat every third-party API as something that will change, and put that seam in from day one.

### Keep the docs and the workflow in sync

My deployment doc and my release workflow drifted apart. The doc describes one way of handing the image to Coolify, and the workflow does something a bit different. Nothing breaks, but a reader of the doc gets the wrong idea. The fix is a habit: treat the workflow files as the source of truth, and check the docs against them each time I change a deploy step.

### Percentiles from buckets

The API traffic page works out p50, p95 and p99 from fixed histogram buckets. That keeps storage small, but the numbers are estimates, and anything above the last bound (5 seconds) lands in one open bucket. For a personal API this is fine. If I needed exact tail latency, I would store a proper sketch per hour instead.

### The poller is a single point

The poller must run as one copy, or two pollers would race on the same playback state. If it stops, nothing records. Health checks and heartbeats tell me when that happens, but they do not fix it. A leader lock in Redis would let me run a standby. For now, a restart policy and alerts are enough, but it is the weakest spot in the design.

## Small things that paid off

- **A `healthcheck` subcommand** in every binary, so distroless images need no `curl`.
- **A readiness check tied to real work**, not to "the process is up". A poller with a dead token shows as unhealthy.
- **`SET NX` before sending an alert**, and a delete if the send fails. No duplicate emails, and no lost ones.
- **Flushing SSE headers at once** and a client-side "silent for 20 seconds" check. Together they catch proxies that buffer or drop the stream.
- **A deploy gate in the workflow** when branch protection was not an option. A direct push gets tested but never shipped.
- **Shipping Go's own time zone data** in the image, so time zone rules do not depend on the base image.

## Closing

The project does one thing: it writes down what I listen to and shows it back to me. Most of the work went into the parts nobody sees: the play rules, the retry clocks, the token alerts. That is where a small tool earns trust. When I open the page and see the song I am playing, with its tempo and key, I know the boring parts are doing their job.
