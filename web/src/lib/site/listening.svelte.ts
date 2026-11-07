import { createSubscriber } from 'svelte/reactivity';
import { api } from '#lib/api/client.js';
import type { NowPlaying } from '#lib/api/types.js';

const STREAM_URL = '/api/v1/listening/now-playing/stream';
const POLL_MS = 15_000;
/** No event this long after opening the stream: fall back to polling. */
const FIRST_EVENT_MS = 15_000;
/** This many stream errors within ERROR_WINDOW_MS: fall back to polling. */
const MAX_ERRORS = 3;
const ERROR_WINDOW_MS = 60_000;
/** While polling as a fallback, try the stream again this often. */
const SSE_RETRY_MS = 5 * 60_000;
/** Hidden this long: close the stream / stop polling until visible again. */
const HIDDEN_PAUSE_MS = 60_000;

/**
 * Shared now-playing state (header chip + home section). Follows the
 * Server-Sent Events stream at /listening/now-playing/stream, one connection
 * per page however many components read `now`, and only while something does.
 * Falls back to polling /listening/now-playing every 15 s when EventSource is
 * missing or the stream misbehaves, and retries the stream every 5 min.
 * Pauses after the tab has been hidden for a minute. Can be seeded from SSR data.
 */
class Listening {
	current = $state<NowPlaying | null>(null);
	/** Client clock when `current` arrived, for ticking progress locally. */
	receivedAt = $state(0);
	loaded = $state(false);
	failed = $state(false);

	#subscribe = createSubscriber(() => {
		let es: EventSource | null = null;
		let errors: number[] = [];
		let firstEventTimer: ReturnType<typeof setTimeout> | undefined;
		let pollTimer: ReturnType<typeof setTimeout> | undefined;
		let retryTimer: ReturnType<typeof setTimeout> | undefined;
		let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
		let polling = false;
		let paused = false;
		const sseSupported = typeof EventSource !== 'undefined';

		const closeStream = () => {
			clearTimeout(firstEventTimer);
			es?.close();
			es = null;
		};

		const stopPolling = () => {
			polling = false;
			clearTimeout(pollTimer);
			clearTimeout(retryTimer);
		};

		const poll = async () => {
			if (!polling) return;
			if (document.visibilityState === 'visible') await this.refresh();
			if (polling) pollTimer = setTimeout(poll, POLL_MS);
		};

		const startPolling = () => {
			closeStream();
			if (polling) return;
			polling = true;
			pollTimer = setTimeout(poll, 0);
			if (sseSupported) {
				retryTimer = setTimeout(() => {
					stopPolling();
					openStream();
				}, SSE_RETRY_MS);
			}
		};

		const openStream = () => {
			closeStream();
			errors = [];
			const source = new EventSource(STREAM_URL);
			es = source;
			firstEventTimer = setTimeout(startPolling, FIRST_EVENT_MS);
			source.addEventListener('now-playing', (e) => {
				clearTimeout(firstEventTimer);
				try {
					this.set(JSON.parse((e as MessageEvent<string>).data) as NowPlaying | null);
				} catch {
					// A malformed event is ignored; the next one replaces it.
				}
			});
			source.addEventListener('error', () => {
				if (es !== source) return;
				const now = Date.now();
				errors = [...errors.filter((t) => now - t < ERROR_WINDOW_MS), now];
				// CLOSED: the browser gave up (e.g. a 503 or a non-stream reply).
				if (source.readyState === EventSource.CLOSED || errors.length >= MAX_ERRORS) {
					startPolling();
				}
			});
		};

		const start = () => {
			paused = false;
			if (sseSupported) openStream();
			else startPolling();
		};

		const pause = () => {
			paused = true;
			closeStream();
			stopPolling();
		};

		const onVisibility = () => {
			clearTimeout(hiddenTimer);
			if (document.visibilityState === 'hidden') {
				hiddenTimer = setTimeout(pause, HIDDEN_PAUSE_MS);
			} else if (paused) {
				start();
			} else if (polling) {
				// Catch up right away instead of waiting for the next tick.
				clearTimeout(pollTimer);
				void poll();
			}
		};

		start();
		document.addEventListener('visibilitychange', onVisibility);
		return () => {
			clearTimeout(hiddenTimer);
			closeStream();
			stopPolling();
			document.removeEventListener('visibilitychange', onVisibility);
		};
	});

	/** Reading this (in an effect/template) keeps the stream (or poller) running. */
	get now(): NowPlaying | null {
		this.#subscribe();
		return this.current;
	}

	seed(np: NowPlaying | null | undefined): void {
		if (np === undefined || this.loaded) return;
		this.set(np);
	}

	set(np: NowPlaying | null): void {
		this.current = np;
		this.receivedAt = Date.now();
		this.loaded = true;
		this.failed = false;
	}

	async refresh(): Promise<void> {
		try {
			this.set(await api<NowPlaying | null>('/api/v1/listening/now-playing'));
		} catch {
			this.failed = true;
			this.loaded = true;
		}
	}
}

export const listening = new Listening();

/** Paused tracks older than this count as "not listening". */
const STALE_MS = 15 * 60_000;

/** The track to show: playing, or paused recently. */
export function activeTrack(np: NowPlaying | null, now: number) {
	if (!np?.track) return null;
	if (np.is_playing) return np.track;
	const at = Date.parse(np.fetched_at);
	return Number.isFinite(at) && now - at > STALE_MS ? null : np.track;
}

/** Progress in ms, interpolated from when the snapshot arrived; clamped to the track. */
export function progressAt(np: NowPlaying | null, receivedAt: number, now: number): number | null {
	if (!np?.track || np.progress_ms == null) return null;
	const elapsed = np.is_playing ? Math.max(0, now - receivedAt) : 0;
	const p = np.progress_ms + elapsed;
	const d = np.track.duration_ms;
	return Math.max(0, d > 0 ? Math.min(p, d) : p);
}
