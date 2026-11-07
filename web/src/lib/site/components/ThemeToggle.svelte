<script lang="ts">
	import Sun from '@lucide/svelte/icons/sun';
	import Moon from '@lucide/svelte/icons/moon';
	import Monitor from '@lucide/svelte/icons/monitor';
	import { MODES, theme, type Mode } from '#lib/theme.svelte.js';

	let { class: klass = '' }: { class?: string } = $props();
	// Unique radio-group name: the header renders a desktop and a mobile toggle.
	const uid = $props.id();

	const ICONS: Record<Mode, typeof Sun> = { light: Sun, dark: Moon, system: Monitor };
</script>

<!-- Segmented light / dark / system switch (radio group). -->
<fieldset class={['toggle', klass]}>
	<legend class="sr-only">Colour theme</legend>
	{#each MODES as m (m.id)}
		{@const Icon = ICONS[m.id]}
		<label title={m.label}>
			<input
				type="radio"
				name="theme-{uid}"
				value={m.id}
				class="sr-only"
				checked={theme.mode === m.id}
				onchange={() => theme.setMode(m.id)}
			/>
			<Icon class="size-4" aria-hidden="true" />
			<span class="sr-only">{m.label}</span>
		</label>
	{/each}
</fieldset>

<style>
	.toggle {
		display: inline-flex;
		border: 1px solid var(--ink);
		border-radius: 999px;
		padding: 2px;
		background: var(--paper);
	}
	label {
		display: grid;
		place-items: center;
		width: 2.75rem;
		height: 2.75rem;
		border-radius: 999px;
		color: var(--ink-soft);
		transition:
			background-color 160ms ease-out,
			color 160ms ease-out;
	}
	label:hover {
		color: var(--ink);
	}
	label:has(input:checked) {
		background: var(--spot-now);
		color: var(--on-spot);
	}
	label:has(input:focus-visible) {
		outline: 2px solid var(--ink);
		outline-offset: 1px;
	}
</style>
