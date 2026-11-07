import type { RequestHandler } from './$types';
import type { PostSummary, ProjectSummary } from '#lib/api/types.js';
import { safe, safeFull } from '#lib/site/data.js';
import { escapeXml } from '#lib/site/format.js';
import { partHref } from '#lib/site/post.js';

const STATIC = ['/', '/projects', '/blog', '/about', '/contact'];
const LIMIT = 50;
const MAX_PAGES = 40;

async function allPosts(f: typeof fetch): Promise<PostSummary[]> {
	const out: PostSummary[] = [];
	for (let page = 1; page <= MAX_PAGES; page++) {
		const r = await safeFull<PostSummary[]>(f, `/api/v1/posts?limit=${LIMIT}&page=${page}`, []);
		out.push(...r.data);
		const total = r.meta?.total ?? 0;
		if (r.data.length < LIMIT || out.length >= total) break;
	}
	return out;
}

export const GET: RequestHandler = async ({ fetch, url }) => {
	const [posts, projects] = await Promise.all([
		allPosts(fetch),
		safe<ProjectSummary[]>(fetch, '/api/v1/projects', [])
	]);
	const origin = url.origin;
	const day = (s: string | null | undefined) => {
		const d = s ? new Date(s) : null;
		return d && !Number.isNaN(d.getTime()) ? d.toISOString().slice(0, 10) : null;
	};
	const entry = (path: string, lastmod?: string | null) =>
		`\t<url><loc>${escapeXml(origin + path)}</loc>${lastmod ? `<lastmod>${lastmod}</lastmod>` : ''}</url>`;

	const lines = [
		...STATIC.map((p) => entry(p, p === '/blog' ? day(posts[0]?.published_at) : null)),
		...posts.flatMap((p) => {
			const lastmod = day(p.updated_at || p.published_at);
			const parts = Math.max(1, p.part_count || 1);
			return Array.from({ length: parts }, (_, i) => entry(partHref(p.slug, i + 1), lastmod));
		}),
		...projects.map((p) => entry(`/projects/${encodeURIComponent(p.slug)}`, day(p.pushed_at)))
	];

	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${lines.join('\n')}
</urlset>
`;
	return new Response(xml, {
		headers: {
			'Content-Type': 'application/xml; charset=utf-8',
			'Cache-Control': 'public, max-age=3600'
		}
	});
};
