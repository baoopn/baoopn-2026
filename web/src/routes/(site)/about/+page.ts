import type { PageLoad } from './$types';
import type { TopArtist } from '#lib/api/types.js';
import { safe } from '#lib/site/data.js';

export const load: PageLoad = async ({ fetch }) => {
	return {
		topArtists: await safe<TopArtist[]>(fetch, '/api/v1/listening/top-artists?range=30d&limit=6', [])
	};
};
