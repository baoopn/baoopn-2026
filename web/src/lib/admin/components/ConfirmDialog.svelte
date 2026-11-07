<script lang="ts">
	import { LoaderCircle } from '@lucide/svelte';
	import Dialog from './Dialog.svelte';

	let {
		open = $bindable(false),
		title,
		message,
		confirmLabel = 'Confirm',
		danger = false,
		onconfirm
	}: {
		open?: boolean;
		title: string;
		message: string;
		confirmLabel?: string;
		danger?: boolean;
		onconfirm: () => Promise<void> | void;
	} = $props();

	let busy = $state(false);

	async function confirm() {
		busy = true;
		try {
			await onconfirm();
			open = false;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open {title}>
	<p class="text-sm text-ink-soft">{message}</p>
	{#snippet footer()}
		<button type="button" class="btn btn-secondary" onclick={() => (open = false)} disabled={busy}>
			Cancel
		</button>
		<button
			type="button"
			class={['btn', danger ? 'btn-danger' : 'btn-primary']}
			onclick={confirm}
			disabled={busy}
		>
			{#if busy}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{/if}
			{confirmLabel}
		</button>
	{/snippet}
</Dialog>
