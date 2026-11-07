<script lang="ts">
	import { ArrowLeft, ExternalLink, LoaderCircle, Save, Trash2 } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { AdminPost, AdminProject, ProjectPatch } from '#lib/api/types.js';
	import MarkdownEditor from '#lib/admin/editor/MarkdownEditor.svelte';
	import ChipInput from '#lib/admin/components/ChipInput.svelte';
	import MediaField from '#lib/admin/components/MediaField.svelte';
	import Switch from '#lib/admin/components/Switch.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import GithubPanel from '#lib/admin/projects/GithubPanel.svelte';
	import { ApiRequestError, del, get, patch } from '#lib/admin/api.js';
	import { guardUnsaved } from '#lib/admin/guard.svelte.js';
	import { errorMessage, slugify } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	type Form = Required<ProjectPatch>;
	const FIELDS: (keyof Form)[] = [
		'slug',
		'title',
		'summary',
		'body_md',
		'tech',
		'period',
		'cover_url',
		'demo_url',
		'post_slug',
		'visible',
		'featured',
		'sort_order'
	];

	function toForm(p: AdminProject): Form {
		return {
			slug: p.slug,
			title: p.title,
			summary: p.summary,
			body_md: p.body_md,
			tech: [...p.tech],
			period: p.period,
			cover_url: p.cover_url,
			demo_url: p.demo_url,
			post_slug: p.post_slug,
			visible: p.visible,
			featured: p.featured,
			sort_order: p.sort_order
		};
	}

	const pid = $derived(page.params.id ?? '');
	let project = $state.raw<AdminProject | null>(null);
	let form = $state<Form | null>(null);
	let baseline = $state<Form | null>(null);
	let error = $state<string | null>(null);
	let saving = $state(false);
	let slugError = $state<string | null>(null);
	let confirmDelete = $state(false);
	let posts = $state<AdminPost[]>([]);

	function set(p: AdminProject) {
		project = p;
		form = toForm(p);
		baseline = toForm(p);
	}

	async function load(id: string) {
		error = null;
		try {
			const p = await get<AdminProject>(`/projects/${id}`);
			if (page.params.id === id) set(p);
		} catch (err) {
			error = errorMessage(err);
		}
	}

	$effect(() => {
		void load(pid);
	});

	get<AdminPost[]>('/posts')
		.then((p) => (posts = p))
		.catch(() => {});

	function changes(): ProjectPatch {
		if (!form || !baseline) return {};
		const out: Record<string, unknown> = {};
		for (const k of FIELDS) {
			if (JSON.stringify(form[k]) !== JSON.stringify(baseline[k]))
				out[k] = $state.snapshot(form[k]);
		}
		return out as ProjectPatch;
	}

	const dirty = $derived(!!form && !!baseline && JSON.stringify(form) !== JSON.stringify(baseline));
	guardUnsaved(() => dirty && !saving);

	async function save() {
		if (!form || saving) return;
		const body = changes();
		if (Object.keys(body).length === 0) return;
		saving = true;
		slugError = null;
		try {
			set(await patch<AdminProject>(`/projects/${pid}`, body));
			toasts.success('Project saved');
		} catch (err) {
			if (err instanceof ApiRequestError && (err.code === 'slug_taken' || err.status === 409)) {
				slugError = err.message || 'Another project already uses this slug.';
			} else {
				toasts.error('Could not save', errorMessage(err));
			}
		} finally {
			saving = false;
		}
	}

	async function remove() {
		try {
			await del(`/projects/${pid}`);
			baseline = form;
			toasts.success('Project deleted');
			await goto('/admin/projects');
		} catch (err) {
			toasts.error('Could not delete', errorMessage(err));
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			void save();
		}
	}

	const name = $derived(project ? project.title || project.repo_full_name || project.slug : '');
	const fallbackTech = $derived(
		project
			? project.gh_topics.length
				? project.gh_topics
				: [project.gh_language].filter(Boolean)
			: []
	);
</script>

<svelte:window {onkeydown} />

<svelte:head>
	<title>{name || 'Project'} · baoopn admin</title>
</svelte:head>

