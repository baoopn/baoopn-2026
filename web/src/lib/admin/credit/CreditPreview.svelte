<script lang="ts">
	import type { CreditImage, CreditLogo, CreditVariant } from '#lib/api/types.js';

	let {
		variant,
		logo,
		url,
		alt
	}: {
		/** Unsaved values for one variant. */
		variant: CreditVariant;
		/** logos[kind][variant.style] */
		logo: CreditLogo;
		url: string;
		alt: string;
	} = $props();

	const HEIGHT = 20;

	// Only link to https URLs, so a half-typed value never becomes a live link.
	const href = $derived(/^https:\/\//i.test(url.trim()) ? url.trim() : undefined);
	const prefix = $derived(variant.prefix.trim());
	const suffix = $derived(variant.suffix.trim());

	const tiles = $derived([
		{ key: 'light', label: 'Light background', bg: '#fff5eb', fg: '#0b0537', image: logo.light },
		{ key: 'dark', label: 'Dark background', bg: '#0b0537', fg: '#fff5eb', image: logo.dark }
	]);

	function width(img: CreditImage): number {
		return img.height > 0 ? Math.round((HEIGHT * img.width) / img.height) : HEIGHT;
	}
</script>

<div class="grid min-w-0 gap-3 sm:grid-cols-2">
	{#each tiles as t (t.key)}
		<figure class="min-w-0">
			<div
				class="flex min-h-20 min-w-0 flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border border-rule px-4 py-5 text-sm"
				style:background={t.bg}
				style:color={t.fg}
			>
				{#if prefix}<span class="min-w-0 break-words">{prefix}</span>{/if}
				<a
					{href}
					target="_blank"
					rel="noopener"
					class="inline-flex min-w-0 items-center gap-2 rounded text-inherit no-underline"
				>
					<img
						class="shrink-0"
						src={t.image.url}
						{alt}
						width={width(t.image)}
						height={HEIGHT}
						style:height="{HEIGHT}px"
						style:width="{width(t.image)}px"
					/>
					{#if suffix}<span class="min-w-0 break-words">{suffix}</span>{/if}
				</a>
			</div>
			<figcaption class="hint">{t.label}</figcaption>
		</figure>
	{/each}
</div>
