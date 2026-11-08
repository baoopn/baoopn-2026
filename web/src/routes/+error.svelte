<script lang="ts">
	// Root boundary: unmatched URLs and errors outside the (site) layout.
	import { page } from '$app/state';
	import ErrorView from '#lib/site/components/ErrorView.svelte';
	import { brandSize, brandSrc } from '#lib/brand.js';

	const wordmark = $derived(brandSize(page.data.brand, 'wordmark_primary', 78, 32));
</script>

<header class="border-b border-ink">
	<div class="mx-auto flex max-w-6xl items-center px-4 py-3 sm:px-6">
		<a href="/" class="flex min-h-11 items-center rounded-[8px]" aria-label="baoopn, home">
			<img
				src={brandSrc(page.data.brand, 'wordmark_primary')}
				alt=""
				width={wordmark.width}
				height={wordmark.height}
				class="h-8 w-auto"
			/>
		</a>
	</div>
</header>
<main id="main">
	<ErrorView />
</main>
