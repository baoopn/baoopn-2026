<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { page } from '$app/state';
	import Seo from '#lib/site/components/Seo.svelte';
	import Toc from '#lib/site/components/Toc.svelte';
	import { absolute, count, dateLong, pad2, safeImage } from '#lib/site/format.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const post = $derived(data.post);
	const cover = $derived(safeImage(post.cover_url));
	const updated = $derived(
		post.updated_at && dateLong(post.updated_at) !== dateLong(post.published_at)
			? post.updated_at
			: ''
	);
	const inSeries = $derived(post.series_posts.length > 1);
	const showToc = $derived(post.toc.length >= 2);

	const schema = $derived({
		'@context': 'https://schema.org',
		'@type': 'BlogPosting',
		headline: post.title,
		description: post.excerpt,
		datePublished: post.published_at,
		dateModified: post.updated_at || post.published_at,
		image: absolute(page.url.origin, post.cover_url),
		keywords: post.tags.join(', ') || undefined,
		url: page.url.origin + page.url.pathname,
		mainEntityOfPage: page.url.origin + page.url.pathname,
		author: { '@type': 'Person', name: data.profile.name, url: page.url.origin + '/' },
		isPartOf: post.series
			? { '@type': 'CreativeWorkSeries', name: post.series_title || post.series }
			: undefined
	});
</script>

<Seo
	title={post.title}
	description={post.excerpt}
	image={post.cover_url}
	type="article"
	published={post.published_at}
	modified={post.updated_at}
	{schema}
/>

