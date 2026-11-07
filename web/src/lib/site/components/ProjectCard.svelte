<script lang="ts">
	import Star from '@lucide/svelte/icons/star';
	import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';
	import SocialIcon from './SocialIcon.svelte';
	import { imgIn } from '#lib/site/motion.js';
	import { count, safeImage, safeUrl, spotFor } from '#lib/site/format.js';
	import type { ProjectSummary } from '#lib/api/types.js';

	let { project, index }: { project: ProjectSummary; index?: number } = $props();

	const cover = $derived(safeImage(project.cover_url));
	const logo = $derived(safeImage(project.logo_url));
	const demo = $derived(safeUrl(project.demo_url));
	const repo = $derived(safeUrl(project.repo_url));
	const initials = $derived(
		project.title
			.split(/[\s\-_.]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => w[0]?.toUpperCase())
			.join('')
	);
	// The URL that failed to load, so a new URL gets a fresh try.
	let coverFailed = $state<string>();
	let logoFailed = $state<string>();
	const showCover = $derived(!!cover && cover !== coverFailed);
	const showLogo = $derived(!!logo && logo !== logoFailed);
	// Ink plate for light logos; otherwise the slug's pastel tile. Both read
	// the same in light and dark themes.
	const ink = $derived(showLogo && project.logo_bg === 'ink');
	const hasActions = $derived(!!demo || !!repo);
	const hasMeta = $derived(
		project.stars > 0 || project.archived || (!!project.language && showCover)
	);
</script>

<article class="card group flex h-full flex-col">
	<a
		href="/projects/{project.slug}"
		class="sleeve relative isolate block rounded-brand"
		tabindex="-1"
		aria-hidden="true"
	>
		{#if showCover}
			<img
				src={cover}
				alt=""
				width="800"
				height="500"
				loading="lazy"
				decoding="async"
				class="cover aspect-[8/5] w-full rounded-brand border border-ink bg-paper-2 object-cover"
				data-img-in
				{@attach imgIn()}
				onerror={() => (coverFailed = cover)}
			/>
		{:else}
			<div
				class="cover relative grid aspect-[8/5] w-full place-items-center overflow-hidden rounded-brand border border-ink text-on-spot"
				style:background={ink ? 'var(--logo-plate-dark)' : spotFor(project.slug)}
			>
				{#if showLogo}
					<img
						src={logo}
						alt=""
						width="320"
						height="200"
						loading="lazy"
						decoding="async"
						class="logo absolute inset-0 m-auto h-[56%] w-[72%] object-contain"
						data-img-in
						{@attach imgIn()}
						onerror={() => (logoFailed = logo)}
					/>
				{:else}
					<span class="font-display text-7xl font-semibold tracking-[-0.04em]">{initials}</span>
				{/if}
				{#if project.language}
					<span class={['label absolute bottom-3 left-3', ink && 'text-spot-butter']}
						>{project.language}</span
					>
				{/if}
				{#if index != null}
					<span class={['label absolute top-3 right-3', ink && 'text-spot-butter']}
						>No. {String(index + 1).padStart(2, '0')}</span
					>
				{/if}
			</div>
		{/if}
	</a>

	<div class="mt-4 flex items-baseline justify-between gap-3">
		<h3 class="min-w-0 font-display text-xl font-semibold tracking-[-0.01em]">
			<a href="/projects/{project.slug}" class="link">{project.title}</a>
		</h3>
		{#if project.period}
			<span class="label shrink-0 text-ink-soft">{project.period}</span>
		{/if}
	</div>
	{#if project.summary}
		<p class="mt-1.5 text-ink-soft">{project.summary}</p>
	{/if}

	{#if project.tech.length}
		<ul class="mt-3 flex flex-wrap gap-1.5" aria-label="Built with">
			{#each project.tech.slice(0, 6) as t (t)}
				<li class="label rounded-full border border-rule-strong px-2 py-0.5">{t}</li>
			{/each}
		</ul>
	{/if}

	{#if hasActions || hasMeta}
		<!-- mt-auto pins the footer to the card bottom so rows line up across the grid. -->
		<div class="mt-auto pt-4">
			<div class="flex flex-wrap items-center gap-2 border-t border-rule pt-3 text-sm">
				{#if demo}
					<a href={demo} target="_blank" rel="noopener noreferrer" class="action">
						<ArrowUpRight class="size-4" aria-hidden="true" />Live site<span class="sr-only"
							>{' '}for {project.title} (opens in a new tab)</span
						>
					</a>
				{/if}
				{#if repo}
					<a href={repo} target="_blank" rel="noopener noreferrer" class="action">
						<SocialIcon icon="github" class="size-4" />Code<span class="sr-only"
							>{' '}for {project.title} (opens in a new tab)</span
						>
					</a>
				{/if}
				{#if hasMeta}
					<span class="ml-auto flex items-center gap-x-3">
						{#if project.stars > 0}
							<span class="inline-flex items-center gap-1 font-mono tabular-nums text-ink-soft">
								<Star class="size-3.5" aria-hidden="true" />
								{count(project.stars)}<span class="sr-only"> stars</span>
							</span>
						{/if}
						{#if project.language && showCover}
							<span class="font-mono text-ink-soft">{project.language}</span>
						{/if}
						{#if project.archived}
							<span class="chip label px-2! py-0! [--chip:var(--spot-butter)]">Archived</span>
						{/if}
					</span>
				{/if}
			</div>
		</div>
	{/if}
</article>

<style>
	/* Hover lift: the cover rises and a flat print offset (a pseudo layer, so only
	   transform and opacity move) shows behind it. Same look as a 4px offset shadow. */
	.sleeve::before {
		content: '';
		position: absolute;
		inset: 0;
		z-index: -1;
		border-radius: inherit;
		background: var(--print-color);
		opacity: 0;
		transition:
			transform var(--motion-base) var(--ease-soft),
			opacity var(--motion-base) var(--ease-soft);
	}
	.cover,
	.logo {
		transition:
			transform var(--motion-base) var(--ease-soft),
			opacity var(--motion-base) var(--ease-soft);
	}
	.card:hover .sleeve::before,
	.card:focus-within .sleeve::before {
		opacity: 1;
		transform: translate(var(--lift), var(--lift));
	}
	.card:hover .cover,
	.card:focus-within .cover {
		transform: translate(calc(-1 * var(--lift)), calc(-1 * var(--lift)));
	}
	.card:hover .logo,
	.card:focus-within .logo {
		transform: scale(1.03);
	}
	/* Labelled pill links in the card footer; the small form of the project page buttons. */
	.action {
		position: relative;
		display: inline-flex;
		align-items: center;
		gap: 0.375rem;
		height: 2.25rem;
		padding: 0 0.8rem;
		border: 1px solid var(--ink);
		border-radius: 999px;
		background: var(--paper);
		color: var(--ink);
		font-family: var(--font-mono);
		font-size: 0.75rem;
		font-weight: 500;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		transition:
			background-color var(--motion-fast) var(--ease-soft),
			transform var(--motion-fast) var(--ease-soft);
	}
	/* 44px hit area; the 8px gap keeps neighbours from overlapping. */
	.action::after {
		content: '';
		position: absolute;
		inset: -5px;
	}
	.action:hover {
		background: var(--paper-2);
	}
	.action:active {
		transform: translate(1px, 1px);
	}
	.action:focus-visible {
		border-radius: 999px;
	}
</style>
