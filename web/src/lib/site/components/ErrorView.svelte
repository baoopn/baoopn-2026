<script lang="ts">
	import { page } from '$app/state';

	const notFound = $derived(page.status === 404);
	const message = $derived(
		notFound
			? "This page isn't in the tracklist. It may have moved, or the link is off by a groove."
			: (page.error?.message ?? 'Something went wrong on our side. Please try again.')
	);
</script>

<svelte:head>
	<title>{notFound ? 'Page not found' : 'Something went wrong'} · Bao Nguyen</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<section class="mx-auto max-w-6xl px-4 py-16 sm:px-6 sm:py-24" aria-labelledby="err-h">
	<p class="chip label [--chip:var(--spot-rose)]">
		<span class="size-1.5 rounded-full bg-on-spot" aria-hidden="true"></span>
		Error {page.status}
	</p>
	<h1 id="err-h" class="mt-6 font-display text-5xl leading-[1] font-semibold tracking-[-0.035em] text-balance sm:text-7xl">
		{notFound ? 'Skipped a track.' : 'The needle slipped.'}
	</h1>
	<p class="mt-5 max-w-xl text-lg text-ink-soft">{message}</p>
	<div class="mt-8 flex flex-wrap gap-3">
		<a href="/" class="btn btn-primary">Back to the start</a>
		<a href="/blog" class="btn btn-secondary">Read the blog</a>
	</div>
</section>
