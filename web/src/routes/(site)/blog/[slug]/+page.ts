import type { PageLoad } from './$types';
import { loadPost } from '#lib/site/post.js';

export const load: PageLoad = async ({ fetch, params }) => ({
	post: await loadPost(fetch, params.slug, 1)
});
