# shellcheck shell=sh
# Shared helpers for backupctl: logging, configuration, storage and database
# access. Sourced by backupctl; not executable on its own.
#
# Never print secrets: log lines name settings, never their values.

STATUS_DIR=${BACKUP_STATUS_DIR:-/tmp/backup-status}
NAME_BASE=baoopn
# Extended regex (awk) for the object names this tool creates.
NAME_RE="^$NAME_BASE-[0-9]{8}T[0-9]{6}Z\\.dump\\.age\$"

# ─── Logging ──────────────────────────────────────────────────────────────────

log() {
	level=$1
	shift
	printf '%s %-5s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$level" "$*" >&2
}
info() { log INFO "$@"; }
warn() { log WARN "$@"; }
err() { log ERROR "$@"; }
die() {
	err "$@"
	exit 1
}

is_uint() {
	case $1 in
	'' | *[!0-9]*) return 1 ;;
	esac
	return 0
}

# ─── Temp files ───────────────────────────────────────────────────────────────

# make_workdir: creates a private temp directory in $WORKDIR. The caller's
# EXIT trap must call cleanup_workdir.
make_workdir() {
	WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/backupctl.XXXXXX") || die "cannot create a temp directory"
	chmod 700 "$WORKDIR"
}

cleanup_workdir() {
	if [ -n "${WORKDIR:-}" ] && [ -d "$WORKDIR" ]; then
		rm -rf "$WORKDIR"
	fi
	WORKDIR=
}

# ─── Status files (health) ────────────────────────────────────────────────────

status_write() {
	mkdir -p "$STATUS_DIR" 2>/dev/null || return 0
	printf '%s\n' "$2" >"$STATUS_DIR/$1" 2>/dev/null || true
}

status_read() {
	if [ -r "$STATUS_DIR/$1" ]; then
		cat "$STATUS_DIR/$1"
	fi
}

# ping_url SUFFIX: optional healthchecks.io-style ping ("" on success,
# "/fail"). Short timeout, errors ignored, URL never logged.
ping_url() {
	[ -n "${BACKUP_PING_URL:-}" ] || return 0
	wget -q -T 10 -O /dev/null "${BACKUP_PING_URL%/}$1" >/dev/null 2>&1 || warn "ping failed (ignored)"
}

# ─── Encryption ───────────────────────────────────────────────────────────────

# POSIX sh has no arrays, so recipients go to a file (age -R FILE), one per
# line, instead of a list of -r flags.
#
# write_recipients FILE: collects BACKUP_AGE_RECIPIENT (space or comma
# separated) and BACKUP_AGE_RECIPIENTS_FILE into FILE. Returns 1 if empty.
write_recipients() {
	: >"$1"
	if [ -n "${BACKUP_AGE_RECIPIENT:-}" ]; then
		set -f
		for r in $(printf '%s' "$BACKUP_AGE_RECIPIENT" | tr ',' ' '); do
			printf '%s\n' "$r" >>"$1"
		done
		set +f
	fi
	if [ -n "${BACKUP_AGE_RECIPIENTS_FILE:-}" ]; then
		[ -r "$BACKUP_AGE_RECIPIENTS_FILE" ] || die "BACKUP_AGE_RECIPIENTS_FILE is not readable"
		sed -e 's/#.*//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^$/d' \
			"$BACKUP_AGE_RECIPIENTS_FILE" >>"$1"
	fi
	[ -s "$1" ]
}

has_recipient() {
	[ -n "${BACKUP_AGE_RECIPIENT:-}" ] || [ -n "${BACKUP_AGE_RECIPIENTS_FILE:-}" ]
}

# check_recipients FILE ERRFILE: age accepts every recipient in FILE.
check_recipients() {
	printf x | age -R "$1" >/dev/null 2>"$2"
}

# ─── Storage (rclone, configured from env only) ───────────────────────────────

