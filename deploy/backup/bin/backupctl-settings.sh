# shellcheck shell=sh
# Admin overrides for the backup schedule and retention (settings key
# "backup"), read every tick by the scheduler. Sourced by backupctl, after
# backupctl-status.sh. Missing or bad values fall back to env.

PAUSED=false
SETTINGS_APPLIED=
RESUMED=

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
			# shellcheck disable=SC2034 # read by cmd_run (backupctl)
			RESUMED=1
		fi
	fi
}
