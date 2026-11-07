import type { RequestHandler } from './$types';
import type { PostSummary, Profile } from '#lib/api/types.js';
import { safe } from '#lib/site/data.js';
import { withDefaults } from '#lib/site/defaults.js';
import { escapeXml } from '#lib/site/format.js';
import { categoryLabel } from '#lib/site/post.js';

export const GET: RequestHandler = async ({ fetch, url }) => {
	const [posts, rawProfile] = await Promise.all([
		safe<PostSummary[]>(fetch, '/api/v1/posts?limit=20', []),
		safe<Partial<Profile> | null>(fetch, '/api/v1/profile', null)
	]);
	const profile = withDefaults(rawProfile);
	const origin = url.origin;
	const rfc822 = (s: string) => {
		const d = new Date(s);
		return Number.isNaN(d.getTime()) ? new Date().toUTCString() : d.toUTCString();
	};

	const items = posts
		.map((p) => {
			const link = `${origin}/blog/${encodeURIComponent(p.slug)}`;
			return `
		<item>
			<title>${escapeXml(p.title)}</title>
			<link>${link}</link>
			<guid isPermaLink="true">${link}</guid>
			<pubDate>${rfc822(p.published_at)}</pubDate>
			<description>${escapeXml(p.excerpt ?? '')}</description>
			<category>${escapeXml(categoryLabel(p.category))}</category>${(p.tags ?? [])
				.map((t) => `\n\t\t\t<category>${escapeXml(t)}</category>`)
				.join('')}
		</item>`;
		})
		.join('');

	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
	<channel>
		<title>${escapeXml(profile.name)} · Blog</title>
		<link>${origin}/blog</link>
		<description>${escapeXml(profile.seo.description || `Writing by ${profile.name}`)}</description>
		<language>en</language>
		<atom:link href="${origin}/rss.xml" rel="self" type="application/rss+xml" />${
			posts[0] ? `\n\t\t<lastBuildDate>${rfc822(posts[0].published_at)}</lastBuildDate>` : ''
		}${items}
	</channel>
</rss>
`;
	return new Response(xml, {
		headers: {
			'Content-Type': 'application/rss+xml; charset=utf-8',
			'Cache-Control': 'public, max-age=600'
		}
	});
};
