import type { PageLoad } from './$types';
import type { PostSummary, ProjectDetail } from '#lib/api/types.js';
import { required, safe } from '#lib/site/data.js';

export const load: PageLoad = async ({ fetch, params }) => {
	const project = await required<ProjectDetail>(
		fetch,
		`/api/v1/projects/${encodeURIComponent(params.slug)}`,
		'Project'
	);
	const post = project.post_slug
		? await safe<PostSummary | null>(
				fetch,
				`/api/v1/posts/${encodeURIComponent(project.post_slug)}`,
				null
			)
		: null;
	return { project, post };
};
