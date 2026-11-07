import type { ApiError, PageMeta } from './types';

/** Error thrown for non-2xx API responses. */
export class ApiRequestError extends Error {
	status: number;
	code: string;
	constructor(status: number, err: ApiError) {
		super(err.message);
		this.status = status;
		this.code = err.code;
	}
}

export interface ApiOptions extends Omit<RequestInit, 'body'> {
	/** SvelteKit's `fetch` from a load function (SSR). Defaults to global fetch. */
	fetch?: typeof fetch;
	/** JSON-encoded unless it is FormData. */
	body?: unknown;
	/** Bearer token (admin API). */
	token?: string | null;
}

export interface ApiResult<T> {
	data: T;
	meta?: PageMeta;
}

/** Calls the same-origin API and unwraps the `{ data, meta }` envelope. */
export async function apiFull<T>(path: string, opts: ApiOptions = {}): Promise<ApiResult<T>> {
	const { fetch: f = fetch, body, token, headers, ...init } = opts;
	const h = new Headers(headers);
	h.set('Accept', 'application/json');
	let payload: BodyInit | undefined;
	if (body instanceof FormData) {
		payload = body;
	} else if (body !== undefined) {
		h.set('Content-Type', 'application/json');
		payload = JSON.stringify(body);
	}
	if (token) h.set('Authorization', `Bearer ${token}`);
	const res = await f(path, { ...init, headers: h, body: payload });
	if (res.status === 204 || res.status === 202) return { data: undefined as T };
	const json = await res.json().catch(() => null);
	if (!res.ok) {
		throw new ApiRequestError(
			res.status,
			json?.error ?? { code: 'http_' + res.status, message: res.statusText || 'Request failed' }
		);
	}
	return { data: json?.data as T, meta: json?.meta };
}

export async function api<T>(path: string, opts: ApiOptions = {}): Promise<T> {
	return (await apiFull<T>(path, opts)).data;
}
