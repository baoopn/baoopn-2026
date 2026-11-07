// HMAC-SHA256 request signing, shared with the API (internal/contact/verify.go):
// signature = hex(HMAC-SHA256(secret, utf8(ts + "." + envelopeTo + "\n") ++ body)).
// Covering the envelope recipient stops replays to a different thread token.

const encoder = new TextEncoder();

/** Concatenates byte arrays. */
export function concat(...parts) {
	const out = new Uint8Array(parts.reduce((n, p) => n + p.byteLength, 0));
	let at = 0;
	for (const p of parts) {
		out.set(p, at);
		at += p.byteLength;
	}
	return out;
}

/** Lower-case hex of bytes. */
export function hex(bytes) {
	return Array.from(new Uint8Array(bytes), (b) => b.toString(16).padStart(2, '0')).join('');
}

/**
 * Signs body (a Uint8Array or ArrayBuffer) at ts (unix seconds, string or
 * number) with secret. Uses WebCrypto, available in Workers and Node 22.
 */
export async function sign(secret, ts, envelopeTo, body) {
	const key = await crypto.subtle.importKey(
		'raw',
		encoder.encode(secret),
		{ name: 'HMAC', hash: 'SHA-256' },
		false,
		['sign']
	);
	const data = concat(encoder.encode(`${ts}.${envelopeTo ?? ''}\n`), new Uint8Array(body));
	return hex(await crypto.subtle.sign('HMAC', key, data));
}

/** The current unix time in seconds. */
export function nowSeconds() {
	return Math.floor(Date.now() / 1000);
}
