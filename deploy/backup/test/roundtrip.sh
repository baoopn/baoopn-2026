#!/bin/sh
# End-to-end test for the backup image, with plain Docker and throwaway
# containers only (no published ports, no cloud storage):
#
#   sh deploy/backup/test/roundtrip.sh
#
# Starts two postgres:17-alpine containers on an internal network, loads the
# repo's migrations plus seed data into the first, backs it up to a local
# rclone remote (a Docker volume) with a generated test key, restores into the
# second and compares row counts and checksums of every table and the goose
# version. Also checks: refusals (no or invalid recipient, wrong or missing
# key, tampered file, non-empty target), --force, --to-file, DATABASE_URL
# with an encoded password, prune rules, the idle mode, the scheduler, the
# healthcheck and that no plaintext dump lands on disk.
#
# Env: BACKUP_TEST_IMAGE (default baoopn-backup:test), SKIP_BUILD=1 to reuse
# an existing image, KEEP_ON_FAIL=1 to leave the containers.
#
# "A && ok || bad" is safe here: ok and bad always succeed.
# shellcheck disable=SC2015
set -eu
export MSYS_NO_PATHCONV=1 # Git Bash: don't rewrite /paths in docker args

# pwd -W: Windows form of the path for docker (Git Bash); plain pwd elsewhere.
ROOT=$(cd "$(dirname "$0")/../../.." && { pwd -W 2>/dev/null || pwd; })
IMAGE=${BACKUP_TEST_IMAGE:-baoopn-backup:test}
ID="bnrt-$$-$(date +%s)"
NET="$ID-net"
VOL="$ID-remote"
SRC="$ID-src"
DST="$ID-dst"
TMP=$(mktemp -d)
PASS=0
FAILED=0

cleanup() {
	rc=$?
	if [ "$rc" -ne 0 ] && [ "${KEEP_ON_FAIL:-}" = 1 ]; then
		echo "KEEP_ON_FAIL: leaving $SRC $DST $NET $VOL"
	else
		docker rm -f "$SRC" "$DST" "$ID-run" "$ID-run2" "$ID-idle" "$ID-part" "$ID-bad" "$ID-watch" "$ID-term" >/dev/null 2>&1 || true
		docker network rm "$NET" >/dev/null 2>&1 || true
		docker volume rm "$VOL" >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP"
	exit "$rc"
}
trap cleanup EXIT INT TERM

ok() {
	PASS=$((PASS + 1))
	echo "  ok   $*"
}
bad() {
	FAILED=$((FAILED + 1))
	echo "  FAIL $*"
}

# bk HOST ARGS...: runs the backup image against database HOST.
bk() {
	host=$1
	shift
	docker run --rm -i --network "$NET" -v "$VOL:/backups" \
		-e PGHOST="$host" -e PGUSER=baoopn -e PGPASSWORD=rt-password -e PGDATABASE=baoopn \
		-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="${RECIPIENT:-}" \
		-e BACKUP_KEEP_DAYS="${KEEP_DAYS:-30}" -e BACKUP_KEEP_MIN="${KEEP_MIN:-7}" \
		-e TMPDIR="${BK_TMPDIR:-/tmp}" "$IMAGE" "$@"
}

# bk_url HOST ARGS...: like bk, but connects as rturl (password rt@pw) with
# DATABASE_URL only.
bk_url() {
	host=$1
	shift
	docker run --rm -i --network "$NET" -v "$VOL:/backups" -e BACKUP_REMOTE=/backups \
		-e BACKUP_AGE_RECIPIENT="${RECIPIENT:-}" \
		-e DATABASE_URL="postgres://rturl:rt%40pw@$host:5432/baoopn?sslmode=disable" \
		"$IMAGE" "$@"
}

# on_vol SCRIPT: runs a shell script in the image with the remote volume.
on_vol() {
	docker run --rm -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c "$1"
}

# with_key FILE CMD...: runs CMD with FILE on stdin.
with_key() {
	f=$1
	shift
	"$@" <"$f"
}

