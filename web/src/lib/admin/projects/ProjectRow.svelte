<script lang="ts">
	import { ArrowDown, ArrowUp, GitFork, GripVertical, Lock, Pencil, Star } from '@lucide/svelte';
	import type { AdminProject } from '#lib/api/types.js';
	import Switch from '../components/Switch.svelte';
	import RelTime from '../components/RelTime.svelte';
	import { compact } from '../format';

	let {
		project: p,
		first,
		last,
		dragging,
		ontoggle,
		onstep,
		ondragstart,
		ondragend,
		ondropon
	}: {
		project: AdminProject;
		first: boolean;
		last: boolean;
		dragging: boolean;
		ontoggle: (field: 'visible' | 'featured', value: boolean) => void;
		onstep: (dir: -1 | 1) => void;
		ondragstart: () => void;
		ondragend: () => void;
		/** Dropped onto this row; `after` = the lower half. */
		ondropon: (after: boolean) => void;
	} = $props();

	let over = $state<'above' | 'below' | null>(null);
	const name = $derived(p.title || p.repo_full_name || p.slug);

	function half(e: DragEvent): 'above' | 'below' {
		const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
		return e.clientY > r.top + r.height / 2 ? 'below' : 'above';
	}
</script>

<li
	class={[
		'relative flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-rule px-1 py-3 transition-colors sm:flex-nowrap',
		dragging && 'opacity-50',
		!p.visible && 'bg-paper-2'
	]}
	ondragover={(e) => {
		e.preventDefault();
		over = half(e);
	}}
	ondragleave={() => (over = null)}
	ondrop={(e) => {
		e.preventDefault();
		const after = half(e) === 'below';
		over = null;
		ondropon(after);
	}}
>
	{#if over}
		<span
			class={['absolute inset-x-0 h-0.5 bg-ink', over === 'above' ? '-top-px' : '-bottom-px']}
			aria-hidden="true"
		></span>
	{/if}

	<div class="flex items-center">
		<span
			class="grid size-11 cursor-grab place-items-center rounded-md text-ink-soft hover:bg-paper-3 active:cursor-grabbing"
			draggable="true"
			role="presentation"
			title="Drag to reorder"
			ondragstart={(e) => {
				e.dataTransfer?.setData('text/plain', String(p.id));
				if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
				ondragstart();
			}}
			{ondragend}
		>
			<GripVertical class="size-4" aria-hidden="true" />
		</span>
		<div class="flex">
			<button
				type="button"
				class="grid size-11 place-items-center rounded-md text-ink-soft hover:bg-paper-3 hover:text-ink disabled:opacity-30"
				aria-label="Move {name} up"
				disabled={first}
				onclick={() => onstep(-1)}
			>
				<ArrowUp class="size-3.5" aria-hidden="true" />
			</button>
			<button
				type="button"
				class="grid size-11 place-items-center rounded-md text-ink-soft hover:bg-paper-3 hover:text-ink disabled:opacity-30"
				aria-label="Move {name} down"
				disabled={last}
				onclick={() => onstep(1)}
			>
				<ArrowDown class="size-3.5" aria-hidden="true" />
			</button>
		</div>
	</div>

	<div class="min-w-0 flex-1 basis-60">
		<div class="flex flex-wrap items-center gap-2">
			<a href="/admin/projects/{p.id}" class="link truncate font-medium">{name}</a>
			{#if !p.github_id}<span class="tag" style:--tag="var(--spot-peach)">manual</span>{/if}
			{#if p.gh_fork}<span class="tag" style:--tag="var(--spot-sky)"
					><GitFork class="size-3" aria-hidden="true" /> fork</span
				>{/if}
			{#if p.gh_archived}<span class="tag" style:--tag="var(--spot-butter)">archived</span>{/if}
			{#if p.gh_private}<span class="tag" style:--tag="var(--spot-rose)"
					><Lock class="size-3" aria-hidden="true" /> private</span
				>{/if}
		</div>
		<p class="mt-0.5 truncate text-sm text-ink-soft">
			{p.summary || p.gh_description || 'No description'}
		</p>
		<p class="mt-1 flex flex-wrap gap-x-3 font-mono text-xs text-ink-soft">
			{#if p.repo_full_name}<span>{p.repo_full_name}</span>{/if}
			{#if p.gh_language}<span>{p.gh_language}</span>{/if}
			{#if p.github_id}
				<span class="inline-flex items-center gap-1"
					><Star class="size-3" aria-hidden="true" /><span class="sr-only">Stars:</span>
					{compact(p.gh_stars)}</span
				>
				<span>pushed <RelTime value={p.gh_pushed_at} /></span>
				<span>synced <RelTime value={p.synced_at} /></span>
			{/if}
			<span>{compact(p.views_30d)} views · 30d</span>
		</p>
	</div>

	<div class="flex items-center gap-4">
		<Switch
			checked={p.visible}
			label="Visible"
			context={name}
			onchange={(v) => ontoggle('visible', v)}
		/>
		<Switch
			checked={p.featured}
			label="Featured"
			context={name}
			onchange={(v) => ontoggle('featured', v)}
		/>
		<a
			href="/admin/projects/{p.id}"
			class="btn btn-ghost btn-icon btn-sm"
			aria-label="Edit {name}"
			title="Edit"
		>
			<Pencil class="size-4" aria-hidden="true" />
		</a>
	</div>
</li>
