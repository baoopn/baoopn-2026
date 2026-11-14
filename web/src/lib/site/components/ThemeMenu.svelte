<script lang="ts">
	import Sun from '@lucide/svelte/icons/sun';
	import Moon from '@lucide/svelte/icons/moon';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Check from '@lucide/svelte/icons/check';
	import { MODES, theme, type Mode } from '#lib/theme.svelte.js';

	const id = $props.id();
	const ICONS: Record<Mode, typeof Sun> = { light: Sun, dark: Moon, system: Monitor };

	let open = $state(false);

	const current = $derived(MODES.find((m) => m.id === theme.mode) ?? MODES[2]);
	const label = $derived(`Colour theme: ${current.label}`);

	function close() {
		const el = document.getElementById(id);
		if (el?.matches(':popover-open')) el.hidePopover();
	}

	function onToggle(e: ToggleEvent & { currentTarget: HTMLDivElement }) {
		open = e.newState === 'open';
		// Put keyboard users on the checked option so arrows work straight away.
		if (open) e.currentTarget.querySelector<HTMLInputElement>('input:checked')?.focus();
	}

	// Tabbing away closes the menu. Focus has already moved, so hidePopover()
	// does not pull it back to the trigger. A null relatedTarget (mousedown on a
	// row label, or outside) is left alone: light dismiss covers outside clicks.
	function onFocusOut(e: FocusEvent & { currentTarget: HTMLDivElement }) {
		const to = e.relatedTarget;
		if (to instanceof Node && !e.currentTarget.contains(to)) close();
	}

	function select(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
		e.preventDefault();
		theme.setMode(e.currentTarget.value as Mode);
		close();
	}

	// Arrow keys check the next radio (change event) and keep the menu open.
	// Enter selects on keydown. Space selects on keyup, so the key release does
	// not land on the trigger and reopen the menu.
	function onKeydown(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
		if (e.key === 'Enter' && !e.repeat) select(e);
		else if (e.key === ' ') e.preventDefault();
	}

	function onKeyup(e: KeyboardEvent & { currentTarget: HTMLInputElement }) {
		if (e.key === ' ') select(e);
	}

	// Arrow keys also fire a synthetic click (detail 0); only a real pointer
	// click closes the menu.
	function onClick(e: MouseEvent) {
		if (e.detail > 0) close();
	}
</script>

<!-- mousedown is cancelled while open so focus stays in the menu; the click then
     toggles it shut instead of focusout closing it and the click reopening it. -->
<button
	type="button"
	class="btn btn-secondary btn-icon trigger"
	popovertarget={id}
	aria-haspopup="dialog"
	aria-expanded={open}
	aria-label={label}
	title={label}
	onmousedown={(e) => open && e.preventDefault()}
>
	<!-- All three render; CSS on html[data-mode] (set before paint) shows one, so
	     the icon is right before hydration. -->
	{#each MODES as m (m.id)}
		{@const Icon = ICONS[m.id]}
		<Icon class="icon-{m.id} size-5" aria-hidden="true" />
	{/each}
</button>

<div {id} popover="auto" class="menu" ontoggle={onToggle} onfocusout={onFocusOut}>
	<fieldset>
		<legend class="sr-only">Colour theme</legend>
		{#each MODES as m (m.id)}
			{@const Icon = ICONS[m.id]}
			<label class="row">
				<input
					type="radio"
					name="theme-menu-{id}"
					value={m.id}
					class="sr-only"
					checked={theme.mode === m.id}
					onchange={() => theme.setMode(m.id)}
					onclick={onClick}
					onkeydown={onKeydown}
					onkeyup={onKeyup}
				/>
				<Icon class="size-4" aria-hidden="true" />
				<span class="grow">{m.label}</span>
				<Check class="check size-4" aria-hidden="true" />
			</label>
		{/each}
	</fieldset>
</div>

<style>
	.trigger :global(svg) {
		display: none;
	}
	:global(html[data-mode='light']) .trigger :global(.icon-light),
	:global(html[data-mode='dark']) .trigger :global(.icon-dark),
	:global(html[data-mode='system']) .trigger :global(.icon-system) {
		display: block;
	}
	.menu {
		position: fixed;
		inset: auto;
		top: calc(var(--header-h) + 0.25rem);
		/* Line up with the header's content edge: max-w-6xl (72rem) + sm:px-6. */
		right: max(1.5rem, calc((100% - 72rem) / 2 + 1.5rem));
		margin: 0;
		width: 11rem;
		max-width: calc(100vw - 2rem);
		padding: 0.375rem;
		background: var(--paper);
		color: var(--ink);
		border: 1px solid var(--ink);
		border-radius: var(--baoopn-radius);
		box-shadow: var(--print-shadow);
	}
	.menu:popover-open {
		animation: menu-in 140ms ease-out;
	}
	@keyframes menu-in {
		from {
			opacity: 0;
			transform: translateY(-4px);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.menu:popover-open {
			animation: none;
		}
	}
	fieldset {
		display: grid;
		gap: 2px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.625rem;
		width: 100%;
		min-height: 2.75rem;
		padding: 0 0.75rem;
		border-radius: calc(var(--baoopn-radius) - 0.375rem);
		font-weight: 500;
		font-size: 0.9375rem;
		cursor: pointer;
		transition:
			background-color 160ms ease-out,
			color 160ms ease-out;
	}
	.row:hover {
		background: var(--paper-2);
	}
	.row:has(input:checked) {
		background: var(--spot-now);
		color: var(--on-spot);
	}
	.row:has(input:focus-visible) {
		outline: 2px solid var(--ink);
		outline-offset: 1px;
	}
	.row :global(.check) {
		visibility: hidden;
	}
	.row:has(input:checked) :global(.check) {
		visibility: visible;
	}
</style>
