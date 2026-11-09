<script lang="ts">
	import { Archive, Inbox, ShieldAlert } from '@lucide/svelte';
	import { page } from '$app/state';
	import type { ThreadStatus, ThreadSummary } from '#lib/api/types.js';
	import SearchInput from '#lib/admin/components/SearchInput.svelte';
	import Segmented from '#lib/admin/components/Segmented.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import EmptyState from '#lib/admin/components/EmptyState.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { errorMessage } from '#lib/admin/format.js';
	import { threadsQuery, unreadQuery } from '#lib/admin/inbox/queries.js';
	import { live } from '#lib/admin/inbox/live.svelte.js';
	import LiveStatus from '#lib/admin/inbox/LiveStatus.svelte';

	let { children } = $props();

	let status = $state<ThreadStatus>('open');
	let q = $state('');

	const threads = createQuery(() => threadsQuery({ status, q }));
	const unread = createQuery(() => unreadQuery());
	const unreadCount = $derived(unread.data ?? 0);
	const list = $derived<ThreadSummary[] | undefined>(threads.data);

	const selectedId = $derived(page.params.id ? Number(page.params.id) : null);

	const options: { value: ThreadStatus; label: string; icon: typeof Inbox }[] = [
		{ value: 'open', label: 'Open', icon: Inbox },
		{ value: 'archived', label: 'Archived', icon: Archive },
		{ value: 'spam', label: 'Spam', icon: ShieldAlert }
	];
</script>

<div
	class="-mx-4 -my-6 grid sm:-mx-6 lg:-mx-8 lg:h-[calc(100dvh-4rem)] lg:grid-cols-[22rem_1fr] xl:grid-cols-[26rem_1fr]"
>
	<section
		aria-label="Threads"
		class={[
			'min-h-0 flex-col border-rule lg:flex lg:border-r',
			selectedId === null ? 'flex' : 'hidden'
		]}
	>
		<div class="flex flex-col gap-3 border-b border-rule px-4 pt-6 pb-4 sm:px-6 lg:px-5">
			<h1 class="font-display text-3xl font-semibold">
				Inbox
				{#if unreadCount > 0}<span class="font-mono text-base text-ink-soft"
						>· {unreadCount} unread</span
					>{/if}
				<LiveStatus />
			</h1>
			<Segmented {options} bind:value={status} label="Thread status" size="sm" />
			<SearchInput
				class="w-full"
				label="Search threads"
				placeholder="Search name, email, text…"
				onsearch={(v) => (q = v)}
			/>
		</div>

		<div class="min-h-0 flex-1 overflow-y-auto" aria-busy={threads.isFetching}>
			{#if threads.isError && !list}
				<ErrorState message={errorMessage(threads.error)} onretry={() => threads.refetch()} />
			{:else if threads.isPending || !list}
				<ul aria-hidden="true">
					{#each [1, 2, 3, 4, 5] as i (i)}
						<li class="flex flex-col gap-2 border-b border-rule px-5 py-4">
							<span class="skeleton h-4 w-1/2"></span>
							<span class="skeleton h-3 w-4/5"></span>
						</li>
					{/each}
				</ul>
			{:else if list.length === 0}
				<EmptyState
					title={q ? 'No matches' : status === 'open' ? 'Inbox zero' : `No ${status} threads`}
					message={q
						? 'Try a different search.'
						: 'Contact form messages and email replies land here.'}
				/>
			{:else}
				<ul>
					{#each list as t (t.id)}
						{@const active = t.id === selectedId}
						{@const fresh = live.fresh.has(t.id)}
						<li class={['border-b border-rule', fresh && 'inbox-fresh']}>
							<a
								href="/admin/inbox/{t.id}"
								aria-current={active ? 'page' : undefined}
								class={[
									'flex gap-3 px-4 py-3.5 sm:px-6 lg:px-5',
									active ? 'bg-spot-select text-on-spot' : 'hover:bg-paper-3'
								]}
							>
								<span
									class={[
										'mt-1.5 size-2.5 shrink-0 rounded-full border',
										t.unread ? 'border-on-spot bg-spot-now' : 'border-transparent'
									]}
									aria-hidden="true"
								></span>
								<span class="min-w-0 flex-1">
									<span class="flex items-baseline justify-between gap-2">
										<span class={['truncate', t.unread ? 'font-semibold' : 'font-medium']}>
											{t.name || t.email}
										</span>
										<RelTime
											value={t.last_message_at}
											class={[
												'shrink-0 font-mono text-xs',
												active ? 'text-on-spot' : 'text-ink-soft'
											].join(' ')}
										/>
									</span>
									<span class={['block truncate text-sm', t.unread && 'font-medium']}
										>{t.subject || '(no subject)'}</span
									>
									<span
										class={['block truncate text-sm', active ? 'text-on-spot' : 'text-ink-soft']}
									>
										{t.last_direction === 'outbound' ? 'You: ' : ''}{t.last_preview}
									</span>
								</span>
								{#if fresh}<span class="sr-only">New.</span>{/if}
								{#if t.unread}<span class="sr-only">Unread.</span>{/if}
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</section>

	<section
		aria-label="Conversation"
		class={['min-h-0 min-w-0 lg:block', selectedId === null ? 'hidden' : 'block']}
	>
		{@render children()}
	</section>
</div>
