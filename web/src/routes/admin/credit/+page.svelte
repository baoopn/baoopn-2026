<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import type { CreditInput } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import CreditForm from '#lib/admin/credit/CreditForm.svelte';
	import CreditPreview from '#lib/admin/credit/CreditPreview.svelte';
	import EmbedSnippet from '#lib/admin/credit/EmbedSnippet.svelte';
	import { creditQuery } from '#lib/admin/credit/queries.js';
	import { errorMessage } from '#lib/admin/format.js';

	const credit = createQuery(() => creditQuery());

	// Local edits on top of the saved values, so a refetch updates every field
	// the admin has not touched. The preview shows the unsaved result.
	let edits = $state<Partial<CreditInput>>({});
	const form = $derived.by((): CreditInput | null => {
		const d = credit.data;
		if (!d) return null;
		return { text: d.text, url: d.url, logo: d.logo, origins: d.origins, ...edits };
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
			<CreditForm credit={credit.data} {form} bind:edits />
		</div>
		<div class="flex min-w-0 flex-col gap-6">
			<CreditPreview {form} logos={credit.data.logos} {alt} />
			<EmbedSnippet
				endpoint={credit.data.endpoint}
				logoUrl={credit.data.logos[form.logo].light.url}
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
