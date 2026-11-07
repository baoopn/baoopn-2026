<script lang="ts">
	let {
		checked = $bindable(false),
		label,
		context,
		hideLabel = false,
		disabled = false,
		onchange
	}: {
		checked?: boolean;
		label: string;
		/** Extra screen-reader-only text, e.g. the row it belongs to. */
		context?: string;
		hideLabel?: boolean;
		disabled?: boolean;
		onchange?: (checked: boolean) => void;
	} = $props();
</script>

<label class={['switch inline-flex min-h-11 items-center gap-3', disabled && 'opacity-50']}>
	<input
		type="checkbox"
		role="switch"
		class="sr-only"
		bind:checked
		{disabled}
		onchange={() => onchange?.(checked)}
	/>
	<span class="track" aria-hidden="true"><span class="thumb"></span></span>
	<span class={hideLabel ? 'sr-only' : 'text-sm font-medium text-ink'}
		>{label}{#if context}<span class="sr-only">: {context}</span>{/if}</span
	>
</label>

<style>
	.track {
		position: relative;
		display: inline-block;
		flex: none;
		width: 2.75rem;
		height: 1.5rem;
		border-radius: 999px;
		border: 1px solid var(--ink);
		background: var(--paper-3);
		transition: background-color 150ms ease-out;
	}
	.thumb {
		position: absolute;
		top: 2px;
		left: 2px;
		width: 1.125rem;
		height: 1.125rem;
		border-radius: 999px;
		border: 1px solid var(--ink);
		background: var(--paper);
		transition: transform 150ms ease-out;
	}
	input:checked + .track {
		background: var(--spot-mint);
	}
	input:checked + .track .thumb {
		transform: translateX(1.25rem);
	}
	input:focus-visible + .track {
		outline: 2px solid var(--ink);
		outline-offset: 2px;
	}
</style>
