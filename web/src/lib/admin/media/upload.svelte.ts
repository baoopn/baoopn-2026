// One media upload with observable progress: Uploading → Converting → Ready | Failed,
// with automatic transport retries, polling backoff and conversion retries.

import {
	ApiRequestError,
	getMedia,
	retryConversion,
	uploadMedia,
	UploadRequestError,
	type Crop
} from '../api';
import type { Media } from '#lib/api/types.js';
import { bytes, errorMessage } from '../format';

export type UploadStage = 'uploading' | 'converting' | 'ready' | 'failed';

const MAX_ATTEMPTS = 3;
const BACKOFF_MS = [1000, 3000];
const POLL_MS = 1500;
const POLL_MAX_MS = 10_000;
const POLL_WINDOW_MS = 120_000;

/** Every live job, so pages can warn before saving with uploads in flight. */
class UploadRegistry {
	jobs = $state.raw<UploadJob[]>([]);
	pending = $derived(this.jobs.filter((j) => j.inFlight).length);
	add(j: UploadJob) {
		if (!this.jobs.includes(j)) this.jobs = [...this.jobs, j];
	}
	remove(j: UploadJob) {
		this.jobs = this.jobs.filter((x) => x !== j);
	}
	/** "1 image is still converting. Save anyway?" */
	get message(): string {
		const n = this.pending;
		return `${n} image${n === 1 ? ' is' : 's are'} still converting. Save anyway? Images that haven't finished are left out.`;
	}
}
export const uploads = new UploadRegistry();

function sleep(ms: number, signal: AbortSignal): Promise<void> {
	return new Promise((resolve, reject) => {
		const t = setTimeout(resolve, ms);
		signal.addEventListener(
			'abort',
			() => {
				clearTimeout(t);
				reject(new DOMException('Cancelled', 'AbortError'));
			},
			{ once: true }
		);
	});
}

let seq = 0;

type Handlers = {
	/** Ready (again, after a conversion retry: `previous` is the media it replaces). */
	onready: (m: Media, previous: Media | null) => void;
	/** Final failure (after automatic retries). */
	onfailed?: (job: UploadJob) => void;
};

export class UploadJob {
	readonly id = ++seq;
	readonly file: File;
	readonly crop: Crop | null;
	stage = $state<UploadStage>('uploading');
	loaded = $state(0);
	total = $state(0);
	/** Current upload attempt, 1..3. */
	attempt = $state(1);
	/** Waiting to retry the upload after a transport error. */
	retrying = $state(false);
	/** Polling gave up after ~2 min; the server keeps converting in the background. */
	slow = $state(false);
	/** ms spent converting (ticks once a second). */
	elapsed = $state(0);
	media = $state.raw<Media | null>(null);
	/** Which step failed: a re-upload, or a conversion retry, fixes it. */
	failKind = $state<'upload' | 'conversion' | null>(null);
	error = $state('');
	/** Ready, but with a caveat (original saved, not WebP). */
	warning = $state('');
	percent = $derived(this.total ? Math.min(100, Math.round((this.loaded / this.total) * 100)) : 0);
	inFlight = $derived(this.stage === 'uploading' || this.stage === 'converting');

	#h: Handlers;
	#abort: AbortController | null = null;
	#timer: ReturnType<typeof setInterval> | undefined;
	#delivered: Media | null = null;
	#detached = false;

	constructor(file: File, crop: Crop | null, handlers: Handlers) {
		this.file = file;
		this.crop = crop;
		this.total = file.size;
		this.#h = handlers;
	}

	/** Uploads (or re-uploads, for Retry) the kept File, retrying transport errors. */
	async start(): Promise<void> {
		const signal = this.#begin();
		this.stage = 'uploading';
		this.media = null;
		let m: Media | undefined;
		for (let attempt = 1; !m; attempt++) {
			this.attempt = attempt;
			this.loaded = 0;
			try {
				m = await uploadMedia(this.file, {
					crop: this.crop,
					signal,
					onprogress: (p) => {
						this.loaded = p.loaded;
						this.total = p.total;
					},
					onsent: () => this.#converting()
				});
			} catch (err) {
				if (signal.aborted) return;
				this.#stopTimer();
				this.stage = 'uploading';
				if (err instanceof UploadRequestError && err.transient && attempt < MAX_ATTEMPTS) {
					const wait = err.retryAfter
						? Math.min(err.retryAfter * 1000, 30_000)
						: BACKOFF_MS[attempt - 1];
					this.retrying = true;
					this.attempt = attempt + 1;
					try {
						await sleep(wait, signal);
					} catch {
						return;
					}
					this.retrying = false;
					continue;
				}
				return this.#fail('upload', this.#uploadMessage(err));
			}
		}
		this.retrying = false;
		this.#converting();
		await this.#settle(m, signal);
	}

