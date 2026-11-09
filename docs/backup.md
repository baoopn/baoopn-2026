# Off-site backups

The `backup` service dumps the Postgres database once a day. It encrypts the
dump with [age](https://age-encryption.org) and uploads it to a private
Cloudflare R2 bucket. Any S3-compatible store or any
[rclone](https://rclone.org) remote works too.

The image is built from `deploy/backup/` in this repository. No registry is
involved: Coolify builds it on the server, the same way as the api and web
images. You can build it anywhere from the public repository:

```bash
docker build -t baoopn-backup https://github.com/baoopn/baoopn-2026.git#main:deploy/backup
```

The server only holds the **public** key. The private key stays with you and
is needed only to restore. A stolen server or a leaked bucket token does not
expose the data.

You can restore on Coolify, on a VPS with plain Docker Compose, on a laptop,
or on a machine without Docker.

## What gets backed up

- The whole `baoopn` database: posts and their parts, projects, settings,
  contact messages, the media table, analytics and `goose_db_version`.
- Format: `pg_dump --format=custom --compress=6 --no-owner --no-privileges`,
  piped into `age`. The plaintext dump never touches the bucket or the disk.

Not backed up:

- The media files in the R2 bucket `baoopn-portfolio`. The database only
  holds references to them. Turn on versioning for that bucket, or copy it
  on a schedule of your own.
- The `baoopn-media` volume (files from before the R2 migration, and the
  originals waiting for WebP conversion).
- Clerk users. Clerk keeps them; the database stores no passwords.

Each backup is two objects:

```
baoopn/<YYYY>/<MM>/baoopn-20261109T030000Z.dump.age        encrypted dump
baoopn/<YYYY>/<MM>/baoopn-20261109T030000Z.dump.age.json   manifest
```

The manifest holds the name, time, size, the sha256 of the encrypted file,
the pg_dump and server versions, the database name and the goose schema
version. Restore checks the sha256 before it decrypts anything.

## Keep these with the age key

A restored database needs the same app settings to work as before. Store
these in your password manager next to the age key:

- `SECRETS_KEY` (Coolify's `SERVICE_PASSWORD_64_SECRETS`). It salts the
  visitor hashes. A new Coolify resource generates a new one. Copy the old
  value across, or the unique-visitor counts start again from zero.
- `CLERK_PUBLISHABLE_KEY`, `CLERK_SECRET_KEY` and `ADMIN_ALLOWED_*`.
- `SMTP_PASSWORD` (the useSend API key).
- `INBOUND_WEBHOOK_SECRET`. It must equal the Email Worker's secret.
- `TURNSTILE_*` and `GITHUB_TOKEN`.
- The media `S3_*` settings and `MEDIA_PUBLIC_URL`.
- The `BACKUP_S3_*` token.

`INTERNAL_API_TOKEN` does not need a copy: any new value works. Nothing in
the database is encrypted with an app key.

## Setup

### 1. Create a private bucket and a token (Cloudflare R2)

1. Cloudflare dashboard, R2, **Create bucket**. Name it
   `baoopn-2026-backups`. Keep it private: no public access, no custom domain.
2. R2, **Manage API tokens**, **Create API token**:
   - Permission: **Object Read & Write**.
   - Scope: **Apply to specific buckets only**, then pick the new bucket.
3. Copy the **Access Key ID**, the **Secret Access Key** and the
   **S3 endpoint** (`https://<account-id>.r2.cloudflarestorage.com`).

A bucket-scoped token cannot list or create buckets. The tool never needs to.

Other providers: set `BACKUP_S3_PROVIDER` (rclone's name: `AWS`, `Minio`,
`Wasabi`, ...) and `BACKUP_S3_REGION`. For anything else, set
`BACKUP_REMOTE` to an rclone path and pass the remote's settings as
`RCLONE_CONFIG_<NAME>_*` variables.

### 2. Create the age key pair

On your own machine:

```bash
docker build -t baoopn-backup https://github.com/baoopn/baoopn-2026.git#main:deploy/backup
docker run --rm baoopn-backup keygen > portfolio-backup-age-key.txt
```

The private key goes to `portfolio-backup-age-key.txt`. The public recipient
(`age1...`) shows on the terminal. Store the key file offline, for example in
your password manager. **If you lose it, nobody can decrypt the backups.**
Do not put it on the server.

`age-keygen -o portfolio-backup-age-key.txt` works too if you have age
installed. To encrypt for more than one key (say, a second person), list
several recipients separated by spaces or commas.

### 3. Configure the service

| Variable | Value |
|---|---|
| `BACKUP_AGE_RECIPIENT` | the `age1...` public key |
| `BACKUP_S3_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` |
| `BACKUP_S3_BUCKET` | `baoopn-2026-backups` |
| `BACKUP_S3_ACCESS_KEY_ID` | the token's access key ID |
| `BACKUP_S3_SECRET_ACCESS_KEY` | the token's secret |

Add them in the Coolify resource's *Environment Variables* tab and redeploy.
For local development, `docker compose --profile backup ...` writes to a
Docker volume and needs no bucket. See the comments in `docker-compose.yml`.

Until the recipient and a full destination are set, the service logs a
warning that names the missing variables, and idles. It never uploads an
unencrypted dump.

Check that it works. Open the backup container's terminal in Coolify and run:

```bash
backupctl backup
backupctl list
```

## Schedule, retention and health

- One backup a day at `BACKUP_TIME` (UTC, default `03:00`).
  `BACKUP_ON_START=true` also runs one when the container starts.
- A failed run gets one retry after `BACKUP_RETRY_MINUTES` (default 30).
  The container never exits because of a failed backup.
- After each good scheduled backup, the service deletes backups older than
  `BACKUP_KEEP_DAYS` (default 30). It always keeps the newest
  `BACKUP_KEEP_MIN` (default 7), however old. `BACKUP_KEEP_DAYS=0` keeps
  everything. Objects with other names in the bucket are never touched.
- The healthcheck stays healthy while the service idles and while it waits
  for its first run. It turns **unhealthy** when no backup has worked in
  `BACKUP_MAX_AGE_HOURS` (default 26). Coolify then shows the resource as
  degraded. Deploys do not wait for this service.
- A new container reads the newest backup in the bucket at start, so a
  redeploy does not reset the health state. If that backup is older than
  `BACKUP_MAX_AGE_HOURS`, the service reports unhealthy until the next good
  run. Set `BACKUP_ON_START=true` to run one at once.
- `docker stop` (and a redeploy) interrupts a running backup. The service
  removes its temp files and uploads nothing; the next run makes a new one.
- `BACKUP_PING_URL` (optional): a GET after each success and `<url>/fail`
  after each failure, as [healthchecks.io](https://healthchecks.io) expects.
  Use it to get an email when backups stop.

Test a restore from time to time. `backupctl verify latest` downloads,
checks and decrypts the newest backup without touching any database. It
needs the private key, which is not on the server, so pipe the key in from
an SSH session (the Coolify terminal cannot do this):

```bash
docker ps --format '{{.Names}}' | grep '^backup-'
docker exec -i <backup-container> backupctl verify latest -i - < portfolio-backup-age-key.txt
```

## Commands

```
backupctl run                 scheduler (the default command)
backupctl backup              one backup now
backupctl list                backups, newest first
backupctl verify  [latest|NAME] [-i KEY]
backupctl restore [latest|NAME] [--force] [--to-file PATH] [-i KEY]
backupctl prune               apply the retention rules now
backupctl keygen              new key pair
backupctl health              healthcheck
```

`-i KEY` is the age private key file. The default is
`/run/secrets/backup-age-key` (or `BACKUP_AGE_IDENTITY_FILE`). `-i -` reads
it from stdin, so the key never has to be stored on the server.

`restore` refuses to write into a database that already has tables in schema
`public`. `--force` drops and replaces the objects that are in the backup
(`pg_restore --clean --if-exists`). The restore runs in a single
transaction: if it fails, nothing changes. `--to-file PATH` only writes the
decrypted `.dump` file, for use with `pg_restore` by hand. It is the only
command that writes plaintext to disk. It refuses to replace an existing
PATH unless you add `--force`. It writes `PATH.partial` first and renames it
at the end, so a failed run leaves PATH as it was.

Log lines count the base tables in schema `public`, the same way for
backup, verify and restore. A backup without goose data shows schema
version `none`.

The database to restore into comes from `DATABASE_URL` or the usual `PGHOST`,
`PGPORT`, `PGUSER`, `PGPASSWORD` and `PGDATABASE`. Point them at another
database to restore there.

## Restore

The order matters:

1. Start **only** Postgres (a new, empty database).
2. Restore the backup into it.
3. Start the app. The api runs the goose migrations at startup, so it moves
   a backup from an older version forward.

If the app already started on the new database, it created empty tables, and
`restore` refuses. Stop the app. Then restore with `--force` (fine when the
app version matches the backup), or empty the schema first:

```sql
DROP SCHEMA public CASCADE; CREATE SCHEMA public;
```

In the examples, `portfolio-backup-age-key.txt` is your private key and the
backup is `latest` (or a name from `backupctl list`).

### (a) Coolify

From an SSH session on the server. The key comes from stdin and is never
saved there.

```bash
docker ps --format '{{.Names}}' | grep -E '^(api|mediaworker|web|backup)-'
docker stop <api-container> <mediaworker-container> <web-container>
docker exec -i <backup-container> backupctl restore latest -i - < portfolio-backup-age-key.txt
#   add --force if the database already has tables (see above)
```

Then **Restart** the resource in Coolify. On a new server: deploy the
resource with the same `BACKUP_*` variables, stop the app containers as
above, restore with `--force` (or empty the schema first), and restart.

### (b) A VPS with plain Docker Compose

```bash
git clone https://github.com/baoopn/baoopn-2026.git && cd baoopn-2026
# .env: SERVICE_USER_POSTGRES, SERVICE_PASSWORD_POSTGRES, SERVICE_PASSWORD_64_INTERNAL,
# SERVICE_PASSWORD_64_SECRETS, SERVICE_URL_WEB, BACKUP_AGE_RECIPIENT and BACKUP_S3_*
docker compose -f docker-compose.coolify.yml up -d postgres
docker compose -f docker-compose.coolify.yml run --rm -T backup \
  restore latest -i - < portfolio-backup-age-key.txt
docker compose -f docker-compose.coolify.yml up -d
```

### (c) A laptop or any machine with Docker

Put the settings in a file, so secrets stay out of your shell history:

```bash
cat > backup.env <<'EOF'
BACKUP_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
BACKUP_S3_BUCKET=baoopn-2026-backups
BACKUP_S3_ACCESS_KEY_ID=<key id>
BACKUP_S3_SECRET_ACCESS_KEY=<secret>
DATABASE_URL=postgres://baoopn:<password>@<host>:5432/baoopn?sslmode=disable
EOF

docker build -t baoopn-backup https://github.com/baoopn/baoopn-2026.git#main:deploy/backup
docker run --rm -i --env-file backup.env --network host \
  baoopn-backup restore latest -i - < portfolio-backup-age-key.txt
```

`--network host` lets the container reach a database on `localhost` (Linux).
To get only the decrypted dump:

```bash
mkdir -p out
docker run --rm -i --env-file backup.env --user "$(id -u)" -v "$PWD/out:/restore" \
  baoopn-backup restore latest --to-file /restore/baoopn.dump -i - < portfolio-backup-age-key.txt
```

### (d) Without Docker

You need `rclone`, `age`, `sha256sum` (macOS: `shasum -a 256`) and the
PostgreSQL 17 (or newer) client tools.

```bash
# rclone settings from env: a remote called "r2"
export RCLONE_CONFIG_R2_TYPE=s3 RCLONE_CONFIG_R2_PROVIDER=Cloudflare \
  RCLONE_CONFIG_R2_REGION=auto RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true \
  RCLONE_CONFIG_R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com \
  RCLONE_CONFIG_R2_ACCESS_KEY_ID=<key id> RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=<secret>

# 1. Pick a backup (newest last)
rclone lsf -R --include 'baoopn-*.dump.age' r2:baoopn-2026-backups/baoopn | sort | tail -n 5
NAME=baoopn-20261109T030000Z.dump.age
DIR=r2:baoopn-2026-backups/baoopn/2026/11

# 2. Download the file and its manifest
rclone copyto "$DIR/$NAME" "$NAME"
rclone copyto "$DIR/$NAME.json" "$NAME.json"

# 3. Check the sha256: both values must match
grep '"sha256"' "$NAME.json"
sha256sum "$NAME"

# 4. Decrypt and check the archive
age -d -i portfolio-backup-age-key.txt -o baoopn.dump "$NAME"
pg_restore --list baoopn.dump | head

# 5. Restore into an empty database
pg_restore --no-owner --no-privileges --exit-on-error --single-transaction \
  -d 'postgres://baoopn:<password>@localhost:5432/baoopn' baoopn.dump

# 6. Delete the plaintext dump
rm baoopn.dump
```

## All settings

| Variable | Default | Meaning |
|---|---|---|
| `BACKUP_AGE_RECIPIENT` | | age public key(s), space or comma separated. Empty: the service idles. |
| `BACKUP_AGE_RECIPIENTS_FILE` | | file with one recipient per line (instead of, or as well as, the above) |
| `BACKUP_AGE_IDENTITY_FILE` | `/run/secrets/backup-age-key` | private key for `restore` and `verify` |
| `BACKUP_S3_ENDPOINT` | | S3 endpoint URL |
| `BACKUP_S3_BUCKET` | | bucket (production: `baoopn-2026-backups`) |
| `BACKUP_S3_ACCESS_KEY_ID` | | access key ID of a bucket-scoped Object Read & Write token |
| `BACKUP_S3_SECRET_ACCESS_KEY` | | secret access key of that token |
| `BACKUP_S3_REGION` | `auto` | region |
| `BACKUP_S3_PROVIDER` | `Cloudflare` | rclone S3 provider name |
| `BACKUP_REMOTE` | | any rclone path; wins over `BACKUP_S3_*` (the dev compose sets `/backups`) |
| `BACKUP_PREFIX` | `baoopn` | key prefix in the bucket |
| `BACKUP_TIME` | `03:00` | daily run time, UTC |
| `BACKUP_ON_START` | `false` | also back up when the container starts |
| `BACKUP_RETRY_MINUTES` | `30` | delay before the one retry; `0` turns it off |
| `BACKUP_KEEP_DAYS` | `30` | delete older backups; `0` keeps all |
| `BACKUP_KEEP_MIN` | `7` | always keep this many newest backups; `0` is allowed |
| `BACKUP_MAX_AGE_HOURS` | `26` | healthcheck limit |
| `BACKUP_PING_URL` | | healthchecks.io-style ping URL |
| `DATABASE_URL` or `PG*` | | the database to back up or restore into |

The compose files set the `PG*` variables for the bundled `postgres` service.
A password in `DATABASE_URL` moves to `PGPASSWORD`, so it never shows in the
process list.

## Limitations

- age does not prove who wrote a file. Anyone with the public key and write
  access to the bucket can upload a backup that decrypts fine. The sha256 in
  the manifest only detects damage, since the same person can write the
  manifest too. Keep the token scoped to this bucket, and turn on R2 object
  lock or versioning if you want old backups to survive a leaked token.
- `BACKUP_PING_URL` is passed to `wget` on its command line, so other
  processes in the container can see it. Use a ping URL that holds no other
  secret.
- `restore --force` replaces only the objects in the backup. Tables that
  exist in the target but not in the backup stay. Empty the schema first for
  a clean copy.

## Testing

`deploy/backup/test/roundtrip.sh` builds the image and runs a full backup and
restore with throwaway Postgres containers and a local Docker volume as the
remote. It needs no cloud access. It compares the row count and a checksum
of every table and the goose version. It also checks the failure cases: no
or bad recipient, wrong or missing key, a changed file, a non-empty target,
partial settings, the retention rules, a stop during a running backup, and
that no plaintext dump lands on disk. The seed data holds a few MB of random
bytes, so verify and restore stream a file larger than a pipe buffer. CI
runs it on every pull request.

```bash
make backup-test
```
