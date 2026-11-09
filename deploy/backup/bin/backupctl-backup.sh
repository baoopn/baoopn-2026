# shellcheck shell=sh
# backupctl backup / prune / list. Sourced by backupctl.

# cmd_backup: pg_dump | age → temp file → upload → verify size → manifest.
# Exits non-zero on any failure. Run it in a subshell from the scheduler.
cmd_backup() {
	has_recipient || die "refusing to back up: no age recipient (set BACKUP_AGE_RECIPIENT or BACKUP_AGE_RECIPIENTS_FILE); backups are never stored unencrypted"
	require_storage
	setup_db
	umask 077
	make_workdir
	trap 'cleanup_workdir' EXIT
	trap 'cleanup_workdir; exit 130' INT TERM

	recipients="$WORKDIR/recipients"
	write_recipients "$recipients" || die "refusing to back up: the age recipient list is empty"
	check_recipients "$recipients" "$WORKDIR/age.err" ||
		die "invalid age recipient: $(head -n 1 "$WORKDIR/age.err")"

	now=$(date -u +%s)
	stamp=$(date -u -d "@$now" +%Y%m%dT%H%M%SZ)
	created_at=$(date -u -d "@$now" +%Y-%m-%dT%H:%M:%SZ)
	name="$NAME_BASE-$stamp.dump.age"
	path=$(object_path "$name")

	info "backup $name: reading database info"
	db_name=$(sql 'SELECT current_database()' 2>"$WORKDIR/psql.err") ||
		die "cannot connect to the database: $(tail -n 1 "$WORKDIR/psql.err")"
	server_version=$(sql 'SHOW server_version') || die "cannot read the server version"
	goose_version=
	if [ "$(sql "SELECT to_regclass('public.goose_db_version') IS NOT NULL")" = t ]; then
		goose_version=$(sql 'SELECT max(version_id) FROM goose_db_version WHERE is_applied') || goose_version=
	fi
	dump_version=$(pg_dump --version)

	info "backup $name: dumping and encrypting"
	out="$WORKDIR/$name"
	# pg_dump → tee → age, with tee's second copy read by pg_restore --list
	# (then drained), so the archive's table list is checked without ever
	# writing plaintext to disk. Fifos instead of pipes: every exit status is
	# checked (POSIX sh has no pipefail).
	mkfifo "$WORKDIR/dump" "$WORKDIR/enc" "$WORKDIR/toc.pipe" || die "cannot create fifos"
	pg_dump -Fc -Z 6 --no-owner --no-privileges -d "$DB_TARGET" >"$WORKDIR/dump" 2>"$WORKDIR/pg_dump.err" &
	dump_pid=$!
	tee "$WORKDIR/toc.pipe" <"$WORKDIR/dump" >"$WORKDIR/enc" 2>"$WORKDIR/tee.err" &
	tee_pid=$!
	{
		pg_restore --list >"$WORKDIR/toc" 2>"$WORKDIR/toc.err"
		echo $? >"$WORKDIR/toc.rc"
		cat >/dev/null
	} <"$WORKDIR/toc.pipe" &
	toc_pid=$!
	age -R "$recipients" -o "$out" <"$WORKDIR/enc" 2>"$WORKDIR/age.err"
	age_rc=$?
	wait "$dump_pid"
	dump_rc=$?
	wait "$tee_pid"
	tee_rc=$?
	wait "$toc_pid"
	if [ "$dump_rc" -ne 0 ]; then
		die "pg_dump failed (exit $dump_rc): $(tail -n 3 "$WORKDIR/pg_dump.err" | tr '\n' ' ')"
	fi
	[ "$age_rc" -eq 0 ] || die "age failed (exit $age_rc): $(head -n 1 "$WORKDIR/age.err")"
	[ "$tee_rc" -eq 0 ] || die "tee failed (exit $tee_rc): $(head -n 1 "$WORKDIR/tee.err")"
	[ "$(cat "$WORKDIR/toc.rc" 2>/dev/null)" = 0 ] ||
		die "the dump is not a readable archive: $(head -n 1 "$WORKDIR/toc.err")"
	tables=$(grep -c -E '^[0-9]+; [0-9]+ [0-9]+ TABLE ' "$WORKDIR/toc")
	[ "$tables" -gt 0 ] || die "the dump contains no tables (wrong or empty database?); not uploading it"

	size=$(wc -c <"$out" | tr -d ' ')
	sha=$(sha256sum "$out" | cut -d' ' -f1)
	[ "$size" -gt 0 ] || die "encrypted dump is empty"

	info "backup $name: uploading $(human_size "$size") to the $STORAGE_DESC"
	rcl copyto "$out" "$path" || die "upload failed"
	remote_json=$(rcl lsjson "$path") || remote_json=
	remote_size=$(printf '%s' "$remote_json" | tr ',' '\n' | sed -n 's/.*"Size":\([0-9]*\).*/\1/p' | head -n 1)
	if [ "$remote_size" != "$size" ]; then
		rcl deletefile "$path" >/dev/null 2>&1 || true
		die "uploaded size mismatch (local $size, remote ${remote_size:-missing}); upload removed"
	fi

	manifest="$out.json"
	{
		printf '{\n'
		printf '  "name": %s,\n' "$(json_str "$name")"
		printf '  "created_at": %s,\n' "$(json_str "$created_at")"
		printf '  "size": %s,\n' "$size"
		printf '  "sha256": %s,\n' "$(json_str "$sha")"
		printf '  "pg_dump_version": %s,\n' "$(json_str "$dump_version")"
		printf '  "server_version": %s,\n' "$(json_str "$server_version")"
		printf '  "database": %s,\n' "$(json_str "$db_name")"
		if is_uint "$goose_version"; then
			printf '  "goose_version": %s,\n' "$goose_version"
		else
			printf '  "goose_version": null,\n'
		fi
		printf '  "format": "pg_dump-custom+age"\n'
		printf '}\n'
	} >"$manifest"
	rcl copyto "$manifest" "$path.json" || {
		rcl deletefile "$path" >/dev/null 2>&1 || true
		die "manifest upload failed; backup removed"
	}

	info "backup $name: done ($tables tables, $(human_size "$size"), sha256 $sha, schema version ${goose_version:-none})"
	printf '%s\n' "$name"
}

