import type { Brand, BrandSlot } from '#lib/api/types.js';
import { api } from '#lib/api/client.js';
import { brandSrc } from '#lib/brand.js';

const OK_TTL = 60_000;
const FAIL_TTL = 10_000;
/** Every SSR page waits on this, so a slow API must not hold pages up. */
const TIMEOUT = 2_000;

let cached: { brand: Brand | null; until: number } | null = null;
let inflight: Promise<Brand | null> | null = null;
// Fetches are numbered so an older one that lands late never overwrites a newer result.
let started = 0;
let stored = 0;

/** One GET /api/v1/brand, aborted after TIMEOUT. Never throws; null on any failure. */
async function load(f: typeof fetch): Promise<Brand | null> {
	const seq = ++started;
	let brand: Brand | null = null;
	try {
		// Not safe(): it takes no request options, and this needs the abort signal.
		const b = await api<Brand | null>('/api/v1/brand', {
			fetch: f,
			signal: AbortSignal.timeout(TIMEOUT)
		});
		if (b?.assets) brand = b;
	} catch {
		// API down, error status or timeout: fall back to the redirect URLs.
	}
	if (seq > stored) {
		stored = seq;
		cached = { brand, until: Date.now() + (brand ? OK_TTL : FAIL_TTL) };
	}
	return brand;
}

/**
 * The brand doc, memoised in process: 60 s after a success, 10 s after a
 * failure or timeout (null). Never throws, and never waits longer than ~2 s.
 * `fresh` skips the memo and refreshes it (admin pages, so a replace shows at once).
 */
export async function getBrand(f: typeof fetch, opts: { fresh?: boolean } = {}): Promise<Brand | null> {
	if (opts.fresh) return load(f);
	if (cached && cached.until > Date.now()) return cached.brand;
	if (!inflight) inflight = load(f).finally(() => (inflight = null));
	return inflight;
}

/** 302 to the current file for a slot (/favicon.ico and friends). */
export async function brandRedirect(f: typeof fetch, slot: BrandSlot): Promise<Response> {
	const brand = await getBrand(f);
	return new Response(null, {
		status: 302,
		headers: { Location: brandSrc(brand, slot), 'Cache-Control': 'public, max-age=3600' }
	});
}
