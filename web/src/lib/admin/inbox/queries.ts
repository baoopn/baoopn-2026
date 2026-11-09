// Inbox query keys, fetchers and mutation option factories (TanStack Query).
// Queries poll only while the live stream is down: reading `live.status`
// inside a createQuery thunk makes `refetchInterval` reactive.

import {
	keepPreviousData,
	mutationOptions,
	queryOptions,
	type QueryClient
} from '@tanstack/svelte-query';
import type { ThreadDetail, ThreadMessage, ThreadStatus, ThreadSummary } from '#lib/api/types.js';
import { del, get, patch, post, qs } from '../api';
import { live } from './live.svelte';

export type ThreadFilter = { status: ThreadStatus; q: string };

export const inboxKeys = {
	all: ['inbox'] as const,
	threadsAll: ['inbox', 'threads'] as const,
	threads: (f: ThreadFilter) => ['inbox', 'threads', f] as const,
	thread: (id: number) => ['inbox', 'thread', id] as const,
	unread: ['inbox', 'unread'] as const
};

const poll = (ms: number) => (live.status === 'live' ? false : ms);

export function unreadQuery() {
	return queryOptions({
		queryKey: inboxKeys.unread,
		queryFn: () => get<{ count: number }>('/threads/unread-count').then((r) => r.count),
		refetchInterval: poll(60_000)
	});
}

export function threadsQuery(f: ThreadFilter) {
	return queryOptions({
		queryKey: inboxKeys.threads(f),
		queryFn: () => get<ThreadSummary[]>('/threads' + qs(f)),
		placeholderData: keepPreviousData,
		refetchInterval: poll(30_000)
	});
}

export function threadQuery(id: number) {
	return queryOptions({
		queryKey: inboxKeys.thread(id),
		queryFn: () => get<ThreadDetail>(`/threads/${id}`),
		refetchInterval: poll(20_000)
	});
}

// ── Cache helpers ───────────────────────────────────────────────────────────

type ListSnapshot = [readonly unknown[], ThreadSummary[] | undefined][];

function snapshotLists(qc: QueryClient): ListSnapshot {
	return qc.getQueriesData<ThreadSummary[]>({ queryKey: inboxKeys.threadsAll });
}

function restoreLists(qc: QueryClient, snap: ListSnapshot | undefined): void {
	for (const [key, data] of snap ?? []) qc.setQueryData(key, data);
}

/** Insert or replace a message in the cached thread detail. */
export function upsertMessage(qc: QueryClient, threadId: number, msg: ThreadMessage): void {
	qc.setQueryData<ThreadDetail>(inboxKeys.thread(threadId), (cur) => {
		if (!cur) return cur;
		const exists = cur.messages.some((m) => m.id === msg.id);
		return {
			...cur,
			messages: exists
				? cur.messages.map((m) => (m.id === msg.id ? msg : m))
				: [...cur.messages, msg]
		};
	});
}

function invalidate(qc: QueryClient, ...keys: (readonly unknown[])[]): Promise<void[]> {
	return Promise.all(keys.map((queryKey) => qc.invalidateQueries({ queryKey })));
}

/** Find a thread's summary in the detail cache or any cached list. */
function findSummary(qc: QueryClient, id: number): ThreadSummary | undefined {
	const d = qc.getQueryData<ThreadDetail>(inboxKeys.thread(id));
	if (d) return d.thread;
	for (const [, list] of snapshotLists(qc)) {
		const t = list?.find((x) => x.id === id);
		if (t) return t;
	}
	return undefined;
}

// ── Mutations ───────────────────────────────────────────────────────────────

