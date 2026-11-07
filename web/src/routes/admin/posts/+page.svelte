<script lang="ts">
	import { ExternalLink, NotebookPen, Pencil, Plus } from '@lucide/svelte';
	import type { AdminPost, PostState } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import Segmented from '#lib/admin/components/Segmented.svelte';
	import SearchInput from '#lib/admin/components/SearchInput.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import EmptyState from '#lib/admin/components/EmptyState.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import StateChip from '#lib/admin/components/StateChip.svelte';
	import { get, qs } from '#lib/admin/api.js';
	import { Resource } from '#lib/admin/resource.svelte.js';
	import { dateTime, num } from '#lib/admin/format.js';

	type Filter = 'all' | PostState;
	let filter = $state<Filter>('all');
	let q = $state('');

	const posts = new Resource(() =>
		get<AdminPost[]>('/posts' + qs({ state: filter === 'all' ? '' : filter, q }))
	);

	$effect(() => {
		void posts.load();
	});

	const options: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'draft', label: 'Drafts' },
		{ value: 'scheduled', label: 'Scheduled' },
		{ value: 'published', label: 'Published' }
	];

	const summary = $derived(
		posts.data ? `${posts.data.length} ${posts.data.length === 1 ? 'post' : 'posts'}` : 'Loading…'
	);
</script>

<PageHeader title="Posts" eyebrow="Blog" description={summary}>
	{#snippet actions()}
		<a href="/admin/posts/new" class="btn btn-primary">
			<Plus class="size-4" aria-hidden="true" /> New post
		</a>
	{/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
	<Segmented {options} bind:value={filter} label="Filter by state" />
	<SearchInput label="Search posts" placeholder="Search title, tags…" onsearch={(v) => (q = v)} />
</div>

{#if posts.error && !posts.data}
	<ErrorState message={posts.error} onretry={() => posts.load()} />
{:else if !posts.data}
	<div class="flex flex-col gap-2" aria-busy="true">
		{#each [1, 2, 3, 4, 5] as i (i)}
			<div class="skeleton h-14"></div>
		{/each}
		<span class="sr-only">Loading posts</span>
	</div>
{:else if posts.data.length === 0}
	<EmptyState
		icon={NotebookPen}
		title={q || filter !== 'all' ? 'No posts match' : 'No posts yet'}
		message={q || filter !== 'all'
			? 'Try another filter or search.'
			: 'Write the first one; drafts stay private until you publish.'}
	>
		<a href="/admin/posts/new" class="btn btn-primary">
			<Plus class="size-4" aria-hidden="true" /> New post
		</a>
	</EmptyState>
{:else}
	<div class="overflow-x-auto" aria-busy={posts.loading}>
		<table class="data-table">
			<caption class="sr-only">Posts</caption>
			<thead>
				<tr>
					<th scope="col">Title</th>
					<th scope="col">State</th>
					<th scope="col" class="num">Parts</th>
					<th scope="col" class="num">Views</th>
					<th scope="col">Updated</th>
					<th scope="col"><span class="sr-only">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#each posts.data as p (p.id)}
					<tr>
						<td class="max-w-[28rem]">
							<a href="/admin/posts/{p.id}" class="link font-medium">{p.title || '(untitled)'}</a>
							<p class="mt-0.5 flex flex-wrap items-center gap-1.5 font-mono text-xs text-ink-soft">
								<span class="truncate">/{p.slug}</span>
								{#each p.tags.slice(0, 3) as tag (tag)}
									<span class="tag">{tag}</span>
								{/each}
							</p>
						</td>
						<td>
							<StateChip state={p.state} />
							{#if p.state === 'scheduled' && p.published_at}
								<p class="mt-1 font-mono text-xs text-ink-soft">{dateTime(p.published_at)}</p>
							{/if}
						</td>
						<td class="num font-mono tabular-nums">{num(p.parts?.length || 1)}</td>
						<td class="num font-mono tabular-nums">{num(p.views)}</td>
						<td class="font-mono text-sm text-ink-soft"><RelTime value={p.updated_at} /></td>
						<td>
							<div class="flex justify-end gap-1">
								<a
									href="/admin/posts/{p.id}"
									class="btn btn-ghost btn-icon btn-sm"
									aria-label="Edit {p.title}"
									title="Edit"
								>
									<Pencil class="size-4" aria-hidden="true" />
								</a>
								{#if p.state === 'published'}
									<a
										href="/blog/{p.slug}"
										target="_blank"
										rel="noopener"
										class="btn btn-ghost btn-icon btn-sm"
										aria-label="View {p.title} on the site"
										title="View on site"
									>
										<ExternalLink class="size-4" aria-hidden="true" />
									</a>
								{/if}
							</div>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
