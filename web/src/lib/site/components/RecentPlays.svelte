<script lang="ts">
	import AlbumArt from './AlbumArt.svelte';
	import RelTime from './RelTime.svelte';
	import { artistNames, pad2, safeUrl } from '#lib/site/format.js';
	import type { RecentPlay } from '#lib/api/types.js';

	let { items }: { items: RecentPlay[] } = $props();
</script>

{#if items.length === 0}
	<p class="border-y border-rule py-6 text-ink-soft">Nothing on the turntable lately.</p>
{:else}
	<ol class="border-t border-ink">
		{#each items as play, i (play.id)}
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
