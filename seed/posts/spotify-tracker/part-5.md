A tracker is only useful if it keeps running. A gap of a week means a week of music that never made it into the history. This part covers how I build, deploy and watch the system, and how I moved over from the old Directus version.

## One image

Every binary ships in one Docker image. The Dockerfile has three build stages: one for each SvelteKit app, and one for Go. The Go stage copies both frontend builds in, so `go:embed` can pick them up, and then builds every command under `./cmd/...`.

The runtime image is distroless (`static-debian12:debug-nonroot`). It runs as a non-root user. I picked the `debug` variant on purpose: it adds a small busybox shell, which Coolify's web terminal needs. That is how I run one-off jobs such as the importer inside the running stack.

Distroless images have no `curl`, so health checks need another path. Each binary has a `healthcheck` subcommand that probes its own ops server:

```yaml
api:
  image: spotify-tracker-app:latest
  command: ["/usr/local/bin/api"]
  healthcheck:
    test: ["CMD", "/usr/local/bin/api", "healthcheck"]
poller:
  image: spotify-tracker-app:latest
  command: ["/usr/local/bin/poller"]
  healthcheck:
    test: ["CMD", "/usr/local/bin/poller", "healthcheck"]
```

The image also ships Go's own copy of the IANA time zone database and points `ZONEINFO` at it. The dashboard groups stats by the viewer's time zone, and the base image's zone data can lag behind real rule changes.

## Health, readiness and metrics

Each service runs a small ops server on a private port with `/healthz`, `/readyz` and `/metrics` (Prometheus). Only the API's `/healthz` is public.

Readiness means more than "the process is up". The poller reports not ready if no poll has worked in the last 20 intervals (at least 2 minutes). The enricher reports not ready if no sweep has worked in 5 intervals (at least 10 minutes). A poller stuck on a bad token will show as unhealthy, even though it is still running.

The heartbeats in Redis back the service list on the admin overview. Each worker writes its last success, its last error, and a short detail line such as "playing: <track>" or "last sweep processed 3 track(s)".

Logs are JSON from `log/slog`, with request IDs on HTTP calls.

## Migrations

Migrations are plain SQL files run by goose. They are embedded in the binary, and the API applies them on start. A Postgres advisory lock wraps the run, so two API copies starting at once cannot both migrate. There is also a `migrate` command for running them by hand. The rule in the repo is simple: never edit a migration that has been applied; add a new one.

## Deploys

All changes go through pull requests. CI runs on every PR:

- Go: a check that generated protobuf code is up to date, a `go mod tidy` check, golangci-lint, tests with the race detector, and a build.
- Each frontend: `svelte-check`, a time-handling check (plus the contrast check for the admin app), eslint, and a build.
- A check of all three compose files, and a Docker build.

Merging to `main` runs the release workflow. It runs CI again, builds the image and pushes it to GitHub's container registry with provenance and an SBOM, then calls the Coolify API to start a deploy. It polls Coolify until the deploy reports `finished`, so a failed deploy turns the workflow red.

My repo is private on a personal account, and branch protection for that needs GitHub Pro. So I enforce "PRs only" in two other ways. A local pre-push hook refuses pushes to `main`. And the deploy job checks that the commit came from a merged PR:

```bash
prs=$(gh api "repos/${{ github.repository }}/commits/${{ github.sha }}/pulls" \
  --jq '[.[] | select(.merged_at != null and .base.ref == "main")] | length')
if [ "$prs" -eq 0 ]; then
  echo "::error::Commit ${{ github.sha }} was pushed to main without a merged pull request. Not deploying. Open a PR instead."
  exit 1
fi
```

A direct push still gets built and tested. It never gets deployed.

There are three compose files: one for local work, one for any VPS (with Caddy for HTTPS and nightly Postgres backups), and one for Coolify (with Traefik routing). Postgres, Redis and the gRPC port stay on the private network in all of them.

## Alerts

The biggest risk to the tracker is the Spotify token. Refresh tokens now expire 180 days after you authorize, and refreshing does not reset that. If I forget, the poller stops recording.

So the API runs an expiry check. It works out which alert stage the token is in:

```go
func Stage(left time.Duration, stages []int) (int, bool) {
	if left <= 0 {
		return 0, true
	}
	sorted := append([]int(nil), stages...)
	sort.Ints(sorted)
	for _, s := range sorted {
		if s > 0 && left <= time.Duration(s)*24*time.Hour {
			return s, true
		}
	}
	return 0, false
}
```

The default stages are 14, 7, 3 and 1 days before expiry, plus 0 for "expired". Each email has a direct link to Settings, then Spotify, in the dashboard. Re-authorizing is one click and one Spotify consent screen. The poller picks up the new token with no restart.

If Spotify rejects the token with `invalid_grant`, a separate alert goes out at most once a day.

Alerts must not spam. Each one goes through a `Once` helper that sets a Redis key with `SET NX` and a TTL before it sends. If the send fails, it deletes the key so the next check tries again. SMTP settings and recipients live in the dashboard, with the password stored encrypted.

## Moving from Directus

The `importer` command copies data from the old Directus collections over the Directus REST API. It is idempotent: catalog rows are upserts, and history rows dedupe on `(track_id, played_at)`. I could run it while the old workers were still recording, and run it again right before the switch.

It has two modes:

| Mode | Imports |
|---|---|
| `enriched` (default) | Only enriched track features, plus the tracks, albums, artists and links they point to. No history. |
| `all` | Every collection, history included. Meant for dev and testing. |

I went with `enriched`. Each imported feature row saves a Soundcharts call. I chose not to carry the old history over, and started the new history fresh.

When rows clash, the importer keeps the more complete one, and it never overwrites a track that is already enriched in the new database. A `-dry-run` flag runs the whole thing without writing.

The cut-over was a short checklist: deploy the new stack with the Spotify token seeded from the environment, run the importer, stop the old poller and enricher, run the importer once more, and point the frontend at the new API.

Part 6 is about what I learned along the way.
