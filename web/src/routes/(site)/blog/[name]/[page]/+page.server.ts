import { error, redirect } from '@sveltejs/kit';
import type { PageServerLoad } from './$types';
import { api, ApiRequestError } from '#lib/api/client.js';

/** Old v3 URLs (/blog/<name>/<page>) permanently redirect to the imported post. */
export const load: PageServerLoad = async ({ fetch, params }) => {
	const path = `/blog/${params.name}/${params.page}`;
	let slug: string | undefined;
	try {
		slug = (
			await api<{ slug: string }>(`/api/v1/posts/legacy?path=${encodeURIComponent(path)}`, {
				fetch
			})
		)?.slug;
	} catch (e) {
		if (!(e instanceof ApiRequestError && e.status === 404)) {
			error(503, "This post couldn't be looked up right now. Please try again shortly.");
		}
	}
	if (!slug) error(404, 'Post not found');
	redirect(301, `/blog/${encodeURIComponent(slug)}`);
};
