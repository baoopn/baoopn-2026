import { error, redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';
import { loadPost, partHref } from '#lib/site/post.js';

export const load: PageLoad = async ({ fetch, params }) => {
	if (!/^[1-9]\d{0,2}$/.test(params.part)) error(404, 'Post not found');
	const no = Number(params.part);
	if (no === 1) redirect(301, partHref(params.slug, 1));
	return { post: await loadPost(fetch, params.slug, no) };
};
