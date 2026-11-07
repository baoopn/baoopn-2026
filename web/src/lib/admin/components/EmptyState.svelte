<script lang="ts">
	import type { Component, Snippet } from 'svelte';
	import { Inbox } from '@lucide/svelte';

	let {
		title,
		message,
		icon: Icon = Inbox,
		children
	}: { title: string; message?: string; icon?: Component; children?: Snippet } = $props();
</script>

<div class="flex flex-col items-start gap-3 px-6 py-12 sm:items-center sm:text-center">
	<Icon class="size-7 text-ink-soft" aria-hidden="true" />
	<div>
		<p class="font-display text-xl font-semibold text-ink">{title}</p>
		{#if message}<p class="mt-1 max-w-md text-sm text-ink-soft">{message}</p>{/if}
	</div>
	{@render children?.()}
</div>
