<script lang="ts">
	import { live } from './live.svelte';

	const labels = {
		connecting: 'Connecting…',
		live: 'Live',
		reconnecting: 'Reconnecting… (checking every 30 s)',
		offline: 'Offline',
		paused: 'Paused'
	} as const satisfies Record<typeof live.status, string>;
</script>

<span
	role="status"
	aria-live="polite"
	class="inline-flex items-center gap-1.5 align-middle font-mono text-xs font-normal text-ink-soft"
>
	<span
		aria-hidden="true"
		class={[
			'size-2 shrink-0 rounded-full border',
			live.status === 'live'
				? 'border-on-spot bg-spot-mint'
				: live.status === 'offline' || live.status === 'paused'
					? 'border-ink-soft bg-transparent'
					: 'border-on-spot bg-spot-butter'
		]}
	></span>
	{labels[live.status]}
</span>
