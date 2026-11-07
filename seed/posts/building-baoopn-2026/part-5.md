The code is half the work. The other half is getting it onto a server in a way I trust: every change tested, every deploy traceable to a pull request, and nothing private in a repo I may open up one day. This part covers the images, the compose file, CI, the deploy gate, and the public-safety check.

## Two images

**The API** builds in a Go Alpine image with `CGO_ENABLED=0` and `-trimpath`, and the commit hash goes into the binary through `-ldflags -X`. The runtime image is distroless static, running as the non-root user. It holds the binary and an empty media folder, and nothing else: no shell, no package manager, no curl.

That last point shaped the health check. Docker's usual `HEALTHCHECK` runs `curl` or `wget`, and neither exists in the image. So the binary checks itself:

```go
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(app.Healthcheck(envOr("HTTP_ADDR", ":8080")))
	}
	// ...
}
```

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/api", "healthcheck"]
```

`api healthcheck` probes `/healthz` on the local server and exits 0 or 1. One binary, two jobs.

The media folder is created in the build stage and copied in with the non-root owner. A fresh named volume starts as a copy of that folder, so the API can write uploads to it without a permission fix.

**The web app** builds with Node on Alpine. The final stage holds the SvelteKit build and the production dependencies, and runs as the `node` user. Its health check fetches a static file.

## One compose file

Production is one Docker Compose resource on Coolify with three services:

| Service | Exposure |
|---|---|
| `postgres` | internal, with a named volume |
| `api` | internal, no domain, with the media volume |
| `web` | the one public service, on port 3000 |

Coolify builds both images on the server from the repo, so there is no image registry. Traefik in front of it handles TLS.

Coolify also fills in secrets on the first deploy. A variable named like `SERVICE_PASSWORD_64_INTERNAL` makes it generate a random value and keep it. I use that for the database password, the internal token the proxy sends to the API, and the secrets key that salts visitor hashes. I never type those values, and they never leave the server.

```yaml
- SECRETS_KEY=${SERVICE_PASSWORD_64_SECRETS}
- INTERNAL_API_TOKEN=${SERVICE_PASSWORD_64_INTERNAL}
```

The services start in order through health checks. The API waits for Postgres to be healthy, and the web app waits for the API. The API runs its embedded migrations on start.

For local work there is a second compose file. By default it runs Postgres alone, and I run the API and the Vite dev server on my machine. A `full` profile builds and runs the whole stack, the same way production does.

## CI

`ci.yml` runs on every pull request and on `main`:

- **Go:** a `go mod tidy` drift check, `gofmt`, `go vet`, `go test -race` against a Postgres 17 service container, and `go build`.
- **Web:** `pnpm check` (Svelte and TypeScript) and `pnpm build`.
- **Worker:** `node --test` for the inbound email worker. Its pure decisions (which addresses to accept, what to do with each API status) sit in their own file so plain Node can test them.
- **Compose:** `docker compose config` on both compose files, to catch a broken file before Coolify does.
- **Docker:** both images build on pull requests, with no push.
- **Public safety:** a script that scans tracked files for things that must never be published. More on it below.

The database tests skip themselves when `TEST_DATABASE_URL` is not set, so `go test` still works on a machine with no Postgres.

## Merging is the deploy

`main` changes through pull requests alone, with green CI. When a commit lands on `main`, `release.yml` runs CI again, then asks Coolify's API to deploy, then polls until the deploy finishes or fails. Builds happen on the server, so it allows up to 20 minutes.

The repo is private on a free plan. GitHub does not enforce branch rulesets there, so nothing on GitHub's side stops a direct push to `main`. I closed that gap in two places.

First, a `pre-push` hook in the repo refuses pushes to `main`. Each clone turns it on with `git config core.hooksPath .githooks`.

Second, and more important, the deploy job checks where the commit came from. It asks GitHub which merged pull requests contain the commit. If there are none, it fails and does not deploy:

```yaml
- name: Require a merged pull request
  if: github.event_name == 'push'
  env:
    GH_TOKEN: ${{ github.token }}
  run: |
    set -euo pipefail
    prs=$(gh api "repos/${{ github.repository }}/commits/${{ github.sha }}/pulls" \
      --jq '[.[] | select(.merged_at != null and .base.ref == "main")] | length')
    if [ "$prs" -eq 0 ]; then
      echo "::error::Commit ${{ github.sha }} was pushed to main without a merged pull request. Not deploying. Open a PR instead."
      exit 1
    fi
```

The hook can be skipped with `--no-verify`. The gate cannot be skipped from my laptop. A direct push still gets built and tested, but it never reaches production. Manual runs from the Actions tab are allowed, for the rare case I need to redeploy by hand.

The deploy job also stays off until a repository variable holds the Coolify resource ID. That let me merge and test before the site went live, with no risk of an early deploy.

I saved the GitHub ruleset as JSON in the repo. When the repo goes public, one `gh api` call applies it, and GitHub enforces the rule too.

## Keeping it safe to publish

I may make this repo public. So I treat it as public now.

The public-safety script runs `grep` over every tracked file for patterns that should never be there:

```sh
# ... (first, a check for machine-specific home folder paths)
check "Clerk secret keys" 'sk_(live|test)_[A-Za-z0-9]{20,}'
check "Clerk live publishable keys" 'pk_live_[A-Za-z0-9]{20,}'
check "useSend API keys" '\bus_[A-Za-z0-9]{24,}'
check "webhook secrets" 'whsec_[A-Za-z0-9+/=]{16,}'
check "GitHub tokens" '\b(ghp|gho|ghs|github_pat)_[A-Za-z0-9_]{20,}'
check "private key blocks" '-----BEGIN [A-Z ]*PRIVATE KEY-----'
```

It also fails if a `.env` file or the `.secrets/` folder is tracked. It runs in CI on every pull request, so a leak blocks the merge instead of reaching the history.

The rest is habit:

- **Secrets live in the environment.** Real values sit in a git-ignored `.env` on my machine, a git-ignored `.secrets/` folder, Coolify's settings, or the worker's secret store. `.env.example` lists every variable with a note and no value.
- **Docs use placeholders.** The deploy guide says `example.com`, `reply.example.com` and `<your coolify host>`. Anyone can follow it, and it names nothing of mine.
- **Personal notes stay out.** Design notes that name other projects or my own setup live in a git-ignored folder under `docs/`. They help me, and they never reach the repo.

None of this is complex. The point is that each rule is a check a machine runs, not a thing I have to remember.

Part 6 closes the series with what I learned and what I would change.
