import { QueryClient } from '@tanstack/svelte-query';
import { ApiRequestError } from './api';

/** One QueryClient per admin session (created in routes/admin/+layout.svelte). */
export function createAdminQueryClient(): QueryClient {
	return new QueryClient({
		defaultOptions: {
			queries: {
				staleTime: 30_000,
				refetchOnWindowFocus: true,
				refetchOnReconnect: true,
				// 4xx (404, 401, 403, 422…) will not change on retry; network and 5xx might.
				retry: (n, err) => !(err instanceof ApiRequestError && err.status < 500) && n < 2
			}
		}
	});
}