	/** Re-queue the server-side conversion; re-uploads if the original expired (410). */
	async retryConversion(): Promise<void> {
		const prev = this.media;
		if (!prev) return this.start();
		const signal = this.#begin();
		this.#converting(true);
		try {
			const m = await retryConversion(prev.id);
			if (signal.aborted) return;
			// A 202 carries no body through the JSON client: poll from the known id.
			await this.#settle(m ?? { ...prev, status: 'processing' }, signal);
		} catch (err) {
			if (signal.aborted) return;
			if (err instanceof ApiRequestError && err.status === 410) return this.start();
			this.#fail('conversion', errorMessage(err));
		}
	}

	/** Resume polling after the 2-minute window ran out. */
	async checkAgain(): Promise<void> {
		if (!this.media) return;
		const signal = this.#begin();
		this.#converting(true);
		await this.#settle(this.media, signal);
	}

	/** Stop everything (Remove). */
	cancel(): void {
		this.#abort?.abort();
		this.#abort = null;
		this.#stopTimer();
		uploads.remove(this);
	}

	/**
	 * The owner unmounted: stop polling and timers but let an in-progress byte
	 * upload finish so the media row still gets created (it shows up in the list).
	 */
	detach(): void {
		this.#detached = true;
		uploads.remove(this);
		this.#stopTimer();
		if (this.stage !== 'uploading') this.cancel();
	}

	#begin(): AbortSignal {
		this.#abort?.abort();
		this.#abort = new AbortController();
		this.error = '';
		this.warning = '';
		this.failKind = null;
		this.slow = false;
		this.retrying = false;
		if (!this.#detached) uploads.add(this);
		return this.#abort.signal;
	}

	async #settle(m: Media, signal: AbortSignal): Promise<void> {
		this.media = m;
		const deadline = Date.now() + POLL_WINDOW_MS;
		let delay = POLL_MS;
		try {
			while (m.status === 'processing') {
				if (this.#detached) return this.cancel();
				if (Date.now() > deadline) {
					this.#stopTimer();
					this.slow = true;
					return;
				}
				await sleep(delay, signal);
				try {
					m = await getMedia(m.id);
					this.media = m;
					delay = POLL_MS;
				} catch {
					if (signal.aborted) return;
					delay = Math.min(delay * 2, POLL_MAX_MS);
				}
			}
		} catch {
			return; // aborted while waiting
		}
		this.#stopTimer();
		if (this.#detached) return this.cancel();
		if (m.status === 'failed') return this.#fail('conversion', m.error || 'Conversion failed');
		this.stage = 'ready';
		// SVG, PDF and animated images are stored as is (converted: false, no error);
		// only a fallback after a failed conversion carries an error.
		if (!m.converted && m.error) {
			this.warning = `Saved the original (WebP conversion failed): ${m.error}`;
		}
		const previous = this.#delivered;
		this.#delivered = m;
		this.#h.onready(m, previous);
	}

	#fail(kind: 'upload' | 'conversion', message: string) {
		this.#stopTimer();
		this.retrying = false;
		this.failKind = kind;
		this.error = message;
		this.stage = 'failed';
		if (!this.#detached) this.#h.onfailed?.(this);
	}

	#uploadMessage(err: unknown): string {
		const msg = errorMessage(err);
		if (err instanceof ApiRequestError && err.status === 413) {
			return `${msg} This file is ${bytes(this.file.size)}, over the server's upload limit.`;
		}
		return msg;
	}

	#converting(restart = false) {
		if (this.stage === 'converting' && this.#timer && !restart) return;
		this.loaded = this.total;
		this.stage = 'converting';
		this.#stopTimer();
		const t0 = Date.now();
		this.elapsed = 0;
		this.#timer = setInterval(() => (this.elapsed = Date.now() - t0), 1000);
	}

	#stopTimer() {
		clearInterval(this.#timer);
		this.#timer = undefined;
	}
}
