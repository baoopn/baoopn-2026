<script lang="ts">
	import { relativeClock } from '../clock.svelte';
	import { dateTime, relative } from '../format';

	let { value, class: className = '' }: { value: string | null | undefined; class?: string } =
		$props();

	const rel = $derived(relative(value, relativeClock.now));
</script>

{#if value}
	<time datetime={value} title={dateTime(value)} class={['whitespace-nowrap', className]}
		>{rel}</time
	>
{:else}
	<span class={['text-ink-soft', className]}>–</span>
{/if}
