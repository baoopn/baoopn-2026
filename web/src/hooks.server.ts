import type { Handle, HandleFetch } from '@sveltejs/kit/hooks';
import { API_URL, INTERNAL_API_TOKEN, TRUST_CLOUDFLARE } from '$app/env/private';

/**
 * The SvelteKit server is the only public service. It proxies /api/* and
 * /media/* to the Go API on the internal network, forwarding the client IP
 * and country with the shared INTERNAL_API_TOKEN so the API can trust them.
 *
 * Client-supplied copies of those headers are always stripped. Cloudflare's
 * CF-Connecting-IP / CF-IPCountry are honoured only with TRUST_CLOUDFLARE=true,
 * which must be paired with an origin firewall that only admits Cloudflare;
 * otherwise anyone hitting the origin could spoof them. Without it the client
 * IP is adapter-node's getClientAddress() (XFF_DEPTH=1 behind Traefik).
 */
const HOP_HEADERS = ['connection', 'keep-alive', 'transfer-encoding', 'upgrade', 'host'];
const SPOOFABLE = [
	'x-client-ip',
	'x-client-country',
	'x-internal-token',
	'cf-connecting-ip',
	'cf-ipcountry'
];

function clientHeaders(request: Request, ip: string): Headers {
	const h = new Headers();
	const cfIP = TRUST_CLOUDFLARE ? request.headers.get('cf-connecting-ip') : null;
	h.set('X-Client-IP', cfIP ?? ip);
	const country = TRUST_CLOUDFLARE ? request.headers.get('cf-ipcountry') : null;
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

export const handle: Handle = async ({ event, resolve }) => {
	const { pathname, search } = event.url;
	if (pathname.startsWith('/api/') || pathname.startsWith('/media/')) {
		const headers = forwardHeaders(event.request.headers, event.request, event.getClientAddress());
		const res = await fetch(API_URL + pathname + search, {
			method: event.request.method,
			headers,
			body: ['GET', 'HEAD'].includes(event.request.method) ? undefined : event.request.body,
			// @ts-expect-error duplex is required by undici for streamed bodies
			duplex: 'half',
			redirect: 'manual'
		});
		const out = new Headers(res.headers);
		out.delete('content-encoding');
		out.delete('content-length');
		return new Response(res.body, { status: res.status, headers: out });
	}
	return resolve(event, {
		preload: ({ type }) => type === 'font' || type === 'js' || type === 'css'
	});
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
