<script lang="ts">
	import { onMount } from 'svelte';
	import AlbumArt from './AlbumArt.svelte';
	import EqBars from './EqBars.svelte';
	import SpotifyGlyph from './SpotifyGlyph.svelte';
	import { relativeClock, secondClock } from '#lib/site/clock.svelte.js';
	import { activeTrack, listening, progressAt } from '#lib/site/listening.svelte.js';
	import { artistNames, duration, safeUrl } from '#lib/site/format.js';
	import type { NowPlaying } from '#lib/api/types.js';

	let { initial = null }: { initial?: NowPlaying | null } = $props();

	// Render the SSR snapshot until mounted (so hydration matches), then seed
	// the shared poller with it and follow the live state.
	let mounted = $state(false);
	onMount(() => {
		listening.seed(initial ?? null);
		mounted = true;
	});

	const np = $derived(mounted ? listening.now : (initial ?? null));
	const receivedAt = $derived(mounted ? listening.receivedAt : 0);
	const track = $derived(activeTrack(np, mounted ? relativeClock.now : 0));
	const playing = $derived(!!track && !!np?.is_playing);
	const durationMs = $derived(track?.duration_ms ?? 0);
	const progressMs = $derived(
		progressAt(track ? np : null, receivedAt, playing && mounted ? secondClock.now : receivedAt)
	);
	const ratio = $derived(progressMs != null && durationMs > 0 ? progressMs / durationMs : 0);
	const trackUrl = $derived(safeUrl(track?.external_url));
</script>

<div class={['card print rounded-brand bg-paper-2 [--print:var(--spot-now)]', !playing && 'idle']}>
	<p class="sr-only" aria-live="polite" aria-atomic="true">
		{track ? `${playing ? 'Now playing' : 'Paused'}: ${track.name} by ${artistNames(track.artists)}` : ''}
	</p>
	{#if track}
		<AlbumArt
			src={track.album?.cover_image_url}
			alt={track.album?.name ? `Album cover of ${track.album.name}` : ''}
			class="art w-full rounded-[10px] border border-ink"
			eager
		/>
		<div class="min-w-0">
			<p class={['chip label', !playing && '[--chip:var(--spot-butter)]']}>
				<EqBars {playing} />
				{playing ? 'Now playing' : 'Paused'}
			</p>
			<p class="mt-4 font-display text-2xl leading-tight font-semibold tracking-[-0.02em] break-words sm:text-3xl">
				{#if trackUrl}
					<a href={trackUrl} target="_blank" rel="noopener noreferrer" class="link">{track.name}</a>
				{:else}
					{track.name}
				{/if}
			</p>
			<p class="mt-1 font-medium break-words">{artistNames(track.artists)}</p>
			{#if track.album?.name}
				<p class="mt-0.5 text-sm break-words text-ink-soft">{track.album.name}</p>
			{/if}

			<div class="mt-5">
				<div
					class="relative h-3"
					role="progressbar"
					aria-label="Playback progress"
					aria-valuemin={0}
					aria-valuemax={Math.round(durationMs / 1000)}
					aria-valuenow={progressMs == null ? undefined : Math.round(progressMs / 1000)}
					aria-valuetext={progressMs == null ? undefined : `${duration(progressMs)} of ${duration(durationMs)}`}
				>
					<div class="absolute inset-x-0 top-1/2 h-px bg-rule-strong"></div>
					<div
						class="fill absolute top-1/2 left-0 h-0.5 w-full origin-left bg-ink"
						style:transform="translateY(-50%) scaleX({ratio})"
					></div>
					<div
						class="dot absolute top-1/2 size-3 -translate-y-1/2 rounded-full border-2 border-paper bg-ink"
						style:left="calc({ratio * 100}% - 0.375rem)"
					></div>
				</div>
				<div class="mt-1.5 flex justify-between font-mono text-xs text-ink-soft tabular-nums" aria-hidden="true">
					<span>{duration(progressMs)}</span>
					<span>{duration(durationMs)}</span>
				</div>
			</div>
			{#if trackUrl}
				<a href={trackUrl} target="_blank" rel="noopener noreferrer" class="btn btn-secondary mt-5">
					<SpotifyGlyph class="size-5" /> Listen on Spotify
					<span class="sr-only">(opens in a new tab)</span>
				</a>
			{/if}
		</div>
	{:else}
		<div class="art grid aspect-square w-full place-items-center rounded-[10px] border border-ink bg-spot-lilac text-on-spot">
			<EqBars class="h-10 w-10" />
		</div>
		<div class="min-w-0">
			<p class="chip label [--chip:var(--spot-butter)]">Off air</p>
			<p class="mt-4 font-display text-2xl font-semibold tracking-[-0.02em] sm:text-3xl">
				Not listening right now
			</p>
			<p class="mt-1 text-ink-soft">
				{mounted && listening.failed ? "The record player isn't answering." : 'Headphones are off. Recent plays are below.'}
			</p>
		</div>
	{/if}
</div>

<style>
	.card {
		display: grid;
		gap: 1.25rem;
		padding: 1.25rem;
		align-items: center;
	}
	.card :global(.art) {
		max-width: 12rem;
	}
	@media (min-width: 36rem) {
		.card {
			grid-template-columns: 10rem minmax(0, 1fr);
			gap: 1.5rem;
		}
	}
	.idle {
		opacity: 0.92;
	}
	.fill,
	.dot {
		transition:
			transform 1s linear,
			left 1s linear;
	}
	@media (prefers-reduced-motion: reduce) {
		.fill,
		.dot {
			transition: none;
		}
	}
</style>
