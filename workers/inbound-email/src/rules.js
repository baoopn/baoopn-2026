// Pure decisions of the Email Worker, kept apart so they can be tested with
// plain `node --test`.

/** The largest message forwarded to the API (the API reads at most 10 MiB). */
export const MAX_BYTES = 10 * 1024 * 1024;

/** Waits between attempts: three attempts in all. */
export const BACKOFF_MS = [500, 2000];

const REPLY_LOCAL = /^reply\+[0-9a-f]{32}$/i;

/**
 * Why a message must be refused before it is read, or null to accept it.
 * The recipient must be reply+<32 hex>@<domain>.
 */
export function rejectReason(to, size, domain) {
	const addr = String(to ?? '').trim().toLowerCase();
	const at = addr.lastIndexOf('@');
	const local = at > 0 ? addr.slice(0, at) : '';
	const host = at > 0 ? addr.slice(at + 1) : '';
	if (!domain || host !== String(domain).toLowerCase() || !REPLY_LOCAL.test(local)) {
		return 'Unknown address';
	}
	if (Number(size) > MAX_BYTES) {
		return 'Message too large';
	}
	return null;
}

/**
 * API answers that retrying cannot fix: 400 and 422 (a malformed request).
 * 404 is not one: it means the hook is not configured on the API, our own
 * misconfiguration, which a fix and a retry can recover from.
 */
const PERMANENT = new Set([400, 422]);

/**
 * What to do with the API's answer:
 * "done" (2xx), "reject" (413: refuse the message), "drop" (400, 422: log and
 * stop) or "retry" (everything else, including 401 and 403 for clock skew or a
 * secret rotated on one side first, 404 for a hook not yet configured, 408,
 * 425, 429 and 5xx).
 */
export function classify(status) {
	if (status >= 200 && status < 300) return 'done';
	if (status === 413) return 'reject';
	if (PERMANENT.has(status)) return 'drop';
	return 'retry';
}
