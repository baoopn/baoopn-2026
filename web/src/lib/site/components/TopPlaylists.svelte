<script lang="ts">
	import { onMount } from 'svelte';
	import SpotifyGlyph from './SpotifyGlyph.svelte';
	import { periodRange, plural, safeUrl } from '#lib/site/format.js';
	import { theme } from '#lib/theme.svelte.js';
	import type { PlaylistPeriod, TopPlaylists } from '#lib/api/types.js';

	let { data }: { data: TopPlaylists | null } = $props();

	const uid = $props.id();
	const PERIODS: { id: PlaylistPeriod; label: string }[] = [
		{ id: 'week', label: 'Week' },
		{ id: 'month', label: 'Month' },
		{ id: 'year', label: 'Year' }
	];
	const SPOTIFY_ID = /^[A-Za-z0-9]+$/;

	const available = $derived(PERIODS.filter((p) => (data?.periods?.[p.id]?.length ?? 0) > 0));

	let picked = $state<PlaylistPeriod | null>(null);
	const period = $derived(available.find((p) => p.id === picked)?.id ?? available[0]?.id ?? null);
	const entries = $derived(period && data ? data.periods[period] : []);
	const newest = $derived(entries[0]);
	const older = $derived(entries.slice(1));

	const newestUrl = $derived(safeUrl(newest?.spotify_url));
	const embedSrc = $derived.by(() => {
		if (!newest || !SPOTIFY_ID.test(newest.spotify_id)) return undefined;
		const dark = theme.resolved === 'dark' ? '&theme=0' : '';
		return `https://open.spotify.com/embed/playlist/${newest.spotify_id}?utm_source=generator${dark}`;
	});

	// The embed theme is only known in the browser; mount the iframe there so
	// it loads once with the right colours.
	let mounted = $state(false);
	onMount(() => {
		mounted = true;
	});
</script>

{#if newest && period}
	<div class="mt-12">
		<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
			<h3 class="label text-ink-soft">Top 50 playlists</h3>
			{#if available.length > 1}
				<fieldset class="segmented">
					<legend class="sr-only">Playlist period</legend>
					{#each available as p (p.id)}
						<label class="label">
							<input
								type="radio"
								name="top-playlists-{uid}"
								value={p.id}
								class="sr-only"
								checked={period === p.id}
								onchange={() => (picked = p.id)}
							/>
							{p.label}
						</label>
					{/each}
				</fieldset>
			{/if}
		</div>

		<div class="grid gap-10 border-t border-ink pt-6 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
			<div class="min-w-0">
				<p
					class="font-display text-xl leading-tight font-semibold tracking-[-0.02em] break-words sm:text-2xl"
				>
					{newest.name}
				</p>
				<p class="mt-1 text-sm text-ink-soft">
					{periodRange(newest.period_start, newest.period_end, newest.time_zone)}
				</p>
				<p class="label mt-2 text-ink-soft">
					{plural(newest.track_count, 'track')} · {plural(newest.total_plays, 'play')}
				</p>
				{#if newestUrl}
					<a
						href={newestUrl}
						target="_blank"
						rel="noopener noreferrer"
						class="btn btn-secondary mt-4"
					>
						<SpotifyGlyph class="size-5" /> Open in Spotify
						<span class="sr-only">(opens in a new tab)</span>
					</a>
				{/if}
				{#if embedSrc}
					<div class="embed mt-6 rounded-brand border border-rule-strong">
						{#if mounted}
							<iframe
								src={embedSrc}
								title="Spotify playlist: {newest.name}"
								width="100%"
								height="352"
								loading="lazy"
								style="border:0"
								allow="autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture"
							></iframe>
						{/if}
					</div>
				{/if}
			</div>

			{#if older.length > 0}
				<div class="min-w-0">
					<h4 class="label mb-3 text-ink-soft">Earlier</h4>
					<ul class="border-t border-rule-strong">
						{#each older as entry (entry.spotify_id + entry.period_start)}
							{@const href = safeUrl(entry.spotify_url)}
							<li class="row">
								<span class="min-w-0 flex-1 truncate font-semibold">
									{#if href}
										<a {href} target="_blank" rel="noopener noreferrer" class="link">
											{entry.label}<span class="sr-only"> (opens in a new tab)</span>
										</a>
									{:else}
										{entry.label}
									{/if}
								</span>
								<span class="shrink-0 font-mono text-xs text-ink-soft tabular-nums">
									{plural(entry.track_count, 'track')}
								</span>
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.segmented {
		display: inline-flex;
		border: 1px solid var(--ink);
		border-radius: 999px;
		padding: 2px;
		background: var(--paper);
	}
	.segmented label {
		display: grid;
		place-items: center;
		min-width: 4.25rem;
		min-height: 2.75rem;
		padding-inline: 0.85rem;
		border-radius: 999px;
		color: var(--ink-soft);
		transition:
			background-color 160ms ease-out,
			color 160ms ease-out;
	}
	.segmented label:hover {
		color: var(--ink);
	}
	.segmented label:has(input:checked) {
		background: var(--spot-now);
		color: var(--on-spot);
	}
	.segmented label:has(input:focus-visible) {
		outline: 2px solid var(--ink);
		outline-offset: 1px;
	}
	.embed {
		height: 352px;
		overflow: hidden;
		background: var(--paper-2);
	}
	.embed iframe {
		display: block;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		min-height: 3.25rem;
		padding-block: 0.55rem;
		border-bottom: 1px solid var(--rule);
	}
</style>
