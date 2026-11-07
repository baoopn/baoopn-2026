<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { page } from '$app/state';
	import Seo from '#lib/site/components/Seo.svelte';
	import Toc from '#lib/site/components/Toc.svelte';
	import { absolute, count, dateLong, pad2, safeImage } from '#lib/site/format.js';
	import { categoryChip, categoryHref, categoryLabel, partHref, partLabel } from '#lib/site/post.js';
	import type { PostDetail } from '#lib/api/types.js';

	let { post, author }: { post: PostDetail; author: string } = $props();

	const part = $derived(post.part);
	const total = $derived(post.parts.length);
	const multi = $derived(total > 1);
	const first = $derived(part.no === 1);
	const cover = $derived(first ? safeImage(post.cover_url) : undefined);
	const updated = $derived(
		post.updated_at && dateLong(post.updated_at) !== dateLong(post.published_at)
			? post.updated_at
			: ''
	);
	const showToc = $derived(part.toc.length >= 2);
	const path = $derived(partHref(post.slug, part.no));
	const title = $derived(first ? post.title : `${partLabel(part)} · ${post.title}`);

	const schema = $derived.by(() => {
		const origin = page.url.origin;
		const url = origin + partHref(post.slug, 1);
		return {
			'@context': 'https://schema.org',
			'@type': 'BlogPosting',
			headline: post.title,
			description: post.excerpt,
			datePublished: post.published_at,
			dateModified: post.updated_at || post.published_at,
			image: absolute(origin, post.cover_url),
			keywords: post.tags.join(', ') || undefined,
			url,
			mainEntityOfPage: url,
			author: { '@type': 'Person', name: author, url: origin + '/' },
			hasPart: multi
				? post.parts.map((p) => ({
						'@type': 'WebPageElement',
						position: p.no,
						name: partLabel(p),
						url: origin + partHref(post.slug, p.no)
					}))
				: undefined
		};
	});
</script>

<Seo
	{title}
	description={post.excerpt}
	image={post.cover_url}
	type="article"
	{path}
	published={post.published_at}
	modified={post.updated_at}
	{schema}
/>

{#snippet partsList()}
	<ol class="mt-2">
		{#each post.parts as p (p.no)}
			{@const current = p.no === part.no}
			<li>
				<a
					href={partHref(post.slug, p.no)}
					class={[
						'flex min-h-11 items-center gap-3 rounded-[6px] px-2 py-1.5',
						current ? 'bg-spot-select font-semibold text-on-spot' : 'hover:bg-paper'
					]}
					aria-current={current ? 'page' : undefined}
				>
					<span class="label w-6 shrink-0" aria-hidden="true">{pad2(p.no)}</span>
					<span class="min-w-0">{partLabel(p)}</span>
				</a>
			</li>
		{/each}
	</ol>
{/snippet}

<article class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="mx-auto max-w-3xl pt-10 sm:pt-14">
		<div class="flex flex-wrap items-center gap-x-4">
			<a href="/blog" class="link label inline-flex min-h-11 items-center gap-1.5 text-ink-soft">
				<ArrowLeft class="size-4" aria-hidden="true" /> All posts
			</a>
			<a
				href={categoryHref(post.category)}
				class="inline-flex min-h-11 items-center"
				aria-label="More {categoryLabel(post.category).toLowerCase()} posts"
			>
				<span class="chip label" style:--chip={categoryChip(post.category)}>{categoryLabel(post.category)}</span>
			</a>
		</div>
		<h1 class="mt-4 font-display text-4xl leading-[1.05] font-semibold tracking-[-0.03em] text-balance break-words sm:text-5xl lg:text-6xl">
			{post.title}
		</h1>
		{#if multi}
			<p class="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2">
				<span class="chip label [--chip:var(--spot-lilac)]">Part {part.no} of {total}</span>
				{#if part.title.trim()}
					<span class="font-display text-xl font-semibold tracking-[-0.015em] text-balance sm:text-2xl">
						{part.title}
					</span>
				{/if}
			</p>
		{/if}
		{#if post.excerpt && first}
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
				<dd>
					{#if multi}
						{part.reading_minutes || 1} min this part · {post.reading_minutes || 1} min in all
					{:else}
						{post.reading_minutes || 1} min
					{/if}
				</dd>
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

	<div class={['body mt-10', (showToc || multi) && 'with-side']}>
		{#if multi}
			<nav aria-label="Parts" class="parts hidden xl:block">
				<p class="label border-b border-ink pb-2 text-ink-soft">Parts · {total}</p>
				{@render partsList()}
			</nav>
		{/if}

		<div class="main min-w-0">
			{#if multi}
				<details class="mb-4 rounded-brand border border-ink bg-paper-2 p-4 xl:hidden">
					<summary class="label flex min-h-11 cursor-pointer items-center">
						Parts · {part.no} of {total}
					</summary>
					<nav aria-label="Parts">{@render partsList()}</nav>
				</details>
			{/if}

			{#if showToc}
				<details class="mb-8 rounded-brand border border-ink p-4 xl:hidden">
					<summary class="label flex min-h-11 cursor-pointer items-center">On this page</summary>
					<ol class="mt-2 space-y-1">
						{#each part.toc as item (item.id)}
							<li style:padding-left="{Math.max(0, item.level - 2) * 0.85}rem">
								<a href="#{item.id}" class="link inline-flex min-h-11 items-center text-sm">{item.text}</a>
							</li>
						{/each}
					</ol>
				</details>
			{/if}

			<div class="prose">
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- sanitized server-rendered Markdown -->
				{@html part.content_html}
			</div>

			{#if post.prev || post.next}
				<nav aria-label="Parts of this post" class="pager mt-12 border-t border-ink pt-6">
					{#if post.prev}
						<a href={partHref(post.slug, post.prev.no)} class="pager-link" rel="prev">
							<span class="label flex items-center gap-1.5 text-ink-soft"><ArrowLeft class="size-3.5" aria-hidden="true" /> Part {post.prev.no}</span>
							<span class="mt-1 font-display text-lg leading-snug font-semibold">{partLabel(post.prev)}</span>
						</a>
					{:else}
						<span></span>
					{/if}
					{#if post.next}
						<a href={partHref(post.slug, post.next.no)} class="pager-link text-right" rel="next">
							<span class="label flex items-center justify-end gap-1.5 text-ink-soft">Part {post.next.no} <ArrowRight class="size-3.5" aria-hidden="true" /></span>
							<span class="mt-1 font-display text-lg leading-snug font-semibold">{partLabel(post.next)}</span>
						</a>
					{/if}
				</nav>
			{/if}
		</div>

		{#if showToc}
			<aside class="toc hidden xl:block" aria-label="Table of contents">
				<Toc items={part.toc} />
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
		.body.with-side {
			max-width: none;
			display: grid;
			/* Article centred under the header; parts on the left, TOC on the right. */
			/* Side columns get real widths so part titles wrap by phrase, not by
			   word; the article column gives way first. */
			grid-template-columns: minmax(11rem, 14rem) minmax(0, 48rem) minmax(10rem, 13rem);
			justify-content: center;
			gap: 2.5rem;
		}
		.body.with-side > .main {
			grid-column: 2;
			grid-row: 1;
		}
		.parts,
		.toc {
			grid-row: 1;
			width: 100%;
			max-width: 15rem;
			position: sticky;
			top: calc(var(--header-h) + 1.5rem);
			align-self: start;
			max-height: calc(100dvh - var(--header-h) - 3rem);
			overflow-y: auto;
		}
		.parts {
			grid-column: 1;
			justify-self: end;
		}
		.toc {
			grid-column: 3;
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
