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
		cat >"$IDENTITY_FILE" || die "age identity not readable from stdin"
	elif [ -r "$IDENTITY" ] && [ ! -d "$IDENTITY" ]; then
		cat "$IDENTITY" >"$IDENTITY_FILE" || die "age identity not readable at $IDENTITY"
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
# decryption closes it and CMD sees EOF instead of waiting forever. Both
# exit codes are checked.
decrypt_into() {
	name=$1
	shift
	fifo="$WORKDIR/plain.fifo"
	rm -f "$fifo"
	mkfifo "$fifo" || die "cannot create a fifo"
	age -d -i "$IDENTITY_FILE" "$CIPHER" >"$fifo" 2>"$WORKDIR/age.err" &
	age_pid=$!
	"$@" <"$fifo"
	cmd_rc=$?
	wait "$age_pid"
	age_rc=$?
	rm -f "$fifo"
	# A real age error is written to age.err; a reader that stopped early
	# kills age with SIGPIPE and leaves it empty.
	if [ "$age_rc" -ne 0 ] && { [ -s "$WORKDIR/age.err" ] || [ "$cmd_rc" -eq 0 ]; }; then
		die "$name: decryption failed (wrong identity?): $(head -n 1 "$WORKDIR/age.err")"
	fi
	return "$cmd_rc"
}

toc_list() {
	pg_restore --list >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"
}

restore_setup() {
	require_storage
	umask 077
	make_workdir
	trap 'cleanup_workdir' EXIT
	trap 'cleanup_workdir; exit 130' INT TERM
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
	entries=$(grep -c '^[0-9]' "$WORKDIR/toc")
	info "$name: ok, $entries archive entries, database $(manifest_field "$MANIFEST" database), server $(manifest_field "$MANIFEST" server_version), schema version $(manifest_field "$MANIFEST" goose_version)"
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
		fetch_checked "$name"
		info "$name: decrypting to $TO_FILE"
		# umask 077: the file holds the plaintext data.
		if ! age -d -i "$IDENTITY_FILE" -o "$TO_FILE" "$CIPHER" 2>"$WORKDIR/age.err"; then
			rm -f "$TO_FILE"
			die "$name: decryption failed (wrong identity?): $(head -n 1 "$WORKDIR/age.err")"
		fi
		if ! pg_restore --list "$TO_FILE" >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"; then
			die "$name: not a valid pg_dump archive: $(head -n 1 "$WORKDIR/toc.err")"
		fi
		entries=$(grep -c '^[0-9]' "$WORKDIR/toc")
		info "$name: decrypted dump written to $TO_FILE ($entries archive entries)"
		info "restore it with: pg_restore --no-owner --no-privileges --exit-on-error -d DATABASE $TO_FILE"
		return 0
	fi

	# Check the target before downloading anything.
	setup_db
	tables=$(sql "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'" 2>"$WORKDIR/psql.err") ||
		die "cannot connect to the target database: $(tail -n 1 "$WORKDIR/psql.err")"
	if [ "$tables" -gt 0 ] && [ "$FORCE" != true ]; then
		die "the target database already has $tables table(s) in schema public; refusing to restore over them (use --force to drop and replace the objects in the backup)"
	fi

	fetch_checked "$name"

	if [ "$FORCE" = true ] && [ "$tables" -gt 0 ]; then
		info "$name: restoring with --clean (replacing $tables existing table(s))"
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
	goose=$(manifest_field "$MANIFEST" goose_version)
	info "$name: restored ($tables tables in schema public, schema version ${goose:-unknown})"
	info "start the app next: it applies any newer migrations at startup"
}