<div class="mb-6 flex flex-wrap items-center gap-3 border-b border-ink pb-4">
	<a href="/admin/projects" class="btn btn-ghost btn-sm"
		><ArrowLeft class="size-4" aria-hidden="true" /> Projects</a
	>
	<h1 class="min-w-0 flex-1 truncate font-display text-2xl font-semibold">{name || 'Project'}</h1>
	{#if dirty}<span class="text-warn font-mono text-xs" role="status">unsaved</span>{/if}
	{#if project?.visible}
		<a href="/projects/{project.slug}" target="_blank" rel="noopener" class="btn btn-secondary">
			<ExternalLink class="size-4" aria-hidden="true" /> View
		</a>
	{/if}
	{#if project}
		<button
			type="button"
			class="btn btn-ghost btn-icon"
			aria-label="Delete project"
			title="Delete project"
			onclick={() => (confirmDelete = true)}
		>
			<Trash2 class="size-4" aria-hidden="true" />
		</button>
	{/if}
	<button type="button" class="btn btn-primary" onclick={save} disabled={!dirty || saving}>
		{#if saving}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Save
				class="size-4"
				aria-hidden="true"
			/>{/if}
		Save
	</button>
</div>

{#if error}
	<ErrorState title="Could not load this project" message={error} onretry={() => load(pid)} />
{:else if !form || !project}
	<div class="grid gap-6 lg:grid-cols-[1fr_20rem]" aria-busy="true">
		<div class="skeleton h-[32rem]"></div>
		<div class="skeleton h-80"></div>
		<span class="sr-only">Loading project</span>
	</div>
{:else}
	<form
		class="grid gap-8 lg:grid-cols-[minmax(0,1fr)_20rem]"
		onsubmit={(e) => {
			e.preventDefault();
			void save();
		}}
	>
		<div class="flex min-w-0 flex-col gap-5">
			<div class="grid gap-4 sm:grid-cols-2">
				<div>
					<label class="field-label" for="pj-title">Title</label>
					<input
						id="pj-title"
						class="input"
						bind:value={form.title}
						placeholder={project.repo_full_name.split('/').pop()}
						aria-describedby="pj-title-hint"
					/>
					<p id="pj-title-hint" class="hint">Empty uses the repo name.</p>
				</div>
				<div>
					<label class="field-label" for="pj-slug">Slug</label>
					<input
						id="pj-slug"
						class="input font-mono text-sm"
						bind:value={form.slug}
						onblur={() => form && (form.slug = slugify(form.slug))}
						oninput={() => (slugError = null)}
						aria-invalid={!!slugError}
						aria-describedby="pj-slug-msg"
						spellcheck="false"
					/>
					{#if slugError}
						<p id="pj-slug-msg" class="field-error" role="alert">{slugError}</p>
					{:else}
						<p id="pj-slug-msg" class="hint">/projects/{form.slug}</p>
					{/if}
				</div>
			</div>

			<div>
				<label class="field-label" for="pj-summary">Summary</label>
				<textarea
					id="pj-summary"
					class="input"
					rows="2"
					bind:value={form.summary}
					placeholder={project.gh_description}
					aria-describedby="pj-summary-hint"></textarea>
				<p id="pj-summary-hint" class="hint">
					{#if !form.summary && project.gh_description}
						Empty uses the GitHub description: “{project.gh_description}”
					{:else}
						One or two sentences for the project card.
					{/if}
				</p>
			</div>

			<div>
				<label class="field-label" for="pj-tech">Tech</label>
				<ChipInput
					id="pj-tech"
					bind:values={form.tech}
					suggestions={[...project.gh_topics, ...Object.keys(project.gh_languages)]}
					describedby="pj-tech-hint"
				/>
				<p id="pj-tech-hint" class="hint">
					{#if form.tech.length === 0 && fallbackTech.length}
						Empty uses GitHub: {fallbackTech.join(', ')}
					{:else}
						Enter or comma to add.
					{/if}
				</p>
			</div>

			<div class="grid gap-4 sm:grid-cols-2">
				<div>
					<label class="field-label" for="pj-period">Period</label>
					<input
						id="pj-period"
						class="input"
						bind:value={form.period}
						placeholder="2024 – present"
					/>
				</div>
				<div>
					<label class="field-label" for="pj-demo">Demo URL</label>
					<input
						id="pj-demo"
						class="input"
						type="url"
						bind:value={form.demo_url}
						placeholder={project.gh_homepage || 'https://'}
						aria-describedby="pj-demo-hint"
					/>
					{#if !form.demo_url && project.gh_homepage}
						<p id="pj-demo-hint" class="hint">Empty uses the GitHub homepage.</p>
					{/if}
				</div>
			</div>

			<div>
				<p class="field-label" id="pj-body-label">Case study</p>
				<MarkdownEditor
					bind:value={form.body_md}
					label="Case study"
					height="26rem"
					placeholder="Write a case study, or leave empty to show the GitHub README."
				/>
			</div>
		</div>

		<aside
			class="flex flex-col gap-5 lg:sticky lg:top-20 lg:self-start"
			aria-label="Project settings"
		>
			<div class="flex flex-col">
				<Switch bind:checked={form.visible} label="Visible on the site" />
				<Switch bind:checked={form.featured} label="Featured on the home page" />
			</div>
			<div>
				<label class="field-label" for="pj-order">Sort order</label>
				<input id="pj-order" class="input" type="number" step="1" bind:value={form.sort_order} />
			</div>
			<MediaField id="pj-cover" label="Cover image" bind:value={form.cover_url} />
			<div>
				<label class="field-label" for="pj-post">Related post</label>
				<select id="pj-post" class="input" bind:value={form.post_slug}>
					<option value="">None</option>
					{#each posts as p (p.id)}
						<option value={p.slug}
							>{p.title || p.slug}{p.state !== 'published' ? ` (${p.state})` : ''}</option
						>
					{/each}
					{#if form.post_slug && !posts.some((p) => p.slug === form?.post_slug)}
						<option value={form.post_slug}>{form.post_slug}</option>
					{/if}
				</select>
			</div>
			<GithubPanel {project} />
		</aside>
	</form>
{/if}

<ConfirmDialog
	bind:open={confirmDelete}
	title="Delete this project?"
	message={project?.github_id
		? 'The project is removed. A synced repo comes back hidden on the next GitHub sync unless it is archived.'
		: 'This manual project is removed permanently. This cannot be undone.'}
	confirmLabel="Delete project"
	danger
	onconfirm={remove}
/>
