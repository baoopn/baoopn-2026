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
PULSE_PID=
PAUSED=false
SETTINGS_APPLIED=

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

# status_probe: STATUS_DB=on when the database has the status tables.
status_probe() {
	STATUS_PROBED=$(date -u +%s)
	STATUS_DB=off
	[ -n "$DB_OK" ] || return 0
	if ! status_psql "SELECT to_regclass('backup_agent') IS NOT NULL AND to_regclass('backup_runs') IS NOT NULL AND to_regclass('settings') IS NOT NULL"; then
		status_bad probe
		return 0
	fi
	status_good probe
	if [ "$STATUS_RESULT" = t ]; then
		STATUS_DB=on
	elif [ -z "$STATUS_OFF_LOGGED" ]; then
		STATUS_OFF_LOGGED=1
		info "status reporting off: the database has no backup status tables (older schema or another database); backups run as usual"
	fi
}

# status_reprobe: probes again every 5 minutes.
status_reprobe() {
	[ $(($(date -u +%s) - STATUS_PROBED)) -lt 300 ] || status_probe
}

# status_interrupted: runs left "running" by a stopped container failed.
status_interrupted() {
	status_sql startup "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Interrupted: the backup service stopped during this run' WHERE status='running'"
}

# ─── Settings (admin overrides) ───────────────────────────────────────────────

# env_defaults: validated env values, the fallback for every override.
env_defaults() {
	ENV_TIME=${BACKUP_TIME:-03:00}
	valid_time "$ENV_TIME" || ENV_TIME=03:00
	ENV_KEEP_DAYS_RAW=${BACKUP_KEEP_DAYS:-30}
	ENV_KEEP_MIN_RAW=${BACKUP_KEEP_MIN:-7}
	ENV_KEEP_DAYS=$ENV_KEEP_DAYS_RAW
	is_uint "$ENV_KEEP_DAYS" || ENV_KEEP_DAYS=
	ENV_KEEP_MIN=$ENV_KEEP_MIN_RAW
	is_uint "$ENV_KEEP_MIN" || ENV_KEEP_MIN=
	ENV_MAX_AGE=${BACKUP_MAX_AGE_HOURS:-26}
	is_uint "$ENV_MAX_AGE" || ENV_MAX_AGE=26
	BACKUP_TIME=$ENV_TIME
	MAX_AGE_HOURS=$ENV_MAX_AGE
}

valid_time() {
	case $1 in
	[01][0-9]:[0-5][0-9] | 2[0-3]:[0-5][0-9]) return 0 ;;
	esac
	return 1
}

# in_range VALUE MIN MAX: a whole number from MIN to MAX.
in_range() {
	is_uint "$1" && [ "${#1}" -le 6 ] && [ "$1" -ge "$2" ] && [ "$1" -le "$3" ]
}

# pick FIELD RAW VALID ENV: sets PICK_VAL to RAW when VALID is 1, else to
# ENV, and PICK_SRC to override or default. A new bad RAW logs one WARN.
pick() {
	PICK_SRC=default
	PICK_VAL=$4
	eval "_prev=\${BAD_$1:-}"
	if [ -z "$2" ]; then
		eval "BAD_$1="
	elif [ "$3" = 1 ]; then
		eval "BAD_$1="
		PICK_SRC=override
		PICK_VAL=$2
	else
		# shellcheck disable=SC2154 # set by eval
		[ "$_prev" = "$2" ] ||
			warn "settings: ignoring $1 \"$(clean_text "$2" 40)\" (not valid); using the default ${4:-from the environment}"
		eval "BAD_$1=\$2"
	fi
}

# note FIELD TEXT: one INFO line when an effective setting changes. The
# first values from env are not logged: the scheduler line shows them.
note() {
	eval "_old=\${NOTE_$1:-}"
	# shellcheck disable=SC2154 # set by eval
	[ "$_old" != "$2" ] || return 0
	eval "NOTE_$1=\$2"
	[ -z "$SETTINGS_APPLIED" ] && [ "$PICK_SRC" = default ] && return 0
	info "settings: $2"
}

src_label() {
	if [ "$PICK_SRC" = override ]; then printf 'admin override'; else printf 'default'; fi
}

# apply_settings: reads the settings doc and sets BACKUP_TIME,
# BACKUP_KEEP_DAYS, BACKUP_KEEP_MIN, MAX_AGE_HOURS and PAUSED. Missing or
# bad values fall back to env. Writes the paused and max_age_hours status
# files for the healthcheck. A failed read keeps the current values.
apply_settings() {
	if [ "$STATUS_DB" = on ]; then
		if status_sql settings "SELECT translate(value->>'backup_time', E'|\n\r', ''),
			translate(value->>'keep_days', E'|\n\r', ''), translate(value->>'keep_min', E'|\n\r', ''),
			translate(value->>'max_age_hours', E'|\n\r', ''), coalesce(value->>'paused', 'false') = 'true',
			updated_at::text FROM settings WHERE key = 'backup'"; then
			_line=$(printf '%s\n' "$STATUS_RESULT" | head -n 1)
			apply_line "$_line"
		fi
	else
		apply_line ''
	fi
	if [ "$PAUSED" = true ]; then status_write paused 1; else rm -f "$STATUS_DIR/paused"; fi
	status_write max_age_hours "$MAX_AGE_HOURS"
	SETTINGS_APPLIED=1
}

