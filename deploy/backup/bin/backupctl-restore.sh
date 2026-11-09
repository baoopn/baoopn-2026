# shellcheck shell=sh
# backupctl restore / verify. Sourced by backupctl.
#
# The plaintext dump never touches the disk, except with restore --to-file:
# age decrypts into a fifo that pg_restore reads.

# parse_restore_args ARGS...: sets WANT, FORCE, TO_FILE, IDENTITY.
parse_restore_args() {
	WANT=
	FORCE=false
	TO_FILE=
	IDENTITY=${BACKUP_AGE_IDENTITY_FILE:-/run/secrets/backup-age-key}
	while [ $# -gt 0 ]; do
		case $1 in
		--force) FORCE=true ;;
		--to-file)
			[ $# -ge 2 ] || die "--to-file needs a path"
			TO_FILE=$2
			shift
			;;
		--to-file=*) TO_FILE=${1#--to-file=} ;;
		-i | --identity)
			[ $# -ge 2 ] || die "$1 needs a path (or - for stdin)"
			IDENTITY=$2
			shift
			;;
		-*) die "unknown option: $1" ;;
		*)
			[ -z "$WANT" ] || die "only one backup name may be given"
			WANT=$1
			;;
		esac
		shift
	done
	WANT=${WANT:-latest}
}

# load_identity: copies the age identity (a file, or stdin for "-") to a
# 0600 file in the 0700 workdir, which the EXIT trap removes. Background
# jobs get /dev/null as stdin, so age cannot read the key from stdin itself.
load_identity() {
	IDENTITY_FILE="$WORKDIR/identity"
	if [ "$IDENTITY" = - ]; then
		[ ! -t 0 ] || die "-i - reads the age key from stdin: pipe it in (... < key.txt)"
		# tr: a key saved through a terminal may have CRLF line ends.
		tr -d '\r' >"$IDENTITY_FILE" || die "age identity not readable from stdin"
	elif [ -r "$IDENTITY" ] && [ ! -d "$IDENTITY" ]; then
		tr -d '\r' <"$IDENTITY" >"$IDENTITY_FILE" || die "age identity not readable at $IDENTITY"
	else
		die "age identity not readable at $IDENTITY: mount your key there or pass -i PATH (or -i - to read it from stdin)"
	fi
	chmod 600 "$IDENTITY_FILE"
	[ -s "$IDENTITY_FILE" ] || die "age identity not readable: the key is empty"
}

# fetch_checked NAME: downloads NAME and its manifest into the workdir and
# checks the ciphertext sha256 against the manifest. Sets CIPHER, MANIFEST.
fetch_checked() {
	name=$1
	path=$(object_path "$name")
	CIPHER="$WORKDIR/$name"
	MANIFEST="$CIPHER.json"

	info "$name: downloading"
	rcl copyto "$path.json" "$MANIFEST" || die "$name: manifest not found or not readable"
	rcl copyto "$path" "$CIPHER" || die "$name: backup file not found or not readable"

	want_sha=$(manifest_field "$MANIFEST" sha256)
	[ -n "$want_sha" ] || die "$name: the manifest has no sha256"
	got_sha=$(sha256sum "$CIPHER" | cut -d' ' -f1)
	if [ "$got_sha" != "$want_sha" ]; then
		die "$name: sha256 mismatch (manifest $want_sha, file $got_sha): the file is damaged or was changed"
	fi
	info "$name: sha256 ok"
}

# decrypt_into NAME CMD...: decrypts CIPHER into a fifo and runs CMD with the
# fifo on stdin. The shell opens the fifo for age (not age -o), so a failed
# decryption closes it and CMD sees EOF instead of waiting forever. After
# CMD, the rest of the stream is drained: pg_restore may stop reading early,
# and age must still reach the end, so every chunk is authenticated and age
# never dies of SIGPIPE. Both exit codes are checked.
decrypt_into() {
	name=$1
	shift
	fifo="$WORKDIR/plain.fifo"
	rm -f "$fifo"
	mkfifo "$fifo" || die "cannot create a fifo"
	age -d -i "$IDENTITY_FILE" "$CIPHER" >"$fifo" 2>"$WORKDIR/age.err" &
	age_pid=$!
	{
		"$@"
		cmd_rc=$?
		cat >/dev/null
	} <"$fifo"
	wait "$age_pid"
	age_rc=$?
	rm -f "$fifo"
	if [ "$age_rc" -ne 0 ]; then
		die "$name: decryption failed (wrong identity, or a damaged file): $(head -n 1 "$WORKDIR/age.err")"
	fi
	return "$cmd_rc"
}

