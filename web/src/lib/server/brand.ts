import type { Brand, BrandSlot } from '#lib/api/types.js';
import { brandSrc } from '#lib/brand.js';
import { safe } from '#lib/site/data.js';

const OK_TTL = 60_000;
const FAIL_TTL = 10_000;

let cached: { brand: Brand | null; until: number } | null = null;
let inflight: Promise<Brand | null> | null = null;

/**
 * The brand doc, memoised in process: 60 s after a success, 10 s after a
 * failure (null). Never throws.
 */
export async function getBrand(f: typeof fetch): Promise<Brand | null> {
	const now = Date.now();
	if (cached && cached.until > now) return cached.brand;
	if (inflight) return inflight;
	inflight = (async () => {
		const brand = await safe<Brand | null>(f, '/api/v1/brand', null);
		const ok = !!brand?.assets;
		cached = { brand: ok ? brand : null, until: Date.now() + (ok ? OK_TTL : FAIL_TTL) };
		return cached.brand;
	})().finally(() => (inflight = null));
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
