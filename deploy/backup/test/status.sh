#!/bin/sh
# Test for the backup status the sidecar writes to the database (the admin
# page reads it), with plain Docker and throwaway containers only:
#
#   sh deploy/backup/test/status.sh
#
# One postgres:17-alpine container holds four databases: baoopn and idle
# (the repo's migrations), plain (one table, no status tables) and restored
# (empty). Backups go to a local rclone remote (a Docker volume). Checks:
# the heartbeat (no secret values), run-now requests, admin overrides and
# bad values, pause, a database outage, resume after a long pause, a new
# daily time, run-now across the daily time, a lost final write, old
# requests, SQL safety, a database without the status tables, a read-only
# role, interrupted runs, trimming old runs, CLI runs, a restore without
# status data, a quick docker stop and the idle mode. The services tick
# every 2 s (BACKUP_TICK_SECONDS) so the waits stay short; one more service
# keeps the default 30 s tick for the docker stop check.
#
# Env: BACKUP_TEST_IMAGE (default baoopn-backup:test), SKIP_BUILD=1 to reuse
# an existing image, KEEP_ON_FAIL=1 to leave the containers.
#
# "A && ok || bad" is safe here: ok and bad always succeed.
# shellcheck disable=SC2015
set -eu
export MSYS_NO_PATHCONV=1 # Git Bash: don't rewrite /paths in docker args

ROOT=$(cd "$(dirname "$0")/../../.." && { pwd -W 2>/dev/null || pwd; })
IMAGE=${BACKUP_TEST_IMAGE:-baoopn-backup:test}
ID="bnst-$$-$(date +%s)"
NET="$ID-net"
VOL="$ID-remote"
PG="$ID-pg"
TMP=$(mktemp -d)
PASS=0
FAILED=0
# Tick and status probe interval of the services, in seconds.
TICK=2
PROBE=5
# A daily time 12 hours away: no scheduled backup during the test.
FAR=$(date -u -d "@$(($(date +%s) + 43200))" +%H:%M)

cleanup() {
	rc=$?
	if [ "$rc" -ne 0 ] && [ "${KEEP_ON_FAIL:-}" = 1 ]; then
		echo "KEEP_ON_FAIL: leaving $ID-* containers, $NET and $VOL"
	else
		for c in pg main idle evil plain ro slow; do docker rm -f "$ID-$c" >/dev/null 2>&1 || true; done
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

# q DB SQL: one value (or rows) from database DB.
q() {
	docker exec -i "$PG" psql -X -q -A -t -v ON_ERROR_STOP=1 -U baoopn -d "$1" -c "$2"
}

# is DB SQL WANT: the query returns exactly WANT.
is() { [ "$(q "$1" "$2" 2>/dev/null)" = "$3" ]; }

# wait_for SECONDS CMD...: polls CMD once a second.
wait_for() {
	n=$1
	shift
	i=0
	until "$@" >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt "$n" ] || return 1
		sleep 1
	done
}

logs() { docker logs "$ID-$1" 2>&1; }
in_logs() { logs "$1" | grep -q -F -- "$2"; }
health() { docker exec "$ID-$1" backupctl health 2>&1; }
health_is() { health "$1" | grep -q -- "$2"; }

# svc NAME DB [docker args...]: the scheduler against database DB. PGU and
# PGP pick the role; RCPT overrides the recipient. A later -e flag overrides
# a default (docker keeps the last value).
svc() {
	n=$1
	d=$2
	shift 2
	docker run -d --name "$ID-$n" --network "$NET" -v "$VOL:/backups" \
		-e PGHOST="$PG" -e PGUSER="${PGU:-baoopn}" -e PGPASSWORD="${PGP:-st-password}" -e PGDATABASE="$d" \
		-e BACKUP_REMOTE=/backups -e BACKUP_AGE_RECIPIENT="${RCPT:-$RECIPIENT}" -e BACKUP_TIME="$FAR" \
		-e BACKUP_TICK_SECONDS="$TICK" "$@" "$IMAGE" run >/dev/null
}

