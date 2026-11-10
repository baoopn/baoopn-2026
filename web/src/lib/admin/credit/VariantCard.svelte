<script lang="ts">
	import type {
		CreditLogo,
		CreditLogoKind,
		CreditLogoStyle,
		CreditVariant
	} from '#lib/api/types.js';
	import Segmented from '../components/Segmented.svelte';
	import CreditPreview from './CreditPreview.svelte';

	type Field = keyof CreditVariant;

	let {
		kind,
		label,
		variant,
		logos,
		url,
		alt,
		prefixMax,
		suffixMax,
		errors,
		isDefault,
		onedit
	}: {
		kind: CreditLogoKind;
		label: string;
		/** Unsaved values for this variant. */
		variant: CreditVariant;
		/** logos[style] for this kind. */
		logos: Record<CreditLogoStyle, CreditLogo>;
		url: string;
		alt: string;
		prefixMax: number;
		suffixMax: number;
		/** Errors keyed prefix, suffix and style. */
		errors: Partial<Record<Field, string>>;
		isDefault: boolean;
		onedit: <K extends Field>(field: K, value: CreditVariant[K]) => void;
	} = $props();

	const styleOptions: { value: CreditLogoStyle; label: string }[] = [
		{ value: 'color', label: 'Color' },
		{ value: 'mono', label: 'Mono' }
	];

	const id = $derived(`credit-${kind}`);
	const prefixLength = $derived([...variant.prefix.trim()].length);
	const suffixLength = $derived([...variant.suffix.trim()].length);
	const logo = $derived(logos[variant.style] ?? logos.color);
</script>

<section
	class="flex min-w-0 flex-col gap-4 rounded-lg border border-rule p-4"
	aria-labelledby="{id}-h"
>
	<div class="flex flex-wrap items-center gap-2">
		<h3 id="{id}-h" class="field-label mb-0">{label}</h3>
		{#if isDefault}
			<span class="tag border border-on-spot" style:--tag="var(--spot-mint)">Default</span>
		{/if}
	</div>

	{#snippet counter(length: number, max: number)}
		<span class={['font-mono text-xs', length > max ? 'text-danger' : 'text-ink-soft']}>
			{length} / {max}
		</span>
	{/snippet}

	<div>
		<div class="flex items-center justify-between gap-2">
			<label class="field-label" for="{id}-prefix">Prefix</label>
			{@render counter(prefixLength, prefixMax)}
		</div>
		<input
			id="{id}-prefix"
			class="input"
			type="text"
			autocomplete="off"
			value={variant.prefix}
			oninput={(e) => onedit('prefix', e.currentTarget.value)}
			aria-invalid={!!errors.prefix}
			aria-describedby="{id}-prefix-msg"
		/>
		{#if errors.prefix}
			<p id="{id}-prefix-msg" class="field-error" role="alert">{errors.prefix}</p>
		{:else}
			<p id="{id}-prefix-msg" class="hint">
				Text before the logo, for example "by". Leave it empty for no text.
			</p>
		{/if}
	</div>

	<div>
		<p class="field-label">Style</p>
		<div id="{id}-style-group">
			<Segmented
				options={styleOptions}
				value={variant.style}
				label="{label} style"
				onchange={(v) => onedit('style', v)}
			/>
		</div>
		{#if errors.style}
			<p class="field-error" role="alert">{errors.style}</p>
		{:else}
			<p class="hint">Color: filled tile. Mono: glyph only, navy on light pages, cream on dark.</p>
		{/if}
	</div>

	<div>
		<div class="flex items-center justify-between gap-2">
			<label class="field-label" for="{id}-suffix">Suffix</label>
			{@render counter(suffixLength, suffixMax)}
		</div>
		<input
			id="{id}-suffix"
			class="input"
			type="text"
			autocomplete="off"
			value={variant.suffix}
			oninput={(e) => onedit('suffix', e.currentTarget.value)}
			aria-invalid={!!errors.suffix}
			aria-describedby="{id}-suffix-msg"
		/>
		{#if errors.suffix}
			<p id="{id}-suffix-msg" class="field-error" role="alert">{errors.suffix}</p>
		{:else}
			<p id="{id}-suffix-msg" class="hint">
				Text after the logo, inside the link. Leave it empty for no text.
			</p>
		{/if}
		{#if variant.suffix.trim()}
			<p class="hint">
				Sites that use an older snippet do not show the suffix. Copy the new code from Embed
				snippet.
			</p>
		{/if}
	</div>

	<CreditPreview {variant} {logo} {url} {alt} />
</section>
