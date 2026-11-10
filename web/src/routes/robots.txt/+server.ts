import type { RequestHandler } from './$types';

// /admin is not disallowed: crawlers must be able to fetch it to see its
// X-Robots-Tag: noindex (hooks.server.ts). A disallowed URL can still be indexed.
export const GET: RequestHandler = ({ url }) => {
	const body = `User-agent: *
Allow: /
Disallow: /api/

Sitemap: ${url.origin}/sitemap.xml
`;
	return new Response(body, {
		headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'public, max-age=86400' }
	});
};
