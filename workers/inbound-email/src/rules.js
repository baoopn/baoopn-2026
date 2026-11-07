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
 * What to do with the API's answer:
 * "done" (2xx), "reject" (413: refuse the message), "drop" (other 4xx: log
 * and stop, retrying will not help) or "retry" (5xx and anything else).
 */
export function classify(status) {
	if (status >= 200 && status < 300) return 'done';
	if (status === 413) return 'reject';
	if (status >= 400 && status < 500) return 'drop';
	return 'retry';
}