toc_list() {
	pg_restore --list >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"
}

# cleanup_restore: removes the workdir and, after an interrupted --to-file,
# the plaintext PATH.partial.
cleanup_restore() {
	[ -z "${partial:-}" ] || rm -f "$partial"
	cleanup_workdir
}

restore_setup() {
	require_storage
	umask 077
	make_workdir
	partial=
	trap 'cleanup_restore' EXIT
	trap 'cleanup_restore; exit 130' INT TERM
	load_identity
}

cmd_verify() {
	parse_restore_args "$@"
	[ "$FORCE" = false ] && [ -z "$TO_FILE" ] || die "verify takes only a backup name and -i"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1
	fetch_checked "$name"
	info "$name: decrypting and reading the archive"
	decrypt_into "$name" toc_list ||
		die "$name: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
	tables=$(toc_tables "$WORKDIR/toc")
	info "$name: ok, $(plural "$tables" table) in schema public, database $(manifest_field "$MANIFEST" database), server $(manifest_field "$MANIFEST" server_version), schema version $(schema_version "$MANIFEST")"
}

restore_db() {
	pg_restore --no-owner --no-privileges --exit-on-error --single-transaction "$@" \
		-d "$DB_TARGET" 2>"$WORKDIR/restore.err"
}

cmd_restore() {
	parse_restore_args "$@"
	restore_setup
	name=$(resolve_name "$WANT") || exit 1

	if [ -n "$TO_FILE" ]; then
		if [ -e "$TO_FILE" ] && [ "$FORCE" != true ]; then
			die "$TO_FILE already exists; refusing to overwrite it (use --force)"
		fi
		fetch_checked "$name"
		info "$name: decrypting to $TO_FILE"
		# umask 077: the file holds the plaintext data. Write it under a
		# temporary name, so a failure never removes or damages PATH.
		partial="$TO_FILE.partial"
		if ! age -d -i "$IDENTITY_FILE" -o "$partial" "$CIPHER" 2>"$WORKDIR/age.err"; then
			rm -f "$partial"
			die "$name: decryption failed (wrong identity, or a damaged file): $(head -n 1 "$WORKDIR/age.err")"
		fi
		if ! pg_restore --list "$partial" >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"; then
			rm -f "$partial"
			die "$name: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
		fi
		mv -f "$partial" "$TO_FILE" || die "cannot write $TO_FILE"
		tables=$(toc_tables "$WORKDIR/toc")
		info "$name: decrypted dump written to $TO_FILE ($(plural "$tables" table) in schema public)"
		info "restore it with: pg_restore --no-owner --no-privileges --exit-on-error -d DATABASE $TO_FILE"
		return 0
	fi

	# Check the target before downloading anything.
	setup_db
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'" 2>"$WORKDIR/psql.err") ||
		die "cannot connect to the target database: $(tail -n 1 "$WORKDIR/psql.err")"
	if [ "$tables" -gt 0 ] && [ "$FORCE" != true ]; then
		die "the target database already has $(plural "$tables" table) in schema public; refusing to restore over them (use --force to drop and replace the objects in the backup)"
	fi

	fetch_checked "$name"

	if [ "$FORCE" = true ] && [ "$tables" -gt 0 ]; then
		info "$name: restoring with --clean (replacing $(plural "$tables" "existing table"))"
		set -- --clean --if-exists
	else
		info "$name: restoring into an empty database"
		set --
	fi
	if ! decrypt_into "$name" restore_db "$@"; then
		err "$name: pg_restore failed; nothing was changed (single transaction)"
		tail -n 5 "$WORKDIR/restore.err" >&2
		exit 1
	fi
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'") || tables='?'
	info "$name: restored ($(plural "$tables" table) in schema public, schema version $(schema_version "$MANIFEST"))"
	info "start the app next: it applies any newer migrations at startup"
}
