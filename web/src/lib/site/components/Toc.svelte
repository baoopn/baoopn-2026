<script lang="ts">
	import type { Attachment } from 'svelte/attachments';
	import type { TocItem } from '#lib/api/types.js';

	let { items }: { items: TocItem[] } = $props();

	let active = $state('');
	const minLevel = $derived(Math.min(...items.map((i) => i.level), 4));

	/** Highlights the heading nearest the top of the viewport. */
	function spy(list: TocItem[]): Attachment {
		return () => {
			const visible: Record<string, boolean> = {};
			const io = new IntersectionObserver(
				(entries) => {
					for (const e of entries) visible[e.target.id] = e.isIntersecting;
					const first = list.find((i) => visible[i.id]);
					if (first) active = first.id;
				},
				{ rootMargin: '0px 0px -70% 0px' }
			);
			for (const i of list) {
				const el = document.getElementById(i.id);
				if (el) io.observe(el);
			}
			return () => io.disconnect();
		};
	}
</script>

<nav aria-labelledby="toc-h" {@attach spy(items)}>
	<h2 id="toc-h" class="label border-b border-ink pb-2 text-ink-soft">On this page</h2>
	<ol class="mt-2">
		{#each items as item (item.id)}
			<li style:padding-left="{(item.level - minLevel) * 0.85}rem">
				<a
					href="#{item.id}"
					class="toc-link"
					aria-current={active === item.id ? 'location' : undefined}>{item.text}</a
				>
			</li>
		{/each}
	</ol>
</nav>

<style>
	.toc-link {
		display: flex;
		align-items: center;
		min-height: 2.75rem;
		padding: 0.35rem 0 0.35rem 0.75rem;
		border-left: 2px solid var(--rule);
		font-size: 0.875rem;
		line-height: 1.35;
		color: var(--ink-soft);
		transition:
			color 160ms ease-out,
			border-color 160ms ease-out;
	}
	.toc-link:hover {
		color: var(--ink);
	}
	.toc-link[aria-current='location'] {
		color: var(--ink);
		font-weight: 600;
		border-left-color: var(--ink);
	}
</style>
