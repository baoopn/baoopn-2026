// Admin API calls: prefix /api/admin, attach the Clerk session token and
// route 401/403 back to the auth gate.

import { ApiRequestError, apiFull, type ApiOptions, type ApiResult } from '#lib/api/client.js';
import type { ApiError, Media } from '#lib/api/types.js';
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

export type Crop = { x: number; y: number; w: number; h: number };

/** Upload byte progress. */
export type UploadProgressEvent = { loaded: number; total: number };

/** A failed upload request. `status` 0 is a network failure or timeout. */
export class UploadRequestError extends ApiRequestError {
	/** Seconds from a 429/503 Retry-After header. */
	retryAfter: number | null;
	constructor(status: number, err: ApiError, retryAfter: number | null = null) {
		super(status, err);
		this.retryAfter = retryAfter;
	}
	/** Transport-level failures worth retrying automatically. */
	get transient(): boolean {
		return this.status === 0 || this.status === 429 || this.status >= 500;
	}
}

const UPLOAD_TIMEOUT_MS = 5 * 60_000;

/**
 * Upload one file to the media library over XHR (fetch has no upload progress).
 * Raster images are converted to WebP on the server; `crop` is in ORIGINAL image
 * pixels (after EXIF orientation). Resolves on 201 (ready) and on 202
 * (`status: "processing"`: poll {@link getMedia}).
 */
export async function uploadMedia(
	file: File,
	opts: {
		crop?: Crop | null;
		onprogress?: (p: UploadProgressEvent) => void;
		/** Called once every byte is sent: the server is now converting. */
		onsent?: () => void;
		signal?: AbortSignal;
	} = {}
): Promise<Media> {
	const fd = new FormData();
	fd.append('file', file);
	const { crop } = opts;
	if (crop) {
		fd.append('crop_x', String(Math.round(crop.x)));
		fd.append('crop_y', String(Math.round(crop.y)));
		fd.append('crop_w', String(Math.round(crop.w)));
		fd.append('crop_h', String(Math.round(crop.h)));
	}
	const token = await auth.getToken();
	return new Promise<Media>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open('POST', '/api/admin/media');
		xhr.timeout = UPLOAD_TIMEOUT_MS;
		xhr.setRequestHeader('Accept', 'application/json');
		if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
		xhr.upload.onprogress = (e) => {
			if (e.lengthComputable) opts.onprogress?.({ loaded: e.loaded, total: e.total });
		};
		xhr.upload.onload = () => opts.onsent?.();
		xhr.onerror = () =>
			reject(
				new UploadRequestError(0, { code: 'network', message: 'Network error during upload' })
			);
		xhr.ontimeout = () =>
			reject(new UploadRequestError(0, { code: 'timeout', message: 'The upload timed out' }));
		xhr.onabort = () => reject(new DOMException('Upload cancelled', 'AbortError'));
		xhr.onload = () => {
			let json: { data?: Media; error?: ApiError } | null = null;
			try {
				json = JSON.parse(xhr.responseText);
			} catch {
				/* non-JSON body (proxy error page) */
			}
			if (xhr.status >= 200 && xhr.status < 300 && json?.data) return resolve(json.data);
			const ra = Number(xhr.getResponseHeader('Retry-After'));
			const err = new UploadRequestError(
				xhr.status,
				json?.error ?? { code: 'http_' + xhr.status, message: xhr.statusText || 'Upload failed' },
				Number.isFinite(ra) && ra > 0 ? ra : null
			);
			if (xhr.status === 401 || xhr.status === 403) auth.rejected(xhr.status);
			reject(err);
		};
		opts.signal?.addEventListener('abort', () => xhr.abort(), { once: true });
		xhr.send(fd);
	});
}

export const getMedia = (id: number) => get<Media>(`/media/${id}`);

/** Re-queue a failed conversion. Rejects with 410 when the original has expired. */
// The JSON client drops 202 bodies (data: undefined), so the caller polls from the id it has.
export const retryConversion = (id: number) => post<Media | undefined>(`/media/${id}/retry`);
