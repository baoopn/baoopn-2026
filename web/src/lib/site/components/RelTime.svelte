<script lang="ts">
	import { onMount } from 'svelte';
	import { relativeClock } from '#lib/site/clock.svelte.js';
	import { dateShort, relative } from '#lib/site/format.js';

	let { value, class: klass = '' }: { value: string; class?: string } = $props();

	// SSR prints the date; once mounted it switches to a live relative time.
	let mounted = $state(false);
	onMount(() => {
		mounted = true;
	});
	const label = $derived(mounted ? relative(value, relativeClock.now) : dateShort(value));
</script>

<time datetime={value} class={klass} title={dateShort(value)}>{label}</time>
