The API is a Go service built on Fiber. It serves public reads, admin writes, presigned uploads and the contact form. It keeps its data in a SQLite file inside the same process. This part explains why that fits, and how I handle migrations, caching, backups and deploys.

## Why SQLite fits here

The first plan used a hosted Postgres database. I reversed that before launch and moved to an embedded SQLite file. The reasons were plain:

- Traffic is low, and public reads go through Redis first.
- Writes come from a small team in the admin, a few at a time.
- A database service is one more thing to pay for, secure and keep up.
- All data access already sat behind `internal/models`, so the API contract did not change.

I use `modernc.org/sqlite`, a SQLite port written in pure Go. That means no C compiler, so the image builds with `CGO_ENABLED=0` and the final binary runs in a plain Alpine image.

The connection setup is short, and every line matters:

```go
dsn := fmt.Sprintf(
	"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
	path,
)

db, err := sql.Open("sqlite", dsn)
// ...

// Serialise access through a single connection (SQLite is a single writer).
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
```

- **WAL mode** lets readers work while a write is in progress.
- **busy_timeout** makes a locked database wait up to five seconds instead of failing.
- **foreign_keys** turns on a check that SQLite leaves off by default.
- **One open connection.** SQLite allows one writer at a time. With a single connection in the pool, the "database is locked" error cannot happen. For a cached, low-traffic API, one connection is plenty.

The PRAGMAs go in the DSN, not in a one-time `Exec`, so every connection the pool opens gets them.

## Migrations ship in the binary

I did not want a separate migrate tool in the image. The SQL files live in `server/migrations` and get embedded with `go:embed`:

```go
//go:embed *.sql
var Files embed.FS
```

On startup, `database.Migrate` creates a `schema_migrations` table if needed, sorts the `*.up.sql` files by name, and applies each one that is not yet recorded. Each file runs in its own transaction, together with the insert that records it:

```go
tx, err := db.BeginTx(ctx, nil)
// ...
if _, err := tx.ExecContext(ctx, string(body)); err != nil {
	_ = tx.Rollback()
	return fmt.Errorf("apply %s: %w", name, err)
}
if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
	_ = tx.Rollback()
	return err
}
```

A failed migration leaves no half-applied state, and the API will not start. Running it on every boot is safe because applied files are skipped. There are three migrations today: the initial schema, contact messages and SMTP settings.

When I moved from Postgres to SQLite, I folded the old migrations into one fresh `000001_init`. There was no real data yet, so there was no history to keep.

## Caching

Handlers talk to a small `Cache` interface with `Get`, `Set`, `Delete` and `DeletePattern`. There are two versions: one backed by Redis, and a `Noop` that stores nothing. On startup, the API tries Redis. If Redis does not answer, the API logs a warning and runs with `Noop`. Responses are slower, but still right.

Keys include the language and the page, for example `muben:blog:list:published:vi:p2`. Publish, unpublish, update and delete all clear the list keys with a pattern and the two slug keys for the post. An update clears both the old slug and the new one, since an editor can rename a slug.

The API also caps the page number at 100,000. Without a cap, a client could ask for any page and fill Redis with keys.

## Guard rails

Fiber's limiter middleware sets per-IP limits:

| Route group | Requests per minute |
|---|---|
| All `/api/v1` content routes | 120 |
| Admin routes | 60 |
| Presigned upload URL | 20 |
| Public contact form | 5 |

The client IP comes from the `CF-Connecting-IP` header when present, and the socket peer when not. I do not trust `X-Forwarded-For`. Its first value is set by the client, so trusting it would let anyone dodge every limit by changing a header.

Input checks count characters, not bytes, for text fields. Vietnamese text uses multi-byte characters, and a byte limit would reject valid posts far below the stated limit. The Tiptap JSON, on the other hand, has a byte cap of 1 MiB per language, because that limit is about memory, not words.

## The dev token

For local work, the API accepts a static admin token so I can test admin routes without Clerk. That is risky, so it has three locks:

1. The API refuses to start if the token is set in production.
2. The middleware ignores the token when the app runs in production.
3. Even outside production, the token works for a request with no proxy headers (`CF-Ray`, `X-Forwarded-For` and similar) and a loopback or private source IP.

The third lock came from a real gap. The token is also given to the browser bundle for local use. If a non-production API were exposed through a tunnel, anyone who read the bundle could run the CMS. Checking for proxy headers closes that, and a client that forges those headers can do nothing but deny itself.

## Backups

The `backup` sidecar is an Alpine image with `sqlite3` and `rclone`, and one shell script. Once a day it:

1. Takes a consistent snapshot with `sqlite3 ".backup"`. This uses SQLite's online backup API, so it is safe while the API writes.
2. Gzips the snapshot and uploads it to a dated key in a private bucket.
3. Deletes backups older than seven days.

```sh
sqlite3 "${DB_PATH}" ".backup '${snap}'"
gzip -f "${snap}"
rclone copyto "${snap}.gz" "r2:${BACKUP_BUCKET}/backups/muben-${ts}.db.gz"
rclone delete --min-age "${BACKUP_RETENTION_DAYS}d" "r2:${BACKUP_BUCKET}/backups/"
```

The rclone remote is built from environment variables, so there is no config file to manage. If no bucket is set, the script idles and logs a reminder instead of crash-looping. The docs warn in bold not to point it at the public media bucket, since a backup is a full copy of the database.

To restore, I stop the API, copy the file onto the volume, delete the old `-wal` and `-shm` files, and start the API again. A daily snapshot means up to a day of lost edits. That is fine for this content. If it ever is not, Litestream can stream the WAL to storage for near-zero loss.

## Deploys

There are two supported routes:

- **Coolify**, using one self-contained compose file, with Coolify's proxy handling TLS.
- **A plain VPS**, merging a base compose file with production and tunnel overlays, with secrets injected by Doppler.

The base compose file has no host ports and no environment details.

CI runs on every pull request: lint, type check and build for the web app; `go build`, `go vet`, `go test -race` and a tidy check for the API; a check that the logo script is a no-op; a parse check for all compose stacks; and two repo rules (no em dashes, no committed `.env` files). The deploy step triggers Coolify, waits for it to finish, then smoke-tests the live site. One of those tests asserts that the dev token returns 401 in production.
