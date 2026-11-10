<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import type { CreditInput } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import CreditForm, { type CreditEdits } from '#lib/admin/credit/CreditForm.svelte';
	import EmbedSnippet from '#lib/admin/credit/EmbedSnippet.svelte';
	import { creditQuery } from '#lib/admin/credit/queries.js';
	import { errorMessage } from '#lib/admin/format.js';

	const credit = createQuery(() => creditQuery());

	// Local edits on top of the saved values, so a refetch updates every field
	// the admin has not touched. Each variant card previews the unsaved result.
	let edits = $state<CreditEdits>({});
	const form = $derived.by((): CreditInput | null => {
		const d = credit.data;
		if (!d) return null;
		const { variants, ...rest } = edits;
		return {
			url: d.url,
			logo: d.logo,
			origins: d.origins,
			...rest,
			variants: {
				wordmark: { ...d.variants.wordmark, ...variants?.wordmark },
				mark: { ...d.variants.mark, ...variants?.mark }
			}
		};
	});
	/** Alt text the public endpoint sends: the site host without "www.". */
	const alt = $derived.by(() => {
		try {
			return new URL(credit.data?.endpoint ?? '').hostname.replace(/^www\./, '');
		} catch {
			return 'baoopn.com';
		}
	});
</script>

<PageHeader
	title="Credit"
	eyebrow="Site"
	description="The credit line client sites show in their footers."
/>

{#if credit.data && form}
	<div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,28rem)]">
		<div class="min-w-0">
			<CreditForm credit={credit.data} {form} {alt} bind:edits />
		</div>
		<div class="flex min-w-0 flex-col gap-6">
			<EmbedSnippet
				endpoint={credit.data.endpoint}
				logoUrl={credit.data.logos[form.logo][form.variants[form.logo].style].light.url}
				defaultLogo={form.logo}
			/>
		</div>
	</div>
{:else if credit.isError}
	<ErrorState message={errorMessage(credit.error)} onretry={() => credit.refetch()} />
{:else}
	<div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,28rem)]" aria-busy="true">
		<div class="skeleton h-96"></div>
		<div class="skeleton h-64"></div>
		<span class="sr-only">Loading the credit settings</span>
	</div>
{/if}
