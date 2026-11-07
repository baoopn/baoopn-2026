<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import RelTime from './RelTime.svelte';
	import { api } from '#lib/api/client.js';
	import { listening } from '#lib/site/listening.svelte.js';
	import { artistNames, pad2, safeUrl } from '#lib/site/format.js';
	import type { RecentPlay } from '#lib/api/types.js';

	let { items, limit = 6 }: { items: RecentPlay[]; limit?: number } = $props();

	// Refetched when the now-playing track changes (the previous one has just
	// landed in the history); until then the SSR list is shown.
	let fresh = $state<RecentPlay[] | null>(null);
	const list = $derived(fresh ?? items);

	/** undefined until the shared now-playing state has loaded. */
	const trackId = $derived.by(() => {
		const np = listening.now;
		return listening.loaded ? (np?.track?.id ?? null) : undefined;
	});
	let lastId: string | null | undefined;

	$effect(() => {
		const id = trackId;
		if (id === undefined) return;
		if (lastId === undefined || id === lastId) {
			lastId = id;
			return;
		}
		lastId = id;
		// Give the tracker a moment to record the finished play.
		const timer = setTimeout(async () => {
			try {
				fresh = await api<RecentPlay[]>(`/api/v1/listening/recent?limit=${limit}`);
			} catch {
				// Keep the list we have.
			}
		}, 5_000);
		return () => clearTimeout(timer);
	});
</script>

{#if list.length === 0}
	<p class="border-y border-rule py-6 text-ink-soft">Nothing on the turntable lately.</p>
{:else}
	<ol class="border-t border-ink">
		{#each list as play, i (play.id)}
			{@const href = safeUrl(play.track.external_url)}
			<li class="row">
				<span class="label w-6 shrink-0 text-ink-soft" aria-hidden="true">{pad2(i + 1)}</span>
				<AlbumArt
					src={play.track.album?.cover_image_url}
					class="size-11 rounded-[6px] border border-rule-strong"
				/>
				<div class="min-w-0 flex-1">
					<p class="truncate font-semibold">
						{#if href}
							<a {href} target="_blank" rel="noopener noreferrer" class="link">{play.track.name}</a>
						{:else}
							{play.track.name}
						{/if}
					</p>
					<p class="truncate text-sm text-ink-soft">{artistNames(play.track.artists)}</p>
				</div>
				<RelTime value={play.played_at} class="shrink-0 font-mono text-xs text-ink-soft tabular-nums" />
			</li>
		{/each}
	</ol>
{/if}

<style>
	.row {
		display: flex;
		align-items: center;
		gap: 0.875rem;
		min-height: 4rem;
		padding-block: 0.55rem;
		border-bottom: 1px solid var(--rule);
	}
</style>
