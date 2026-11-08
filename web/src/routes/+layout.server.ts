import type { LayoutServerLoad } from './$types';
import { getBrand } from '#lib/server/brand.js';

// Brand asset URLs for every page (favicons, logos). Never throws: null makes
// the components fall back to the /api/v1/brand/<slot> redirects.
export const load: LayoutServerLoad = async ({ fetch }) => ({ brand: await getBrand(fetch) });
