# shellcheck shell=sh
# Backup status in the database, for the admin page. Sourced by backupctl.
#
# The sidecar writes its state (backup_agent), one row per backup
# (backup_runs) and reads the admin settings (settings key "backup"). All of
# it is best effort: a failed status write logs one WARN per kind of failure
# and never changes a backup's result or the healthcheck. Without the status
# tables (an older schema or another database) every function here is a
# no-op and the service works as before.
#
# Untrusted values reach SQL only as psql variables (:'name'), never spliced
# into the query text. No secret is ever written: only whether it is set.

STATUS_DB=off
STATUS_PROBED=0
STATUS_OFF_LOGGED=
STATUS_FAILING=
STATUS_PID=
STATUS_TAG=main
STATUS_RESULT=
DB_OK=
RUN_ID=
RUN_ACTIVE=
PEND_ID=
PULSE_PID=

# status_psql QUERY [name=value ...]: runs QUERY with psql variables in the
# background (SIGTERM stays prompt) and sets STATUS_RESULT to its output.
# psql interpolates :'name' only in -f input, not with -c.
status_psql() {
	_q=$1
	shift
	_n=$#
	while [ "$_n" -gt 0 ]; do
		set -- "$@" -v "$1"
		shift
		_n=$((_n - 1))
	done
	# shellcheck disable=SC2153 # STATUS_DIR is set in backupctl-common.sh
	_out="$STATUS_DIR/sql.$STATUS_TAG.out"
	_err="$STATUS_DIR/sql.$STATUS_TAG.err"
	STATUS_RESULT=
	mkdir -p "$STATUS_DIR" 2>/dev/null || return 1
	printf '%s\n' "$_q" | PGCONNECT_TIMEOUT=5 \
		PGOPTIONS="${PGOPTIONS:-} -c statement_timeout=5000 -c lock_timeout=2000" \
		psql -X -q -w -A -t -F '|' -v ON_ERROR_STOP=1 "$@" -d "$DB_TARGET" -f - >"$_out" 2>"$_err" &
	STATUS_PID=$!
	wait "$STATUS_PID"
	_rc=$?
	STATUS_PID=
	STATUS_RESULT=$(cat "$_out" 2>/dev/null)
	rm -f "$_out"
	return "$_rc"
}

# status_sql OP QUERY [name=value ...]: status_psql while reporting is on.
# OP names the kind of failure: the first failure of a kind logs one WARN,
# the next success of that kind logs "status reporting back".
status_sql() {
	[ "$STATUS_DB" = on ] || return 1
	_op=$1
	shift
	if status_psql "$@"; then
		status_good "$_op"
		return 0
	fi
	status_bad "$_op"
	return 1
}

status_bad() {
	case " $STATUS_FAILING " in *" $1 "*) return 0 ;; esac
	STATUS_FAILING="$STATUS_FAILING $1"
	_msg=$(clean_text "$(grep -v '^[[:space:]]*$' "$STATUS_DIR/sql.$STATUS_TAG.err" 2>/dev/null | head -n 1)" 200)
	warn "status reporting failed ($1): ${_msg:-no error message}; backups are not affected"
}

status_good() {
	case " $STATUS_FAILING " in *" $1 "*) ;; *) return 0 ;; esac
	# shellcheck disable=SC2086 # a word list
	STATUS_FAILING=$(printf '%s\n' $STATUS_FAILING | grep -v -x -F "$1" | tr '\n' ' ')
	info "status reporting back ($1)"
}

# clean_text TEXT MAX: printable ASCII only, cut to MAX characters.
clean_text() {
	printf '%s' "$1" | LC_ALL=C tr -cd ' -~' | cut -c "1-$2"
}

# status_init: database settings and the first probe. Never fails.
status_init() {
	if (setup_db) >/dev/null 2>&1; then
		setup_db
		DB_OK=1
	fi
	status_probe
}

# status_probe: STATUS_DB=on when the database has the status tables, off
# when it answers that it has not. A failed probe (the database is down)
# keeps the last state, so a short outage never resets the admin settings.
status_probe() {
	STATUS_PROBED=$(date -u +%s)
	[ -n "$DB_OK" ] || return 0
	if ! status_psql "SELECT to_regclass('backup_agent') IS NOT NULL AND to_regclass('backup_runs') IS NOT NULL AND to_regclass('settings') IS NOT NULL"; then
		status_bad probe
		return 0
	fi
	status_good probe
	if [ "$STATUS_RESULT" = t ]; then
		STATUS_DB=on
		return 0
	fi
	STATUS_DB=off
	if [ -z "$STATUS_OFF_LOGGED" ]; then
		STATUS_OFF_LOGGED=1
		info "status reporting off: the database has no backup status tables (older schema or another database); backups run as usual"
	fi
}

