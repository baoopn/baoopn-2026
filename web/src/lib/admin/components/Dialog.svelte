<script lang="ts">
	import type { Snippet } from 'svelte';
	import { X } from '@lucide/svelte';

	let {
		open = $bindable(false),
		title,
		description,
		variant = 'modal',
		children,
		footer,
		onclose
	}: {
		open?: boolean;
		title: string;
		description?: string;
		/** `modal`: centred dialog, `drawer`: right-hand side panel. */
		variant?: 'modal' | 'drawer';
		children: Snippet;
		footer?: Snippet;
		onclose?: () => void;
	} = $props();

	const titleId = $props.id();

	/** Keeps the native <dialog> in sync with `open` (showModal gives focus trapping + Esc). */
	function modal(node: HTMLDialogElement) {
		if (open && !node.open) node.showModal();
		else if (!open && node.open) node.close();
	}

	function handleClose() {
		if (open) {
			open = false;
			onclose?.();
		}
	}
</script>

<dialog
	{@attach modal}
	aria-labelledby={titleId}
	class={['dlg', variant === 'drawer' ? 'dlg-drawer' : 'dlg-modal']}
	onclose={handleClose}
	onclick={(e) => {
		if (e.target === e.currentTarget) handleClose();
	}}
>
	{#if open}
		<div class="flex h-full max-h-[inherit] flex-col">
			<header class="flex items-start justify-between gap-4 border-b border-rule px-5 py-4">
				<div class="min-w-0">
					<h2 id={titleId} class="text-lg font-semibold text-ink">{title}</h2>
					{#if description}<p class="mt-0.5 text-sm text-ink-soft">{description}</p>{/if}
				</div>
				<button
					type="button"
					class="btn btn-ghost btn-icon btn-sm"
					aria-label="Close"
					onclick={handleClose}
				>
					<X class="size-4" aria-hidden="true" />
				</button>
			</header>
			<div class="min-h-0 flex-1 overflow-y-auto px-5 py-4">
				{@render children()}
			</div>
			{#if footer}
				<footer class="flex flex-wrap justify-end gap-2 border-t border-rule px-5 py-3">
					{@render footer()}
				</footer>
			{/if}
		</div>
	{/if}
</dialog>

<style>
	.dlg {
		padding: 0;
		border: 1px solid var(--ink);
		background: var(--surface);
		color: var(--text);
		box-shadow: var(--print-shadow);
	}
	.dlg::backdrop {
		background: color-mix(in oklab, var(--ink) 28%, transparent);
	}
	.dlg-modal {
		width: min(32rem, calc(100vw - 2rem));
		max-height: calc(100dvh - 2rem);
		margin: auto;
		border-radius: 1.25rem;
	}
	.dlg-drawer {
		width: min(36rem, 100vw);
		height: 100dvh;
		max-height: 100dvh;
		margin: 0 0 0 auto;
		border-radius: 1.25rem 0 0 1.25rem;
	}
	.dlg[open].dlg-drawer {
		animation: slide-in 180ms ease-out;
	}
	.dlg[open].dlg-modal {
		animation: pop-in 150ms ease-out;
	}
	@keyframes slide-in {
		from {
			transform: translateX(24px);
			opacity: 0;
		}
	}
	@keyframes pop-in {
		from {
			transform: scale(0.97);
			opacity: 0;
		}
	}
</style>