{#snippet seriesNav()}
	<nav aria-labelledby="series-h" class="rounded-brand border border-ink bg-paper-2 p-4">
		<p id="series-h" class="label text-ink-soft">
			Series · {post.series_title || post.series}
		</p>
		<ol class="mt-2">
			{#each post.series_posts as sp, i (sp.slug)}
				{@const current = sp.slug === post.slug}
				<li>
					<a
						href="/blog/{sp.slug}"
						class={['flex min-h-11 items-center gap-3 rounded-[6px] px-2 py-1.5', current ? 'bg-spot-select font-semibold text-on-spot' : 'hover:bg-paper']}
						aria-current={current ? 'page' : undefined}
					>
						<span class="label w-6 shrink-0" aria-hidden="true">{pad2(sp.series_order || i + 1)}</span>
						<span class="min-w-0">{sp.title}</span>
					</a>
				</li>
			{/each}
		</ol>
	</nav>
{/snippet}

<article class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="mx-auto max-w-3xl pt-10 sm:pt-14">
		<a href="/blog" class="link label inline-flex min-h-11 items-center gap-1.5 text-ink-soft">
			<ArrowLeft class="size-4" aria-hidden="true" /> All posts
		</a>
		{#if post.series}
			<p class="mt-4">
				<a href="/blog?series={encodeURIComponent(post.series)}" class="chip label min-h-11 [--chip:var(--spot-lilac)]">
					{post.series_title || post.series}{post.series_order ? ` · Part ${post.series_order}` : ''}
					{#if inSeries}<span>of {post.series_posts.length}</span>{/if}
				</a>
			</p>
		{/if}
		<h1 class="mt-4 font-display text-4xl leading-[1.05] font-semibold tracking-[-0.03em] text-balance break-words sm:text-5xl lg:text-6xl">
			{post.title}
		</h1>
		{#if post.excerpt}
			<p class="mt-4 text-lg text-ink-soft sm:text-xl">{post.excerpt}</p>
		{/if}
		<dl class="meta mt-6 border-y border-rule py-3">
			<div>
				<dt class="label text-ink-soft">Published</dt>
				<dd><time datetime={post.published_at}>{dateLong(post.published_at)}</time></dd>
			</div>
			{#if updated}
				<div>
					<dt class="label text-ink-soft">Updated</dt>
					<dd><time datetime={updated}>{dateLong(updated)}</time></dd>
				</div>
			{/if}
			<div>
				<dt class="label text-ink-soft">Reading</dt>
				<dd>{post.reading_minutes || 1} min</dd>
			</div>
			{#if post.views > 0}
				<div>
					<dt class="label text-ink-soft">Views</dt>
					<dd class="font-mono tabular-nums">{count(post.views)}</dd>
				</div>
			{/if}
		</dl>
		{#if post.tags.length}
			<ul class="mt-2 flex flex-wrap gap-x-1.5" aria-label="Tags">
				{#each post.tags as tag (tag)}
					<li>
						<a href="/blog?tag={encodeURIComponent(tag)}" class="inline-flex min-h-11 items-center">
							<span class="label rounded-full border border-rule-strong px-2.5 py-1 normal-case tracking-[0.02em] hover:bg-paper-2">#{tag}</span>
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</header>

	{#if cover}
		<img
			src={cover}
			alt=""
			width="1600"
			height="840"
			fetchpriority="high"
			class="print mx-auto mt-8 aspect-[40/21] w-full max-w-5xl rounded-brand bg-paper-2 object-cover [--print:var(--spot-now)]"
		/>
	{/if}

	<div class={['body mt-10', showToc && 'with-toc']}>
		<div class="min-w-0">
			{#if showToc}
				<details class="mb-8 rounded-brand border border-ink p-4 xl:hidden">
					<summary class="label flex min-h-11 cursor-pointer items-center">On this page</summary>
					<ol class="mt-2 space-y-1">
						{#each post.toc as item (item.id)}
							<li style:padding-left="{Math.max(0, item.level - 2) * 0.85}rem">
								<a href="#{item.id}" class="link inline-flex min-h-11 items-center text-sm">{item.text}</a>
							</li>
						{/each}
					</ol>
				</details>
			{/if}

			<div class="prose">
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitized server-rendered Markdown -->
				{@html post.content_html}
			</div>

			{#if inSeries}
				<div class="mt-12">{@render seriesNav()}</div>
			{/if}

			{#if post.prev || post.next}
				<nav aria-label="More posts" class="pager mt-12 border-t border-ink pt-6">
					{#if post.prev}
						<a href="/blog/{post.prev.slug}" class="pager-link" rel="prev">
							<span class="label flex items-center gap-1.5 text-ink-soft"><ArrowLeft class="size-3.5" aria-hidden="true" /> Previous</span>
							<span class="mt-1 font-display text-lg leading-snug font-semibold">{post.prev.title}</span>
						</a>
					{:else}
						<span></span>
					{/if}
					{#if post.next}
						<a href="/blog/{post.next.slug}" class="pager-link text-right" rel="next">
							<span class="label flex items-center justify-end gap-1.5 text-ink-soft">Next <ArrowRight class="size-3.5" aria-hidden="true" /></span>
							<span class="mt-1 font-display text-lg leading-snug font-semibold">{post.next.title}</span>
						</a>
					{/if}
				</nav>
			{/if}
		</div>

		{#if showToc}
			<aside class="toc hidden xl:block" aria-label="Table of contents">
				<Toc items={post.toc} />
			</aside>
		{/if}
	</div>
</article>

<style>
	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: 0.75rem 2.25rem;
	}
	.meta dd {
		margin-top: 0.15rem;
		font-weight: 500;
	}
	.body {
		max-width: 48rem;
		margin-inline: auto;
	}
	@media (min-width: 80rem) {
		.body.with-toc {
			max-width: none;
			display: grid;
			/* Article centred under the header; the TOC sits in the right gutter. */
			grid-template-columns: minmax(0, 1fr) minmax(0, 48rem) minmax(0, 1fr);
			gap: 2.5rem;
		}
		.body.with-toc > :first-child {
			grid-column: 2;
		}
		.toc {
			grid-column: 3;
			max-width: 15rem;
			position: sticky;
			top: 1.5rem;
			align-self: start;
			max-height: calc(100dvh - 3rem);
			overflow-y: auto;
		}
	}
	.pager {
		display: grid;
		gap: 1rem;
	}
	@media (min-width: 40rem) {
		.pager {
			grid-template-columns: 1fr 1fr;
		}
	}
	.pager-link {
		display: flex;
		flex-direction: column;
		min-height: 2.75rem;
		padding: 0.75rem;
		border-radius: 10px;
		transition: background-color 160ms ease-out;
	}
	.pager-link:hover {
		background: var(--paper-2);
	}
</style>