# status_reprobe: probes again every 5 minutes (BACKUP_STATUS_PROBE_SECONDS,
# for tests).
status_reprobe() {
	_every=${BACKUP_STATUS_PROBE_SECONDS:-300}
	is_uint "$_every" || _every=300
	[ $(($(date -u +%s) - STATUS_PROBED)) -lt "$_every" ] || status_probe
}

# status_interrupted: runs left "running" by a stopped container failed.
status_interrupted() {
	status_sql startup "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Interrupted: the backup service stopped during this run' WHERE status='running'"
}

# status_sweep: each tick between backups. Retries a final run write that
# failed, then fails runs left "running" for over 6 hours (a lost final
# write of an older process). A CLI backup in another process is never that
# old, so it is left alone.
status_sweep() {
	run_flush
	status_sql sweep "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Interrupted: the backup service lost track of this run'
		WHERE status='running' AND started_at < now() - interval '6 hours'"
}

# ─── Agent heartbeat ──────────────────────────────────────────────────────────

# heartbeat STATE [IDLE_REASON]: upserts the one backup_agent row.
heartbeat() {
	[ "$STATUS_DB" = on ] || return 0
	_dest=
	_prov=
	_bucket=
	if [ -n "${BACKUP_REMOTE:-}" ]; then
		_dest=rclone
	elif [ "$(s3_missing | wc -w)" -lt 4 ]; then
		_dest=s3
		_prov=${BACKUP_S3_PROVIDER:-Cloudflare}
		_bucket=${BACKUP_S3_BUCKET:-}
	fi
	_prefix=${BACKUP_PREFIX:-baoopn}
	_next=$NEXT
	[ "$PAUSED" != true ] || _next=
	[ "$1" != idle ] || _next=
	_fd=$(status_read first_due)
	_ls=$(status_read last_success)
	is_uint "$_fd" || _fd=
	is_uint "$_ls" || _ls=
	is_uint "$_next" || _next=
	_on=false
	case ${BACKUP_ON_START:-false} in true | 1 | yes) _on=true ;; esac
	status_sql heartbeat "INSERT INTO backup_agent AS a (id, version, started_at, last_seen_at, state,
		idle_reason, destination, s3_provider, s3_bucket, prefix, recipient_set, s3_endpoint_set,
		s3_access_key_set, s3_secret_set, ping_url_set, env_backup_time, env_keep_days, env_keep_min,
		env_max_age_hours, retry_minutes, on_start, settings_applied_at, next_run_at, first_due_at,
		last_success_at)
	VALUES (1, :'version', to_timestamp(:'started'::bigint), now(), :'state', NULLIF(:'reason', ''),
		NULLIF(:'dest', ''), NULLIF(:'provider', ''), NULLIF(:'bucket', ''), NULLIF(:'prefix', ''),
		:'rcpt'::boolean, :'endpoint'::boolean, :'key'::boolean, :'secret'::boolean, :'ping'::boolean,
		NULLIF(:'env_time', ''), NULLIF(:'env_keep_days', '')::int, NULLIF(:'env_keep_min', '')::int,
		NULLIF(:'env_max_age', '')::int, NULLIF(:'retry', '')::int, :'on_start'::boolean,
		NULLIF(:'applied', '')::timestamptz, to_timestamp(NULLIF(:'next', '')::bigint),
		to_timestamp(NULLIF(:'first_due', '')::bigint), to_timestamp(NULLIF(:'last_success', '')::bigint))
	ON CONFLICT (id) DO UPDATE SET version = EXCLUDED.version, started_at = EXCLUDED.started_at,
		last_seen_at = EXCLUDED.last_seen_at, state = EXCLUDED.state, idle_reason = EXCLUDED.idle_reason,
		destination = EXCLUDED.destination, s3_provider = EXCLUDED.s3_provider,
		s3_bucket = EXCLUDED.s3_bucket, prefix = EXCLUDED.prefix, recipient_set = EXCLUDED.recipient_set,
		s3_endpoint_set = EXCLUDED.s3_endpoint_set, s3_access_key_set = EXCLUDED.s3_access_key_set,
		s3_secret_set = EXCLUDED.s3_secret_set, ping_url_set = EXCLUDED.ping_url_set,
		env_backup_time = EXCLUDED.env_backup_time, env_keep_days = EXCLUDED.env_keep_days,
		env_keep_min = EXCLUDED.env_keep_min, env_max_age_hours = EXCLUDED.env_max_age_hours,
		retry_minutes = EXCLUDED.retry_minutes, on_start = EXCLUDED.on_start,
		settings_applied_at = EXCLUDED.settings_applied_at, next_run_at = EXCLUDED.next_run_at,
		first_due_at = EXCLUDED.first_due_at,
		last_success_at = coalesce(EXCLUDED.last_success_at, a.last_success_at)" \
		version="$(clean_text "${BACKUP_VERSION:-dev}" 100)" started="$AGENT_STARTED" state="$1" \
		reason="$(clean_text "${2:-}" 1000)" dest="$_dest" provider="$(clean_text "$_prov" 100)" \
		bucket="$(clean_text "$_bucket" 200)" prefix="$(clean_text "$_prefix" 200)" \
		rcpt="$(set_flag "$(has_recipient && echo 1)")" endpoint="$(set_flag "${BACKUP_S3_ENDPOINT:-}")" \
		key="$(set_flag "${BACKUP_S3_ACCESS_KEY_ID:-}")" secret="$(set_flag "${BACKUP_S3_SECRET_ACCESS_KEY:-}")" \
		ping="$(set_flag "${BACKUP_PING_URL:-}")" env_time="$ENV_TIME" env_keep_days="$ENV_KEEP_DAYS" \
		env_keep_min="$ENV_KEEP_MIN" env_max_age="$ENV_MAX_AGE" retry="${RETRY_MINUTES:-}" \
		on_start="$_on" applied="${SETTINGS_UPDATED_AT:-}" next="$_next" first_due="$_fd" last_success="$_ls"
}

