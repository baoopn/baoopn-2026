// TanStack Query keys for the admin inbox: the single source of truth for
// queries.ts (fetchers, mutations) and events.js (stream → cache).
// Plain JS with JSDoc types and no $app imports, so `node --test` can load it.

export const inboxKeys = Object.freeze({
	all: /** @type {const} */ (['inbox']),
	threadsAll: /** @type {const} */ (['inbox', 'threads']),
	/**
	 * One cached thread list per filter.
	 * @template F
	 * @param {F} f
	 */
	threads: (f) => /** @type {const} */ (['inbox', 'threads', f]),
	/** @param {number} id */
	thread: (id) => /** @type {const} */ (['inbox', 'thread', id]),
	unread: /** @type {const} */ (['inbox', 'unread'])
});
