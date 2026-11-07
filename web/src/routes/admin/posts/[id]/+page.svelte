<script lang="ts">
	import { ArrowLeft, ExternalLink, History, LoaderCircle, Save, Trash2 } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { SvelteMap, SvelteSet } from 'svelte/reactivity';
	import { api } from '#lib/api/client.js';
	import type { AdminPost, PostInput, PreviewResult, TagCount } from '#lib/api/types.js';
	import MarkdownEditor from '#lib/admin/editor/MarkdownEditor.svelte';
	import PostMeta from '#lib/admin/posts/PostMeta.svelte';
	import PostParts from '#lib/admin/posts/PostParts.svelte';
	import { emptyPart, estimateMinutes, normalizeInput, partKey } from '#lib/admin/posts/parts.js';
	import { partHref } from '#lib/site/post.js';
	import StateChip from '#lib/admin/components/StateChip.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import { ApiRequestError, del, get, post, put } from '#lib/admin/api.js';
	import { clearDraft, readDraft, writeDraft, type LocalDraft } from '#lib/admin/drafts.js';
	import { guardUnsaved } from '#lib/admin/guard.svelte.js';
	import { dateTime, errorMessage, slugify, wordCount } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';
	import { uploads } from '#lib/admin/media/upload.svelte.js';

	const routeId = $derived(page.params.id ?? 'new');
	const isNew = $derived(routeId === 'new');

	function emptyForm(): PostInput {
		return {
			slug: '',
			title: '',
			excerpt: '',
			cover_url: '',
			category: 'tech',
			tags: [],
			status: 'draft',
			published_at: null,
			parts: [emptyPart()]
		};
	}

	function toForm(p: AdminPost): PostInput {
		return {
			slug: p.slug,
			title: p.title,
			excerpt: p.excerpt,
			cover_url: p.cover_url,
			category: p.category === 'life' ? 'life' : 'tech',
			tags: [...(p.tags ?? [])],
			status: p.status,
			published_at: p.published_at,
			parts: p.parts?.length
				? p.parts.map((x) => ({ title: x.title, content_md: x.content_md }))
				: [emptyPart()]
		};
	}

	let form = $state<PostInput>(emptyForm());
	let baseline = $state(JSON.stringify(emptyForm()));
	let current = $state.raw<AdminPost | null>(null);
	let loadedFor = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let slugTouched = $state(false);
	let slugError = $state<string | null>(null);
	let saving = $state(false);
	let confirmUploads = $state(false);
	let keys = $state<number[]>([partKey()]);
	let active = $state(0);
	// Reading minutes known from the API, by Markdown text (saved parts + last preview).
	const knownMinutes = new SvelteMap<string, number>();
	let savedTexts = new SvelteSet<string>();
	let lastPreviewed: string | null = null;
	let restorable = $state.raw<LocalDraft<PostInput> | null>(null);
	let confirmDelete = $state(false);

	const snapshot = $derived(JSON.stringify(form));
	const dirty = $derived(loadedFor === routeId && snapshot !== baseline);
	const activePart = $derived(form.parts[active] ?? form.parts[0]);
	const partWords = $derived(wordCount(activePart.content_md));
	const totalWords = $derived(form.parts.reduce((n, p) => n + wordCount(p.content_md), 0));
	const minutesFor = (md: string) => knownMinutes.get(md) ?? estimateMinutes(md);
	const partMinutes = $derived(Math.max(1, minutesFor(activePart.content_md)));
	const totalMinutes = $derived(
		Math.max(1, form.parts.reduce((n, p) => n + minutesFor(p.content_md), 0))
	);

	function onpreview(r: PreviewResult, md: string) {
		// Keep saved parts' values and only the latest preview of the open part.
		if (lastPreviewed !== null && !savedTexts.has(lastPreviewed)) knownMinutes.delete(lastPreviewed);
		knownMinutes.set(md, r.reading_minutes);
		lastPreviewed = md;
	}
	function viewHref(i: number): string | undefined {
		if (current?.state !== 'published' || i >= current.parts.length) return undefined;
		return partHref(current.slug, i + 1);
	}

	function reset(f: PostInput, p: AdminPost | null) {
		form = f;
		baseline = JSON.stringify(f);
		current = p;
		slugTouched = !!p;
		slugError = null;
		keys = f.parts.map(() => partKey());
		active = 0;
		knownMinutes.clear();
		for (const x of p?.parts ?? []) knownMinutes.set(x.content_md, x.reading_minutes);
		savedTexts = new SvelteSet(knownMinutes.keys());
		lastPreviewed = null;
	}

	function offerDraft(key: string) {
		const d = readDraft<PostInput>(key);
		restorable = d && JSON.stringify(d.value) !== baseline ? d : null;
	}

	async function load(rid: string) {
		loadError = null;
		if (rid === 'new') {
			reset(emptyForm(), null);
			loadedFor = rid;
			offerDraft('post:new');
			return;
		}
		try {
			const p = await get<AdminPost>(`/posts/${rid}`);
			if (page.params.id !== rid) return;
			reset(toForm(p), p);
			loadedFor = rid;
			offerDraft(`post:${rid}`);
		} catch (err) {
			loadError = errorMessage(err);
		}
	}

	// Load when the id changes (skipped right after creating, see save()).
	$effect(() => {
		const rid = routeId;
		if (loadedFor !== rid) void load(rid);
	});

	// Autosave to localStorage while there are unsaved changes.
	$effect(() => {
		if (!dirty) return;
		const key = `post:${routeId}`;
		const value = $state.snapshot(form);
		const t = setTimeout(() => writeDraft(key, value), 800);
		return () => clearTimeout(t);
	});

	let tagSuggestions = $state<string[]>([]);
	api<TagCount[]>('/api/v1/tags')
		.then((tags) => (tagSuggestions = tags.map((t) => t.tag)))
		.catch(() => {});

	guardUnsaved(() => dirty && !saving);

	function ontitle() {
		if (!slugTouched) form.slug = slugify(form.title);
	}

	async function save(force = false) {
		if (saving) return;
		if (!force && uploads.pending > 0) {
			confirmUploads = true;
			return;
		}
		if (!form.title.trim()) {
			toasts.error('A title is required');
			document.getElementById('post-title')?.focus();
			return;
		}
		saving = true;
		slugError = null;
		const body: PostInput = { ...$state.snapshot(form), slug: form.slug?.trim() || undefined };
		try {
			const wasNew = isNew;
			const p = wasNew
				? await post<AdminPost>('/posts', body)
				: await put<AdminPost>(`/posts/${routeId}`, body);
			clearDraft(`post:${routeId}`);
			restorable = null;
			reset(toForm(p), p);
			loadedFor = String(p.id);
			toasts.success(
				p.state === 'scheduled'
					? 'Scheduled'
					: p.state === 'published'
						? 'Published'
						: 'Draft saved'
			);
			if (wasNew) await goto(`/admin/posts/${p.id}`, { replaceState: true });
		} catch (err) {
			if (err instanceof ApiRequestError && err.code === 'slug_taken') {
				slugError = err.message || 'Another post already uses this slug.';
				document.getElementById('post-meta')?.scrollIntoView({ block: 'nearest' });
			} else {
				toasts.error('Could not save', errorMessage(err));
			}
		} finally {
			saving = false;
		}
	}

	async function remove() {
		try {
			await del(`/posts/${routeId}`);
			clearDraft(`post:${routeId}`);
			baseline = snapshot;
			toasts.success('Post deleted');
			await goto('/admin/posts');
		} catch (err) {
			toasts.error('Could not delete', errorMessage(err));
		}
	}

	function restore() {
		if (!restorable) return;
		form = normalizeInput(restorable.value, emptyForm());
		keys = form.parts.map(() => partKey());
		active = 0;
		slugTouched = true;
		restorable = null;
	}

	function discardDraft() {
		clearDraft(`post:${routeId}`);
		restorable = null;
	}

	function onkeydown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			void save();
		}
	}
