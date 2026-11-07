import type { LayoutLoad } from './$types';
import type { Profile } from '#lib/api/types.js';
import { safe } from '#lib/site/data.js';
import { withDefaults } from '#lib/site/defaults.js';

export const load: LayoutLoad = async ({ fetch }) => {
	const profile = await safe<Partial<Profile> | null>(fetch, '/api/v1/profile', null);
	return { profile: withDefaults(profile) };
};
