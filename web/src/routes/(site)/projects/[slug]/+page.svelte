<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';
	import Star from '@lucide/svelte/icons/star';
	import GitFork from '@lucide/svelte/icons/git-fork';
	import Seo from '#lib/site/components/Seo.svelte';
	import SocialIcon from '#lib/site/components/SocialIcon.svelte';
	import { count, dateLong, safeImage, safeUrl, spotFor } from '#lib/site/format.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const p = $derived(data.project);
	const cover = $derived(safeImage(p.cover_url));
	const demo = $derived(safeUrl(p.demo_url));
	const repo = $derived(safeUrl(p.repo_url));

	const SPOT_ORDER = ['sky', 'peach', 'mint', 'lilac', 'butter', 'rose'];
	const languages = $derived.by(() => {
		const entries = Object.entries(p.languages ?? {}).filter(([, b]) => b > 0);
		const total = entries.reduce((s, [, b]) => s + b, 0);
		if (!total) return [];
		const sorted = entries.sort((a, b) => b[1] - a[1]);
		const top = sorted.slice(0, 5);
		const rest = sorted.slice(5).reduce((s, [, b]) => s + b, 0);
		if (rest > 0) top.push(['Other', rest]);
		return top.map(([name, bytes], i) => ({
			name,
			pct: (bytes / total) * 100,
			color: `var(--spot-${SPOT_ORDER[i % SPOT_ORDER.length]})`
		}));
	});
	const tags = $derived([...new Set([...p.tech, ...p.topics])]);
	const pct = (n: number) => (n >= 10 ? n.toFixed(0) : n.toFixed(1)) + '%';
</script>

<Seo title={p.title} description={p.summary} image={p.cover_url} />

<article class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="pt-10 sm:pt-14">
		<a href="/projects" class="link label inline-flex min-h-11 items-center gap-1.5 text-ink-soft">
			<ArrowLeft class="size-4" aria-hidden="true" /> All projects
		</a>
		<div class="mt-4 grid gap-8 border-b border-ink pb-8 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)] lg:items-end">
			<div class="min-w-0">
				<p class="label flex flex-wrap gap-x-3 text-ink-soft">
					{#if p.period}<span>{p.period}</span>{/if}
					{#if p.language}<span>{p.language}</span>{/if}
					{#if p.archived}<span>Archived</span>{/if}
				</p>
				<h1 class="mt-3 font-display text-4xl leading-[1.02] font-semibold tracking-[-0.035em] text-balance break-words sm:text-6xl">
					{p.title}
				</h1>
				{#if p.summary}
					<p class="mt-4 max-w-2xl text-lg text-ink-soft sm:text-xl">{p.summary}</p>
				{/if}
				<div class="mt-6 flex flex-wrap gap-3">
					{#if demo}
						<a href={demo} target="_blank" rel="noopener noreferrer" class="btn btn-primary">
							Visit live site <ArrowUpRight class="size-4" aria-hidden="true" />
						</a>
					{/if}
					{#if repo}
						<a href={repo} target="_blank" rel="noopener noreferrer" class="btn btn-secondary">
							<SocialIcon icon="github" class="size-4" /> Source code
						</a>
					{/if}
				</div>
			</div>

			<dl class="stats">
				{#if repo}
					<div>
						<dt class="label text-ink-soft">Stars</dt>
						<dd class="flex items-center gap-2"><Star class="size-5" aria-hidden="true" />{count(p.stars)}</dd>
					</div>
					<div>
						<dt class="label text-ink-soft">Forks</dt>
						<dd class="flex items-center gap-2"><GitFork class="size-5" aria-hidden="true" />{count(p.forks)}</dd>
					</div>
				{/if}
				{#if p.pushed_at}
					<div class="col-span-2">
						<dt class="label text-ink-soft">Last push</dt>
						<dd class="text-xl!"><time datetime={p.pushed_at}>{dateLong(p.pushed_at)}</time></dd>
					</div>
				{/if}
			</dl>
		</div>
	</header>

	{#if cover}
		<img
			src={cover}
			alt=""
			width="1600"
			height="900"
			fetchpriority="high"
			class="print mt-10 aspect-[16/9] w-full rounded-brand bg-paper-2 object-cover [--print:var(--spot-now)]"
		/>
	{/if}

	<div class="mt-10 grid gap-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
		<div class="min-w-0">
			{#if p.body_html}
				{#if p.body_source === 'readme'}
					<p class="label mb-6 text-ink-soft">From the README</p>
				{/if}
				<div class="prose max-w-[70ch]">
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitized server-rendered Markdown -->
					{@html p.body_html}
				</div>
			{:else}
				<div class="grid aspect-[16/7] place-items-center rounded-brand border border-ink text-on-spot" style:background={spotFor(p.slug)}>
					<p class="font-display text-2xl font-semibold">Write-up coming soon</p>
				</div>
			{/if}
		</div>

		<aside class="space-y-8 lg:sticky lg:top-6 lg:self-start" aria-label="Project details">
			{#if languages.length}
				<section aria-labelledby="lang-h">
					<h2 id="lang-h" class="label border-b border-ink pb-2 text-ink-soft">Languages</h2>
					<div class="mt-3 flex h-3 overflow-hidden rounded-full border border-ink" aria-hidden="true">
						{#each languages as l (l.name)}
							<span class="h-full border-r border-ink last:border-r-0" style:width="{l.pct}%" style:background={l.color}></span>
						{/each}
					</div>
					<ul class="mt-3 space-y-1.5 text-sm">
						{#each languages as l (l.name)}
							<li class="flex items-center gap-2">
								<span class="size-3 rounded-full border border-ink" style:background={l.color} aria-hidden="true"></span>
								<span class="flex-1">{l.name}</span>
								<span class="font-mono text-ink-soft tabular-nums">{pct(l.pct)}</span>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if tags.length}
				<section aria-labelledby="tech-h">
					<h2 id="tech-h" class="label border-b border-ink pb-2 text-ink-soft">Built with</h2>
					<ul class="mt-3 flex flex-wrap gap-x-1.5">
						{#each tags as t (t)}
							<li>
								<a href="/projects?tech={encodeURIComponent(t)}" class="inline-flex min-h-11 items-center">
									<span class="label rounded-full border border-rule-strong px-2.5 py-1 normal-case tracking-[0.02em]">{t}</span>
								</a>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if data.post}
				<section aria-labelledby="post-h">
					<h2 id="post-h" class="label border-b border-ink pb-2 text-ink-soft">The write-up</h2>
					<a href="/blog/{data.post.slug}" class="related print mt-3 block rounded-[10px] bg-paper-2 p-4 [--print:var(--spot-select)]">
						<span class="font-display text-lg leading-snug font-semibold">{data.post.title}</span>
						<span class="label mt-2 flex items-center gap-1.5 text-ink-soft">
							{data.post.reading_minutes || 1} min read <ArrowRight class="size-3.5" aria-hidden="true" />
						</span>
					</a>
				</section>
			{/if}
		</aside>
	</div>
</article>

<style>
	.stats {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 1.25rem 2rem;
	}
	.stats dd {
		margin-top: 0.25rem;
		font-family: var(--font-display);
		font-size: 2.25rem;
		font-weight: 600;
		letter-spacing: -0.02em;
		font-variant-numeric: tabular-nums;
	}
	.related {
		transition: transform 160ms ease-out;
	}
	.related:hover {
		transform: translate(-1px, -1px);
	}
</style>
