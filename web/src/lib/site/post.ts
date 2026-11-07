import type { PostDetail, PostPartRef } from '#lib/api/types.js';
import { required } from '#lib/site/data.js';

type Fetch = typeof fetch;

/** URL of part `no` of a post: part 1 lives at /blog/<slug>. */
export function partHref(slug: string, no: number): string {
	const base = `/blog/${encodeURIComponent(slug)}`;
	return no > 1 ? `${base}/${no}` : base;
}

/** Display title of a part; an empty title reads "Part N". */
export function partLabel(ref: PostPartRef): string {
	return ref.title.trim() || `Part ${ref.no}`;
}

/** Loads a post with one of its parts (404 when the post or part does not exist). */
export async function loadPost(f: Fetch, slug: string, no: number): Promise<PostDetail> {
	const q = no > 1 ? `?part=${no}` : '';
	const post = await required<PostDetail>(f, `/api/v1/posts/${encodeURIComponent(slug)}${q}`, 'Post');
	const part = { ...post.part, title: post.part?.title ?? '', toc: post.part?.toc ?? [] };
	return {
		...post,
		tags: post.tags ?? [],
		part,
		parts: post.parts?.length ? post.parts : [{ no: part.no, title: part.title }],
		part_count: post.part_count || post.parts?.length || 1
	};
}
