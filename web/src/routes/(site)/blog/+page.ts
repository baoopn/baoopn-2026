import type { PageLoad } from './$types';
import type { PostSummary, TagCount } from '#lib/api/types.js';
import { safe, safeFull } from '#lib/site/data.js';
import { parseCategory } from '#lib/site/post.js';

const PAGE_SIZE = 10;

export const load: PageLoad = async ({ fetch, url }) => {
	const tag = url.searchParams.get('tag') ?? '';
	// An unknown category shows every post (the "All" tab).
	const category = parseCategory(url.searchParams.get('category'));
	const pageNo = Math.max(1, Number.parseInt(url.searchParams.get('page') ?? '1', 10) || 1);

	const q = new URLSearchParams({ page: String(pageNo), limit: String(PAGE_SIZE) });
	if (category) q.set('category', category);
	if (tag) q.set('tag', tag);
	const tagsQuery = category ? `?category=${category}` : '';

	const [posts, tags] = await Promise.all([
		safeFull<PostSummary[]>(fetch, `/api/v1/posts?${q}`, []),
		safe<TagCount[]>(fetch, `/api/v1/tags${tagsQuery}`, [])
	]);
	const total = posts.meta?.total ?? posts.data.length;
	const limit = posts.meta?.limit ?? PAGE_SIZE;
	return {
		posts: posts.data,
		tags,
		tag,
		category,
		page: posts.meta?.page ?? pageNo,
		pages: Math.max(1, Math.ceil(total / limit)),
		total
	};
};
