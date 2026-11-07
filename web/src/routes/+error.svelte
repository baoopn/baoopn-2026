<script lang="ts">
	// Root boundary: unmatched URLs and errors outside the (site) layout.
	import ErrorView from '#lib/site/components/ErrorView.svelte';
</script>

<header class="border-b border-ink">
	<div class="mx-auto flex max-w-6xl items-center px-4 py-3 sm:px-6">
		<a href="/" class="flex min-h-11 items-center rounded-[8px]" aria-label="baoopn, home">
			<img src="/logo/baoopn-wordmark-primary.svg" alt="" width="78" height="32" class="h-8 w-auto" />
		</a>
	</div>
</header>
<main id="main">
	<ErrorView />
</main>
