<script lang="ts">
	import { onMount } from 'svelte';
	import EqBars from './EqBars.svelte';
	import { relativeClock } from '#lib/site/clock.svelte.js';
	import { activeTrack, listening } from '#lib/site/listening.svelte.js';
	import { artistNames } from '#lib/site/format.js';

	// SSR has no live state; read (and start polling) only once mounted so
	// hydration matches the server markup.
	let mounted = $state(false);
	onMount(() => {
		mounted = true;
	});
	const np = $derived(mounted ? listening.now : null);
	const track = $derived(activeTrack(np, relativeClock.now));
	const playing = $derived(!!track && !!np?.is_playing);
</script>

<a
	href="/#listening"
	class={[
		'chip label max-w-full min-h-11 no-underline transition-colors',
		!playing && '[--chip:var(--paper-2)]'
	]}
	aria-label={track
		? `${playing ? 'Now playing' : 'Paused'}: ${track.name} by ${artistNames(track.artists)}`
		: 'Listening: off air'}
>
	<EqBars {playing} />
	{#if track}
		<span class="shrink-0">{playing ? 'Now' : 'Paused'}</span>
		<span class="truncate normal-case tracking-normal">
			{track.name} <span aria-hidden="true">·</span> {artistNames(track.artists)}
		</span>
	{:else}
		<span>Off air</span>
	{/if}
</a>
