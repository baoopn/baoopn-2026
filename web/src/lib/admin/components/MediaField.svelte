<script lang="ts">
	import { onDestroy } from 'svelte';
	import { ImageUp, LoaderCircle, X } from '@lucide/svelte';
	import type { Crop } from '../api';
	import { IMAGE_ACCEPT, isImageFile } from '../media/preview';
	import { UploadJob } from '../media/upload.svelte';
	import ImageCropper, { type Aspect } from './ImageCropper.svelte';
	import UploadProgress from './UploadProgress.svelte';

	let {
		value = $bindable(''),
		label,
		id,
		accept,
		image = true,
		aspect = 'free',
		hint
	}: {
		value?: string;
		label: string;
		id: string;
		/** Defaults to every image type (incl. HEIC/HEIF) when `image`. */
		accept?: string;
		/** Show an image thumbnail and crop before uploading. */
		image?: boolean;
		/** Crop preset the cropper opens with. */
		aspect?: Aspect;
		hint?: string;
	} = $props();

	let broken = $state(false);
	let job = $state.raw<UploadJob | null>(null);
	let cropFile = $state.raw<File | null>(null);
	let cropOpen = $state(false);
	const busy = $derived(!!job?.inFlight && !job.slow);

	// Leaving the page stops polling; the media row still finishes on the server.
	onDestroy(() => job?.detach());

	function clearJob() {
		job?.cancel();
		job = null;
	}

	function upload(file: File, crop: Crop | null): UploadJob {
		clearJob();
		const j = new UploadJob(file, crop, {
			onready: (m) => {
				value = m.url;
				broken = false;
			}
		});
		job = j;
		void j.start();
		return j;
	}

	function onfile(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		if (image && isImageFile(file)) {
			cropFile = file;
			cropOpen = true;
		} else {
			upload(file, null);
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
				placeholder="https://…"
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
						accept={accept ?? (image ? IMAGE_ACCEPT : undefined)}
						class="sr-only"
						aria-label="Upload {label.toLowerCase()}"
						disabled={busy}
						onchange={onfile}
					/>
					{#if busy}
						<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
						{job?.stage === 'converting' ? 'Converting…' : 'Uploading…'}
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
	{#if job}
		<div class="mt-3">
			<UploadProgress {job} onfinished={clearJob} onremove={clearJob} />
		</div>
	{/if}
</div>

{#if image}
	<ImageCropper
		bind:open={cropOpen}
		file={cropFile}
		{aspect}
		title="Crop {label.toLowerCase()}"
		onconfirm={(crop) => (cropFile ? upload(cropFile, crop) : undefined)}
	/>
{/if}
