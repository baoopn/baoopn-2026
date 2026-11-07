<script lang="ts">
	import { ImageUp, LoaderCircle, X } from '@lucide/svelte';
	import { uploadMedia } from '../api';
	import { errorMessage } from '../format';
	import { toasts } from '../toast.svelte';

	let {
		value = $bindable(''),
		label,
		id,
		accept = 'image/*',
		image = true,
		hint
	}: {
		value?: string;
		label: string;
		id: string;
		accept?: string;
		/** Show an image thumbnail. */
		image?: boolean;
		hint?: string;
	} = $props();

	let busy = $state(false);
	let broken = $state(false);

	async function onfile(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		busy = true;
		try {
			const m = await uploadMedia(file);
			value = m.url;
			broken = false;
		} catch (err) {
			toasts.error('Upload failed', errorMessage(err));
		} finally {
			busy = false;
		}
	}
</script>

<div>
	<label class="field-label" for={id}>{label}</label>
	<div class="flex items-start gap-3">
		{#if image}
			<div
				class="grid size-[4.5rem] shrink-0 place-items-center overflow-hidden rounded-lg border border-rule-strong bg-paper-3"
			>
				{#if value && !broken}
					<img src={value} alt="" class="size-full object-cover" onerror={() => (broken = true)} />
				{:else}
					<ImageUp class="size-5 text-ink-soft" aria-hidden="true" />
				{/if}
			</div>
		{/if}
		<div class="min-w-0 flex-1">
			<input
				{id}
				class="input"
				type="text"
				inputmode="url"
				placeholder="/media/… or https://…"
				bind:value
				oninput={() => (broken = false)}
				aria-describedby={hint ? `${id}-hint` : undefined}
			/>
			<div class="mt-2 flex flex-wrap gap-2">
				<label
					class={[
						'btn btn-secondary btn-sm focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-ink',
						busy && 'pointer-events-none opacity-50'
					]}
				>
					<input
						type="file"
						{accept}
						class="sr-only"
						aria-label="Upload {label.toLowerCase()}"
						disabled={busy}
						onchange={onfile}
					/>
					{#if busy}
						<LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Uploading…
					{:else}
						<ImageUp class="size-4" aria-hidden="true" /> Upload
					{/if}
				</label>
				{#if value}
					<button type="button" class="btn btn-ghost btn-sm" onclick={() => (value = '')}>
						<X class="size-4" aria-hidden="true" /> Clear
					</button>
				{/if}
			</div>
			{#if hint}<p id="{id}-hint" class="hint">{hint}</p>{/if}
		</div>
	</div>
</div>
