<script lang="ts">
	import { ImageUp, LoaderCircle, RotateCcw } from '@lucide/svelte';
	import { invalidateAll } from '$app/navigation';
	import type { AdminBrandAsset } from '#lib/api/types.js';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import { admin, put } from '#lib/admin/api.js';
	import { bytes, errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	let {
		asset,
		onchange
	}: { asset: AdminBrandAsset; onchange: (a: AdminBrandAsset) => void } = $props();

	let busy = $state(false);
	let error = $state<string | null>(null);
	let confirmOpen = $state(false);

	const id = $derived('brand-' + asset.slot);
	const swatches = $derived(
		[
			{ key: 'light', name: 'light', bg: '#fff5eb' },
			{ key: 'dark', name: 'dark', bg: '#0b0537' }
		].filter((s) => asset.preview === 'both' || asset.preview === s.key)
	);

	/** Preview sizes in CSS px; null means "logo, sized by height". */
	const sizes = $derived.by((): number[] | null => {
		switch (asset.slot) {
			case 'favicon_ico':
				return [16, 32, 48];
			case 'favicon_svg':
				return [32, 64];
			case 'apple_touch_icon':
				return [90];
			default:
				return null;
		}
	});

	async function done(next: AdminBrandAsset, title: string) {
		onchange(next);
		toasts.success(title, 'The public site picks up changes within a minute.');
		await invalidateAll();
	}

	async function onfile(e: Event & { currentTarget: HTMLInputElement }) {
		const input = e.currentTarget;
		const file = input.files?.[0];
		input.value = '';
		if (!file || busy) return;
		busy = true;
		error = null;
		try {
			const fd = new FormData();
			fd.append('file', file);
			await done(await put<AdminBrandAsset>('/brand/' + asset.slot, fd), `${asset.label} replaced`);
		} catch (err) {
			error = errorMessage(err);
			toasts.error(`Could not replace the ${asset.label.toLowerCase()}`, error);
		} finally {
			busy = false;
		}
	}

	// Not `del()`: that helper is typed void, and the reset returns the default asset.
	async function reset() {
		busy = true;
		error = null;
		try {
			await done(
				await admin<AdminBrandAsset>('/brand/' + asset.slot, { method: 'DELETE' }),
				`${asset.label} reset`
			);
		} catch (err) {
			error = errorMessage(err);
			toasts.error(`Could not reset the ${asset.label.toLowerCase()}`, error);
		} finally {
			busy = false;
		}
	}
</script>

<li class="card flex flex-col gap-3 p-4" aria-labelledby="{id}-h" aria-busy={busy}>
	<div class="flex flex-wrap items-start justify-between gap-2">
		<div class="min-w-0">
			<h3 id="{id}-h" class="font-medium">{asset.label}</h3>
			<p id="{id}-rule" class="hint mt-0">{asset.rule}</p>
		</div>
		<span
			class="tag border border-on-spot"
			style:--tag={asset.custom ? 'var(--spot-mint)' : 'var(--paper-3)'}
		>
			{asset.custom ? 'Custom' : 'Default'}
		</span>
	</div>

	<div class="flex flex-wrap gap-2">
		{#each swatches as s (s.key)}
			<figure class="flex min-w-0 flex-1 flex-col gap-1">
				<div
					class="flex min-h-24 flex-wrap items-end justify-center gap-4 rounded-[8px] border border-rule p-4"
					style:background={s.bg}
				>
					{#if sizes}
						{#each sizes as px (px)}
							<img
								src={asset.url}
								alt="{asset.label} at {px} px on a {s.name} background"
								width={px}
								height={px}
								style:width="{px}px"
								style:height="{px}px"
							/>
						{/each}
					{:else}
						<img
							src={asset.url}
							alt="{asset.label} on a {s.name} background"
							width={asset.width || undefined}
							height={asset.height || undefined}
							class={asset.slot === 'wordmark_primary' ? 'h-12 w-auto' : 'size-16'}
						/>
					{/if}
				</div>
				<figcaption class="font-mono text-xs text-ink-soft">On {s.name}</figcaption>
			</figure>
		{/each}
	</div>

	<p class="text-sm text-ink-soft">
		{#if asset.custom}
			<span class="text-ink">{asset.original || 'Uploaded file'}</span>
			· {bytes(asset.size_bytes)} · {asset.width}×{asset.height} · updated
			<RelTime value={asset.updated_at} />
		{:else}
			Brand-kit default · {bytes(asset.size_bytes)} · {asset.width}×{asset.height}
		{/if}
	</p>

	{#if error}
		<p id="{id}-err" class="field-error mt-0" role="alert">{error}</p>
	{/if}

	<div class="mt-auto flex flex-wrap gap-2">
		<label
			class={[
				'btn btn-secondary btn-sm focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-ink',
				busy && 'pointer-events-none opacity-50'
			]}
		>
			<input
				type="file"
				accept={asset.accept.join(',')}
				class="sr-only"
				disabled={busy}
				aria-describedby={error ? `${id}-rule ${id}-err` : `${id}-rule`}
				onchange={onfile}
			/>
			{#if busy}
				<LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Working…
			{:else}
				<ImageUp class="size-4" aria-hidden="true" /> Replace…
			{/if}
			<span class="sr-only">{asset.label}</span>
		</label>
		<button
			type="button"
			class="btn btn-ghost btn-sm"
			disabled={!asset.custom || busy}
			onclick={() => (confirmOpen = true)}
		>
			<RotateCcw class="size-4" aria-hidden="true" /> Reset to default
			<span class="sr-only">{asset.label}</span>
		</button>
	</div>
</li>

<ConfirmDialog
	bind:open={confirmOpen}
	title="Reset the {asset.label.toLowerCase()}?"
	message="The brand-kit default comes back. The uploaded file is kept for 7 days, then deleted."
	confirmLabel="Reset to default"
	onconfirm={reset}
/>
