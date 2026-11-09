<script lang="ts">
	import {
		Archive,
		ArchiveRestore,
		ArrowLeft,
		Copy,
		MailOpen,
		ShieldAlert,
		ShieldCheck,
		Trash2
	} from '@lucide/svelte';
	import { tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import type { ThreadStatus } from '#lib/api/types.js';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import MessageBubble, { type ShownMessage } from '#lib/admin/inbox/MessageBubble.svelte';
	import Composer from '#lib/admin/inbox/Composer.svelte';
	import { ApiRequestError } from '#lib/admin/api.js';
	import {
		deleteThreadMutation,
		markReadMutation,
		markUnreadMutation,
		retryMessageMutation,
		sendReplyMutation,
		setStatusMutation,
		threadQuery
	} from '#lib/admin/inbox/queries.js';
	import { live } from '#lib/admin/inbox/live.svelte.js';
	import { dateTime, errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	const qc = useQueryClient();
	const id = $derived(Number(page.params.id));

	const detail = createQuery(() => threadQuery(id));
	const markRead = createMutation(() => markReadMutation(qc));
	const markUnreadM = createMutation(() => markUnreadMutation(qc));
	const setStatusM = createMutation(() => setStatusMutation(qc));
	const deleteM = createMutation(() => deleteThreadMutation(qc));
	const sendM = createMutation(() => sendReplyMutation(qc));
	const retryM = createMutation(() => retryMessageMutation(qc));

	const visible = () => document.visibilityState === 'visible';

	/** Optimistic replies not yet acknowledged by the API. */
	let pending = $state<ShownMessage[]>([]);
	let localSeq = -1;
	/** Ids of messages this tab sent: their arrival scrolls even when scrolled up (never rendered, so a plain Set). */
	const mine = new Set<number>();

	const d = $derived(detail.data && detail.data.thread.id === id ? detail.data : undefined);
	/** The thread is gone (deleted here or in another tab). */
	const gone = $derived(detail.error instanceof ApiRequestError && detail.error.status === 404);

	// Mark read whenever the thread id changes (while the tab is visible).
	$effect(() => {
		const tid = id;
		untrack(() => {
			if (visible()) markRead.mutate(tid);
		});
	});

	// A visitor message on this thread while we're looking at it: it's read.
	$effect(() =>
		live.onInbound((tid) => {
			if (tid === id && visible()) markRead.mutate(tid);
		})
	);

	function onvisibilitychange() {
		if (visible() && d?.thread.unread) markRead.mutate(id);
	}

	/** A pending reply the server already stored (seen via the live stream, then a refetch). */
	function landed(p: ShownMessage): boolean {
		if (!d || !p.request_id) return false;
		const mid = live.ownReplies.get(p.request_id);
		return mid !== undefined && d.messages.some((m) => m.id === mid);
	}

	const messages = $derived<ShownMessage[]>(
		d ? [...d.messages, ...pending.filter((p) => p.thread_id === id && !landed(p))] : []
	);
	const count = $derived(messages.length);

	// ── Scroll: stick to the bottom, or offer a pill when scrolled up ──
	let scroller = $state<HTMLDivElement | null>(null);
	let atBottom = true;
	let showPill = $state(false);
	let lastId = NaN;
	let lastCount = 0;

	function toBottom(el: HTMLDivElement) {
		void tick().then(() => el.scrollTo({ top: el.scrollHeight }));
	}

	function onscroll() {
		const el = scroller;
		if (!el) return;
		atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= 48;
		if (atBottom) showPill = false;
	}

	$effect(() => {
		const n = count;
		const tid = id;
		const el = scroller;
		if (!el || n === 0) return;
		untrack(() => {
			if (tid !== lastId) {
				// First load or another thread: jump to the newest message.
				lastId = tid;
				lastCount = n;
				atBottom = true;
				showPill = false;
				toBottom(el);
				return;
			}
			if (n > lastCount) {
				const newest = messages[n - 1];
				if (atBottom || newest?.local || (newest && mine.has(newest.id))) {
					showPill = false;
					toBottom(el);
				} else {
					showPill = true;
				}
			}
			lastCount = n;
		});
	});

	function jumpToNewest() {
		showPill = false;
		scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
	}

	async function deliver(local: ShownMessage) {
		pending = pending.map((p) => (p.id === local.id ? { ...p, status: 'sending', error: '' } : p));
		try {
			const msg = await sendM.mutateAsync({
				threadId: local.thread_id ?? id,
				body: local.body,
				request_id: local.request_id ?? crypto.randomUUID()
			});
			mine.add(msg.id);
			pending = pending.filter((p) => p.id !== local.id);
			if (msg.status === 'failed') toasts.error('Reply not sent', msg.error || undefined);
		} catch (err) {
			const reason =
				err instanceof ApiRequestError ? err.message : 'Network error, the reply was not sent.';
			pending = pending.map((p) =>
				p.id === local.id ? { ...p, status: 'failed', error: reason } : p
			);
		}
	}

	function send(body: string) {
		const now = new Date().toISOString();
		const local: ShownMessage = {
			id: localSeq--,
			local: true,
			thread_id: id,
			request_id: crypto.randomUUID(),
			direction: 'outbound',
			source: 'admin',
			body,
			full_body: '',
			status: 'sending',
			error: '',
			from_address: '',
			other_sender: false,
			attachments_dropped: 0,
			created_at: now,
			sent_at: null
		};
		pending = [...pending, local];
		void deliver(local);
	}

	async function retry(m: ShownMessage) {
		if (m.local) return deliver(m);
		try {
			const msg = await retryM.mutateAsync({ threadId: id, mid: m.id });
			if (msg.status === 'sent') toasts.success('Reply sent');
			else toasts.error('Still failing', msg.error || undefined);
		} catch (err) {
			toasts.error('Retry failed', errorMessage(err));
		}
	}

	async function setStatus(status: ThreadStatus) {
		try {
			await setStatusM.mutateAsync({ id, status });
			const labels = { open: 'Moved to inbox', archived: 'Archived', spam: 'Marked as spam' };
			toasts.success(labels[status]);
		} catch (err) {
			toasts.error('Could not update the thread', errorMessage(err));
		}
	}

	async function markUnread() {
		try {
			await markUnreadM.mutateAsync(id);
			toasts.info('Marked as unread');
			await goto('/admin/inbox');
		} catch (err) {
			toasts.error('Could not mark unread', errorMessage(err));
		}
	}

	let confirmDelete = $state(false);
	async function remove() {
		try {
			await deleteM.mutateAsync(id);
			toasts.success('Thread deleted');
			await goto('/admin/inbox');
		} catch (err) {
			toasts.error('Could not delete', errorMessage(err));
		}
	}

	async function copyEmail() {
		if (!d) return;
		try {
			await navigator.clipboard.writeText(d.thread.email);
			toasts.success('Email copied', d.thread.email);
		} catch {
			toasts.error('Could not copy', d.thread.email);
		}
	}
</script>

<svelte:document {onvisibilitychange} />

<svelte:head>
	<title>{d ? d.thread.subject || d.thread.name : 'Thread'} · Inbox · baoopn admin</title>
</svelte:head>

<div class="flex h-[calc(100dvh-4rem)] min-h-0 flex-col">
	{#if detail.isError && (!d || gone)}
		<div class="p-6">
			<a href="/admin/inbox" class="btn btn-ghost btn-sm mb-4 lg:hidden">
				<ArrowLeft class="size-4" aria-hidden="true" /> Inbox
			</a>
			<ErrorState
				title="Could not load this thread"
				message={gone ? 'This thread was deleted.' : errorMessage(detail.error)}
				onretry={gone ? undefined : () => detail.refetch()}
			/>
		</div>
	{:else if !d}
		<div class="flex flex-col gap-4 p-6" aria-busy="true">
			<span class="skeleton h-8 w-1/2"></span>
			<span class="skeleton h-20 w-3/5"></span>
			<span class="skeleton ml-auto h-16 w-2/5"></span>
			<span class="sr-only">Loading thread</span>
		</div>
	{:else}
		{@const t = d.thread}
		<header
			class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-rule px-4 py-3 sm:px-6"
		>
			<a href="/admin/inbox" class="btn btn-ghost btn-icon lg:hidden" aria-label="Back to inbox">
				<ArrowLeft class="size-5" aria-hidden="true" />
			</a>
			<div class="min-w-0 flex-1">
				<h2 class="truncate font-display text-xl font-semibold">{t.subject || '(no subject)'}</h2>
				<p class="truncate text-sm text-ink-soft">
					{t.name} · <a class="link" href="mailto:{t.email}">{t.email}</a>
					· started <time datetime={t.created_at}>{dateTime(t.created_at)}</time>
				</p>
			</div>
			<div class="flex flex-wrap items-center gap-1" role="toolbar" aria-label="Thread actions">
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					onclick={copyEmail}
					aria-label="Copy email"
					title="Copy email"
				>
					<Copy class="size-4" aria-hidden="true" />
				</button>
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					onclick={markUnread}
					aria-label="Mark unread"
					title="Mark unread"
				>
					<MailOpen class="size-4" aria-hidden="true" />
				</button>
				{#if t.status === 'archived'}
					<button
						type="button"
						class="btn btn-ghost btn-icon"
						onclick={() => setStatus('open')}
						aria-label="Unarchive"
						title="Unarchive"
					>
						<ArchiveRestore class="size-4" aria-hidden="true" />
					</button>
				{:else}
					<button
						type="button"
						class="btn btn-ghost btn-icon"
						onclick={() => setStatus('archived')}
						aria-label="Archive"
						title="Archive"
					>
						<Archive class="size-4" aria-hidden="true" />
					</button>
				{/if}
				{#if t.status === 'spam'}
					<button
						type="button"
						class="btn btn-ghost btn-icon"
						onclick={() => setStatus('open')}
						aria-label="Not spam"
						title="Not spam"
					>
						<ShieldCheck class="size-4" aria-hidden="true" />
					</button>
				{:else}
					<button
						type="button"
						class="btn btn-ghost btn-icon"
						onclick={() => setStatus('spam')}
						aria-label="Mark as spam"
						title="Mark as spam"
					>
						<ShieldAlert class="size-4" aria-hidden="true" />
					</button>
				{/if}
				<button
					type="button"
					class="btn btn-ghost btn-icon"
					onclick={() => (confirmDelete = true)}
					aria-label="Delete thread"
					title="Delete thread"
				>
					<Trash2 class="size-4" aria-hidden="true" />
				</button>
			</div>
		</header>

		{#if t.status !== 'open'}
			<p class="border-b border-rule bg-paper-2 px-6 py-2 font-mono text-xs text-ink-soft">
				This thread is in <strong class="text-ink">{t.status}</strong>.
			</p>
		{/if}

		<div class="relative min-h-0 flex-1">
			<div bind:this={scroller} {onscroll} class="h-full overflow-y-auto px-4 py-6 sm:px-6">
				<ol class="mx-auto flex max-w-3xl flex-col gap-5" aria-label="Messages" aria-live="polite">
					{#each messages as m (m.id)}
						<MessageBubble message={m} onretry={retry} />
					{/each}
				</ol>
			</div>
			{#if showPill}
				<button
					type="button"
					class="btn btn-secondary btn-sm absolute bottom-3 left-1/2 -translate-x-1/2"
					onclick={jumpToNewest}
				>
					New message <span aria-hidden="true">↓</span>
				</button>
			{/if}
		</div>

		<Composer enabled={d.reply_enabled} to={t.name || t.email} onsend={send} />
	{/if}
</div>

<ConfirmDialog
	bind:open={confirmDelete}
	title="Delete this thread?"
	message="The thread and all its messages are removed permanently. This cannot be undone."
	confirmLabel="Delete thread"
	danger
	onconfirm={remove}
/>