# queue DB: a run-now request, as the API makes it; prints its id.
queue() {
	q "$1" "INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', now()) RETURNING id"
}

# set_backup JSON: the admin settings doc.
set_backup() {
	q baoopn "INSERT INTO settings (key, value) VALUES ('backup', '$1')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()" >/dev/null
}

echo "== setup"
if [ "${SKIP_BUILD:-}" != 1 ]; then
	docker build -q -f "$ROOT/deploy/backup/Dockerfile" -t "$IMAGE" "$ROOT/deploy/backup" >/dev/null
fi
docker network create --internal "$NET" >/dev/null
docker volume create "$VOL" >/dev/null
docker run --rm -u 0 -v "$VOL:/backups" --entrypoint sh "$IMAGE" -c 'chown 10001:10001 /backups'
docker run -d --name "$PG" --network "$NET" -e POSTGRES_USER=baoopn -e POSTGRES_PASSWORD=st-password \
	-e POSTGRES_DB=baoopn postgres:17-alpine >/dev/null
docker run --rm "$IMAGE" keygen >"$TMP/key.txt" 2>/dev/null
RECIPIENT=$(sed -n 's/^# public key: //p' "$TMP/key.txt")
wait_for 60 docker exec "$PG" pg_isready -U baoopn -d baoopn -h 127.0.0.1 || {
	echo "postgres did not start"
	exit 1
}
sleep 1
for d in idle plain restored; do q baoopn "CREATE DATABASE $d" >/dev/null; done
# Schema: the Up part of every goose migration, then goose's own table.
schema() {
	for f in "$ROOT"/migrations/*.sql; do
		awk '/^-- \+goose Up/ { up = 1; next } /^-- \+goose Down/ { up = 0 } up' "$f"
	done
	echo "CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL,"
	echo "  is_applied boolean NOT NULL, tstamp timestamp DEFAULT now());"
	echo "INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true);"
}
for d in baoopn idle; do schema | docker exec -i "$PG" psql -X -q -v ON_ERROR_STOP=1 -U baoopn -d "$d" >/dev/null; done
q plain "CREATE TABLE notes (id int PRIMARY KEY, body text); INSERT INTO notes VALUES (1, 'hello')" >/dev/null
q baoopn "CREATE ROLE ro LOGIN PASSWORD 'ro-pass'; GRANT USAGE ON SCHEMA public TO ro;
	GRANT SELECT ON ALL TABLES IN SCHEMA public TO ro; GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO ro" >/dev/null
# A run left "running" by a stopped container, 250 old finished runs, and
# a run-now request for the idle service.
STALE=$(q baoopn "INSERT INTO backup_runs (kind, status, started_at) VALUES ('scheduled', 'running', now() - interval '1 hour') RETURNING id")
q baoopn "INSERT INTO backup_runs (kind, status, started_at, finished_at)
	SELECT 'scheduled', 'ok', now() - g * interval '1 day', now() - g * interval '1 day' FROM generate_series(1, 250) g" >/dev/null
IDLE_RUN=$(queue idle)
q idle "INSERT INTO settings (key, value) VALUES ('backup', '{\"keep_days\": 9}')" >/dev/null

svc main baoopn -e BACKUP_STATUS_PROBE_SECONDS="$PROBE"
# S3 settings without an endpoint: idles. Marker values must never show.
docker run -d --name "$ID-idle" --network "$NET" -e PGHOST="$PG" -e PGUSER=baoopn -e PGPASSWORD=st-password \
	-e PGDATABASE=idle -e BACKUP_AGE_RECIPIENT="$RECIPIENT" -e BACKUP_TICK_SECONDS="$TICK" -e BACKUP_S3_BUCKET=st-bucket \
	-e BACKUP_S3_ACCESS_KEY_ID=st-marker-key -e BACKUP_S3_SECRET_ACCESS_KEY=st-marker-secret \
	-e BACKUP_PING_URL=https://st-marker-ping.invalid/x "$IMAGE" run >/dev/null

echo "== 1 heartbeat"
if wait_for 40 is baoopn "SELECT count(*) FROM backup_agent" 1 &&
	is baoopn "SELECT state || ' ' || destination || ' ' || recipient_set || ' ' || version FROM backup_agent" "waiting rclone true dev"; then
	ok "heartbeat row within 40 s: waiting, rclone, recipient set"
else
	bad "heartbeat: $(q baoopn 'SELECT row_to_json(a) FROM backup_agent a')"
fi
wait_for 40 is idle "SELECT state FROM backup_agent" idle || true
q idle "SELECT row_to_json(a) FROM backup_agent a" >"$TMP/idle.row"
logs idle >"$TMP/idle.log"
if grep -q '"state":"idle"' "$TMP/idle.row" && grep -q 'BACKUP_S3_ENDPOINT' "$TMP/idle.row" &&
	grep -q '"s3_secret_set":true' "$TMP/idle.row" && grep -q '"ping_url_set":true' "$TMP/idle.row" &&
	grep -q '"s3_endpoint_set":false' "$TMP/idle.row" && ! grep -q -F -e st-marker -e "$RECIPIENT" "$TMP/idle.row" "$TMP/idle.log"; then
	ok "idle agent row names what is missing and holds no secret values"
else
	bad "idle row: $(cat "$TMP/idle.row")"
fi
is idle "SELECT (SELECT updated_at FROM settings WHERE key = 'backup') = settings_applied_at FROM backup_agent" t &&
	ok "an idle service still marks the settings as applied" || bad "idle settings_applied_at not set"

echo "== 8 interrupted run"
is baoopn "SELECT status || ': ' || error FROM backup_runs WHERE id = $STALE" \
	"failed: Interrupted: the backup service stopped during this run" &&
	ok "a run left running is marked failed at start" || bad "stale run: $(q baoopn "SELECT status, error FROM backup_runs WHERE id = $STALE")"

echo "== 13 idle"
if wait_for 70 is idle "SELECT status FROM backup_runs WHERE id = $IDLE_RUN" failed &&
	q idle "SELECT error FROM backup_runs WHERE id = $IDLE_RUN" | grep -q '^Backups are not configured: .*BACKUP_S3_ENDPOINT'; then
	ok "idle service fails a run-now request with the reason"
else
	bad "idle request: $(q idle "SELECT status, error FROM backup_runs WHERE id = $IDLE_RUN")"
fi

echo "== 2 run now"
RUN=$(queue baoopn)
if wait_for 60 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" ok; then
	row=$(q baoopn "SELECT name || ' ' || size_bytes || ' ' || sha256 || ' ' || tables || ' ' || pruned FROM backup_runs WHERE id = $RUN")
	name=${row%% *}
	if [ "$(echo "$row" | wc -w)" -eq 5 ] && echo "$row" | grep -q ' [0-9a-f]\{64\} ' &&
		docker run --rm -v "$VOL:/backups" --entrypoint find "$IMAGE" /backups -name "$name" | grep -q .; then
		ok "a queued run turns ok within 60 s with name, size, sha256, tables and pruned; object present"
	else
		bad "run row: $row"
	fi
else
	bad "queued run: $(q baoopn "SELECT status, error FROM backup_runs WHERE id = $RUN")"
fi

echo "== 9 trim"
n=$(q baoopn "SELECT count(*) FROM backup_runs WHERE status IN ('ok', 'failed')")
[ "$n" -le 200 ] && is baoopn "SELECT count(*) FROM backup_runs WHERE id = $RUN" 1 &&
	ok "250 old runs trimmed to $n, the newest kept" || bad "trim left $n finished runs"

echo "== 3 overrides"
set_backup '{"keep_days":14,"max_age_hours":48}'
if wait_for 40 in_logs main "settings: keep 14 days (admin override)" &&
	wait_for 10 sh -c "[ \"\$(docker exec $ID-main cat /tmp/backup-status/max_age_hours)\" = 48 ]"; then
	ok "keep_days override applies and is logged; max_age_hours status file follows"
else
	bad "override: $(logs main | grep settings)"
fi
wait_for 40 is baoopn "SELECT (SELECT updated_at FROM settings WHERE key = 'backup') = settings_applied_at FROM backup_agent" t &&
	ok "settings_applied_at matches the settings row" || bad "settings_applied_at not set"
set_backup '{"keep_days":99999,"max_age_hours":48}'
if wait_for 40 in_logs main 'ignoring keep_days "99999"' && wait_for 5 in_logs main "settings: keep 30 days (default)" &&
	is baoopn "SELECT env_keep_days FROM backup_agent" 30; then
	ok "an invalid keep_days logs a WARN and falls back to the env value"
else
	bad "invalid override: $(logs main | grep settings)"
fi

echo "== 4 paused"
set_backup '{"paused":true,"paused_at":"2026-01-01T00:00:00Z"}'
if wait_for 40 health_is main '^ok: paused' && wait_for 5 is baoopn "SELECT state || ' ' || (next_run_at IS NULL) FROM backup_agent" "paused true"; then
	ok "paused: health ok: paused, state paused, no next run"
else
	bad "paused: $(health main); $(q baoopn 'SELECT state, next_run_at FROM backup_agent')"
fi
RUN=$(queue baoopn)
wait_for 60 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" ok && ok "a queued run still runs while paused" ||
	bad "queued while paused: $(q baoopn "SELECT status, error FROM backup_runs WHERE id = $RUN")"
echo "== 4b database outage while paused"
docker stop "$PG" >/dev/null
sleep $((TICK + PROBE + 15)) # more than a tick and a probe
health_is main '^ok: paused' && in_logs main "status reporting failed" &&
	ok "while the database is down the service stays paused and warns" || bad "outage: $(health main)"
docker start "$PG" >/dev/null
wait_for 60 docker exec "$PG" pg_isready -U baoopn -d baoopn -h 127.0.0.1 || true
wait_for 60 in_logs main "status reporting back" && health_is main '^ok: paused' &&
	ok "after the outage reporting comes back, still paused" || bad "after outage: $(health main)"

echo "== 4c resume after a long pause"
# The last success looks 50 years old: resuming must catch up at once.
docker exec "$ID-main" sh -c 'echo 1 >/tmp/backup-status/last_success'
before=$(q baoopn "SELECT coalesce(max(id), 0) FROM backup_runs")
set_backup '{"paused":false}'
if wait_for 60 in_logs main "resumed: no backup in the last" &&
	wait_for 60 is baoopn "SELECT count(*) FROM backup_runs WHERE id > $before AND kind = 'scheduled' AND status = 'ok'" 1 &&
	health_is main '^ok: last backup'; then
	ok "resuming with an old last success backs up at once; health ok"
else
	bad "resume catch-up: $(health main); $(logs main | grep resum)"
fi

echo "== 14 earlier backup time"
EARLY=$(date -u -d "@$(($(date +%s) - 3600))" +%H:%M)
set_backup "{\"backup_time\":\"$EARLY\"}"
wait_for 40 is baoopn "SELECT (SELECT updated_at FROM settings WHERE key = 'backup') = settings_applied_at FROM backup_agent" t || true
is baoopn "SELECT next_run_at < now() + interval '13 hours' FROM backup_agent" t &&
	ok "a time already past today keeps the sooner planned run (no skipped day)" ||
	bad "next run after $EARLY: $(q baoopn 'SELECT next_run_at FROM backup_agent')"

# lock_hold SECONDS SQL: holds a lock in the background; unlock SECONDS ends it.
lock_hold() { docker exec -d "$PG" psql -U baoopn -d baoopn -c "BEGIN; $2; SELECT pg_sleep($1);"; }
unlock() {
	q baoopn "SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE query LIKE '%pg_sleep($1)%' AND pid <> pg_backend_pid()" >/dev/null
}

echo "== 15 run now across the daily time"
# The first minute boundary at least 15 s ahead (15 to 74 s).
soon=$((($(date +%s) + 15 + 59) / 60 * 60))
SOON=$(date -u -d "@$soon" +%H:%M)
lock_hold 181 "LOCK TABLE projects" # pg_dump waits for it
set_backup "{\"backup_time\":\"$SOON\"}"
RUN=$(queue baoopn)
wait_for 40 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" running || true
while [ "$(date +%s)" -le $((soon + 5)) ]; do sleep 1; done
unlock 181
wait_for 60 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" ok || true
sleep $((2 * TICK + 5)) # a tick and more for a second backup to show
if is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" ok &&
	is baoopn "SELECT count(*) FROM backup_runs WHERE id > $RUN" 0 &&
	is baoopn "SELECT next_run_at > now() + interval '23 hours' FROM backup_agent" t; then
	ok "a run-now backup that ends after the daily time counts as that run (one backup)"
else
	bad "across the daily time: $(q baoopn "SELECT id, kind, status FROM backup_runs WHERE id >= $RUN")"
fi

echo "== 16 lost final write"
lock_hold 121 "LOCK TABLE projects"
RUN=$(queue baoopn)
wait_for 40 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" running || true
OLD=$(q baoopn "INSERT INTO backup_runs (kind, status, started_at) VALUES ('cli', 'running', now() - interval '7 hours') RETURNING id")
NEW=$(q baoopn "INSERT INTO backup_runs (kind, status, started_at) VALUES ('cli', 'running', now()) RETURNING id")
# Run writes fail (lock_timeout) for 15 s after the backup ends.
lock_hold 15 "LOCK TABLE backup_runs IN EXCLUSIVE MODE"
sleep 1
unlock 121
if wait_for 90 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" ok &&
	in_logs main "status reporting failed (run)" && wait_for 40 in_logs main "status reporting back (run)"; then
	ok "a failed final write is retried until the run row is ok"
else
	bad "lost write: $(q baoopn "SELECT status FROM backup_runs WHERE id = $RUN")"
fi
wait_for 40 is baoopn "SELECT status FROM backup_runs WHERE id = $OLD" failed &&
	is baoopn "SELECT status FROM backup_runs WHERE id = $NEW" running &&
	ok "a run left running for 7 hours fails; a recent one (another process) is kept" ||
	bad "sweep: $(q baoopn "SELECT id, status, error FROM backup_runs WHERE id IN ($OLD, $NEW)")"
q baoopn "DELETE FROM backup_runs WHERE id = $NEW" >/dev/null

echo "== 17 old request"
last=$(q baoopn "SELECT max(id) FROM backup_runs")
OLDQ=$(q baoopn "INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', now() - interval '11 minutes') RETURNING id")
if wait_for 40 is baoopn "SELECT error FROM backup_runs WHERE id = $OLDQ" "Not picked up by the backup service within 10 minutes"; then
	sleep 5
	is baoopn "SELECT count(*) FROM backup_runs WHERE id > $last AND id <> $OLDQ" 0 &&
		ok "a request older than 10 minutes fails without a backup" || bad "a backup ran for an old request"
else
	bad "old request: $(q baoopn "SELECT status, error FROM backup_runs WHERE id = $OLDQ")"
fi
set_backup '{}'

echo "== 10 cli"
if docker exec "$ID-main" backupctl backup >/dev/null 2>"$TMP/cli.err" &&
	is baoopn "SELECT kind || ' ' || status FROM backup_runs ORDER BY id DESC LIMIT 1" "cli ok"; then
	ok "backupctl backup records a cli run"
else
	bad "cli run: $(tail -n 3 "$TMP/cli.err")"
fi

echo "== 11 restore"
if docker run --rm -i --network "$NET" -v "$VOL:/backups" -e PGHOST="$PG" -e PGUSER=baoopn \
	-e PGPASSWORD=st-password -e PGDATABASE=restored -e BACKUP_REMOTE=/backups "$IMAGE" \
	restore latest -i - <"$TMP/key.txt" >"$TMP/out" 2>&1 &&
	is restored "SELECT (SELECT count(*) FROM backup_runs) + (SELECT count(*) FROM backup_agent)" 0 &&
	is restored "SELECT count(*) FROM settings WHERE key = 'backup'" 1; then
	ok "a restore has empty backup_runs and backup_agent and keeps the backup settings"
else
	bad "restore: $(tail -n 3 "$TMP/out")"
fi

echo "== 12 stop"
sleep 2
t0=$(date +%s)
docker stop "$ID-main" >/dev/null
took=$(($(date +%s) - t0))
[ "$took" -lt 5 ] && ok "docker stop takes ${took}s" || bad "docker stop took ${took}s"
# The default 30 s tick, against plain: no status writes. A stop during the
# tick sleep must not wait for the sleep to end.
svc slow plain -e BACKUP_TICK_SECONDS=30
if wait_for 40 in_logs slow "next backup at"; then
	sleep 2
	t0=$(date +%s)
	docker stop "$ID-slow" >/dev/null
	took=$(($(date +%s) - t0))
	[ "$took" -lt 5 ] && ok "docker stop during a 30 s tick sleep takes ${took}s" ||
		bad "docker stop (30 s tick) took ${took}s"
else
	bad "slow: no next backup log: $(logs slow | tail -n 3)"
fi

echo "== 5 sql safety"
# SQL in the recipient (fails the run) and in the prefix (stored as text).
EVIL="x'); DROP TABLE settings; --"
RUN=$(queue baoopn)
RCPT="age1'); DROP TABLE settings; --" svc evil baoopn -e BACKUP_PREFIX="$EVIL"
if wait_for 60 is baoopn "SELECT status FROM backup_runs WHERE id = $RUN" failed &&
	q baoopn "SELECT error FROM backup_runs WHERE id = $RUN" | grep -q '^invalid age recipient' &&
	is baoopn "SELECT prefix FROM backup_agent" "$EVIL" &&
	is baoopn "SELECT to_regclass('settings') IS NOT NULL" t; then
	ok "SQL in the recipient and prefix: run failed, prefix stored with its quote, settings exists"
else
	bad "sql safety: $(q baoopn "SELECT status, error FROM backup_runs WHERE id = $RUN"); $(q baoopn 'SELECT prefix FROM backup_agent')"
fi
docker rm -f "$ID-evil" >/dev/null

echo "== 6 no status tables / 7 read-only role"
svc plain plain -e BACKUP_ON_START=true -e BACKUP_PREFIX=plain
PGU=ro PGP=ro-pass svc ro baoopn -e BACKUP_ON_START=true -e BACKUP_PREFIX=ro
wait_for 90 health_is plain '^ok: last backup' || true
wait_for 90 health_is ro '^ok: last backup' || true
# At least two more ticks of the read-only service.
sleep $((2 * TICK + 5))
n=$(logs plain | grep -c "status reporting off: the database has no backup status tables" || true)
health_is plain '^ok: last backup' && [ "$n" -eq 1 ] &&
	ok "a database without status tables: backup ok, reporting off logged once" ||
	bad "no status tables: $n off lines; $(health plain)"
logs ro >"$TMP/ro.log"
dups=$(grep 'status reporting failed' "$TMP/ro.log" | cut -d' ' -f2- | sort | uniq -d)
hb=$(grep -c 'WARN.*status reporting failed (heartbeat)' "$TMP/ro.log" || true)
health_is ro '^ok: last backup' && [ "$hb" -eq 1 ] && [ -z "$dups" ] &&
	ok "a read-only role: backup ok, each status failure warned once" ||
	bad "read-only role: $hb heartbeat warnings, repeats: $dups; $(health ro)"

echo
echo "status: $PASS passed, $FAILED failed"
[ "$FAILED" -eq 0 ]
