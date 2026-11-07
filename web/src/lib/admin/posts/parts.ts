import type { PostInput } from '#lib/api/types.js';
import { wordCount } from '../format';

export type PartInput = PostInput['parts'][number];

/** The API accepts 1–20 parts per post. */
export const MAX_PARTS = 20;

let seq = 0;
/** Stable client-side identity for a part (keys the list and the editor). */
export function partKey(): number {
	return ++seq;
}

export function emptyPart(): PartInput {
	return { title: '', content_md: '' };
}

/** Local estimate (220 wpm), used until the API has rendered the text. */
export function estimateMinutes(md: string): number {
	const words = wordCount(md);
	return words ? Math.max(1, Math.round(words / 220)) : 0;
}

/**
 * Coerces a stored local draft into the current PostInput shape. Drafts from
 * before posts had parts carry a single `content_md`; it becomes part 1.
 */
export function normalizeInput(
	v: Partial<PostInput> & { content_md?: unknown },
	fallback: PostInput
): PostInput {
	const parts = Array.isArray(v.parts)
		? v.parts
				.filter((p) => p && typeof p === 'object')
				.slice(0, MAX_PARTS)
				.map((p) => ({ title: String(p.title ?? ''), content_md: String(p.content_md ?? '') }))
		: typeof v.content_md === 'string'
			? [{ title: '', content_md: v.content_md }]
			: [];
	return {
		slug: typeof v.slug === 'string' ? v.slug : fallback.slug,
		title: typeof v.title === 'string' ? v.title : fallback.title,
		excerpt: typeof v.excerpt === 'string' ? v.excerpt : fallback.excerpt,
		cover_url: typeof v.cover_url === 'string' ? v.cover_url : fallback.cover_url,
		category: v.category === 'tech' || v.category === 'life' ? v.category : fallback.category,
		tags: Array.isArray(v.tags) ? v.tags.map(String) : fallback.tags,
		status: v.status === 'published' || v.status === 'draft' ? v.status : fallback.status,
		published_at:
			typeof v.published_at === 'string' || v.published_at === null
				? v.published_at
				: fallback.published_at,
		parts: parts.length ? parts : [emptyPart()]
	};
}
