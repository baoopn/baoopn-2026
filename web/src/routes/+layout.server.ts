import type { LayoutServerLoad } from './$types';
import { getBrand } from '#lib/server/brand.js';

// Brand asset URLs for every page (favicons, logos). Never throws: null makes
// the components fall back to the /api/v1/brand/<slot> redirects.
//
// Admin pages skip the memo, so invalidateAll() after a replace or reset shows
// the new file at once (and refreshes the memo for the public site). This adds
// no new lever: it is one GET of the public /api/v1/brand per request.
// untrack: reading the path must not rerun this load on every client navigation.
export const load: LayoutServerLoad = async ({ fetch, url, untrack }) => {
	const admin = untrack(() => url.pathname === '/admin' || url.pathname.startsWith('/admin/'));
	return { brand: await getBrand(fetch, { fresh: admin }) };
};