# backup_with_status: one backup in a subshell, then status files + ping.
backup_with_status() {
	(cmd_backup)
	rc=$?
	if [ "$rc" -eq 0 ]; then
		status_write last_success "$(date -u +%s)"
		ping_url ""
	else
		status_write last_error "$(date -u +%s)"
		err "backup failed"
		ping_url /fail
	fi
	return "$rc"
}

# cmd_prune: deletes backups older than BACKUP_KEEP_DAYS, but always keeps
# the newest BACKUP_KEEP_MIN complete backups (0 keeps none by count).
# Objects with names this tool does not create are never counted or touched.
cmd_prune() {
	require_storage
	keep_days=${BACKUP_KEEP_DAYS:-30}
	keep_min=${BACKUP_KEEP_MIN:-7}
	is_uint "$keep_days" || die "BACKUP_KEEP_DAYS must be a whole number of days"
	is_uint "$keep_min" || die "BACKUP_KEEP_MIN must be a whole number"
	if [ "$keep_days" -eq 0 ]; then
		info "prune: BACKUP_KEEP_DAYS=0, keeping every backup"
		return 0
	fi
	cutoff_epoch=$(($(date -u +%s) - keep_days * 86400))
	cutoff="$NAME_BASE-$(date -u -d "@$cutoff_epoch" +%Y%m%dT%H%M%SZ).dump.age"

	# One listing for both sets; any listing error stops the prune, so a
	# failed list can never make every old backup look unprotected.
	listing=$(remote_listing) || die "prune: cannot list backups; nothing deleted"
	complete=$(complete_from "$listing")
	all=$(names_from "$listing")
	keep=$(printf '%s\n' "$complete" | cut -d';' -f1 | head -n "$keep_min")
	victims=$(
		{
			printf '%s\n' "$keep" | sed 's/^/K /'
			printf '%s\n' "$all" | sed 's/^/A /'
		} | awk -v cutoff="$cutoff" '
			$1 == "K" { keep[$2] = 1; next }
			$1 == "A" && $2 != "" && !($2 in keep) && $2 < cutoff { print $2 }
		'
	)
	if [ -z "$victims" ]; then
		info "prune: nothing older than $keep_days days beyond the newest $keep_min"
		return 0
	fi
	count=0
	failed=0
	for v in $victims; do
		valid_name "$v" || continue
		dir=$(dirname "$(object_path "$v")")
		if rcl delete "$dir" --max-depth 1 --include "/$v" --include "/$v.json"; then
			info "prune: deleted $v"
			count=$((count + 1))
		else
			err "prune: could not delete $v"
			failed=1
		fi
	done
	rcl rmdirs --leave-root "$REMOTE_BASE" >/dev/null 2>&1 || true
	info "prune: deleted $count backup(s)"
	return "$failed"
}

cmd_list() {
	require_storage
	objs=$(list_objects) || die "cannot list backups"
	if [ -z "$objs" ]; then
		info "no backups found"
		return 0
	fi
	printf '%-44s %10s  %s\n' NAME SIZE 'UPLOADED (UTC)'
	printf '%s\n' "$objs" | while IFS=';' read -r n s t; do
		printf '%-44s %10s  %s\n' "$n" "$(human_size "$s")" "$t"
	done
}