# setup_storage: sets REMOTE_BASE (rclone path of the backup prefix) and
# exports the RCLONE_CONFIG_BACKUP_* settings for the S3 remote. Returns 1
# when no complete destination is configured.
setup_storage() {
	export RCLONE_CONFIG="${RCLONE_CONFIG:-/dev/null}"
	prefix=${BACKUP_PREFIX:-baoopn}
	prefix=${prefix#/}
	prefix=${prefix%/}
	if [ -n "${BACKUP_REMOTE:-}" ]; then
		REMOTE_BASE="${BACKUP_REMOTE%/}/$prefix"
		# shellcheck disable=SC2034 # used by the sourcing scripts
		STORAGE_DESC="rclone remote"
		return 0
	fi
	[ -z "$(s3_missing)" ] || return 1
	export RCLONE_CONFIG_BACKUP_TYPE=s3
	export RCLONE_CONFIG_BACKUP_PROVIDER="${BACKUP_S3_PROVIDER:-Cloudflare}"
	export RCLONE_CONFIG_BACKUP_ENDPOINT="$BACKUP_S3_ENDPOINT"
	export RCLONE_CONFIG_BACKUP_REGION="${BACKUP_S3_REGION:-auto}"
	export RCLONE_CONFIG_BACKUP_ACCESS_KEY_ID="$BACKUP_S3_ACCESS_KEY_ID"
	export RCLONE_CONFIG_BACKUP_SECRET_ACCESS_KEY="$BACKUP_S3_SECRET_ACCESS_KEY"
	export RCLONE_CONFIG_BACKUP_NO_CHECK_BUCKET=true
	export RCLONE_CONFIG_BACKUP_ENV_AUTH=false
	REMOTE_BASE="backup:${BACKUP_S3_BUCKET}/$prefix"
	# shellcheck disable=SC2034 # used by the sourcing scripts
	STORAGE_DESC="S3 bucket"
}

require_storage() {
	setup_storage && return 0
	missing=$(s3_missing)
	die "no backup destination: set BACKUP_REMOTE, or all of BACKUP_S3_ENDPOINT, BACKUP_S3_BUCKET, BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY (missing: ${missing% })"
}

# s3_missing: names (never values) of the unset BACKUP_S3_* settings.
s3_missing() {
	for v in BACKUP_S3_ENDPOINT BACKUP_S3_BUCKET BACKUP_S3_ACCESS_KEY_ID BACKUP_S3_SECRET_ACCESS_KEY; do
		eval "val=\${$v:-}"
		# shellcheck disable=SC2154 # set by eval
		[ -n "$val" ] || printf '%s ' "$v"
	done
}

# config_untouched: nothing at all is set (an install without backups).
config_untouched() {
	! has_recipient && [ -z "${BACKUP_REMOTE:-}" ] && [ "$(s3_missing | wc -w)" -eq 4 ]
}

# config_problems: what is missing when backups are partly configured, by
# variable name only (empty output = complete).
config_problems() {
	out=
	has_recipient || out="BACKUP_AGE_RECIPIENT is not set"
	if [ -z "${BACKUP_REMOTE:-}" ]; then
		missing=$(s3_missing)
		if [ -n "$missing" ]; then
			[ -z "$out" ] || out="$out; "
			out="${out}incomplete destination, missing: ${missing% }"
		fi
	fi
	printf '%s' "$out"
}

# rcl ARGS...: rclone with quiet output; errors go to stderr.
rcl() {
	rclone -q --retries 3 --low-level-retries 10 "$@"
}

# object_path NAME: remote path of a backup object (<prefix>/<YYYY>/<MM>/NAME).
object_path() {
	stamp=${1#"$NAME_BASE"-}
	printf '%s/%s/%s/%s\n' "$REMOTE_BASE" "$(printf %s "$stamp" | cut -c1-4)" \
		"$(printf %s "$stamp" | cut -c5-6)" "$1"
}

# remote_listing: prints "path;size;modtime" for every backup object (dumps
# and manifests). rclone's stderr is kept apart from the listing. Any error
# fails, except a missing prefix (no backups yet), which is an empty list.
remote_listing() {
	errf=$(mktemp "${TMPDIR:-/tmp}/backupctl-ls.XXXXXX") || return 1
	if out=$(rcl lsf -R --files-only --format pst --separator ';' \
		--include '*.dump.age' --include '*.dump.age.json' "$REMOTE_BASE" 2>"$errf"); then
		rm -f "$errf"
		[ -z "$out" ] || printf '%s\n' "$out"
		return 0
	fi
	if [ -z "$out" ] && grep -q 'directory not found' "$errf" &&
		! grep -v 'directory not found' "$errf" | grep -q ERROR; then
		rm -f "$errf"
		return 0
	fi
	sed 's/^/rclone: /' "$errf" | tail -n 5 >&2
	rm -f "$errf"
	return 1
}

# complete_from LISTING: "name;size;modtime" for every backup that has both
# its .dump.age and its .json manifest, newest first. Lone files (an
# interrupted upload) are ignored; prune still removes them once old.
# Objects with names this tool does not create are never listed.
complete_from() {
	printf '%s\n' "$1" | awk -F';' -v re="$NAME_RE" '
		NF < 2 { next }
		{ n = $1; sub(/.*\//, "", n) }
		n ~ /\.dump\.age\.json$/ { sub(/\.json$/, "", n); if (n ~ re) man[n] = 1; next }
		n ~ re { size[n] = $2; t[n] = $3 }
		END { for (n in size) if (n in man) print n ";" size[n] ";" t[n] }
	' | sort -r
}

# names_from LISTING: every valid backup name present (dump or manifest),
# newest first.
names_from() {
	printf '%s\n' "$1" | awk -F';' -v re="$NAME_RE" '
		{ n = $1; sub(/.*\//, "", n); sub(/\.json$/, "", n) }
		n ~ re { print n }
	' | sort -ru
}

list_objects() {
	listing=$(remote_listing) || return 1
	complete_from "$listing"
}

# valid_name NAME: accepts only names this tool creates.
valid_name() {
	printf '%s\n' "$1" | awk -v re="$NAME_RE" '$0 ~ re { ok = 1 } END { exit !ok }'
}

# resolve_name latest|NAME: prints the backup name or fails.
resolve_name() {
	want=${1:-latest}
	if [ "$want" = latest ]; then
		objs=$(list_objects) || die "cannot list backups"
		name=$(printf '%s\n' "$objs" | head -n 1 | cut -d';' -f1)
		[ -n "$name" ] || die "no backups found"
		printf '%s\n' "$name"
		return 0
	fi
	case $want in
	*.dump.age) ;;
	*) want="$want.dump.age" ;;
	esac
	valid_name "$want" || die "not a backup name: expected $NAME_BASE-YYYYMMDDTHHMMSSZ.dump.age"
	printf '%s\n' "$want"
}

# name_epoch NAME: seconds since the epoch from a backup name's timestamp.
name_epoch() {
	s=${1#"$NAME_BASE"-}
	s=${s%%Z*}
	date -u -d "$(printf '%s' "$s" | sed 's/^\(....\)\(..\)\(..\)T\(..\)\(..\)\(..\)$/\1-\2-\3 \4:\5:\6/')" +%s 2>/dev/null
}

# ─── Database ─────────────────────────────────────────────────────────────────

# pct_decode STRING: decodes %XX escapes (backslashes stay literal).
pct_decode() {
	printf '%b' "$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/%\([0-9A-Fa-f][0-9A-Fa-f]\)/\\x\1/g')"
}

# setup_db: sets DB_TARGET, the -d argument for psql, pg_dump and pg_restore.
# DATABASE_URL wins over the PG* variables. Its password moves to PGPASSWORD
# and DB_TARGET is the URL without it, so the password never appears in a
# command line (ps). DB_TARGET is never logged.
setup_db() {
	if [ -n "${DATABASE_URL:-}" ]; then
		case $DATABASE_URL in
		postgres://* | postgresql://*) ;;
		*) die "DATABASE_URL must start with postgres:// or postgresql://" ;;
		esac
		scheme=${DATABASE_URL%%://*}
		rest=${DATABASE_URL#*://}
		authority=${rest%%[/?]*}
		tail=${rest#"$authority"}
		case $authority in
		*@*)
			userinfo=${authority%@*}
			hostport=${authority##*@}
			user=${userinfo%%:*}
			case $userinfo in
			*:*)
				PGPASSWORD=$(pct_decode "${userinfo#*:}")
				export PGPASSWORD
				;;
			esac
			if [ -n "$user" ]; then
				DB_TARGET="$scheme://$user@$hostport$tail"
			else
				DB_TARGET="$scheme://$hostport$tail"
			fi
			;;
		*) DB_TARGET=$DATABASE_URL ;;
		esac
		return 0
	fi
	DB_TARGET=${PGDATABASE:-${PGUSER:-}}
	[ -n "$DB_TARGET" ] && [ -n "${PGHOST:-}" ] ||
		die "no database: set DATABASE_URL, or PGHOST, PGPORT, PGUSER, PGPASSWORD and PGDATABASE"
	case $DB_TARGET in
	*=* | *://*) die "PGDATABASE must be a plain database name" ;;
	esac
}

# sql QUERY: runs one query against DB_TARGET, prints unaligned tuples.
sql() {
	psql -X -q -A -t -v ON_ERROR_STOP=1 -d "$DB_TARGET" -c "$1"
}

json_str() {
	printf '"%s"' "$(printf %s "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\n\r')"
}

# manifest_field FILE KEY: reads a top-level string or number value from a
# manifest written by cmd_backup (one key per line).
manifest_field() {
	sed -n "s/^[[:space:]]*\"$2\":[[:space:]]*\"\{0,1\}\([^\",]*\)\"\{0,1\},\{0,1\}[[:space:]]*\$/\1/p" "$1" | head -n 1
}

human_size() {
	awk -v b="$1" 'BEGIN {
		split("B KiB MiB GiB TiB", u, " "); i = 1
		while (b >= 1024 && i < 5) { b /= 1024; i++ }
		printf (i == 1 ? "%d %s" : "%.1f %s"), b, u[i]
	}'
}
