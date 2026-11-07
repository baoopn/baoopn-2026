<script lang="ts">
	import Disc from '@lucide/svelte/icons/disc-3';
	import { safeImage } from '#lib/site/format.js';

	let {
		src,
		alt = '',
		class: klass = 'size-12 rounded-[6px]',
		eager = false
	}: { src?: string | null; alt?: string; class?: string; eager?: boolean } = $props();

	const url = $derived(safeImage(src));
	let failedSrc = $state<string | null>(null);
	const failed = $derived(!!url && failedSrc === url);
</script>

{#if url && !failed}
	<img
		src={url}
		{alt}
		width="300"
		height="300"
		loading={eager ? 'eager' : 'lazy'}
		decoding="async"
		referrerpolicy="no-referrer"
		class={['aspect-square shrink-0 bg-paper-2 object-cover', klass]}
		onerror={() => (failedSrc = url ?? null)}
	/>
{:else}
	<span
		class={['grid aspect-square shrink-0 place-items-center bg-spot-lilac text-on-spot', klass]}
		role={alt ? 'img' : undefined}
		aria-label={alt || undefined}
	>
		<Disc class="size-1/2" aria-hidden="true" />
	</span>
{/if}
