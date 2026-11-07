<script lang="ts" module>
	import type { ThreadMessage } from '#lib/api/types.js';

	/** A message as shown: server messages plus optimistic local ones (negative ids). */
	export type ShownMessage = ThreadMessage & {
		request_id?: string;
		local?: boolean;
		thread_id?: number;
	};
</script>

<script lang="ts">
	import {
		Check,
		CircleAlert,
		LoaderCircle,
		Mail,
		Paperclip,
		RotateCw,
		TriangleAlert
	} from '@lucide/svelte';
	import { dateTime, stamp } from '../format';

	let {
		message: m,
		onretry
	}: {
		message: ShownMessage;
		onretry?: (m: ShownMessage) => void;
	} = $props();

	let showFull = $state(false);
	let retrying = $state(false);

	const out = $derived(m.direction === 'outbound');
	const sourceLabel = $derived(
		m.source === 'form' ? 'Contact form' : m.source === 'email' ? 'Email' : 'Admin'
	);
	const body = $derived(showFull && m.full_body ? m.full_body : m.body);

	async function retry() {
		if (!onretry) return;
		retrying = true;
		try {
			await onretry(m);
		} finally {
			retrying = false;
		}
	}
</script>

<li class={['flex flex-col gap-1', out ? 'items-end' : 'items-start']}>
	<div class="flex items-center gap-2 px-1 font-mono text-[0.6875rem] text-ink-soft">
		<span class="tag" style:--tag={out ? 'var(--spot-select)' : 'var(--spot-sky)'}
			>{sourceLabel}</span
		>
		<time datetime={m.created_at} title={dateTime(m.created_at)}>{stamp(m.created_at)}</time>
	</div>

	{#if m.other_sender}
		<p
			class="flex max-w-[min(36rem,90%)] items-start gap-2 rounded-lg bg-spot-butter px-3 py-2 text-xs text-on-spot"
		>
			<TriangleAlert class="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
			<span
				>Sent from <strong class="font-semibold">{m.from_address}</strong>, not the thread's
				address. Check before replying.</span
			>
		</p>
	{/if}

	<div
		class={[
			'max-w-[min(36rem,90%)] rounded-2xl border px-4 py-3 text-[0.9375rem] leading-relaxed whitespace-pre-wrap',
			out
				? 'rounded-br-md border-ink bg-ink text-paper'
				: 'rounded-bl-md border-rule-strong bg-paper-2 text-ink',
			m.status === 'failed' && 'border-danger'
		]}
		style:overflow-wrap="anywhere"
	>
		{body}
	</div>

	<div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-1 text-xs text-ink-soft">
		{#if m.full_body}
			<button
				type="button"
				class="link inline-flex min-h-8 items-center gap-1"
				aria-expanded={showFull}
				onclick={() => (showFull = !showFull)}
			>
				<Mail class="size-3.5" aria-hidden="true" />
				{showFull ? 'Hide quoted text' : 'Show quoted text'}
			</button>
		{/if}
		{#if m.attachments_dropped > 0}
			<span class="inline-flex items-center gap-1">
				<Paperclip class="size-3.5" aria-hidden="true" />
				{m.attachments_dropped}
				{m.attachments_dropped === 1 ? 'attachment' : 'attachments'} dropped (not stored)
			</span>
		{/if}
		{#if out}
			{#if m.status === 'sending'}
				<span class="inline-flex items-center gap-1" role="status">
					<LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" /> Sending…
				</span>
			{:else if m.status === 'sent'}
				<span class="inline-flex items-center gap-1" title={dateTime(m.sent_at)}>
					<Check class="size-3.5" aria-hidden="true" /> Sent
				</span>
			{:else if m.status === 'failed'}
				<span class="text-danger inline-flex items-center gap-1" role="alert">
					<CircleAlert class="size-3.5" aria-hidden="true" />
					Failed{m.error ? `: ${m.error}` : ''}
				</span>
				{#if onretry}
					<button
						type="button"
						class="btn btn-secondary btn-sm"
						onclick={retry}
						disabled={retrying}
					>
						<RotateCw class={['size-3.5', retrying && 'animate-spin']} aria-hidden="true" />
						Retry
					</button>
				{/if}
			{/if}
		{/if}
	</div>
</li>
