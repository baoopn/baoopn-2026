<script lang="ts">
	import type { CreditImage, CreditInput, CreditLogo, CreditLogoKind } from '#lib/api/types.js';

	let {
		form,
		logos,
		alt
	}: {
		/** Unsaved form values. */
		form: CreditInput;
		logos: Record<CreditLogoKind, CreditLogo>;
		alt: string;
	} = $props();

	const HEIGHT = 20;

	const logo = $derived(logos[form.logo] ?? logos.wordmark);
	// Only link to https URLs, so a half-typed value never becomes a live link.
	const href = $derived(/^https:\/\//i.test(form.url.trim()) ? form.url.trim() : undefined);
	const text = $derived(form.text.trim());

	const tiles = $derived([
		{ key: 'light', label: 'Light background', bg: '#fff5eb', fg: '#0b0537', image: logo.light },
		{ key: 'dark', label: 'Dark background', bg: '#0b0537', fg: '#fff5eb', image: logo.dark }
	]);

	function width(img: CreditImage): number {
		return img.height > 0 ? Math.round((HEIGHT * img.width) / img.height) : HEIGHT;
	}
</script>

<section class="card flex min-w-0 flex-col gap-4 p-4 sm:p-5" aria-labelledby="credit-preview-h">
	<div>
		<h2 id="credit-preview-h" class="eyebrow">Preview</h2>
		<p class="hint mt-1">How the line looks in a client footer, before you save.</p>
	</div>

	<div class="grid min-w-0 gap-3">
		{#each tiles as t (t.key)}
			<figure class="min-w-0">
				<div
					class="flex min-h-20 min-w-0 flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border border-rule px-4 py-5 text-sm"
					style:background={t.bg}
					style:color={t.fg}
				>
					<span class="min-w-0 break-words">{text}</span>
					<a {href} target="_blank" rel="noopener" class="inline-flex shrink-0 rounded">
						<img
							src={t.image.url}
							{alt}
							width={width(t.image)}
							height={HEIGHT}
							style:height="{HEIGHT}px"
							style:width="{width(t.image)}px"
						/>
					</a>
				</div>
				<figcaption class="hint">{t.label}</figcaption>
			</figure>
		{/each}
	</div>
</section>
