<script lang="ts">
	import { FolderGit2, LoaderCircle, Plus, RefreshCw, X } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import type { AdminProject, ProjectPatch, SyncResponse } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import Segmented from '#lib/admin/components/Segmented.svelte';
	import SearchInput from '#lib/admin/components/SearchInput.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import EmptyState from '#lib/admin/components/EmptyState.svelte';
	import Dialog from '#lib/admin/components/Dialog.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import ProjectRow from '#lib/admin/projects/ProjectRow.svelte';
	import { get, patch, post } from '#lib/admin/api.js';
	import { Resource } from '#lib/admin/resource.svelte.js';
	import { SYNC_RUNNING_MESSAGE, errorMessage, slugify } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	type Filter = 'all' | 'visible' | 'hidden' | 'forks' | 'archived';

	const projects = new Resource(async () =>
		(await get<AdminProject[]>('/projects')).sort((a, b) => a.sort_order - b.sort_order)
	);
	$effect(() => {
		void projects.load();
	});

	let filter = $state<Filter>('all');
	let q = $state('');

	function matches(p: AdminProject): boolean {
		if (filter === 'visible' && !p.visible) return false;
		if (filter === 'hidden' && p.visible) return false;
		if (filter === 'forks' && !p.gh_fork) return false;
		if (filter === 'archived' && !p.gh_archived) return false;
		if (!q) return true;
		const hay = [p.title, p.repo_full_name, p.slug, p.gh_description, p.gh_language, ...p.tech]
			.join(' ')
			.toLowerCase();
		return hay.includes(q.toLowerCase());
	}

	const all = $derived(projects.data ?? []);
	const shown = $derived(all.filter(matches));
	const counts = $derived({
		visible: all.filter((p) => p.visible).length,
		total: all.length
	});
	const lastSync = $derived(
		all.reduce<string | null>(
			(acc, p) => (p.synced_at && (!acc || p.synced_at > acc) ? p.synced_at : acc),
			null
		)
	);

	const options: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'visible', label: 'Visible' },
		{ value: 'hidden', label: 'Hidden' },
		{ value: 'forks', label: 'Forks' },
		{ value: 'archived', label: 'Archived' }
	];

	// ---- inline toggles ------------------------------------------------------
	async function toggle(p: AdminProject, field: 'visible' | 'featured', val: boolean) {
		const before = all;
		projects.set(all.map((x) => (x.id === p.id ? { ...x, [field]: val } : x)));
		try {
			const body: ProjectPatch = { [field]: val };
			const updated = await patch<AdminProject>(`/projects/${p.id}`, body);
			projects.set((projects.data ?? []).map((x) => (x.id === p.id ? updated : x)));
		} catch (err) {
			projects.set(before);
			toasts.error('Could not update the project', errorMessage(err));
		}
	}

	// ---- reorder -------------------------------------------------------------
	let saveTimer: ReturnType<typeof setTimeout> | undefined;

	/** Move `id` next to `targetId` in the full ordering (before when moving up). */
	function move(id: number, targetId: number, after: boolean) {
		if (id === targetId) return;
		const list = [...all];
		const from = list.findIndex((p) => p.id === id);
		const [item] = list.splice(from, 1);
		let to = list.findIndex((p) => p.id === targetId);
		if (after) to += 1;
		list.splice(to, 0, item);
		projects.set(list.map((p, i) => ({ ...p, sort_order: i })));
		clearTimeout(saveTimer);
		saveTimer = setTimeout(saveOrder, 500);
	}

	async function saveOrder() {
		try {
			await post('/projects/reorder', { ids: all.map((p) => p.id) });
			toasts.success('Order saved');
		} catch (err) {
			toasts.error('Could not save the order', errorMessage(err));
			void projects.load(true);
		}
	}

	function step(p: AdminProject, dir: -1 | 1) {
		const i = shown.findIndex((x) => x.id === p.id);
		const target = shown[i + dir];
		if (target) move(p.id, target.id, dir === 1);
	}

	let dragId = $state<number | null>(null);

	// ---- sync ----------------------------------------------------------------
	let syncing = $state(false);
	let syncResult = $state<SyncResponse | null>(null);
	async function sync() {
		syncing = true;
		try {
			syncResult = await post<SyncResponse>('/projects/sync');
			await projects.load(true);
		} catch (err) {
			toasts.error('GitHub sync failed', errorMessage(err));
		} finally {
			syncing = false;
		}
	}

	// ---- add manual ----------------------------------------------------------
	let adding = $state(false);
	let newTitle = $state('');
	let newSlug = $state('');
	let slugTouched = $state(false);
	let creating = $state(false);
	let createError = $state<string | null>(null);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		if (!newTitle.trim()) {
			createError = 'A title is required.';
			return;
		}
		creating = true;
		createError = null;
		try {
			const p = await post<AdminProject>('/projects', {
				title: newTitle.trim(),
				slug: newSlug.trim() || undefined
			});
			adding = false;
			toasts.success('Project created', 'It starts hidden. Fill it in, then make it visible.');
			await goto(`/admin/projects/${p.id}`);
		} catch (err) {
			createError = errorMessage(err);
		} finally {
			creating = false;
		}
	}
