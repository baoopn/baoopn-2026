import type { PageLoad } from './$types';
import type { PostDetail } from '#lib/api/types.js';
import { required } from '#lib/site/data.js';

export const load: PageLoad = async ({ fetch, params }) => {
	const post = await required<PostDetail>(
		fetch,
		`/api/v1/posts/${encodeURIComponent(params.slug)}`,
		'Post'
	);
	return {
		post: {
			...post,
			tags: post.tags ?? [],
			toc: post.toc ?? [],
			series_posts: post.series_posts ?? []
		}
	};
};
