<script lang="ts">
	import { page } from '$app/state';
	import Seo from '#lib/site/components/Seo.svelte';
	import ProjectCard from '#lib/site/components/ProjectCard.svelte';
	import { pad2 } from '#lib/site/format.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const MAX_CHIPS = 18;

	/** Tech chips, most used first. */
	const techs = $derived.by(() => {
		const counts: Record<string, number> = {};
		for (const p of data.projects) for (const t of p.tech) counts[t] = (counts[t] ?? 0) + 1;
		return Object.entries(counts)
			.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
			.slice(0, MAX_CHIPS)
			.map(([tech, n]) => ({ tech, n }));
	});
	const active = $derived(page.url.searchParams.get('tech') ?? '');
	const shown = $derived(
		active
			? data.projects.filter((p) => p.tech.some((t) => t.toLowerCase() === active.toLowerCase()))
			: data.projects
	);
</script>

<Seo
	title="Projects"
	description="Things Bao Nguyen has built: apps, APIs, tools and experiments, with source and write-ups."
/>

<div class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="border-b border-ink pt-12 pb-6 sm:pt-16">
		<p class="label text-ink-soft">Side A · Selected work</p>
		<h1 class="mt-3 font-display text-5xl font-semibold tracking-[-0.035em] sm:text-6xl">Projects</h1>
		<p class="mt-3 max-w-2xl text-lg text-ink-soft">
			Apps, APIs and experiments. Most are open source; some have a write-up on the blog.
		</p>
	</header>

	{#if techs.length > 0}
		<nav aria-label="Filter by technology" class="border-b border-rule py-3">
			<ul class="flex flex-wrap gap-x-1.5">
				<li>
					<a href="/projects" class="hit" data-sveltekit-noscroll aria-current={active ? undefined : 'page'}>
						<span class="pill label">All <span class="text-ink-soft">{pad2(data.projects.length)}</span></span>
					</a>
				</li>
				{#each techs as { tech, n } (tech)}
					{@const on = active.toLowerCase() === tech.toLowerCase()}
					<li>
						<a
							href="/projects?tech={encodeURIComponent(tech)}"
							class="hit"
							data-sveltekit-noscroll
							aria-current={on ? 'page' : undefined}
						>
							<span class="pill label">{tech} <span class="count">{pad2(n)}</span></span>
						</a>
					</li>
				{/each}
			</ul>
		</nav>
	{/if}

	<p class="sr-only" aria-live="polite">{shown.length} projects shown</p>

	{#if data.projects.length === 0}
		<p class="py-16 text-lg text-ink-soft">No projects to show right now. Check back soon.</p>
	{:else if shown.length === 0}
		<div class="py-16">
			<p class="text-lg text-ink-soft">Nothing tagged “{active}”.</p>
			<a href="/projects" class="btn btn-secondary mt-4">Show all projects</a>
		</div>
	{:else}
		<ul class="mt-10 grid gap-x-8 gap-y-14 sm:grid-cols-2 lg:grid-cols-3">
			{#each shown as project, i (project.slug)}
				<li><ProjectCard {project} index={i} /></li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.hit {
		display: inline-flex;
		align-items: center;
		min-height: 2.75rem;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		min-height: 2rem;
		padding: 0 0.75rem;
		border-radius: 999px;
		border: 1px solid var(--rule-strong);
		text-transform: none;
		letter-spacing: 0.02em;
		transition: background-color 160ms ease-out;
	}
	.hit:hover .pill {
		background: var(--paper-2);
	}
	.hit[aria-current='page'] .pill {
		background: var(--spot-select);
		color: var(--on-spot);
		border-color: var(--on-spot);
	}
	.count {
		opacity: 0.75;
	}
</style>
