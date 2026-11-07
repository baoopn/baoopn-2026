<script lang="ts">
	import { ExternalLink } from '@lucide/svelte';
	import type { AdminProject } from '#lib/api/types.js';
	import RelTime from '../components/RelTime.svelte';
	import { compact } from '../format';

	let { project: p }: { project: AdminProject } = $props();

	const langs = $derived.by(() => {
		const entries = Object.entries(p.gh_languages).sort((a, b) => b[1] - a[1]);
		const total = entries.reduce((s, [, v]) => s + v, 0);
		return total ? entries.slice(0, 5).map(([k, v]) => ({ k, share: v / total })) : [];
	});
</script>

<section class="card p-4" aria-labelledby="gh-h">
	<h2 id="gh-h" class="eyebrow mb-3">From GitHub</h2>
	{#if !p.github_id}
		<p class="text-sm text-ink-soft">Manual project: no repository is linked.</p>
	{:else}
		<dl class="flex flex-col gap-2 text-sm">
			<div>
				<dt class="sr-only">Repository</dt>
				<dd>
					<a
						href={p.repo_url}
						target="_blank"
						rel="noopener"
						class="link inline-flex items-center gap-1 font-mono"
					>
						{p.repo_full_name}
						<ExternalLink class="size-3.5" aria-hidden="true" />
					</a>
				</dd>
			</div>
			{#if p.gh_description}
				<div>
					<dt class="eyebrow">Description</dt>
					<dd class="text-ink-soft">{p.gh_description}</dd>
				</div>
			{/if}
			{#if p.gh_topics.length}
				<div>
					<dt class="eyebrow mb-1">Topics</dt>
					<dd class="flex flex-wrap gap-1">
						{#each p.gh_topics as t (t)}<span class="tag">{t}</span>{/each}
					</dd>
				</div>
			{/if}
			{#if langs.length}
				<div>
					<dt class="eyebrow mb-1">Languages</dt>
					<dd class="flex flex-col gap-1">
						{#each langs as l (l.k)}
							<span class="flex items-center gap-2 font-mono text-xs">
								<span class="w-20 truncate">{l.k}</span>
								<span class="h-2 flex-1 border-l border-ink" aria-hidden="true">
									<span
										class="block h-full rounded-r-[3px] border-r-2 border-ink"
										style:width="{Math.max(2, l.share * 100)}%"
										style:background="var(--spot-chart)"
									></span>
								</span>
								<span class="w-10 text-right tabular-nums">{Math.round(l.share * 100)}%</span>
							</span>
						{/each}
					</dd>
				</div>
			{/if}
		</dl>
		<dl class="mt-2 grid grid-cols-2 gap-2 border-t border-rule pt-2 font-mono text-xs">
			<div>
				<dt class="eyebrow">Stars</dt>
				<dd>{compact(p.gh_stars)}</dd>
			</div>
			<div>
				<dt class="eyebrow">Forks</dt>
				<dd>{compact(p.gh_forks)}</dd>
			</div>
			<div>
				<dt class="eyebrow">Pushed</dt>
				<dd><RelTime value={p.gh_pushed_at} /></dd>
			</div>
			<div>
				<dt class="eyebrow">Synced</dt>
				<dd><RelTime value={p.synced_at} /></dd>
			</div>
		</dl>
		<dl class="mt-2 flex flex-col gap-2 text-sm">
			{#if p.gh_homepage}
				<div>
					<dt class="eyebrow">Homepage</dt>
					<dd class="truncate">
						<a href={p.gh_homepage} target="_blank" rel="noopener" class="link">{p.gh_homepage}</a>
					</dd>
				</div>
			{/if}
		</dl>
	{/if}
</section>
