import type { PageLoad } from './$types';
import type { NowPlaying, PostSummary, ProjectSummary, RecentPlay } from '#lib/api/types.js';
import { safe } from '#lib/site/data.js';

export const load: PageLoad = async ({ fetch }) => {
	const [featured, posts, recent, nowPlaying] = await Promise.all([
		safe<ProjectSummary[]>(fetch, '/api/v1/projects?featured=1', []),
		safe<PostSummary[]>(fetch, '/api/v1/posts?limit=3', []),
		safe<RecentPlay[]>(fetch, '/api/v1/listening/recent?limit=6', []),
		safe<NowPlaying | null>(fetch, '/api/v1/listening/now-playing', null)
	]);
	// Featured first; top up to three with other visible projects.
	let projects = featured.slice(0, 3);
	if (projects.length < 3) {
		const all = await safe<ProjectSummary[]>(fetch, '/api/v1/projects', []);
		const seen = new Set(projects.map((p) => p.slug));
		projects = [...projects, ...all.filter((p) => !seen.has(p.slug))].slice(0, 3);
	}
	return { projects, posts: posts.slice(0, 3), recent, nowPlaying };
};
