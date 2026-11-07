The last part covers how the code gets from a pull request to production, how the containers are locked down, and what I learned from the rewrite.

## CI as the gate

Every push runs CI. Nothing deploys unless it passes. The Go job checks formatting, runs `go vet`, then runs the tests with the race detector:

```yaml
- run: go mod verify
- run: test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
- run: go vet ./...
- run: go test -race -covermode=atomic -coverprofile=coverage.out ./...
```

A second step starts a real MongoDB replica set, Redis and a local S3 server, then runs the integration tests against them. I wanted the transaction and stock code tested against a real database, not a mock. After that, `govulncheck` looks for known flaws in the Go packages the code calls. `golangci-lint` also runs.

The SvelteKit job checks that the API types match `openapi.yaml` (from part 2), then runs `check`, `lint`, `test` and `build` for both apps, plus `pnpm audit`.

On top of that, Trivy scans the dependencies, the config files and the built images for known flaws and leaked secrets. CodeQL runs its `security-extended` rules. GitHub Actions are pinned by commit SHA, not by tag, so an action cannot change under me.

The old pipeline did none of this. It used `ssh-keyscan` to trust the server's key on first contact, built on the server, and synced files with `rsync --delete`.

## From green CI to production

When CI passes on `main`, a deploy workflow builds four images: the API, the image worker, the storefront and the admin. The API and the worker come from one Dockerfile with two targets. Each image is tagged `sha-<commit>`, so tags never move. Each image ships with an SBOM (a list of what is inside it) and a signed build provenance, so anyone can check that an image came from this repo's workflow:

```bash
gh attestation verify oci://ghcr.io/<owner>/atcreations-api:sha-<commit> --owner <owner>
```

Production runs on Coolify, a self-hosted deploy platform. The workflow sets the new tag on the Coolify resource and starts a deploy. Coolify never builds. It pulls the images CI built, so what was tested is what runs.

The repo also keeps a plain Docker Compose stack in `deploy/`, with Caddy in front, for a host without Coolify. Its deploy script pulls every image, waits for health checks and reverts to the last tag on failure.

## Locked-down containers

The Go binaries are static and run on `distroless/static` as a non-root user. There is no shell in the image. The health check is the binary itself:

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot@sha256:... AS api
COPY --from=build /out/api /api
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]
```

Two build tags keep the image codecs pure Go: `nodynamic` stops them from loading native libraries, and `wasm2go` compiles the HEIC decoder to Go. That is what lets HEIC photos from iPhones work in a distroless image.

Base images are pinned by digest. Containers run with a read-only root filesystem, all Linux capabilities dropped, `no-new-privileges`, memory limits and log rotation. MongoDB and Redis sit on an internal network with no published port. MongoDB has auth and a keyfile, and the app user can only read and write its own database. Redis runs with protected mode on, the default user off, and an ACL user that cannot run commands like `FLUSHALL`, `CONFIG` or `KEYS`.

## Backups and errors

A nightly job runs `mongodump` and encrypts the archive with age to a public key. The private key never touches the server. The archive is kept for two weeks and can be copied to off-host storage. The deploy docs include a restore drill, because a backup you have never restored is only a hope.

Errors from all three apps go to errex, a small self-hosted error tracker that speaks Sentry's protocol, so the apps use the normal Sentry SDKs. When an error is new, or a fixed one comes back, errex calls a webhook, and a tiny relay turns it into an email. The tracker runs on a different machine from the app, so it can still report when the app server is down.

## Lessons learned

### Write the security model down

The table in `docs/SECURITY.md` (old behaviour, risk, new behaviour) was the most useful document in the project. It turned a vague "the old code feels unsafe" into a checklist. Each row became a design rule, and each rule became code and tests. I would start the next rewrite with that table, before writing any code.

### Delete whole classes of bugs

The best security fixes were not patches. They removed the thing that could go wrong:

- No first-party passwords or JWTs, so no reset-link or weak-secret bugs.
- No cookies on the API, so no CSRF.
- `DisallowUnknownFields` on every body, so no mass assignment.
- Prices only from the server, so no $0 orders.
- Random object keys, so no file overwrites.

Each of these is one decision, and it holds without anyone having to remember it later.

### Contract first pays off

Making `openapi.yaml` the source of truth, with a CI check for drift, caught many small mistakes. A renamed field breaks the TypeScript build in both apps right away, not in production.

### Let Redis fail

I made every Redis use fail open: no cache means slower pages, and the rate limiter falls back to memory. That kept Redis simple. It needs no persistence and no backups, and losing it is never an outage.

### Limits I accepted

Some trade-offs are worth stating plainly, because I may revisit them:

- **Search is prefix-only.** It is fast and index-backed, but "ote" will not find "tote". If shoppers need fuzzy search, I would add a real search engine instead of stretching MongoDB.
- **Staff changes are serialized in one process.** The last-admin guard relies on an in-process lock, so the API runs as a single replica. Scaling out would mean moving that lock to Redis or into a transaction.
- **Role changes are not instant.** They apply when the current Clerk session token expires, about a minute. A ban is instant, so that is the tool for urgent cases.
- **Analytics can double-count in a crash.** A crash between the MongoDB write and the Redis cleanup can count one batch twice. For a small shop's traffic chart, I chose simple over exact.

### What I'd do the same way again

Go for the API, SvelteKit for both frontends, a separate worker for anything that decodes user files, and a dry run mode on every migration script. The rewrite took longer than patching would have. But the store now runs on code I can explain line by line, and the rules that keep it safe are written down where the next person can find them.
