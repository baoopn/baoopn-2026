// Pure timing and status rules for the live inbox stream (live.svelte.ts).
// Plain JS with JSDoc types and no $app imports, so `node --test` can load it.

export const BACKOFF_MIN_MS = 1_000;
export const BACKOFF_MAX_MS = 30_000;
export const DEFAULT_RETRY_AFTER_S = 10;

/**
 * Reconnect delay for the given failed-attempt count: 1 s doubling to 30 s,
 * with ±50 % jitter so many tabs don't reconnect in lockstep.
 * @param {number} attempt 0 for the first retry after a good connection.
 * @param {() => number} [random] Returns [0, 1); injectable for tests.
 */
export function backoffDelay(attempt, random = Math.random) {
	const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** Math.max(0, attempt));
	return Math.round(base * (0.5 + random()));
}

/**
 * Delay after a 429/503: the server's Retry-After (seconds, default 10 s),
 * never shorter than the backoff.
 * @param {string | null} header
 * @param {number} backoffMs
 */
export function retryAfterDelay(header, backoffMs) {
	const ra = Number(header);
	const s = header && Number.isFinite(ra) && ra > 0 ? ra : DEFAULT_RETRY_AFTER_S;
	return Math.max(s * 1000, backoffMs);
}

/**
 * @typedef {'connecting' | 'live' | 'reconnecting' | 'offline' | 'paused'} LiveStatus
 */

/**
 * The status to show once the 2 s grace period after losing (or never
 * reaching) the stream has passed. Offline and paused win: they say why.
 * @param {LiveStatus} status
 * @param {boolean} ready The current connection has received `ready`.
 * @returns {LiveStatus}
 */
export function statusAfterGrace(status, ready) {
	if (ready || status === 'offline' || status === 'paused') return status;
	return 'reconnecting';
}
