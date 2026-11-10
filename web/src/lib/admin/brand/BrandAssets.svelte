<script lang="ts">
	import { RefreshCw } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { AdminBrandAsset, BrandName } from '#lib/api/types.js';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import Segmented from '#lib/admin/components/Segmented.svelte';
	import { get } from '#lib/admin/api.js';
	import { errorMessage } from '#lib/admin/format.js';
	import BrandSlot from './BrandSlot.svelte';
	import { BRAND_COPY, apiBase, groupAssets, parseBrand } from './layout.js';

	const brandOptions: { value: BrandName; label: string }[] = [
		{ value: 'baoopn', label: 'baoopn' },
		{ value: 'listening', label: 'listening' }
	];

	const brand = $derived(parseBrand(page.url.searchParams.get('brand')));
	const base = $derived(apiBase(brand));

	// Tagged with the brand it came from, so a switch never shows the old list.
	let data = $state<{ brand: BrandName; assets: AdminBrandAsset[] } | null>(null);
	const assets = $derived(data?.brand === brand ? data.assets : null);
	let loadError = $state<string | null>(null);
	let loading = $state(false);
	// Each load gets a fresh token; a response for an older token is ignored,
	// so a slow reply for the previous brand cannot overwrite the current one.
	let token = 0;

	async function load(from: BrandName) {
		const mine = ++token;
		loading = true;
		loadError = null;
		try {
			const next = await get<AdminBrandAsset[]>(apiBase(from));
			if (mine === token) data = { brand: from, assets: next };
		} catch (err) {
			if (mine === token) loadError = errorMessage(err);
		} finally {
			if (mine === token) loading = false;
		}
	}

	$effect(() => {
		void load(brand);
	});

	const groups = $derived(assets ? groupAssets(assets) : []);

	function replace(next: AdminBrandAsset) {
		if (data)
			data = { ...data, assets: data.assets.map((a) => (a.slot === next.slot ? next : a)) };
	}

	// Shallow: only the URL changes, so the page keeps its data, scroll and focus.
	function pick(next: BrandName) {
		const url = new URL(page.url.href);
		if (next === 'listening') url.searchParams.set('brand', next);
		else url.searchParams.delete('brand');
		void goto(url, { replace: true, shallow: true });
	}
</script>

<section class="card flex flex-col gap-4 p-4" aria-labelledby="brand-h">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 id="brand-h" class="eyebrow">Brand assets</h2>
			<p class="hint mt-1">{BRAND_COPY[brand].intro}</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<Segmented options={brandOptions} value={brand} label="Brand" size="sm" onchange={pick} />
			<button
				type="button"
				class="btn btn-ghost btn-sm"
				onclick={() => load(brand)}
				disabled={loading}
				aria-label="Reload the brand assets"
			>
				<RefreshCw class="size-4 {loading ? 'animate-spin' : ''}" aria-hidden="true" />
			</button>
		</div>
	</div>

	{#if loadError}
		<ErrorState message={loadError} onretry={() => load(brand)} />
	{:else if !assets}
		<div class="grid gap-4 md:grid-cols-2" aria-busy="true">
			<div class="skeleton h-48"></div>
			<div class="skeleton h-48"></div>
			<span class="sr-only">Loading the brand assets</span>
		</div>
	{:else}
		{#key brand}
			{#each groups as g (g.key)}
				<section class="flex flex-col gap-3" aria-labelledby="brand-{brand}-{g.key}-h">
					<h3 id="brand-{brand}-{g.key}-h" class="font-mono text-xs tracking-[0.04em] uppercase">
						{g.label}
					</h3>
					<ul class="grid gap-4 md:grid-cols-2">
						{#each g.assets as asset (asset.slot)}
							<BrandSlot {asset} {brand} {base} onchange={replace} />
						{/each}
					</ul>
				</section>
			{/each}
		{/key}
	{/if}
</section>
