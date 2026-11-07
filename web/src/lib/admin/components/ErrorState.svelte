<script lang="ts">
	import { CircleAlert, RefreshCw } from '@lucide/svelte';

	let {
		title = 'Could not load this',
		message,
		onretry
	}: { title?: string; message?: string | null; onretry?: () => void } = $props();
</script>

<div role="alert" class="flex flex-col items-start gap-3 px-6 py-10 sm:items-center sm:text-center">
	<span class="inline-flex items-center gap-2 rounded-full bg-spot-rose px-3 py-1 text-on-spot">
		<CircleAlert class="size-4" aria-hidden="true" />
		<span class="eyebrow text-on-spot">Error</span>
	</span>
	<div>
		<p class="font-display text-xl font-semibold text-ink">{title}</p>
		{#if message}<p class="mt-1 max-w-md font-mono text-sm break-words text-ink-soft">
				{message}
			</p>{/if}
	</div>
	{#if onretry}
		<button type="button" class="btn btn-secondary btn-sm" onclick={onretry}>
			<RefreshCw class="size-4" aria-hidden="true" /> Try again
		</button>
	{/if}
</div>