apply_line() {
	_t=$(printf '%s' "$1" | cut -d'|' -f1)
	_kd=$(printf '%s' "$1" | cut -d'|' -f2)
	_km=$(printf '%s' "$1" | cut -d'|' -f3)
	_ma=$(printf '%s' "$1" | cut -d'|' -f4)
	_p=$(printf '%s' "$1" | cut -d'|' -f5)
	SETTINGS_UPDATED_AT=$(printf '%s' "$1" | cut -d'|' -f6)
	case $SETTINGS_UPDATED_AT in [0-9]*) ;; *) SETTINGS_UPDATED_AT= ;; esac

	v=0
	valid_time "$_t" && v=1
	pick backup_time "$_t" "$v" "$ENV_TIME"
	BACKUP_TIME=$PICK_VAL
	note backup_time "daily at $BACKUP_TIME UTC ($(src_label))"

	v=0
	in_range "$_kd" 0 3650 && v=1
	pick keep_days "$_kd" "$v" "$ENV_KEEP_DAYS_RAW"
	BACKUP_KEEP_DAYS=$PICK_VAL
	note keep_days "keep $BACKUP_KEEP_DAYS days ($(src_label))"

	v=0
	in_range "$_km" 0 1000 && v=1
	pick keep_min "$_km" "$v" "$ENV_KEEP_MIN_RAW"
	BACKUP_KEEP_MIN=$PICK_VAL
	note keep_min "keep at least $BACKUP_KEEP_MIN backups ($(src_label))"

	v=0
	in_range "$_ma" 25 720 && v=1
	pick max_age_hours "$_ma" "$v" "$ENV_MAX_AGE"
	MAX_AGE_HOURS=$PICK_VAL
	note max_age_hours "alert after $MAX_AGE_HOURS hours without a backup ($(src_label))"

	_was=$PAUSED
	if [ "$_p" = t ]; then PAUSED=true; else PAUSED=false; fi
	if [ "$_was" != "$PAUSED" ]; then
		if [ "$PAUSED" = true ]; then
			info "settings: scheduled backups paused (admin)"
		else
			info "settings: scheduled backups resumed (admin)"
		fi
	fi
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

# queued_run: a run-now request is waiting.
queued_run() {
	status_sql queue "SELECT 1 FROM backup_runs WHERE status = 'queued' LIMIT 1" && [ "$STATUS_RESULT" = 1 ]
}

# fail_queued REASON: answers run-now requests while backups are off.
fail_queued() {
	status_sql queue "UPDATE backup_runs SET status='failed', finished_at=now(),
		error='Backups are not configured: ' || :'reason' WHERE status='queued'" \
		reason="$(clean_text "$1" 500)"
}

# ─── Run rows ─────────────────────────────────────────────────────────────────

# run_begin KIND: claims a queued request, or adds a running row. Sets RUN_ID.
run_begin() {
	RUN_ID=
	RUN_ACTIVE=1
	status_sql run "UPDATE backup_agent SET state='running', last_seen_at=now() WHERE id=1;
	WITH c AS (UPDATE backup_runs SET status='running', started_at=now()
		WHERE id=(SELECT id FROM backup_runs WHERE status='queued' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id),
	i AS (INSERT INTO backup_runs (kind, status, started_at)
		SELECT :'kind', 'running', now() WHERE NOT EXISTS (SELECT 1 FROM c) RETURNING id)
	SELECT id FROM c UNION ALL SELECT id FROM i" kind="$1" || return 0
	RUN_ID=$(printf '%s\n' "$STATUS_RESULT" | head -n 1)
	is_uint "$RUN_ID" || RUN_ID=
}

TRIM_SQL="DELETE FROM backup_runs WHERE status IN ('ok','failed') AND id NOT IN
	(SELECT id FROM backup_runs WHERE status IN ('ok','failed') ORDER BY id DESC LIMIT 200)"

# run_ok RESULT_FILE: the run row gets the backup's name, size and counts.
run_ok() {
	RUN_ACTIVE=
	[ -n "$RUN_ID" ] || return 0
	_name=$(result_field "$1" name)
	_size=$(result_field "$1" size)
	_sha=$(result_field "$1" sha256)
	_tables=$(result_field "$1" tables)
	_goose=$(result_field "$1" goose)
	for _v in _size _tables _goose; do
		eval "is_uint \"\$$_v\" || $_v="
	done
	status_sql run "UPDATE backup_runs SET status='ok', finished_at=now(), name=NULLIF(:'name', ''),
		size_bytes=NULLIF(:'size', '')::bigint, sha256=NULLIF(:'sha', ''), tables=NULLIF(:'tables', '')::int,
		goose_version=NULLIF(:'goose', '')::bigint WHERE id=:'id'::bigint;
	UPDATE backup_agent SET state=:'state', last_seen_at=now(), last_success_at=now() WHERE id=1;
	$TRIM_SQL" id="$RUN_ID" name="$(clean_text "$_name" 200)" size="$_size" \
		sha="$(clean_text "$_sha" 64)" tables="$_tables" goose="$_goose" state="$(agent_state)"
}

# run_failed ERR: the run row failed with ERR (a log line, no secrets).
run_failed() {
	RUN_ACTIVE=
	[ -n "$RUN_ID" ] || return 0
	status_sql run "UPDATE backup_runs SET status='failed', finished_at=now(), error=:'err'
		WHERE id=:'id'::bigint;
	UPDATE backup_agent SET state=:'state', last_seen_at=now() WHERE id=1;
	$TRIM_SQL" id="$RUN_ID" err="$(clean_text "$1" 500)" state="$(agent_state)"
	RUN_ID=
}

# run_pruned ID COUNT [ERR]: what the prune after a backup did.
run_pruned() {
	[ -n "$1" ] || return 0
	_c=$2
	is_uint "$_c" || _c=
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
