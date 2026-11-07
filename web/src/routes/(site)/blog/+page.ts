import type { PageLoad } from './$types';
import type { PostSummary, TagCount } from '#lib/api/types.js';
import { safe, safeFull } from '#lib/site/data.js';

const PAGE_SIZE = 10;

export const load: PageLoad = async ({ fetch, url }) => {
	const tag = url.searchParams.get('tag') ?? '';
	const series = url.searchParams.get('series') ?? '';
	const pageNo = Math.max(1, Number.parseInt(url.searchParams.get('page') ?? '1', 10) || 1);

	const q = new URLSearchParams({ page: String(pageNo), limit: String(PAGE_SIZE) });
	if (tag) q.set('tag', tag);
	if (series) q.set('series', series);

	const [posts, tags] = await Promise.all([
		safeFull<PostSummary[]>(fetch, `/api/v1/posts?${q}`, []),
		safe<TagCount[]>(fetch, '/api/v1/tags', [])
	]);
	const total = posts.meta?.total ?? posts.data.length;
	const limit = posts.meta?.limit ?? PAGE_SIZE;
	return {
		posts: posts.data,
		tags,
		tag,
		series,
		page: posts.meta?.page ?? pageNo,
		pages: Math.max(1, Math.ceil(total / limit)),
		total
	};
};
