// Pure helpers for the /api and /media proxy in hooks.server.ts. Plain JS with
// JSDoc types and no $app imports, so `node --test` can load this file.

/**
 * Hop-by-hop headers (RFC 9110 7.6.1) plus host, which fetch sets from the
 * upstream URL. Names listed in the Connection header are dropped as well.
 */
export const HOP_HEADERS = Object.freeze([
	'connection',
	'keep-alive',
	'transfer-encoding',
	'upgrade',
	'expect',
	'te',
	'trailer',
	'proxy-connection',
	'proxy-authorization',
	'host'
]);

/**
 * Deletes the hop-by-hop headers from h, including any named in Connection.
 * @param {Headers} h
 * @returns {Headers} the same Headers, for chaining
 */
export function stripHopHeaders(h) {
	const listed = (h.get('connection') ?? '')
		.split(',')
		.map((s) => s.trim().toLowerCase())
		.filter(Boolean);
	for (const k of [...HOP_HEADERS, ...listed]) h.delete(k);
	return h;
}

/**
 * True when the fetch failed because the client went away (its request
 * signal aborted), not because the API is down.
 * @param {unknown} err
 * @param {AbortSignal | undefined} signal
 */
export function isClientAbort(err, signal) {
	if (signal?.aborted) return true;
	return err instanceof Error && err.name === 'AbortError';
}

/**
 * The API's error envelope with 503. Not 502: Traefik's error-pages
 * middleware rewrites 502 bodies into an HTML page.
 * @returns {Response}
 */
export function upstreamUnavailable() {
	const body = {
		error: {
			code: 'upstream_unavailable',
			message: 'The API is not reachable. Try again soon.'
		}
	};
	return new Response(JSON.stringify(body), {
		status: 503,
		headers: {
			'Content-Type': 'application/json; charset=utf-8',
			'Cache-Control': 'no-store'
		}
	});
}
