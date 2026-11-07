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
	import { tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { ThreadDetail, ThreadMessage, ThreadStatus, ThreadSummary } from '#lib/api/types.js';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import MessageBubble, { type ShownMessage } from '#lib/admin/inbox/MessageBubble.svelte';
	import Composer from '#lib/admin/inbox/Composer.svelte';
	import { ApiRequestError, del, get, patch, post } from '#lib/admin/api.js';
	import { Resource } from '#lib/admin/resource.svelte.js';
	import { inbox } from '#lib/admin/inbox.svelte.js';
	import { dateTime, errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	const id = $derived(Number(page.params.id));

	const detail = new Resource(() => get<ThreadDetail>(`/threads/${id}`));

	/** Optimistic replies not yet acknowledged by the API. */
	let pending = $state<ShownMessage[]>([]);
	let localSeq = -1;

	// Load + mark read whenever the thread id changes.
	$effect(() => {
		const tid = id;
		void detail.load();
		post(`/threads/${tid}/read`)
			.then(() => inbox.changed())
			.catch(() => {});
	});

	// Poll the open thread every 20 s.
	$effect(() => {
		const timer = setInterval(() => {
			if (document.visibilityState === 'visible') void detail.load(true);
		}, 20_000);
		return () => clearInterval(timer);
	});

	const d = $derived(detail.data && detail.data.thread.id === id ? detail.data : undefined);
	const messages = $derived<ShownMessage[]>(
		d ? [...d.messages, ...pending.filter((p) => p.thread_id === id)] : []
	);
	const count = $derived(messages.length);

	/** Keep the newest message in view (re-runs when the message count changes). */
	function autoscroll(node: HTMLDivElement) {
		if (count === 0) return;
		void tick().then(() => node.scrollTo({ top: node.scrollHeight }));
	}

	function upsert(msg: ThreadMessage) {
		const cur = detail.data;
		if (!cur || cur.thread.id !== id) return;
		const exists = cur.messages.some((x) => x.id === msg.id);
		detail.set({
			...cur,
			messages: exists
				? cur.messages.map((x) => (x.id === msg.id ? msg : x))
				: [...cur.messages, msg]
		});
	}

	async function deliver(local: ShownMessage) {
		pending = pending.map((p) => (p.id === local.id ? { ...p, status: 'sending', error: '' } : p));
		try {
			const msg = await post<ThreadMessage>(`/threads/${local.thread_id}/messages`, {
				body: local.body,
				request_id: local.request_id
			});
			pending = pending.filter((p) => p.id !== local.id);
			upsert(msg);
			inbox.changed();
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
			const msg = await post<ThreadMessage>(`/threads/${id}/messages/${m.id}/retry`);
			upsert(msg);
			if (msg.status === 'sent') toasts.success('Reply sent');
			else toasts.error('Still failing', msg.error || undefined);
		} catch (err) {
			toasts.error('Retry failed', errorMessage(err));
		}
	}

	async function setStatus(status: ThreadStatus) {
		const cur = detail.data;
		try {
			const t = await patch<ThreadSummary>(`/threads/${id}`, { status });
			if (cur) detail.set({ ...cur, thread: t });
			inbox.changed();
			const labels = { open: 'Moved to inbox', archived: 'Archived', spam: 'Marked as spam' };
			toasts.success(labels[status]);
		} catch (err) {
			toasts.error('Could not update the thread', errorMessage(err));
		}
	}

	async function markUnread() {
		try {
			await post(`/threads/${id}/unread`);
			inbox.changed();
			toasts.info('Marked as unread');
			await goto('/admin/inbox');
		} catch (err) {
			toasts.error('Could not mark unread', errorMessage(err));
		}
	}

	let confirmDelete = $state(false);
	async function remove() {
		try {
			await del(`/threads/${id}`);
			inbox.changed();
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

<svelte:head>
	<title>{d ? d.thread.subject || d.thread.name : 'Thread'} · Inbox · baoopn admin</title>
</svelte:head>

<div class="flex h-[calc(100dvh-4rem)] min-h-0 flex-col">
	{#if detail.error && !d}
		<div class="p-6">
			<a href="/admin/inbox" class="btn btn-ghost btn-sm mb-4 lg:hidden">
				<ArrowLeft class="size-4" aria-hidden="true" /> Inbox
			</a>
			<ErrorState
				title="Could not load this thread"
				message={detail.error}
				onretry={() => detail.load()}
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

		<div {@attach autoscroll} class="min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-6">
			<ol class="mx-auto flex max-w-3xl flex-col gap-5" aria-label="Messages" aria-live="polite">
				{#each messages as m (m.id)}
					<MessageBubble message={m} onretry={retry} />
				{/each}
			</ol>
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
