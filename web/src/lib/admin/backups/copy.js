// Plain-English copy for the backups page: health codes, run kinds and run
// statuses. Plain JS with JSDoc types and no $app imports, so `node --test`
// can load it (test/backups-copy.test.js).

/** @typedef {'ok' | 'warn' | 'danger' | 'info' | 'muted'} Tone */
/** @typedef {{ label: string; tone: Tone; title: string; explanation: string }} HealthCopy */

/** Chip fill for each tone (spot inks from app.css). */
/** @type {Record<Tone, string>} */
export const TONE_SPOT = {
	ok: 'var(--spot-mint)',
	warn: 'var(--spot-butter)',
	danger: 'var(--spot-rose)',
	info: 'var(--spot-sky)',
	// Text on a spot is always --on-spot, so muted needs a real spot fill too.
	muted: 'var(--spot-lilac)'
};

export const DEFAULT_MAX_AGE_HOURS = 26;

/** @type {Record<string, HealthCopy>} */
export const HEALTH = {
	ok: {
		label: 'Working',
		tone: 'ok',
		title: 'Backups are working',
		explanation: 'The last backup finished in time.'
	},
	warning: {
		label: 'Warning',
		tone: 'warn',
		title: 'Last backup failed; will retry',
		explanation: 'An older backup is still recent enough. The service tries again soon.'
	},
	stale: {
		label: 'Stale',
		tone: 'danger',
		title: 'No backup in the last {hours} hours',
		explanation: 'Check the last error below and the backup service logs.'
	},
	waiting: {
		label: 'Waiting',
		tone: 'info',
		title: 'Waiting for the first backup',
		explanation: 'The first backup runs at the next scheduled time.'
	},
	paused: {
		label: 'Paused',
		tone: 'muted',
		title: 'Scheduled backups are paused',
		explanation: 'Back up now still works while scheduled backups are paused.'
	},
	not_configured: {
		label: 'Not set up',
		tone: 'warn',
		title: 'Backups are not set up',
		explanation: 'Set the backup variables on the backup service, then restart it.'
	},
	offline: {
		label: 'Offline',
		tone: 'danger',
		title: 'The backup service has not checked in since {since}',
		explanation: 'It may be stopped or unable to reach the database.'
	},
	not_reporting: {
		label: 'Not reporting',
		tone: 'muted',
		title: 'The backup service has not reported yet (not deployed, or an older version)',
		explanation: 'Deploy the current backup service to see its status here.'
	}
};

/** @type {HealthCopy} */
export const HEALTH_FALLBACK = {
	label: 'Unknown',
	tone: 'muted',
	title: 'Backup status is unknown',
	explanation: 'Reload the page. If this stays, the API may be newer than this page.'
};

/**
 * Copy for a health code, with its placeholders filled in.
 * @param {string} code
 * @param {{ hours?: number | null; since?: string | null; idleReason?: string | null }} [ctx]
 * @returns {HealthCopy}
 */
export function healthCopy(code, ctx = {}) {
	const base = Object.hasOwn(HEALTH, code) ? HEALTH[code] : HEALTH_FALLBACK;
	const title = base.title
		.replace('{hours}', String(ctx.hours ?? DEFAULT_MAX_AGE_HOURS))
		.replace('{since}', ctx.since || 'a while ago');
	const explanation =
		code === 'not_configured' && ctx.idleReason ? ctx.idleReason : base.explanation;
	return { ...base, title, explanation };
}

/** @type {Record<string, string>} */
export const RUN_KIND = {
	scheduled: 'Scheduled',
	startup: 'On start',
	retry: 'Retry',
	manual: 'Back up now',
	cli: 'Command line'
};

/** Short kind labels for the runs table. */
/** @type {Record<string, string>} */
export const RUN_KIND_SHORT = {
	scheduled: 'Scheduled',
	startup: 'Startup',
	retry: 'Retry',
	manual: 'Manual',
	cli: 'CLI'
};

/** @param {string} kind */
export function runKindShort(kind) {
	return Object.hasOwn(RUN_KIND_SHORT, kind) ? RUN_KIND_SHORT[kind] : 'Other';
}

/** @param {string} kind */
export function runKindLabel(kind) {
	return Object.hasOwn(RUN_KIND, kind) ? RUN_KIND[kind] : 'Other';
}

/** @type {Record<string, { label: string; tone: Tone }>} */
export const RUN_STATUS = {
	queued: { label: 'Queued', tone: 'info' },
	running: { label: 'Running', tone: 'info' },
	ok: { label: 'Done', tone: 'ok' },
	failed: { label: 'Failed', tone: 'danger' }
};

/**
 * @param {string} status
 * @returns {{ label: string; tone: Tone }}
 */
export function runStatusCopy(status) {
	return Object.hasOwn(RUN_STATUS, status)
		? RUN_STATUS[status]
		: { label: 'Unknown', tone: 'muted' };
}

/**
 * Why "Back up now" is unavailable, or null when it can run.
 * @param {string} health
 * @param {{ status: string } | null} activeRun
 * @returns {string | null}
 */
export function runBlockedReason(health, activeRun) {
	if (activeRun?.status === 'queued') return 'A backup is already queued.';
	if (activeRun) return 'A backup is running now.';
	if (health === 'offline') return 'The backup service is offline.';
	if (health === 'not_configured') return 'Backups are not set up.';
	if (health === 'not_reporting') return 'The backup service has not reported yet.';
	return null;
}

const p2 = (/** @type {number} */ n) => String(n).padStart(2, '0');

/**
 * Exact UTC stamp, e.g. "2026-11-09 03:00 UTC"; "" for a missing or bad value.
 * @param {string | null | undefined} iso
 */
export function utcStamp(iso) {
	if (!iso) return '';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return '';
	return `${d.getUTCFullYear()}-${p2(d.getUTCMonth() + 1)}-${p2(d.getUTCDate())} ${p2(d.getUTCHours())}:${p2(d.getUTCMinutes())} UTC`;
}

/**
 * The run that just ended, when the watched active run is gone from a fresh
 * status: matched by id, else the newest finished run (the queued stand-in
 * has id 0). Null while a run is still active or nothing was watched.
 * @template {{ id: number; status: string }} R
 * @param {R | null} watched
 * @param {{ active_run: R | null; last_run: R | null; runs: R[] }} next
 * @returns {R | null}
 */
export function finishedRun(watched, next) {
	if (!watched || next.active_run) return null;
	const byId = watched.id ? next.runs.find((r) => r.id === watched.id) : undefined;
	const run = byId ?? next.last_run;
	return run && (run.status === 'ok' || run.status === 'failed') ? run : null;
}
