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

/**
 * Forwards request to url with the given headers and streams the answer back.
 * fetchFn is injected so tests can stand in for the API.
 *
 * res.body is passed through chunk by chunk, so SSE is not buffered. fetch
 * already decoded any compression, so content-encoding and content-length no
 * longer apply; hop-by-hop headers from the API are dropped too.
 * @param {typeof fetch} fetchFn
 * @param {string} url
 * @param {Request} request the incoming client request
 * @param {Headers} headers headers to send upstream
 * @returns {Promise<Response>}
 */
export async function proxyFetch(fetchFn, url, request, headers) {
	let res;
	try {
		res = await fetchFn(url, {
			method: request.method,
			headers,
			body: ['GET', 'HEAD'].includes(request.method) ? undefined : request.body,
			// @ts-expect-error duplex is required by undici for streamed bodies
			duplex: 'half',
			redirect: 'manual',
			// A client that goes away (e.g. a closed SSE tab) aborts the upstream request.
			signal: request.signal
		});
	} catch (err) {
		// A client abort is routine; nobody reads this response. Anything else
		// means the API is down or unreachable.
		if (!isClientAbort(err, request.signal)) {
			const path = new URL(url).pathname;
			console.error(JSON.stringify({ msg: 'api proxy failed', path, error: String(err) }));
		}
		return upstreamUnavailable();
	}
	const out = stripHopHeaders(new Headers(res.headers));
	out.delete('content-encoding');
	out.delete('content-length');
	return new Response(res.body, { status: res.status, headers: out });
}
