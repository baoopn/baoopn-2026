// Pure helpers for response security headers and CSP violation reports.
// No $app imports, so node can load this file without a build.

/** Headers set on every response unless the response already has them. */
const BASE_HEADERS = Object.freeze({
	'X-Frame-Options': 'DENY',
	'X-Content-Type-Options': 'nosniff',
	'Referrer-Policy': 'strict-origin-when-cross-origin',
	// autoplay, encrypted-media, fullscreen, clipboard-write and
	// picture-in-picture stay unlisted: the Spotify embed's allow= needs them.
	'Permissions-Policy':
		'camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()'
});

// 180 days. No includeSubDomains or preload. If Cloudflare HSTS is turned on
// for the zone, remove this one (see docs/deployment.md).
const HSTS = 'max-age=15552000';

/**
 * Adds the security headers to `headers` without overwriting existing values.
 * @param {Headers} headers
 * @param {boolean} https true when the request reached us over https
 */
export function setSecurityHeaders(headers, https) {
	for (const [k, v] of Object.entries(BASE_HEADERS)) {
		if (!headers.has(k)) headers.set(k, v);
	}
	if (https && !headers.has('Strict-Transport-Security')) {
		headers.set('Strict-Transport-Security', HSTS);
	}
}

/**
 * Returns a response that carries the security headers. Responses with
 * immutable headers (e.g. a fetch() result returned from an endpoint) are
 * copied first.
 * @param {Response} response
 * @param {boolean} https
 * @returns {Response}
 */
export function withSecurityHeaders(response, https) {
	try {
		setSecurityHeaders(response.headers, https);
		return response;
	} catch {
		const copy = new Response(response.body, response);
		setSecurityHeaders(copy.headers, https);
		return copy;
	}
}

export const CSP_REPORT_MAX_BYTES = 8 * 1024;
const FIELD_MAX = 200;

/**
 * Reads at most `max` bytes of a body. Returns null when the body is larger.
 * @param {ReadableStream<Uint8Array> | null} body
 * @param {number} max
 * @returns {Promise<string | null>}
 */
export async function readCapped(body, max) {
	if (!body) return '';
	const reader = body.getReader();
	const decoder = new TextDecoder();
	let text = '';
	let size = 0;
	for (;;) {
		const { done, value } = await reader.read();
		if (done) break;
		size += value.byteLength;
		if (size > max) {
			await reader.cancel().catch(() => {});
			return null;
		}
		text += decoder.decode(value, { stream: true });
	}
	return text + decoder.decode();
}

/** @param {unknown} v @param {boolean} uri */
function clip(v, uri) {
	if (typeof v !== 'string') return null;
	let s = v;
	if (uri) s = s.split(/[?#]/, 1)[0];
	return s.slice(0, FIELD_MAX);
}

/**
 * Pulls the fields we log out of a CSP report body. Accepts the legacy
 * report-uri format ({"csp-report": {...}}) and the Reporting API format
 * ([{type: "csp-violation", body: {...}}]). Returns null for anything else.
 * @param {string} text
 */
export function summarizeCspReport(text) {
	/** @type {any} */
	let data;
	try {
		data = JSON.parse(text);
	} catch {
		return null;
	}
	if (data && typeof data === 'object' && data['csp-report'] && typeof data['csp-report'] === 'object') {
		const r = data['csp-report'];
		return {
			'document-uri': clip(r['document-uri'], true),
			'violated-directive': clip(r['violated-directive'] ?? r['effective-directive'], false),
			'blocked-uri': clip(r['blocked-uri'], true),
			'source-file': clip(r['source-file'], true)
		};
	}
	const first = Array.isArray(data) ? data[0] : null;
	const b = first && typeof first === 'object' ? first.body : null;
	if (b && typeof b === 'object') {
		return {
			'document-uri': clip(b.documentURL, true),
			'violated-directive': clip(b.effectiveDirective, false),
			'blocked-uri': clip(b.blockedURL, true),
			'source-file': clip(b.sourceFile, true)
		};
	}
	return null;
}

/**
 * A fixed-window limiter: at most `limit` hits per `windowMs`, shared by all
 * callers of the returned function.
 * @param {number} limit
 * @param {number} windowMs
 * @param {() => number} [now]
 */
export function windowLimiter(limit, windowMs, now = Date.now) {
	let start = 0;
	let count = 0;
	return () => {
		const t = now();
		if (t - start >= windowMs) {
			start = t;
			count = 0;
		}
		if (count >= limit) return false;
		count++;
		return true;
	};
}
