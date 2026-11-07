// Admin API calls: prefix /api/admin, attach the Clerk session token and
// route 401/403 back to the auth gate.

import { ApiRequestError, apiFull, type ApiOptions, type ApiResult } from '#lib/api/client.js';
import type { Media } from '#lib/api/types.js';
import { auth } from './auth.svelte';

export { ApiRequestError };

type AdminOptions = Omit<ApiOptions, 'token' | 'fetch'>;

export async function adminFull<T>(path: string, opts: AdminOptions = {}): Promise<ApiResult<T>> {
	const token = await auth.getToken();
	try {
		return await apiFull<T>('/api/admin' + path, { ...opts, token });
	} catch (err) {
		if (err instanceof ApiRequestError && (err.status === 401 || err.status === 403)) {
			auth.rejected(err.status);
		}
		throw err;
	}
}

export async function admin<T>(path: string, opts: AdminOptions = {}): Promise<T> {
	return (await adminFull<T>(path, opts)).data;
}

export const get = <T>(path: string) => admin<T>(path);
export const post = <T>(path: string, body?: unknown) => admin<T>(path, { method: 'POST', body });
export const put = <T>(path: string, body: unknown) => admin<T>(path, { method: 'PUT', body });
export const patch = <T>(path: string, body: unknown) => admin<T>(path, { method: 'PATCH', body });
export const del = (path: string) => admin<void>(path, { method: 'DELETE' });

/** Build a query string from defined, non-empty values. */
export function qs(params: Record<string, string | number | null | undefined>): string {
	const p = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v !== null && v !== undefined && v !== '') p.set(k, String(v));
	}
	const s = p.toString();
	return s ? `?${s}` : '';
}

/** Upload one file to the media library. */
export function uploadMedia(file: File): Promise<Media> {
	const fd = new FormData();
	fd.append('file', file);
	return admin<Media>('/media', { method: 'POST', body: fd });
}
