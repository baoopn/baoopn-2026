import type { PageLoad } from './$types';
import type { ProjectSummary } from '#lib/api/types.js';
import { safe } from '#lib/site/data.js';

export const load: PageLoad = async ({ fetch }) => {
	return { projects: await safe<ProjectSummary[]>(fetch, '/api/v1/projects', []) };
};
