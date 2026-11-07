import type { Handle, HandleFetch } from '@sveltejs/kit/hooks';
import { API_URL, INTERNAL_API_TOKEN, TRUST_CLOUDFLARE } from '$app/env/private';
import { inCloudflare, resolveClient } from '#lib/server/client-ip.js';
import {
	CSP_REPORT_MAX_BYTES,
	readCapped,
	summarizeCspReport,
	windowLimiter,
	withSecurityHeaders
} from '#lib/server/security-headers.js';

/**
 * The SvelteKit server is the only public service. It proxies /api/* and
 * /media/* to the Go API on the internal network, forwarding the client IP
 * and country with the shared INTERNAL_API_TOKEN so the API can trust them.
 *
 * Client-supplied copies of those headers are always stripped. Cloudflare's
 * CF-Connecting-IP / CF-IPCountry are honoured only with TRUST_CLOUDFLARE=true
 * AND when the TCP peer is a Cloudflare edge (lib/server/client-ip.js). The
 * peer is adapter-node's getClientAddress(): with XFF_DEPTH=1 that is the
 * address Traefik appended to X-Forwarded-For, i.e. who connected to Traefik.
 * Someone hitting the origin directly is not a Cloudflare IP, so their CF-*
 * headers are ignored.
 */
const HOP_HEADERS = ['connection', 'keep-alive', 'transfer-encoding', 'upgrade', 'host'];
const SPOOFABLE = [
	'x-client-ip',
	'x-client-country',
	'x-internal-token',
	'cf-connecting-ip',
	'cf-ipcountry'
];

const MINUTE = 60_000;
let lastPeerWarning = 0;

/** With trust on, a peer outside the Cloudflare ranges is logged at most once per minute. */
function warnUntrustedPeer(peer: string) {
	const now = Date.now();
	if (now - lastPeerWarning < MINUTE) return;
	lastPeerWarning = now;
	console.warn(
		JSON.stringify({ msg: 'TRUST_CLOUDFLARE is on but the peer is not a Cloudflare IP', peer })
	);
}

function clientHeaders(request: Request, peer: string): Headers {
	if (TRUST_CLOUDFLARE && !inCloudflare(peer)) warnUntrustedPeer(peer);
	const { ip, country } = resolveClient({
		peer,
		cfIP: request.headers.get('cf-connecting-ip'),
		cfCountry: request.headers.get('cf-ipcountry'),
		trust: TRUST_CLOUDFLARE
	});
	const h = new Headers();
	h.set('X-Client-IP', ip);
	if (country) h.set('X-Client-Country', country);
	if (INTERNAL_API_TOKEN) h.set('X-Internal-Token', INTERNAL_API_TOKEN);
	return h;
}

/** Copies request headers, dropping hop-by-hop and spoofable ones, then adds ours. */
function forwardHeaders(from: Headers, request: Request, ip: string): Headers {
	const headers = new Headers(from);
	for (const k of [...HOP_HEADERS, ...SPOOFABLE]) headers.delete(k);
	for (const [k, v] of clientHeaders(request, ip)) headers.set(k, v);
	return headers;
}

/** Shared by all visitors: CSP reports beyond 60 a minute are dropped unread. */
const allowCspReport = windowLimiter(60, MINUTE);

/** Logs one line per CSP violation report. Always 204; never reaches the API. */
async function cspReport(request: Request): Promise<Response> {
	if (request.method !== 'POST') {
		return new Response(null, { status: 405, headers: { Allow: 'POST' } });
	}
	const declared = Number(request.headers.get('content-length') ?? 0);
	if (declared <= CSP_REPORT_MAX_BYTES && allowCspReport()) {
		const text = await readCapped(request.body, CSP_REPORT_MAX_BYTES).catch(() => null);
		const report = text ? summarizeCspReport(text) : null;
		if (report) console.log(JSON.stringify({ msg: 'csp-report', ...report }));
	}
	return new Response(null, { status: 204 });
}

async function proxy(event: Parameters<Handle>[0]['event']): Promise<Response> {
	const { pathname, search } = event.url;
	const headers = forwardHeaders(event.request.headers, event.request, event.getClientAddress());
	const res = await fetch(API_URL + pathname + search, {
		method: event.request.method,
		headers,
		body: ['GET', 'HEAD'].includes(event.request.method) ? undefined : event.request.body,
		// @ts-expect-error duplex is required by undici for streamed bodies
		duplex: 'half',
		redirect: 'manual',
		// A client that goes away (e.g. a closed SSE tab) aborts the upstream request.
		signal: event.request.signal
	});
	// res.body is passed through as a stream, chunk by chunk, so SSE
	// (/api/v1/listening/now-playing/stream) is not buffered. fetch already
	// decoded any compression, so the encoding and length no longer apply.
	const out = new Headers(res.headers);
	out.delete('content-encoding');
	out.delete('content-length');
	return new Response(res.body, { status: res.status, headers: out });
}

export const handle: Handle = async ({ event, resolve }) => {
	const { pathname } = event.url;
	const https = event.url.protocol === 'https:';
	let response: Response;
	if (pathname === '/csp-report') {
		response = await cspReport(event.request);
	} else if (pathname.startsWith('/api/') || pathname.startsWith('/media/')) {
		response = await proxy(event);
	} else {
		response = await resolve(event, {
			preload: ({ type }) => type === 'font' || type === 'js' || type === 'css'
		});
	}
	return withSecurityHeaders(response, https);
};

/** Server-side load fetches of /api/* go straight to the Go API. */
export const handleFetch: HandleFetch = async ({ event, request, fetch }) => {
	const url = new URL(request.url);
	if (url.origin === event.url.origin && url.pathname.startsWith('/api/')) {
		const headers = forwardHeaders(request.headers, event.request, event.getClientAddress());
		// new Request(url, request) keeps method, body and signal; then override headers.
		return fetch(new Request(API_URL + url.pathname + url.search, new Request(request, { headers })));
	}
	return fetch(request);
};