</script>

<PageHeader
	title="Projects"
	eyebrow="Portfolio"
	description={projects.data
		? `${counts.visible} visible of ${counts.total} · synced ${lastSync ? new Date(lastSync).toLocaleString() : 'never'}`
		: 'Loading…'}
>
	{#snippet actions()}
		<button type="button" class="btn btn-secondary" onclick={sync} disabled={syncing}>
			{#if syncing}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<RefreshCw
					class="size-4"
					aria-hidden="true"
				/>{/if}
			Sync from GitHub
		</button>
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => {
				newTitle = '';
				newSlug = '';
				slugTouched = false;
				createError = null;
				adding = true;
			}}
		>
			<Plus class="size-4" aria-hidden="true" /> Add manual project
		</button>
	{/snippet}
</PageHeader>

{#if syncResult}
	<div
		class="mb-5 flex flex-wrap items-center gap-3 rounded-lg border border-on-spot bg-spot-mint px-4 py-3 text-sm text-on-spot"
		role="status"
	>
		<span class="flex-1">
			{#if 'status' in syncResult}
				{SYNC_RUNNING_MESSAGE}
			{:else}
				Synced <strong>{syncResult.total}</strong> repos: {syncResult.added} added (hidden until you
				show them), {syncResult.updated} updated · <RelTime value={syncResult.synced_at} />
			{/if}
		</span>
		<button
			type="button"
			class="btn btn-ghost btn-icon btn-sm"
			aria-label="Dismiss"
			onclick={() => (syncResult = null)}
		>
			<X class="size-4" aria-hidden="true" />
		</button>
	</div>
{/if}

<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
	<Segmented {options} bind:value={filter} label="Filter projects" />
	<SearchInput
		label="Search projects"
		placeholder="Search name, language…"
		onsearch={(v) => (q = v)}
	/>
</div>

{#if projects.error && !projects.data}
	<ErrorState message={projects.error} onretry={() => projects.load()} />
{:else if !projects.data}
	<div class="flex flex-col gap-2" aria-busy="true">
		{#each [1, 2, 3, 4, 5, 6] as i (i)}<div class="skeleton h-16"></div>{/each}
		<span class="sr-only">Loading projects</span>
	</div>
{:else if shown.length === 0}
	<EmptyState
		icon={FolderGit2}
		title={all.length === 0 ? 'No projects yet' : 'Nothing matches'}
		message={all.length === 0
			? 'Sync your GitHub repos or add a manual project.'
			: 'Try another filter or search.'}
	/>
{:else}
	<p class="mb-2 font-mono text-xs text-ink-soft">
		Order here is the order on the site. Drag the handle, or use the arrows.
	</p>
	<ol class="border-t border-ink" aria-label="Projects in display order">
		{#each shown as p, i (p.id)}
			<ProjectRow
				project={p}
				first={i === 0}
				last={i === shown.length - 1}
				dragging={dragId === p.id}
				ontoggle={(field, val) => toggle(p, field, val)}
				onstep={(dir) => step(p, dir)}
				ondragstart={() => (dragId = p.id)}
				ondragend={() => (dragId = null)}
				ondropon={(after) => {
					if (dragId !== null) move(dragId, p.id, after);
					dragId = null;
				}}
			/>
		{/each}
	</ol>
{/if}

<Dialog
	bind:open={adding}
	title="Add a manual project"
	description="For work that has no public repo."
>
	<form id="new-project" class="flex flex-col gap-4" onsubmit={create}>
		<div>
			<label class="field-label" for="np-title">Title</label>
			<input
				id="np-title"
				class="input"
				bind:value={newTitle}
				oninput={() => {
					if (!slugTouched) newSlug = slugify(newTitle);
				}}
				aria-invalid={!!createError}
				aria-describedby="np-error"
				required
			/>
		</div>
		<div>
			<label class="field-label" for="np-slug">Slug</label>
			<input
				id="np-slug"
				class="input font-mono text-sm"
				bind:value={newSlug}
				oninput={() => (slugTouched = true)}
				aria-describedby="np-slug-hint"
			/>
			<p id="np-slug-hint" class="hint">/projects/{newSlug || '…'}</p>
		</div>
		{#if createError}<p id="np-error" class="field-error" role="alert">{createError}</p>{/if}
	</form>
	{#snippet footer()}
		<button type="button" class="btn btn-secondary" onclick={() => (adding = false)}>Cancel</button>
		<button type="submit" form="new-project" class="btn btn-primary" disabled={creating}>
			{#if creating}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{/if}
			Create
		</button>
	{/snippet}
</Dialog>
