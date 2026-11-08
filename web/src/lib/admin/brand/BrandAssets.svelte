<script lang="ts">
	import { RefreshCw } from '@lucide/svelte';
	import type { AdminBrandAsset } from '#lib/api/types.js';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import { get } from '#lib/admin/api.js';
	import { errorMessage } from '#lib/admin/format.js';
	import BrandSlot from './BrandSlot.svelte';

	let assets = $state<AdminBrandAsset[] | null>(null);
	let loadError = $state<string | null>(null);
	let loading = $state(false);

	async function load() {
		loading = true;
		loadError = null;
		try {
			assets = await get<AdminBrandAsset[]>('/brand');
		} catch (err) {
			loadError = errorMessage(err);
		} finally {
			loading = false;
		}
	}
	void load();

	function replace(next: AdminBrandAsset) {
		if (assets) assets = assets.map((a) => (a.slot === next.slot ? next : a));
	}
</script>

<section class="card flex flex-col gap-4 p-4" aria-labelledby="brand-h">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 id="brand-h" class="eyebrow">Brand assets</h2>
			<p class="hint mt-1">
				Favicons and logos. Changes apply right away, separate from the form above. The public site
				picks up changes within a minute.
			</p>
		</div>
		<button
			type="button"
			class="btn btn-ghost btn-sm"
			onclick={load}
			disabled={loading}
			aria-label="Reload the brand assets"
		>
			<RefreshCw class="size-4 {loading ? 'animate-spin' : ''}" aria-hidden="true" />
		</button>
	</div>

	{#if loadError}
		<ErrorState message={loadError} onretry={load} />
	{:else if !assets}
		<div class="grid gap-4 md:grid-cols-2" aria-busy="true">
			<div class="skeleton h-48"></div>
			<div class="skeleton h-48"></div>
			<span class="sr-only">Loading the brand assets</span>
		</div>
	{:else}
		<ul class="grid gap-4 md:grid-cols-2">
			{#each assets as asset (asset.slot)}
				<BrandSlot {asset} onchange={replace} />
			{/each}
		</ul>
	{/if}
</section>