</script>

<svelte:window {onkeydown} />

<svelte:head>
	<title>{isNew ? 'New post' : form.title || 'Edit post'} · baoopn admin</title>
</svelte:head>

<div class="mb-6 flex flex-wrap items-center gap-3 border-b border-ink pb-4">
	<a href="/admin/posts" class="btn btn-ghost btn-sm"
		><ArrowLeft class="size-4" aria-hidden="true" /> Posts</a
	>
	<div class="flex min-w-0 flex-1 flex-wrap items-center gap-3">
		<h1 class="font-display text-2xl font-semibold">{isNew ? 'New post' : 'Edit post'}</h1>
		{#if current}<StateChip state={current.state} />{/if}
		<span class="font-mono text-xs text-ink-soft" aria-live="polite">
			{#if form.parts.length > 1}
				Part {active + 1}: {partWords.toLocaleString()} words · {partMinutes} min · Total: {totalWords.toLocaleString()}
				words · {totalMinutes} min read
			{:else}
				{totalWords.toLocaleString()} words · {totalMinutes} min read
			{/if}
			{#if dirty}· <strong class="text-warn">unsaved</strong>{:else if current}· saved <RelTime
					value={current.updated_at}
				/>{/if}
		</span>
	</div>
	<div class="flex flex-wrap gap-2">
		{#if current?.state === 'published'}
			<a href="/blog/{current.slug}" target="_blank" rel="noopener" class="btn btn-secondary">
				<ExternalLink class="size-4" aria-hidden="true" /> View
			</a>
		{/if}
		{#if current}
			<button
				type="button"
				class="btn btn-ghost btn-icon"
				aria-label="Delete post"
				title="Delete post"
				onclick={() => (confirmDelete = true)}
			>
				<Trash2 class="size-4" aria-hidden="true" />
			</button>
		{/if}
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => save()}
			disabled={saving || loadedFor !== routeId}
			title="Save (Ctrl/⌘+S)"
		>
			{#if saving}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Save
					class="size-4"
					aria-hidden="true"
				/>{/if}
			Save
		</button>
	</div>
</div>

{#if loadError}
	<ErrorState title="Could not load this post" message={loadError} onretry={() => load(routeId)} />
{:else if loadedFor !== routeId}
	<div class="grid gap-6 xl:grid-cols-[1fr_20rem]" aria-busy="true">
		<div class="flex flex-col gap-4">
			<div class="skeleton h-12"></div>
			<div class="skeleton h-[28rem]"></div>
		</div>
		<div class="skeleton h-96"></div>
		<span class="sr-only">Loading post</span>
	</div>
{:else}
	{#if restorable}
		<div
			class="mb-5 flex flex-wrap items-center gap-3 rounded-lg border border-on-spot bg-spot-sky px-4 py-3 text-sm text-on-spot"
			role="status"
		>
			<History class="size-4" aria-hidden="true" />
			<span class="flex-1"
				>An unsaved local draft from {dateTime(restorable.saved_at)} is available.</span
			>
			<button type="button" class="btn btn-secondary btn-sm" onclick={restore}>Restore</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={discardDraft}>Discard</button>
		</div>
	{/if}

	<div class="grid gap-8 xl:grid-cols-[minmax(0,1fr)_20rem]">
		<div class="flex min-w-0 flex-col gap-4">
			<div>
				<label class="sr-only" for="post-title">Title</label>
				<input
					id="post-title"
					class="input font-display text-2xl font-semibold"
					placeholder="Post title"
					bind:value={form.title}
					oninput={ontitle}
					autocomplete="off"
				/>
			</div>
			<PostParts bind:parts={form.parts} bind:keys bind:active {viewHref} />
			{#key keys[active]}
				<MarkdownEditor
					bind:value={activePart.content_md}
					label="Part {active + 1} content"
					height="calc(100dvh - 22rem)"
					{onpreview}
				/>
			{/key}
		</div>
		<aside id="post-meta" aria-label="Post settings" class="xl:sticky xl:top-20 xl:self-start">
			<PostMeta bind:form bind:slugTouched bind:slugError {tagSuggestions} />
		</aside>
	</div>
{/if}

<ConfirmDialog
	bind:open={confirmDelete}
	title="Delete this post?"
	message="“{form.title ||
		'Untitled'}” is removed permanently, including its view counts. This cannot be undone."
	confirmLabel="Delete post"
	danger
	onconfirm={remove}
/>

<ConfirmDialog
	bind:open={confirmUploads}
	title="Uploads still in progress"
	message={uploads.message}
	confirmLabel="Save anyway"
	onconfirm={() => save(true)}
/>
