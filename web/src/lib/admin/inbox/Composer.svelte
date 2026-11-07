<script lang="ts">
	import { Send } from '@lucide/svelte';

	let {
		enabled,
		to,
		onsend
	}: {
		/** False when SMTP is not configured on the API. */
		enabled: boolean;
		to: string;
		onsend: (body: string) => void;
	} = $props();

	const id = $props.id();
	let body = $state('');
	const MAX = 10_000;
	const trimmed = $derived(body.trim());
	const tooLong = $derived(body.length > MAX);
	const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);

	function send() {
		if (!enabled || !trimmed || tooLong) return;
		onsend(trimmed);
		body = '';
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
			e.preventDefault();
			send();
		}
	}
</script>

<form
	class="border-t border-rule bg-paper px-4 py-3 sm:px-6"
	onsubmit={(e) => {
		e.preventDefault();
		send();
	}}
>
	{#if !enabled}
		<p class="mb-2 rounded-lg bg-spot-butter px-3 py-2 text-sm text-on-spot" id="{id}-off">
			Replies are off: outgoing mail (SMTP) is not configured on the API. Set it up, then reload.
		</p>
	{/if}
	<label for={id} class="sr-only">Reply to {to}</label>
	<div class="flex items-end gap-2">
		<textarea
			{id}
			class="input min-h-[4.5rem] flex-1"
			rows="3"
			placeholder={enabled ? `Reply to ${to}…` : 'Replies are disabled'}
			bind:value={body}
			{onkeydown}
			disabled={!enabled}
			aria-invalid={tooLong}
			aria-describedby={enabled ? `${id}-hint` : `${id}-off`}></textarea>
		<button
			type="submit"
			class="btn btn-primary"
			disabled={!enabled || !trimmed || tooLong}
			aria-label="Send reply"
		>
			<Send class="size-4" aria-hidden="true" />
			<span class="hidden sm:inline">Send</span>
		</button>
	</div>
	{#if enabled}
		<p
			id="{id}-hint"
			class={['mt-1.5 font-mono text-xs', tooLong ? 'text-danger' : 'text-ink-soft']}
		>
			{isMac ? '⌘' : 'Ctrl'}+Enter to send · {body.length.toLocaleString()}/{MAX.toLocaleString()}
		</p>
	{/if}
</form>
