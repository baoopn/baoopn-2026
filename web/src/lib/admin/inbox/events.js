// Map admin inbox SSE events onto the TanStack Query cache.
// Plain JS with JSDoc types and no $app imports, so `node --test` can load it.
// Events carry ids and kinds only; the cache refetches the real data.

/**
 * @typedef {object} CacheLike The subset of QueryClient this module uses.
 * @property {(filters: { queryKey: readonly unknown[] }) => unknown} invalidateQueries
 * @property {(filters: { queryKey: readonly unknown[]; type?: 'inactive' }) => unknown} removeQueries
 */

/**
 * @typedef {object} InboxEventHooks
 * @property {(threadId: number) => void} [fresh] Highlight a thread in the list.
 * @property {(threadId: number) => void} [inbound] A visitor message arrived on a thread.
 * @property {(requestId: string, messageId: number) => void} [ownReply] An admin reply was stored.
 */

import { inboxKeys } from './keys.js';

const ALL = inboxKeys.all;
const THREADS = inboxKeys.threadsAll;
const UNREAD = inboxKeys.unread;
const thread = inboxKeys.thread;

/** @param {unknown} v @returns {v is number} */
const isId = (v) => typeof v === 'number' && Number.isSafeInteger(v) && v > 0;

/**
 * Apply one stream event. Unknown events and malformed JSON are ignored.
 * @param {CacheLike} qc
 * @param {string} type
 * @param {string} data Raw JSON from the data: field.
 * @param {InboxEventHooks} [hooks]
 */
export function applyInboxEvent(qc, type, data, hooks = {}) {
	if (type === 'ready') {
		// Fresh connection (first or after a gap): resync everything.
		qc.invalidateQueries({ queryKey: ALL });
		return;
	}
	/** @type {Record<string, unknown>} */
	let d;
	try {
		const parsed = JSON.parse(data);
		if (!parsed || typeof parsed !== 'object') return;
		d = parsed;
	} catch {
		return;
	}
	const id = d.thread_id;
	if (!isId(id)) return;

	switch (type) {
		case 'thread.created':
			qc.invalidateQueries({ queryKey: THREADS });
			qc.invalidateQueries({ queryKey: UNREAD });
			hooks.fresh?.(id);
			return;
		case 'message.created':
			qc.invalidateQueries({ queryKey: THREADS });
			qc.invalidateQueries({ queryKey: UNREAD });
			qc.invalidateQueries({ queryKey: thread(id) });
			if (d.direction === 'inbound') {
				hooks.fresh?.(id);
				hooks.inbound?.(id);
			}
			if (typeof d.request_id === 'string' && d.request_id && isId(d.message_id)) {
				hooks.ownReply?.(d.request_id, d.message_id);
			}
			return;
		case 'message.updated':
			qc.invalidateQueries({ queryKey: thread(id) });
			return;
		case 'thread.updated':
			qc.invalidateQueries({ queryKey: THREADS });
			qc.invalidateQueries({ queryKey: UNREAD });
			qc.invalidateQueries({ queryKey: thread(id) });
			return;
		case 'thread.deleted':
			// Drop cached copies nobody is looking at; an open view refetches,
			// gets 404 and shows its error state instead of a ghost thread.
			qc.removeQueries({ queryKey: thread(id), type: 'inactive' });
			qc.invalidateQueries({ queryKey: thread(id) });
			qc.invalidateQueries({ queryKey: THREADS });
			qc.invalidateQueries({ queryKey: UNREAD });
			return;
		default:
			return;
	}
}