set_flag() {
	if [ -n "$1" ]; then printf true; else printf false; fi
}

agent_state() {
	if [ "$PAUSED" = true ]; then printf paused; else printf waiting; fi
}

# queued_run: a run-now request is waiting. Requests older than 10 minutes
# fail instead (the API does the same), so an old request never starts a
# surprise backup.
queued_run() {
	status_sql queue "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Not picked up by the backup service within 10 minutes'
		WHERE status='queued' AND requested_at < now() - interval '10 minutes';
	SELECT 1 FROM backup_runs WHERE status = 'queued' LIMIT 1" && [ "$STATUS_RESULT" = 1 ]
}

# fail_queued REASON: answers run-now requests while backups are off.
fail_queued() {
	status_sql queue "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Backups are not configured: ' || :'reason' WHERE status='queued'" \
		reason="$(clean_text "$1" 500)"
}

# ─── Run rows ─────────────────────────────────────────────────────────────────

# run_begin KIND: claims a queued request, or adds a running row. Sets RUN_ID.
# A manual run only claims: with nothing claimed (another process answered
# the request, or the write failed) RUN_ID stays empty and no backup runs.
run_begin() {
	RUN_ID=
	RUN_ACTIVE=1
	status_sql run "UPDATE backup_agent SET state='running', last_seen_at=now() WHERE id=1;
	WITH c AS (UPDATE backup_runs SET status='running', started_at=now()
		WHERE id=(SELECT id FROM backup_runs WHERE status='queued'
			AND coalesce(requested_at, now()) >= now() - interval '10 minutes'
			ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id),
	i AS (INSERT INTO backup_runs (kind, status, started_at)
		SELECT :'kind', 'running', now() WHERE :'kind' <> 'manual' AND NOT EXISTS (SELECT 1 FROM c)
		RETURNING id)
	SELECT id FROM c UNION ALL SELECT id FROM i" kind="$1" || return 0
	RUN_ID=$(printf '%s\n' "$STATUS_RESULT" | head -n 1)
	is_uint "$RUN_ID" || RUN_ID=
}

TRIM_SQL="DELETE FROM backup_runs WHERE status IN ('ok','failed') AND id NOT IN
	(SELECT id FROM backup_runs WHERE status IN ('ok','failed') ORDER BY id DESC LIMIT 200)"

# The final write of a run waits in PEND_* until it succeeds: run_flush
# retries it every tick, so a short database outage during a backup never
# leaves the row "running".

# run_ok RESULT_FILE: the run row gets the backup's name, size and counts.
run_ok() {
	RUN_ACTIVE=
	[ -n "$RUN_ID" ] || return 0
	pend_set ok
	PEND_NAME=$(clean_text "$(result_field "$1" name)" 200)
	PEND_SIZE=$(result_field "$1" size)
	PEND_SHA=$(clean_text "$(result_field "$1" sha256)" 64)
	PEND_TABLES=$(result_field "$1" tables)
	PEND_GOOSE=$(result_field "$1" goose)
	is_uint "$PEND_SIZE" || PEND_SIZE=
	is_uint "$PEND_TABLES" || PEND_TABLES=
	is_uint "$PEND_GOOSE" || PEND_GOOSE=
	run_flush
}

# run_failed ERR: the run row failed with ERR (a log line, no secrets).
run_failed() {
	RUN_ACTIVE=
	[ -n "$RUN_ID" ] || return 0
	pend_set failed
	PEND_ERR=$(clean_text "$1" 500)
	run_flush
	RUN_ID=
}

pend_set() {
	PEND_ID=$RUN_ID
	PEND_STATUS=$1
	PEND_AT=$(date -u +%s)
	PEND_NAME=
	PEND_SIZE=
	PEND_SHA=
	PEND_TABLES=
	PEND_GOOSE=
	PEND_ERR=
	PEND_PRUNED=
	PEND_PERR=
}

# run_flush: the pending final write, if any.
run_flush() {
	[ -n "$PEND_ID" ] || return 0
	status_sql run "UPDATE backup_runs SET status=:'status', finished_at=to_timestamp(:'at'::bigint),
		name=NULLIF(:'name', ''), size_bytes=NULLIF(:'size', '')::bigint, sha256=NULLIF(:'sha', ''),
		tables=NULLIF(:'tables', '')::int, goose_version=NULLIF(:'goose', '')::bigint,
		error=NULLIF(:'err', ''), pruned=coalesce(NULLIF(:'n', '')::int, pruned),
		prune_error=coalesce(NULLIF(:'perr', ''), prune_error) WHERE id=:'id'::bigint;
	UPDATE backup_agent SET state=:'state', last_seen_at=now(),
		last_success_at=CASE WHEN :'status' = 'ok' THEN to_timestamp(:'at'::bigint) ELSE last_success_at END
		WHERE id=1;
	$TRIM_SQL" id="$PEND_ID" status="$PEND_STATUS" at="$PEND_AT" name="$PEND_NAME" \
		size="$PEND_SIZE" sha="$PEND_SHA" tables="$PEND_TABLES" goose="$PEND_GOOSE" err="$PEND_ERR" \
		n="$PEND_PRUNED" perr="$PEND_PERR" state="$(agent_state)" || return 0
	PEND_ID=
}

# run_pruned ID COUNT [ERR]: what the prune after a backup did. Joins the
# pending final write when that has not gone through yet.
run_pruned() {
	[ -n "$1" ] || return 0
	_c=$2
	is_uint "$_c" || _c=
	if [ "$PEND_ID" = "$1" ]; then
		PEND_PRUNED=$_c
		PEND_PERR=$(clean_text "${3:-}" 500)
		run_flush
		return 0
	fi
	status_sql run "UPDATE backup_runs SET pruned=NULLIF(:'n', '')::int, prune_error=NULLIF(:'err', '')
		WHERE id=:'id'::bigint" id="$1" n="$_c" err="$(clean_text "${3:-}" 500)"
}

result_field() {
	sed -n "s/^$2=//p" "$1" 2>/dev/null | head -n 1
}

# status_pulse: keeps last_seen_at fresh during a long backup.
status_pulse() {
	[ "$STATUS_DB" = on ] || return 0
	(
		STATUS_TAG=pulse
		_sp=
		trap 'kill $_sp $STATUS_PID 2>/dev/null; exit 0' TERM INT
		while :; do
			sleep 30 &
			_sp=$!
			wait "$_sp"
			status_sql heartbeat "UPDATE backup_agent SET last_seen_at=now() WHERE id=1"
		done
	) &
	PULSE_PID=$!
}

pulse_stop() {
	[ -n "$PULSE_PID" ] || return 0
	kill "$PULSE_PID" 2>/dev/null
	wait "$PULSE_PID" 2>/dev/null
	PULSE_PID=
}

# status_stop: on SIGTERM. Stops status jobs and marks a running backup
# as interrupted (one best-effort write).
status_stop() {
	[ -z "$STATUS_PID" ] || kill "$STATUS_PID" 2>/dev/null
	pulse_stop
	if [ -n "$RUN_ACTIVE" ] && [ -n "$RUN_ID" ]; then
		run_failed 'Interrupted: the backup service was stopped'
	fi
}
