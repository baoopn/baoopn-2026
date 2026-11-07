// Pure client-IP helpers. Plain JS with JSDoc types and no $app imports, so
// `node --test` can load this file without a SvelteKit build.

/**
 * Cloudflare edge ranges, copied on 2026-11-07 from
 * https://www.cloudflare.com/ips-v4 and https://www.cloudflare.com/ips-v6.
 * Cloudflare changes these rarely; refresh them from those URLs when it does.
 */
export const CLOUDFLARE_RANGES = Object.freeze([
	'173.245.48.0/20',
	'103.21.244.0/22',
	'103.22.200.0/22',
	'103.31.4.0/22',
	'141.101.64.0/18',
	'108.162.192.0/18',
	'190.93.240.0/20',
	'188.114.96.0/20',
	'197.234.240.0/22',
	'198.41.128.0/17',
	'162.158.0.0/15',
	'104.16.0.0/13',
	'104.24.0.0/14',
	'172.64.0.0/13',
	'131.0.72.0/22',
	'2400:cb00::/32',
	'2606:4700::/32',
	'2803:f800::/32',
	'2405:b500::/32',
	'2405:8100::/32',
	'2a06:98c0::/29',
	'2c0f:f248::/32'
]);

/** @typedef {{ family: 4 | 6, value: bigint, address: string }} ParsedIP */

const V4_PART = /^(0|[1-9][0-9]{0,2})$/;
const V6_GROUP = /^[0-9a-fA-F]{1,4}$/;
const MAPPED_PREFIX = 0xffffn << 32n;

/** @param {string} s @returns {bigint | null} */
function parseV4(s) {
	const parts = s.split('.');
	if (parts.length !== 4) return null;
	let v = 0n;
	for (const p of parts) {
		if (!V4_PART.test(p)) return null;
		const n = Number(p);
		if (n > 255) return null;
		v = (v << 8n) | BigInt(n);
	}
	return v;
}

/** @param {string} s @returns {bigint | null} */
function parseV6(s) {
	// Zone ids (fe80::1%eth0) and brackets are rejected by the character check.
	if (!/^[0-9a-fA-F:.]+$/.test(s)) return null;
	const halves = s.split('::');
	if (halves.length > 2) return null;

	/** @param {string} h @returns {bigint[] | null} */
	const groups = (h) => {
		if (h === '') return [];
		const out = [];
		const parts = h.split(':');
		for (let i = 0; i < parts.length; i++) {
			const p = parts[i];
			if (i === parts.length - 1 && p.includes('.')) {
				// Embedded IPv4 in the last 32 bits, e.g. ::ffff:1.2.3.4.
				const v4 = parseV4(p);
				if (v4 === null) return null;
				out.push(v4 >> 16n, v4 & 0xffffn);
			} else if (V6_GROUP.test(p)) {
				out.push(BigInt(parseInt(p, 16)));
			} else {
				return null;
			}
		}
		return out;
	};

	const head = groups(halves[0]);
	if (head === null) return null;
	// An embedded IPv4 may only appear at the very end of the address.
	if (halves.length === 2 && halves[0].includes('.')) return null;
	let all;
	if (halves.length === 2) {
		const tail = groups(halves[1]);
		if (tail === null) return null;
		const missing = 8 - head.length - tail.length;
		if (missing < 1) return null;
		all = [...head, ...Array(missing).fill(0n), ...tail];
	} else {
		all = head;
	}
	if (all.length !== 8) return null;
	return all.reduce((acc, g) => (acc << 16n) | g, 0n);
}

/** @param {bigint} v */
function formatV4(v) {
	return [24n, 16n, 8n, 0n].map((s) => String((v >> s) & 0xffn)).join('.');
}

/** RFC 5952 text: lowercase, no leading zeros, longest zero run (2+) as "::". @param {bigint} v */
function formatV6(v) {
	const g = [];
	for (let i = 7; i >= 0; i--) g.push(Number((v >> BigInt(i * 16)) & 0xffffn));
	let best = -1;
	let bestLen = 1;
	for (let i = 0; i < 8; ) {
		if (g[i] !== 0) {
			i++;
			continue;
		}
		let j = i;
		while (j < 8 && g[j] === 0) j++;
		if (j - i > bestLen) {
			best = i;
			bestLen = j - i;
		}
		i = j;
	}
	const hex = g.map((n) => n.toString(16));
	if (best === -1) return hex.join(':');
	return hex.slice(0, best).join(':') + '::' + hex.slice(best + bestLen).join(':');
}

/**
 * Parses an IPv4 or IPv6 address. IPv4-mapped IPv6 (::ffff:a.b.c.d) becomes
 * IPv4. Returns null for anything else: lists, ports, zone ids, whitespace.
 * @param {unknown} s
 * @returns {ParsedIP | null}
 */
export function parseIP(s) {
	if (typeof s !== 'string' || s.length === 0 || s.length > 45) return null;
	if (!s.includes(':')) {
		const v = parseV4(s);
		return v === null ? null : { family: 4, value: v, address: formatV4(v) };
	}
	const v = parseV6(s);
	if (v === null) return null;
	if (v >> 32n === MAPPED_PREFIX >> 32n) {
		const v4 = v & 0xffffffffn;
		return { family: 4, value: v4, address: formatV4(v4) };
	}
	return { family: 6, value: v, address: formatV6(v) };
}

/** @type {{ family: 4 | 6, base: bigint, mask: bigint }[]} */
const RANGES = CLOUDFLARE_RANGES.map((cidr) => {
	const [addr, bits] = cidr.split('/');
	const ip = parseIP(addr);
	if (!ip) throw new Error(`bad Cloudflare range ${cidr}`);
	const width = ip.family === 4 ? 32n : 128n;
	const host = width - BigInt(bits);
	const mask = ((1n << width) - 1n) ^ ((1n << host) - 1n);
	return { family: ip.family, base: ip.value & mask, mask };
});

/**
 * True when the address is inside one of the Cloudflare edge ranges.
 * @param {string | ParsedIP | null | undefined} ip
 */
export function inCloudflare(ip) {
	const p = typeof ip === 'string' ? parseIP(ip) : ip;
	if (!p) return false;
	return RANGES.some((r) => r.family === p.family && (p.value & r.mask) === r.base);
}

const COUNTRY = /^[A-Z0-9]{2}$/;

/**
 * Picks the client IP and country. The Cloudflare headers are trusted only
 * when trust is on AND the TCP peer is a Cloudflare edge. Anyone else could
 * send CF-Connecting-IP straight to the origin.
 * @param {{ peer: string, cfIP?: string | null, cfCountry?: string | null, trust: boolean }} input
 * @returns {{ ip: string, country: string | null }}
 */
export function resolveClient({ peer, cfIP, cfCountry, trust }) {
	if (trust && inCloudflare(peer)) {
		const client = parseIP(cfIP);
		if (client) {
			const country = typeof cfCountry === 'string' && COUNTRY.test(cfCountry) ? cfCountry : null;
			return { ip: client.address, country };
		}
	}
	return { ip: peer, country: null };
}