# expect_fail DESC PATTERN CMD...: CMD must fail and print PATTERN.
expect_fail() {
	desc=$1
	pattern=$2
	shift 2
	if "$@" >"$TMP/out" 2>&1; then
		bad "$desc: command succeeded"
		sed 's/^/       /' "$TMP/out"
	elif grep -q -- "$pattern" "$TMP/out"; then
		ok "$desc"
	else
		bad "$desc: expected '$pattern' in output"
		sed 's/^/       /' "$TMP/out"
	fi
}

psql_in() {
	c=$1
	shift
	docker exec -i "$c" psql -X -q -v ON_ERROR_STOP=1 -U baoopn -d baoopn "$@"
}

# state CONTAINER: "table rows md5" for every base table in schema public,
# then the goose version.
state() {
	docker exec "$1" psql -X -A -t -F ' ' -U baoopn -d baoopn -c "
		SELECT table_name,
			(xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I',
				table_schema, table_name), false, true, '')))[1]::text,
			(xpath('/row/h/text()', query_to_xml(format(
				'SELECT md5(coalesce(string_agg(t::text, E''\n'' ORDER BY t::text), '''')) AS h FROM %I.%I t',
				table_schema, table_name), false, true, '')))[1]::text
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1"
	docker exec "$1" psql -X -A -t -U baoopn -d baoopn \
		-c "SELECT 'goose_max ' || max(version_id) FROM goose_db_version WHERE is_applied"
}

start_pg() {
	docker run -d --name "$1" --network "$NET" -e POSTGRES_USER=baoopn \
		-e POSTGRES_PASSWORD=rt-password -e POSTGRES_DB=baoopn postgres:17-alpine >/dev/null
}

wait_pg() {
	i=0
	until docker exec "$1" pg_isready -U baoopn -d baoopn -h 127.0.0.1 >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || {
			echo "postgres $1 did not start"
			exit 1
		}
		sleep 1
	done
}

# wait_status CONTAINER FILE: waits up to 60 s for a status file.
wait_status() {
	i=0
	until docker exec "$1" test -f "/tmp/backup-status/$2" 2>/dev/null; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || return 1
		sleep 1
	done
}

# no_plaintext DESC CMD...: runs CMD (a backupctl call) with TMPDIR on the
# volume, and fails if a pg_dump archive (PGDMP header) ever shows up there.
no_plaintext() {
	desc=$1
	shift
	on_vol 'mkdir -p /backups/tmp-watch && rm -f /backups/watch.*'
	docker run -d --name "$ID-watch" -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c '
		while [ ! -f /backups/watch.stop ]; do
			for f in $(find /backups/tmp-watch -type f 2>/dev/null); do
				[ "$(head -c 5 "$f" 2>/dev/null)" = PGDMP ] && echo "$f" >>/backups/watch.hits
			done
		done' >/dev/null
	BK_TMPDIR=/backups/tmp-watch
	"$@" >"$TMP/out" 2>&1 || bad "$desc: command failed"
	BK_TMPDIR=
	on_vol 'touch /backups/watch.stop'
	docker wait "$ID-watch" >/dev/null 2>&1 || true
	docker rm -f "$ID-watch" >/dev/null 2>&1 || true
	hits=$(on_vol 'cat /backups/watch.hits 2>/dev/null; find /backups/tmp-watch -type f -name "*.dump" 2>/dev/null')
	[ -z "$hits" ] && ok "$desc: no plaintext dump on disk" || bad "$desc: plaintext on disk: $hits"
	on_vol 'rm -rf /backups/tmp-watch /backups/watch.*'
}

echo "== setup"
if [ "${SKIP_BUILD:-}" != 1 ]; then
	docker build -q -f "$ROOT/deploy/backup/Dockerfile" -t "$IMAGE" "$ROOT/deploy/backup" >/dev/null
fi
docker network create --internal "$NET" >/dev/null
docker volume create "$VOL" >/dev/null
# The image runs as uid 10001; make sure the fresh volume is writable.
docker run --rm -u 0 -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'chown 10001:10001 /backups'
start_pg "$SRC"
start_pg "$DST"
wait_pg "$SRC"
wait_pg "$DST"

docker run --rm "$IMAGE" keygen >"$TMP/key.txt" 2>"$TMP/keygen.err"
docker run --rm "$IMAGE" keygen >"$TMP/wrong.txt" 2>/dev/null
RECIPIENT=$(sed -n 's/^# public key: //p' "$TMP/key.txt")
case $RECIPIENT in age1*) ok "keygen prints an identity on stdout" ;; *) bad "keygen" ;; esac
grep -q AGE-SECRET-KEY "$TMP/key.txt" && ! grep -q AGE-SECRET-KEY "$TMP/keygen.err" &&
	grep -q -F "$RECIPIENT" "$TMP/keygen.err" && ok "keygen prints the recipient on stderr" || bad "keygen stderr"

# Schema: the Up part of every goose migration, then goose's own table.
for f in "$ROOT"/migrations/*.sql; do
	awk '/^-- \+goose Up/ { up = 1; next } /^-- \+goose Down/ { up = 0 } up' "$f"
done | psql_in "$SRC" >/dev/null
versions=
for f in "$ROOT"/migrations/*.sql; do
	v=${f##*/}
	v=${v%%_*}
	versions="$versions $(printf '%s' "$v" | sed 's/^0*//')"
done
{
	echo "CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL,"
	echo "  is_applied boolean NOT NULL, tstamp timestamp DEFAULT now());"
	echo "INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true);"
	for v in $versions; do echo "INSERT INTO goose_db_version (version_id, is_applied) VALUES ($v, true);"; done
	echo "CREATE TABLE rt_seed (id bigserial PRIMARY KEY, label text NOT NULL, payload jsonb, at timestamptz DEFAULT now());"
	echo "INSERT INTO rt_seed (label, payload) SELECT 'row ' || g || ' café ü 日本', jsonb_build_object('n', g, 's', 'naïve ✓') FROM generate_series(1, 5000) g;"
	# Incompressible data: the ciphertext must be far larger than a pipe
	# buffer, so verify and restore really stream it.
	echo "CREATE TABLE rt_blob (id int PRIMARY KEY, b bytea NOT NULL);"
	echo "INSERT INTO rt_blob SELECT g, decode(md5(random()::text) || md5(random()::text), 'hex') FROM generate_series(1, 100000) g;"
	echo "INSERT INTO settings (key, value) VALUES ('rt_profile', '{\"name\": \"Bảo\", \"n\": 1}') ON CONFLICT DO NOTHING;"
	echo "CREATE ROLE rturl LOGIN SUPERUSER PASSWORD 'rt@pw';"
} | psql_in "$SRC" >/dev/null
state "$SRC" >"$TMP/src.state"
echo "   source: $(wc -l <"$TMP/src.state" | tr -d ' ') tables, $(grep goose_max "$TMP/src.state")"
[ "$(psql_in "$SRC" -A -t -c 'SELECT count(*) FROM settings')" -ge 1 ] && ok "settings has rows" || bad "settings is empty"

echo "== safety"
RECIPIENT_SAVE=$RECIPIENT
RECIPIENT=
expect_fail "backup without a recipient is refused" "never stored unencrypted" bk "$SRC" backup
RECIPIENT=$RECIPIENT_SAVE
n=$(on_vol 'find /backups -type f | wc -l')
[ "$n" -eq 0 ] && ok "nothing was uploaded without a recipient" || bad "files found after refusal: $n"
RECIPIENT="not-a-key"
expect_fail "invalid recipient is refused" "invalid age recipient" bk "$SRC" backup
RECIPIENT=$RECIPIENT_SAVE

echo "== backup"
if bk "$SRC" backup >"$TMP/name" 2>"$TMP/out"; then ok "backup with PG*"; else
	bad "backup"
	cat "$TMP/out"
	exit 1
fi
NAME=$(tail -n 1 "$TMP/name")
sleep 1 # the next backup gets a new name
if bk_url "$SRC" backup >"$TMP/name2" 2>"$TMP/out"; then ok "backup with DATABASE_URL and an encoded password"; else
	bad "backup with DATABASE_URL"
	cat "$TMP/out"
fi
grep -q -F -e 'rt@pw' -e 'rt%40pw' "$TMP/out" "$TMP/name2" && bad "DATABASE_URL password in the output" ||
	ok "DATABASE_URL password not in the output"
bk "$SRC" list >"$TMP/list" 2>&1
grep -q "$NAME" "$TMP/list" && ok "list shows $NAME" || bad "list: $(cat "$TMP/list")"
on_vol "cat /backups/baoopn/*/*/$NAME.json" >"$TMP/manifest"
grep -q '"format": "pg_dump-custom+age"' "$TMP/manifest" && grep -q '"goose_version": [0-9][0-9]*,' "$TMP/manifest" &&
	grep -q '"sha256": "[0-9a-f]\{64\}"' "$TMP/manifest" &&
	ok "manifest has format, sha256 and a numeric goose version" || bad "manifest: $(cat "$TMP/manifest")"
if on_vol "head -c 64 /backups/baoopn/*/*/$NAME | grep -q age-encryption.org"; then
	ok "object starts with the age header"
else bad "object is not age ciphertext"; fi
size=$(on_vol "cat /backups/baoopn/*/*/$NAME | wc -c" | tr -d ' ')
[ "$size" -gt 2000000 ] && ok "ciphertext is larger than 2 MB ($size bytes)" || bad "ciphertext too small: $size"
ntab=$(psql_in "$SRC" -A -t -c "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'")
grep -q "done ($ntab tables in schema public" "$TMP/out" && ok "backup counts $ntab tables in schema public, as restore does" ||
	bad "backup table count: $(grep 'done (' "$TMP/out")"

echo "== verify / restore"
if bk "" verify latest -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then ok "verify latest -i - (correct key, large file)"; else
	bad "verify"
	cat "$TMP/out"
fi
grep -q "ok, $ntab tables in schema public.*schema version [0-9]" "$TMP/out" &&
	ok "verify reports the same table count and the schema version" || bad "verify log: $(tail -n 1 "$TMP/out")"
# A key saved through a terminal (CRLF) still works.
sed 's/$/\r/' "$TMP/key.txt" >"$TMP/key-crlf.txt"
bk "" verify latest -i - <"$TMP/key-crlf.txt" >"$TMP/out" 2>&1 && ok "verify with a CRLF key file" ||
	bad "CRLF key: $(tail -n 2 "$TMP/out")"
expect_fail "verify with the wrong identity fails" "decryption failed" \
	with_key "$TMP/wrong.txt" bk "" verify latest -i -
expect_fail "restore without an identity fails" "identity not readable" bk "$DST" restore latest

if bk "$DST" restore latest -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then ok "restore into an empty database"; else
	bad "restore"
	cat "$TMP/out"
fi
state "$DST" >"$TMP/dst.state"
if cmp -s "$TMP/src.state" "$TMP/dst.state"; then
	ok "row counts and checksums match for $(($(wc -l <"$TMP/src.state") - 1)) tables + goose version"
else
	bad "restored data differs"
	diff "$TMP/src.state" "$TMP/dst.state" || true
fi

expect_fail "restore into a non-empty database is refused" "refusing to restore" \
	with_key "$TMP/key.txt" bk "$DST" restore latest -i -
psql_in "$DST" -c "DELETE FROM rt_seed WHERE id <= 100" >/dev/null
if bk "$DST" restore "$NAME" --force -i - <"$TMP/key.txt" >"$TMP/out" 2>&1; then ok "restore --force by name"; else
	bad "restore --force"
	cat "$TMP/out"
fi
state "$DST" >"$TMP/dst.state"
cmp -s "$TMP/src.state" "$TMP/dst.state" && ok "row counts and checksums match after --force" ||
	bad "restored data differs after --force"

if docker run --rm -i -v "$VOL:/backups" -e BACKUP_REMOTE=/backups --entrypoint sh "$IMAGE" -c \
	'backupctl restore latest --to-file /tmp/out.dump -i - && pg_restore --list /tmp/out.dump | grep -q "TABLE DATA public rt_seed"' \
	<"$TMP/key.txt" >"$TMP/out" 2>&1; then
	ok "restore --to-file writes a dump pg_restore --list accepts (no database)"
else
	bad "--to-file"
	cat "$TMP/out"
fi
# An existing PATH: refused without --force, kept on a failed decryption.
if docker run --rm -i -v "$VOL:/backups" -e BACKUP_REMOTE=/backups --entrypoint sh "$IMAGE" -c '
	cat >/tmp/wrong.txt
	echo keep >/tmp/out.dump
	backupctl restore latest --to-file /tmp/out.dump -i /tmp/wrong.txt 2>/tmp/err && exit 1
	grep -q "already exists" /tmp/err && [ "$(cat /tmp/out.dump)" = keep ] || exit 2
	backupctl restore latest --to-file /tmp/out.dump --force -i /tmp/wrong.txt 2>/tmp/err && exit 3
	grep -q "decryption failed" /tmp/err || exit 4
	[ "$(cat /tmp/out.dump)" = keep ] && [ ! -e /tmp/out.dump.partial ] || exit 5' \
	<"$TMP/wrong.txt" >"$TMP/out" 2>&1; then
	ok "--to-file refuses an existing file and keeps it when decryption fails"
else
	bad "--to-file with an existing file (exit $?)"
	cat "$TMP/out"
fi

no_plaintext "verify" with_key "$TMP/key.txt" bk "" verify latest -i -
no_plaintext "restore --force" with_key "$TMP/key.txt" bk "$DST" restore latest --force -i -

echo "== scheduler"
docker run -d --name "$ID-idle" -e BACKUP_REMOTE=/backups "$IMAGE" run >/dev/null
sleep 2
docker exec "$ID-idle" backupctl health | grep -q '^idle' && ok "unconfigured service idles; health idle" || bad "idle health"
docker logs "$ID-idle" 2>&1 | grep -q "WARN.*disabled" && ok "idle logs a WARN" || bad "idle WARN"
t0=$(date +%s)
docker stop "$ID-idle" >/dev/null
[ $(($(date +%s) - t0)) -lt 8 ] && ok "docker stop is quick (SIGTERM handled)" || bad "docker stop took too long"

# SIGTERM during a running backup: a lock makes pg_dump wait, then stop.
before=$(on_vol 'find /backups/baoopn -name "*.dump.age" | wc -l' | tr -d ' ')
docker exec -d "$SRC" psql -U baoopn -d baoopn -c "BEGIN; LOCK TABLE rt_seed; SELECT pg_sleep(60);"
on_vol 'mkdir -p /backups/tmp-term'
docker run -d --name "$ID-term" --network "$NET" -v "$VOL:/backups" -e TMPDIR=/backups/tmp-term \
	-e PGHOST="$SRC" -e PGUSER=baoopn -e PGPASSWORD=rt-password -e PGDATABASE=baoopn \
	-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_ON_START=true "$IMAGE" run >/dev/null
i=0
until docker logs "$ID-term" 2>&1 | grep -q "dumping and encrypting"; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || break
	sleep 1
done
sleep 1
t0=$(date +%s)
docker stop "$ID-term" >/dev/null
took=$(($(date +%s) - t0))
left=$(on_vol 'find /backups/tmp-term -mindepth 1 | wc -l' | tr -d ' ')
after=$(on_vol 'find /backups/baoopn -name "*.dump.age" | wc -l' | tr -d ' ')
if [ "$took" -lt 8 ] && [ "$left" -eq 0 ] && [ "$after" -eq "$before" ] &&
	docker logs "$ID-term" 2>&1 | grep -q "interrupting the running job"; then
	ok "docker stop interrupts a running backup quickly and removes its temp files"
else
	bad "stop during a backup: ${took}s, $left temp entries, objects $before -> $after"
	docker logs "$ID-term" 2>&1 | tail -n 5
fi
docker rm -f "$ID-term" >/dev/null
psql_in "$SRC" -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE '%pg_sleep(60)%' AND pid <> pg_backend_pid()" >/dev/null
on_vol 'rm -rf /backups/tmp-term'

docker run -d --name "$ID-part" -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_S3_BUCKET=rt-bucket-value \
	-e BACKUP_S3_ACCESS_KEY_ID=rt-key-value "$IMAGE" run >/dev/null
sleep 2
docker logs "$ID-part" >"$TMP/part.log" 2>&1
if grep -q 'BACKUP_S3_ENDPOINT' "$TMP/part.log" && grep -q 'BACKUP_S3_SECRET_ACCESS_KEY' "$TMP/part.log" &&
	! grep -q -F -e rt-bucket-value -e rt-key-value -e "$RECIPIENT" "$TMP/part.log" &&
	docker exec "$ID-part" backupctl health | grep -q '^idle'; then
	ok "partial S3 config idles and names the missing variables (no values)"
else
	bad "partial S3 config: $(cat "$TMP/part.log")"
fi
docker rm -f "$ID-part" >/dev/null

docker run -d --name "$ID-bad" -v "$VOL:/backups" -e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT=age1notakey \
	-e PGHOST="$SRC" -e PGUSER=baoopn -e PGDATABASE=baoopn "$IMAGE" run >/dev/null
sleep 3
if docker logs "$ID-bad" 2>&1 | grep -q 'ERROR.*invalid age recipient' &&
	! docker exec "$ID-bad" backupctl health | grep -q '^idle'; then
	ok "invalid recipient logs an ERROR and keeps scheduling (not idle)"
else
	bad "invalid recipient in the scheduler: $(docker logs "$ID-bad" 2>&1)"
fi
docker rm -f "$ID-bad" >/dev/null

sleep 1
docker run -d --name "$ID-run" --network "$NET" -v "$VOL:/backups" \
	-e PGHOST="$SRC" -e PGUSER=baoopn -e PGPASSWORD=rt-password -e PGDATABASE=baoopn \
	-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_ON_START=true "$IMAGE" run >/dev/null
wait_status "$ID-run" last_success || true
i=0
until docker logs "$ID-run" 2>&1 | grep -q "next backup at"; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || break
	sleep 1
done
docker exec "$ID-run" backupctl health | grep -q '^ok: last backup' && ok "BACKUP_ON_START backs up; health ok" ||
	bad "scheduler health: $(docker exec "$ID-run" backupctl health)"
docker logs "$ID-run" 2>&1 | grep -q "next backup at" && ok "scheduler waits for BACKUP_TIME" || bad "no next-run log"
docker logs "$ID-run" >"$TMP/run.log" 2>&1
rc=0
grep -q -F -e "$RECIPIENT" -e rt-password "$TMP/run.log" || rc=$?
[ "$rc" -eq 1 ] && ok "logs contain no recipient or password" || bad "logs leak config values (grep exit $rc)"
docker rm -f "$ID-run" >/dev/null

docker run -d --name "$ID-run2" --network "$NET" -v "$VOL:/backups" \
	-e PGHOST="$SRC" -e PGUSER=baoopn -e PGPASSWORD=rt-password -e PGDATABASE=baoopn \
	-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="$RECIPIENT" "$IMAGE" run >/dev/null
wait_status "$ID-run2" first_due || true
docker exec "$ID-run2" backupctl health | grep -q '^ok: last backup' &&
	ok "a new container reads the last success from the remote" ||
	bad "health after redeploy: $(docker exec "$ID-run2" backupctl health)"
docker rm -f "$ID-run2" >/dev/null

REAL=$(bk "$SRC" list 2>/dev/null | grep -c '^baoopn-')
NAME2=$(bk "$SRC" list 2>/dev/null | sed -n '2p' | cut -d' ' -f1)
[ "$REAL" -eq 3 ] && [ -n "$NAME2" ] && [ "$NAME2" != "$NAME" ] && ok "three backups listed" ||
	bad "expected 3 backups, found $REAL"

echo "== tamper"
on_vol "f=\$(ls /backups/baoopn/*/*/$NAME2); printf 'X' | dd of=\"\$f\" bs=1 seek=200 conv=notrunc 2>/dev/null"
expect_fail "tampered ciphertext fails the sha256 check" "sha256 mismatch" \
	with_key "$TMP/key.txt" bk "" verify "$NAME2" -i -
grep -q 'decrypting' "$TMP/out" && bad "tampered file was decrypted" || ok "tampered file is not decrypted"

echo "== prune"
# Ten complete, old (2020) backups, an old orphan dump and an old orphan
# manifest, plus a foreign object pair that prune must never touch or count.
# shellcheck disable=SC2016 # expands in the container
on_vol '
	d=/backups/baoopn/2020/01; mkdir -p $d
	for day in 01 02 03 04 05 06 07 08 09 10; do
		n=baoopn-202001${day}T030000Z.dump.age
		echo fake >$d/$n; echo "{}" >$d/$n.json
	done
	mkdir -p /backups/baoopn/2019/12 /backups/baoopn/2030/01
	echo orphan >/backups/baoopn/2019/12/baoopn-20191231T030000Z.dump.age
	echo "{}" >/backups/baoopn/2019/12/baoopn-20191230T030000Z.dump.age.json
	echo foreign >/backups/baoopn/2030/01/other-20300101T000000Z.dump.age
	echo "{}" >/backups/baoopn/2030/01/other-20300101T000000Z.dump.age.json'
count() { on_vol "find /backups -name '$1' | wc -l" | tr -d ' '; }
KEEP_DAYS=0 bk "$SRC" prune >/dev/null 2>&1
[ "$(count '*.dump.age')" -eq $((REAL + 12)) ] && ok "BACKUP_KEEP_DAYS=0 keeps everything" ||
	bad "KEEP_DAYS=0 left $(count '*.dump.age')"
KEEP_DAYS=30 KEEP_MIN=7 bk "$SRC" prune >"$TMP/out" 2>&1 || bad "prune exited non-zero"
left=$(on_vol 'find /backups -name "baoopn-*.dump.age" | sed "s#.*/##" | sort')
old_kept=$((7 - REAL))
oldest_kept=$(printf '202001%02d' $((11 - old_kept)))
dropped=$(printf '202001%02d' $((10 - old_kept)))
n=$(printf '%s\n' "$left" | wc -l | tr -d ' ')
if [ "$n" -eq 7 ] && printf '%s\n' "$left" | grep -q "$NAME" && printf '%s\n' "$left" | grep -q "$oldest_kept" &&
	! printf '%s\n' "$left" | grep -q -e "$dropped" -e 20191231; then
	ok "prune keeps the newest KEEP_MIN=7 ($REAL new + $old_kept old), drops older ones and the orphan"
else
	bad "prune left $n: $left"
	cat "$TMP/out"
fi
[ "$(count 'baoopn-*.json')" -eq 7 ] && ok "prune removes manifests with their dumps and orphan manifests" ||
	bad "manifests left: $(count 'baoopn-*.json')"
[ "$(count 'other-*')" -eq 2 ] && ok "foreign objects are untouched and not counted" || bad "foreign objects changed"
KEEP_DAYS=30 KEEP_MIN=0 bk "$SRC" prune >"$TMP/out" 2>&1 || bad "prune KEEP_MIN=0 exited non-zero"
if [ "$(count 'baoopn-2020*')" -eq 0 ] && [ "$(count 'baoopn-*.dump.age')" -eq "$REAL" ] && [ "$(count 'other-*')" -eq 2 ]; then
	ok "BACKUP_KEEP_MIN=0 deletes every old backup and keeps the recent ones"
else
	bad "KEEP_MIN=0 left: $(on_vol 'find /backups -type f')"
	cat "$TMP/out"
fi

echo
echo "roundtrip: $PASS passed, $FAILED failed"
[ "$FAILED" -eq 0 ]
