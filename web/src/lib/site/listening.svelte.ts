import { createSubscriber } from 'svelte/reactivity';
import { api } from '#lib/api/client.js';
import type { NowPlaying } from '#lib/api/types.js';

const POLL_MS = 15_000;

/**
 * Shared now-playing state (header chip + home section). Polls
 * /listening/now-playing every 15 s, only while something reads `now` and the
 * tab is visible. Can be seeded from SSR data.
 */
class Listening {
	current = $state<NowPlaying | null>(null);
	/** Client clock when `current` arrived, for ticking progress locally. */
	receivedAt = $state(0);
	loaded = $state(false);
	failed = $state(false);

	#subscribe = createSubscriber(() => {
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;
		const tick = async () => {
			if (stopped) return;
			if (document.visibilityState === 'visible') await this.refresh();
			if (!stopped) timer = setTimeout(tick, POLL_MS);
		};
		const onVisibility = () => {
			if (document.visibilityState === 'visible') {
				clearTimeout(timer);
				void tick();
			}
		};
		timer = setTimeout(tick, this.loaded ? POLL_MS : 0);
		document.addEventListener('visibilitychange', onVisibility);
		return () => {
			stopped = true;
			clearTimeout(timer);
			document.removeEventListener('visibilitychange', onVisibility);
		};
	});

	/** Reading this (in an effect/template) keeps the poller running. */
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
