// Live inbox: one fetch-based SSE connection to GET /api/admin/inbox/events
// (Authorization header, never a token in the URL). Events only say *what*
// changed; applyInboxEvent invalidates the matching TanStack queries.
// While the stream is down the queries fall back to interval polling
// (see queries.ts: they read `live.status`).

import type { QueryClient } from '@tanstack/svelte-query';
import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { auth } from '../auth.svelte';
import { applyInboxEvent } from './events.js';
import { createSSEParser } from './sse.js';

export type LiveStatus = 'connecting' | 'live' | 'reconnecting' | 'offline';

const URL_PATH = '/api/admin/inbox/events';
const FRESH_MS = 8_000;
const IDLE_MS = 60_000; // server sends `: keep-alive` every 25 s
const RECONNECTING_AFTER_MS = 2_000;
const HIDDEN_PAUSE_MS = 60_000;
const BACKOFF_MIN_MS = 1_000;
const BACKOFF_MAX_MS = 30_000;
const DEFAULT_RETRY_AFTER_S = 10;
const OWN_REPLIES_MAX = 50;

type Timer = ReturnType<typeof setTimeout>;

class InboxLive {
	status = $state<LiveStatus>('connecting');
	/** Thread ids that just arrived or changed; each expires after 8 s. */
	fresh = new SvelteSet<number>();
	/** request_id → message_id of admin replies the server has stored. */
	ownReplies = new SvelteMap<string, number>();

	#inbound = new Set<(threadId: number) => void>();
	#freshTimers = new Map<number, Timer>();

	#qc: QueryClient | null = null;
	#gen = 0; // bumps on stop/pause: stale async work checks it and bails
	#conn: AbortController | null = null;
	#attempt = 0;
	#ready = false; // the current connection has received `ready`
	#paused = false;
	#retryTimer: Timer | null = null;
	#reconnectingTimer: Timer | null = null;
	#idleTimer: Timer | null = null;
	#hiddenTimer: Timer | null = null;

	/** Call `fn` when a visitor message lands on a thread. Returns an unregister function. */
	onInbound(fn: (threadId: number) => void): () => void {
		this.#inbound.add(fn);
		return () => this.#inbound.delete(fn);
	}

