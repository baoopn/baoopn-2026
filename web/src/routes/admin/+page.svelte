<script lang="ts">
	import { LoaderCircle, Plus, RefreshCw } from '@lucide/svelte';
	import type { Overview, SyncResponse } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import KpiRow, { type Kpi } from '#lib/admin/components/KpiRow.svelte';
	import BarTable from '#lib/admin/components/BarTable.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import EmptyState from '#lib/admin/components/EmptyState.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import { get, post } from '#lib/admin/api.js';
	import { Resource } from '#lib/admin/resource.svelte.js';
	import { SYNC_RUNNING_MESSAGE, compact, errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';
	import { auth } from '#lib/admin/auth.svelte.js';

	const overview = new Resource(() => get<Overview>('/overview'));

	$effect(() => {
		void overview.load();
	});

	const o = $derived(overview.data);

	const kpis: Kpi[] = $derived(
		o
			? [
					{
						label: 'Unread',
						value: compact(o.unread_threads),
						sub: `${compact(o.open_threads)} open threads`,
						href: '/admin/inbox'
					},
					{
						label: 'Published',
						value: compact(o.posts.published),
						sub: `${o.posts.drafts} drafts · ${o.posts.scheduled} scheduled`,
						href: '/admin/posts'
					},
					{
						label: 'Projects',
						value: `${o.projects.visible}/${o.projects.total}`,
						sub: 'visible / total',
						href: '/admin/projects'
					},
					{ label: 'Views · 7d', value: compact(o.views_7d), href: '/admin/analytics' },
					{ label: 'Visitors · 7d', value: compact(o.visitors_7d) },
					{ label: 'Live now', value: compact(o.live_now), live: o.live_now > 0 }
				]
			: ['Unread', 'Published', 'Projects', 'Views · 7d', 'Visitors · 7d', 'Live now'].map(
					(label) => ({ label, value: '' })
				)
	);

	let syncing = $state(false);
	async function sync() {
		syncing = true;
		try {
			const r = await post<SyncResponse>('/projects/sync');
			if ('status' in r) {
				toasts.info('GitHub sync started', SYNC_RUNNING_MESSAGE);
			} else {
				toasts.success(
					'GitHub synced',
					`${r.added} added, ${r.updated} updated, ${r.total} repos in total.`
				);
			}
			void overview.load(true);
		} catch (err) {
			toasts.error('Sync failed', errorMessage(err));
		} finally {
			syncing = false;
		}
	}

</script>

<PageHeader
	title="Overview"
	eyebrow="baoopn.com"
	description="What changed while you were away."
>
	{#snippet actions()}
		<button type="button" class="btn btn-secondary" onclick={sync} disabled={syncing}>
			{#if syncing}
				<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
			{:else}
				<RefreshCw class="size-4" aria-hidden="true" />
			{/if}
			Sync GitHub
		</button>
		<a href="/admin/posts/new" class="btn btn-primary">
			<Plus class="size-4" aria-hidden="true" /> New post
		</a>
	{/snippet}
</PageHeader>

{#if overview.error && !o}
	<ErrorState message={overview.error} onretry={() => overview.load()} />
{:else}
	<KpiRow items={kpis} loading={!o} label="Site at a glance" />

	<div class="mt-10 grid gap-10 lg:grid-cols-[3fr_2fr]">
		<section aria-labelledby="recent-h">
			<div class="mb-3 flex items-baseline justify-between gap-3">
				<h2 id="recent-h" class="font-display text-2xl font-semibold">Recent threads</h2>
				<a href="/admin/inbox" class="link text-sm text-ink-soft">Open inbox</a>
			</div>
			{#if !o}
				<ul class="flex flex-col gap-3" aria-hidden="true">
					{#each [1, 2, 3, 4] as i (i)}
						<li class="skeleton h-14"></li>
					{/each}
				</ul>
			{:else if o.recent_threads.length === 0}
				<EmptyState title="Inbox zero" message="Messages from the contact form land here." />
			{:else}
				<ul class="border-t border-ink">
					{#each o.recent_threads as t (t.id)}
						<li class="border-b border-rule">
							<a
								href="/admin/inbox/{t.id}"
								class="flex min-h-14 items-center gap-3 px-1 py-2.5 hover:bg-paper-3"
							>
								<span
									class={[
										'size-2.5 shrink-0 rounded-full border',
										t.unread ? 'border-on-spot bg-spot-now' : 'border-transparent'
									]}
									aria-hidden="true"
								></span>
								<span class="min-w-0 flex-1">
									<span class="flex items-baseline gap-2">
										<span class={['truncate', t.unread ? 'font-semibold' : 'font-medium']}
											>{t.name}</span
										>
										<span class="truncate text-sm text-ink-soft">{t.subject}</span>
									</span>
									<span class="block truncate text-sm text-ink-soft">
										{t.last_direction === 'outbound' ? 'You: ' : ''}{t.last_preview}
									</span>
								</span>
								<RelTime value={t.last_message_at} class="font-mono text-xs text-ink-soft" />
								{#if t.unread}<span class="sr-only">Unread</span>{/if}
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</section>

		<section aria-labelledby="paths-h">
			<div class="mb-3 flex items-baseline justify-between gap-3">
				<h2 id="paths-h" class="font-display text-2xl font-semibold">Top pages · 7d</h2>
				<a href="/admin/analytics" class="link text-sm text-ink-soft">Analytics</a>
			</div>
			{#if !o}
				<div class="skeleton h-48" aria-hidden="true"></div>
			{:else}
				<BarTable
					caption="Top pages in the last 7 days"
					keyLabel="Path"
					rows={o.top_paths_7d.map((p) => ({
						key: p.key,
						label: p.key,
						href: p.key,
						views: p.views,
						visitors: p.visitors
					}))}
				/>
			{/if}
			{#if o}
				<p class="mt-6 font-mono text-xs text-ink-soft">
					GitHub last synced <RelTime value={o.last_sync_at} />
				</p>
			{/if}
		</section>
	</div>
{/if}
