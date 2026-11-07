<script lang="ts">
	import ChevronLeft from '@lucide/svelte/icons/chevron-left';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import Rss from '@lucide/svelte/icons/rss';
	import Seo from '#lib/site/components/Seo.svelte';
	import PostItem from '#lib/site/components/PostItem.svelte';
	import { pad2 } from '#lib/site/format.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	function href(next: { tag?: string; page?: number }): string {
		const q = new URLSearchParams();
		const tag = next.tag ?? data.tag;
		if (tag) q.set('tag', tag);
		if (next.page && next.page > 1) q.set('page', String(next.page));
		const s = q.toString();
		return s ? `/blog?${s}` : '/blog';
	}

	const heading = $derived(data.tag ? `Tagged #${data.tag}` : 'Blog');
</script>

<Seo
	title={data.tag ? heading : 'Blog'}
	description="Notes on building software: case studies, how-tos and lessons from Bao Nguyen's projects."
	noindex={data.page > 1 && !data.posts.length}
/>

<div class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="flex flex-wrap items-end justify-between gap-4 border-b border-ink pt-12 pb-6 sm:pt-16">
		<div class="min-w-0">
			<p class="label text-ink-soft">Side B · Writing</p>
			<h1 class="mt-3 font-display text-5xl font-semibold tracking-[-0.035em] break-words sm:text-6xl">
				{heading}
			</h1>
			{#if data.tag}
				<a href="/blog" class="link mt-2 inline-flex min-h-11 items-center text-ink-soft">Clear filter</a>
			{:else}
				<p class="mt-3 max-w-2xl text-lg text-ink-soft">
					Case studies, how-tos and things learned the hard way.
				</p>
			{/if}
		</div>
		<a href="/rss.xml" class="btn btn-secondary"><Rss class="size-4" aria-hidden="true" /> RSS</a>
	</header>

	<div class="grid gap-10 pt-6 lg:grid-cols-[minmax(0,1fr)_16rem]">
		<section aria-label="Posts" class="min-w-0">
			{#if data.posts.length === 0}
				<div class="py-14">
					<p class="text-lg text-ink-soft">
						{data.tag ? 'No posts match this filter.' : 'No posts yet. Check back soon.'}
					</p>
					{#if data.tag || data.page > 1}
						<a href="/blog" class="btn btn-secondary mt-4">See all posts</a>
					{/if}
				</div>
			{:else}
				<ul>
					{#each data.posts as post (post.slug)}
						<li><PostItem {post} /></li>
					{/each}
				</ul>
			{/if}

			{#if data.pages > 1}
				<nav aria-label="Pagination" class="mt-8 flex items-center justify-between gap-4">
					{#if data.page > 1}
						<a href={href({ page: data.page - 1 })} class="btn btn-secondary" rel="prev">
							<ChevronLeft class="size-4" aria-hidden="true" /> Newer
						</a>
					{:else}
						<span></span>
					{/if}
					<p class="label text-ink-soft">Page {pad2(data.page)} of {pad2(data.pages)}</p>
					{#if data.page < data.pages}
						<a href={href({ page: data.page + 1 })} class="btn btn-secondary" rel="next">
							Older <ChevronRight class="size-4" aria-hidden="true" />
						</a>
					{:else}
						<span></span>
					{/if}
				</nav>
			{/if}
		</section>

		{#if data.tags.length}
			<aside aria-labelledby="tags-h" class="lg:sticky lg:top-6 lg:self-start">
				<h2 id="tags-h" class="label border-b border-ink pb-2 text-ink-soft">Browse by tag</h2>
				<ul class="mt-2 flex flex-wrap gap-x-1.5 lg:flex-col">
					{#each data.tags as t (t.tag)}
						{@const on = data.tag === t.tag}
						<li>
							<a
								href={on ? href({ tag: '', page: 1 }) : href({ tag: t.tag, page: 1 })}
								class="tag"
								aria-current={on ? 'page' : undefined}
							>
								<span>#{t.tag}</span>
								<span class="font-mono text-xs tabular-nums">{pad2(t.count)}</span>
							</a>
						</li>
					{/each}
				</ul>
			</aside>
		{/if}
	</div>
</div>

<style>
	.tag {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		min-height: 2.75rem;
		padding: 0 0.75rem;
		border-radius: 8px;
		transition: background-color 160ms ease-out;
	}
	.tag:hover {
		background: var(--paper-2);
	}
	.tag[aria-current='page'] {
		background: var(--spot-select);
		color: var(--on-spot);
	}
</style>