	/** Open the stream and keep it open; returns a stop function. */
	start(qc: QueryClient): () => void {
		this.#stop();
		this.#qc = qc;
		this.#attempt = 0;
		this.#paused = false;
		window.addEventListener('online', this.#online);
		window.addEventListener('offline', this.#offline);
		document.addEventListener('visibilitychange', this.#visibility);
		this.status = 'connecting';
		this.#connect();
		return () => this.#stop();
	}

	#stop(): void {
		this.#gen++;
		this.#conn?.abort();
		this.#conn = null;
		this.#qc = null;
		this.#clearTimers();
		clearTimeout(this.#hiddenTimer ?? undefined);
		this.#hiddenTimer = null;
		for (const t of this.#freshTimers.values()) clearTimeout(t);
		this.#freshTimers.clear();
		this.fresh.clear();
		this.ownReplies.clear();
		if (typeof window !== 'undefined') {
			window.removeEventListener('online', this.#online);
			window.removeEventListener('offline', this.#offline);
			document.removeEventListener('visibilitychange', this.#visibility);
		}
		this.status = 'connecting';
	}

	#clearTimers(): void {
		for (const t of [this.#retryTimer, this.#reconnectingTimer, this.#idleTimer]) {
			if (t) clearTimeout(t);
		}
		this.#retryTimer = this.#reconnectingTimer = this.#idleTimer = null;
	}

	/** Drop the current connection and any pending retry (keeps the 2 s status timer). */
	#disconnect(): void {
		this.#gen++;
		this.#ready = false;
		this.#conn?.abort();
		this.#conn = null;
		for (const t of [this.#retryTimer, this.#idleTimer]) if (t) clearTimeout(t);
		this.#retryTimer = this.#idleTimer = null;
	}

	#online = () => {
		if (!this.#qc || this.#paused) return;
		this.#disconnect();
		this.#attempt = 0;
		this.status = 'connecting';
		this.#connect();
	};

	#offline = () => {
		if (!this.#qc) return;
		this.#disconnect();
		this.status = 'offline';
		if (this.#reconnectingTimer) clearTimeout(this.#reconnectingTimer);
		this.#reconnectingTimer = null;
	};

	#visibility = () => {
		if (!this.#qc) return;
		if (document.visibilityState === 'hidden') {
			this.#hiddenTimer ??= setTimeout(() => {
				this.#hiddenTimer = null;
				this.#paused = true;
				this.#disconnect();
				if (this.#reconnectingTimer) clearTimeout(this.#reconnectingTimer);
				this.#reconnectingTimer = null;
				if (this.status !== 'offline') this.status = 'connecting';
			}, HIDDEN_PAUSE_MS);
			return;
		}
		clearTimeout(this.#hiddenTimer ?? undefined);
		this.#hiddenTimer = null;
		if (this.#paused) {
			this.#paused = false;
			this.#attempt = 0;
			this.#connect();
		}
	};

	/** Flip to "reconnecting" if we are still not live 2 s from now. */
	#armReconnecting(): void {
		if (this.#reconnectingTimer) return;
		this.#reconnectingTimer = setTimeout(() => {
			this.#reconnectingTimer = null;
			if (!this.#ready && this.status !== 'offline') this.status = 'reconnecting';
		}, RECONNECTING_AFTER_MS);
	}

	#schedule(delayMs: number): void {
		if (this.#retryTimer) clearTimeout(this.#retryTimer);
		const gen = this.#gen;
		this.#retryTimer = setTimeout(() => {
			this.#retryTimer = null;
			if (gen === this.#gen) this.#connect();
		}, delayMs);
	}

	#backoff(): number {
		const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** this.#attempt);
		this.#attempt++;
		return Math.round(base * (0.5 + Math.random())); // ±50 % jitter
	}

	#resetIdle(conn: AbortController): void {
		if (this.#idleTimer) clearTimeout(this.#idleTimer);
		this.#idleTimer = setTimeout(() => conn.abort(), IDLE_MS);
	}

	#markFresh(id: number): void {
		const old = this.#freshTimers.get(id);
		if (old) clearTimeout(old);
		this.fresh.add(id);
		this.#freshTimers.set(
			id,
			setTimeout(() => {
				this.#freshTimers.delete(id);
				this.fresh.delete(id);
			}, FRESH_MS)
		);
	}

	#ownReply(requestId: string, messageId: number): void {
		this.ownReplies.set(requestId, messageId);
		if (this.ownReplies.size > OWN_REPLIES_MAX) {
			const oldest = this.ownReplies.keys().next().value;
			if (oldest !== undefined) this.ownReplies.delete(oldest);
		}
	}

	async #connect(): Promise<void> {
		const qc = this.#qc;
		if (!qc || this.#paused) return;
		if (!navigator.onLine) {
			this.status = 'offline';
			return; // the 'online' listener reconnects
		}
		this.#disconnect();
		const gen = this.#gen;
		const conn = new AbortController();
		this.#conn = conn;
		this.#armReconnecting();

		let delay: number | null = null;
		try {
			const token = await auth.getToken();
			if (gen !== this.#gen) return;
			if (!token) {
				// No session token right now (Clerk refreshing): try again shortly.
				delay = this.#backoff();
				return;
			}
			const res = await fetch(URL_PATH, {
				headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
				cache: 'no-store',
				signal: conn.signal
			});
			if (gen !== this.#gen) return;
			if (res.status === 401 || res.status === 403) {
				auth.rejected(res.status); // the layout stops us when status leaves signed-in
				this.#disconnect();
				return;
			}
			if (res.status === 429 || res.status === 503) {
				const ra = Number(res.headers.get('Retry-After'));
				const s = Number.isFinite(ra) && ra > 0 ? ra : DEFAULT_RETRY_AFTER_S;
				delay = Math.max(s * 1000, this.#backoff());
				return;
			}
			if (!res.ok || !res.body) {
				delay = this.#backoff();
				return;
			}

			const hooks = {
				fresh: (id: number) => this.#markFresh(id),
				inbound: (id: number) => {
					for (const fn of this.#inbound) fn(id);
				},
				ownReply: (rid: string, mid: number) => this.#ownReply(rid, mid)
			};
			const parser = createSSEParser((type, data) => {
				if (gen !== this.#gen) return;
				if (type === 'ready') {
					this.#ready = true;
					this.#attempt = 0;
					if (this.#reconnectingTimer) clearTimeout(this.#reconnectingTimer);
					this.#reconnectingTimer = null;
					this.status = 'live';
				}
				applyInboxEvent(qc, type, data, hooks);
			});
			const reader = res.body.getReader();
			const decoder = new TextDecoder();
			this.#resetIdle(conn);
			try {
				for (;;) {
					const { done, value } = await reader.read();
					if (done || gen !== this.#gen) break;
					this.#resetIdle(conn);
					parser.push(decoder.decode(value, { stream: true }));
				}
			} finally {
				parser.end();
				reader.cancel().catch(() => {});
			}
			delay = this.#backoff();
		} catch {
			// Network error, idle watchdog abort, or the server went away.
			if (gen === this.#gen) delay = this.#backoff();
		} finally {
			if (gen === this.#gen) {
				if (this.#idleTimer) clearTimeout(this.#idleTimer);
				this.#idleTimer = null;
				this.#conn = null;
				this.#ready = false;
				if (delay !== null) {
					this.#armReconnecting();
					this.#schedule(delay);
				}
			}
		}
	}
}

export const live = new InboxLive();
