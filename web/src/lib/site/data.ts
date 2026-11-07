import { error } from '@sveltejs/kit';
import { api, apiFull, ApiRequestError, type ApiResult } from '#lib/api/client.js';

type Fetch = typeof fetch;

/** GET that never throws: returns `fallback` when the API is down or errors. */
export async function safe<T>(f: Fetch, path: string, fallback: T): Promise<T> {
	try {
		return (await api<T>(path, { fetch: f })) ?? fallback;
	} catch {
		return fallback;
	}
}

export async function safeFull<T>(f: Fetch, path: string, fallback: T): Promise<ApiResult<T>> {
	try {
		const r = await apiFull<T>(path, { fetch: f });
		return { data: r.data ?? fallback, meta: r.meta };
	} catch {
		return { data: fallback };
	}
}

/** GET for a detail page: 404 becomes the 404 page, anything else a 503 page. */
export async function required<T>(f: Fetch, path: string, what: string): Promise<T> {
	try {
		return await api<T>(path, { fetch: f });
	} catch (e) {
		if (e instanceof ApiRequestError && e.status === 404) error(404, `${what} not found`);
		error(503, `This ${what.toLowerCase()} couldn't be loaded right now. Please try again shortly.`);
	}
}
