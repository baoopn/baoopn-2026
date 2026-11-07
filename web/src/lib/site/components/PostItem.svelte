<script lang="ts">
	import { dateShort, count } from '#lib/site/format.js';
	import type { PostSummary } from '#lib/api/types.js';

	let { post, compact = false }: { post: PostSummary; compact?: boolean } = $props();
</script>

<article class="item">
	<div class="meta label text-ink-soft">
		<time datetime={post.published_at}>{dateShort(post.published_at)}</time>
		<span>{post.reading_minutes || 1} min read</span>
	</div>
	<div class="min-w-0">
		{#if post.part_count > 1}
			<p class="mb-1.5">
				<span class="chip label [--chip:var(--spot-lilac)]">{post.part_count} parts</span>
			</p>
		{/if}
		<h3 class={['font-display font-semibold tracking-[-0.015em] text-balance', compact ? 'text-xl' : 'text-2xl']}>
			<a href="/blog/{post.slug}" class="link">{post.title}</a>
		</h3>
		{#if post.excerpt && !compact}
			<p class="mt-1.5 text-ink-soft">{post.excerpt}</p>
		{/if}
		{#if !compact && (post.tags.length || post.views)}
			<div class="mt-1 flex flex-wrap items-center gap-x-1.5">
				{#each post.tags as tag (tag)}
					<a href="/blog?tag={encodeURIComponent(tag)}" class="hit"><span class="tag label">#{tag}</span></a>
				{/each}
				{#if post.views}
					<span class="label ml-1 text-ink-soft">{count(post.views)} views</span>
				{/if}
			</div>
		{/if}
	</div>
</article>

<style>
	.item {
		display: grid;
		gap: 0.4rem 2rem;
		padding-block: 1.25rem;
		border-bottom: 1px solid var(--rule);
	}
	.meta {
		display: flex;
		gap: 1rem;
	}
	@media (min-width: 40rem) {
		.item {
			grid-template-columns: 9rem minmax(0, 1fr);
		}
		.meta {
			flex-direction: column;
			gap: 0.25rem;
			padding-top: 0.35rem;
		}
	}
	/* 44px hit area around a smaller pill */
	.hit {
		display: inline-flex;
		align-items: center;
		min-height: 2.75rem;
	}
	.tag {
		display: inline-flex;
		align-items: center;
		min-height: 1.75rem;
		padding: 0 0.6rem;
		border-radius: 999px;
		border: 1px solid var(--rule-strong);
		text-transform: none;
		transition: background-color 160ms ease-out;
	}
	.hit:hover .tag {
		background: var(--paper-2);
	}
</style>
