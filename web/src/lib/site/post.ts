import type { PostCategory, PostDetail, PostPartRef } from '#lib/api/types.js';
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
		category: parseCategory(post.category) || 'tech',
		tags: post.tags ?? [],
		part,
		parts: post.parts?.length ? post.parts : [{ no: part.no, title: part.title }],
		part_count: post.part_count || post.parts?.length || 1
	};
}

/** The blog categories, in tab order. */
export const CATEGORIES: readonly PostCategory[] = ['tech', 'life'];

const CATEGORY_LABELS: Record<PostCategory, string> = { tech: 'Tech', life: 'Life' };

/** Display label of a category ("Tech", "Life"); an unknown value reads "Tech". */
export function categoryLabel(c: string | null | undefined): string {
	return CATEGORY_LABELS[parseCategory(c) || 'tech'];
}

/** A valid category, or '' for anything else (no filter). */
export function parseCategory(c: string | null | undefined): PostCategory | '' {
	return c === 'tech' || c === 'life' ? c : '';
}

/** The blog list filtered to one category. */
export function categoryHref(c: PostCategory): string {
	return `/blog?category=${c}`;
}

/** Chip colour of a category label. */
export function categoryChip(c: string | null | undefined): string {
	return parseCategory(c) === 'life' ? 'var(--spot-peach)' : 'var(--spot-sky)';
}