export function markReadMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (id: number) => post<void>(`/threads/${id}/read`),
		onMutate: async (id) => {
			await qc.cancelQueries({ queryKey: inboxKeys.threadsAll });
			const lists = snapshotLists(qc);
			const unread = qc.getQueryData<number>(inboxKeys.unread);
			const detail = qc.getQueryData<ThreadDetail>(inboxKeys.thread(id));
			const t = findSummary(qc, id);
			qc.setQueriesData<ThreadSummary[]>({ queryKey: inboxKeys.threadsAll }, (list) =>
				list?.map((x) => (x.id === id && x.unread ? { ...x, unread: false } : x))
			);
			if (detail?.thread.unread) {
				qc.setQueryData<ThreadDetail>(inboxKeys.thread(id), {
					...detail,
					thread: { ...detail.thread, unread: false }
				});
			}
			if (t?.unread && t.status === 'open' && typeof unread === 'number' && unread > 0) {
				qc.setQueryData<number>(inboxKeys.unread, unread - 1);
			}
			return { lists, unread, detail };
		},
		onError: (_err, id, ctx) => {
			if (!ctx) return;
			restoreLists(qc, ctx.lists);
			qc.setQueryData(inboxKeys.unread, ctx.unread);
			if (ctx.detail) qc.setQueryData(inboxKeys.thread(id), ctx.detail);
		},
		onSettled: () => invalidate(qc, inboxKeys.threadsAll, inboxKeys.unread)
	});
}

export function markUnreadMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (id: number) => post<void>(`/threads/${id}/unread`),
		onSettled: (_d, _e, id) =>
			invalidate(qc, inboxKeys.threadsAll, inboxKeys.unread, inboxKeys.thread(id))
	});
}

export function setStatusMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: ({ id, status }: { id: number; status: ThreadStatus }) =>
			patch<ThreadSummary>(`/threads/${id}`, { status }),
		onMutate: async ({ id, status }) => {
			await qc.cancelQueries({ queryKey: inboxKeys.threadsAll });
			const lists = snapshotLists(qc);
			const detail = qc.getQueryData<ThreadDetail>(inboxKeys.thread(id));
			for (const [key, list] of lists) {
				const f = key[2] as ThreadFilter | undefined;
				if (list && f && f.status !== status) {
					qc.setQueryData<ThreadSummary[]>(
						key,
						list.filter((x) => x.id !== id)
					);
				}
			}
			if (detail) {
				qc.setQueryData<ThreadDetail>(inboxKeys.thread(id), {
					...detail,
					thread: { ...detail.thread, status }
				});
			}
			return { lists, detail };
		},
		onSuccess: (t, { id }) => {
			qc.setQueryData<ThreadDetail>(inboxKeys.thread(id), (cur) =>
				cur ? { ...cur, thread: t } : cur
			);
		},
		onError: (_err, { id }, ctx) => {
			if (!ctx) return;
			restoreLists(qc, ctx.lists);
			if (ctx.detail) qc.setQueryData(inboxKeys.thread(id), ctx.detail);
		},
		onSettled: () => invalidate(qc, inboxKeys.threadsAll, inboxKeys.unread)
	});
}

export function deleteThreadMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: (id: number) => del(`/threads/${id}`),
		onMutate: async (id) => {
			await qc.cancelQueries({ queryKey: inboxKeys.threadsAll });
			const lists = snapshotLists(qc);
			qc.setQueriesData<ThreadSummary[]>({ queryKey: inboxKeys.threadsAll }, (list) =>
				list?.filter((x) => x.id !== id)
			);
			return { lists };
		},
		onSuccess: (_d, id) => qc.removeQueries({ queryKey: inboxKeys.thread(id) }),
		onError: (_err, _id, ctx) => restoreLists(qc, ctx?.lists),
		onSettled: () => invalidate(qc, inboxKeys.threadsAll, inboxKeys.unread)
	});
}

export type ReplyInput = { threadId: number; body: string; request_id: string };

export function sendReplyMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: ({ threadId, body, request_id }: ReplyInput) =>
			post<ThreadMessage>(`/threads/${threadId}/messages`, { body, request_id }),
		onSuccess: (msg, { threadId }) => upsertMessage(qc, threadId, msg),
		onSettled: () => invalidate(qc, inboxKeys.threadsAll)
	});
}

export function retryMessageMutation(qc: QueryClient) {
	return mutationOptions({
		mutationFn: ({ threadId, mid }: { threadId: number; mid: number }) =>
			post<ThreadMessage>(`/threads/${threadId}/messages/${mid}/retry`),
		onSuccess: (msg, { threadId }) => upsertMessage(qc, threadId, msg),
		onSettled: () => invalidate(qc, inboxKeys.threadsAll)
	});
}
