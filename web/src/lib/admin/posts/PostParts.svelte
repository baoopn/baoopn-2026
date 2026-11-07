<script lang="ts">
	import { ArrowDown, ArrowUp, ExternalLink, Plus, Trash2 } from '@lucide/svelte';
	import ConfirmDialog from '../components/ConfirmDialog.svelte';
	import { MAX_PARTS, emptyPart, partKey, type PartInput } from './parts';

	let {
		parts = $bindable(),
		keys = $bindable(),
		active = $bindable(0),
		viewHref
	}: {
		parts: PartInput[];
		/** Stable identity per part, parallel to `parts`. */
		keys: number[];
		/** Index of the part open in the editor. */
		active?: number;
		/** Public URL of the part at an index, when it is live. */
		viewHref?: (index: number) => string | undefined;
	} = $props();

	const id = $props.id();
	let confirmDelete = $state(false);

	const label = (p: PartInput, i: number) => p.title.trim() || `Part ${i + 1}`;
	const current = $derived(parts[active] ?? parts[0]);
	const view = $derived(viewHref?.(active));

	function add() {
		if (parts.length >= MAX_PARTS) return;
		parts.push(emptyPart());
		keys.push(partKey());
		active = parts.length - 1;
	}

	function move(delta: -1 | 1) {
		const from = active;
		const to = from + delta;
		if (to < 0 || to >= parts.length) return;
		[parts[from], parts[to]] = [parts[to], parts[from]];
		[keys[from], keys[to]] = [keys[to], keys[from]];
		active = to;
	}

	function remove() {
		if (parts.length <= 1) return;
		const at = active;
		active = Math.max(0, Math.min(at, parts.length - 2));
		parts.splice(at, 1);
		keys.splice(at, 1);
	}
</script>

<section aria-labelledby="{id}-h" class="rounded-[10px] border border-rule-strong bg-paper-2 p-2">
	<div class="flex flex-wrap items-center gap-2">
		<h2 id="{id}-h" class="eyebrow px-1">Parts · {parts.length}/{MAX_PARTS}</h2>
		<ol class="flex min-w-0 flex-1 flex-wrap gap-1">
			{#each parts as part, i (keys[i] ?? i)}
				<li>
					<button
						type="button"
						class="part-tab"
						aria-current={i === active ? 'true' : undefined}
						onclick={() => (active = i)}
					>
						<span class="font-mono text-xs tabular-nums">{String(i + 1).padStart(2, '0')}</span>
						<span class="max-w-[12rem] truncate">{label(part, i)}</span>
					</button>
				</li>
			{/each}
		</ol>
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={add}
			disabled={parts.length >= MAX_PARTS}
			title={parts.length >= MAX_PARTS ? `A post has at most ${MAX_PARTS} parts` : 'Add a part'}
		>
			<Plus class="size-4" aria-hidden="true" /> Add part
		</button>
	</div>

	<div class="mt-2 flex flex-wrap items-end gap-2 border-t border-rule px-1 pt-2">
		<div class="min-w-[12rem] flex-1">
			<label class="field-label" for="{id}-title">Part {active + 1} title</label>
			<input
				id="{id}-title"
				class="input"
				bind:value={current.title}
				placeholder="Part {active + 1}"
				maxlength="200"
				autocomplete="off"
				aria-describedby="{id}-title-hint"
			/>
			<p id="{id}-title-hint" class="sr-only">Empty shows as “Part {active + 1}”.</p>
		</div>
		<div class="flex gap-1">
			<button
				type="button"
				class="btn btn-ghost btn-icon"
				aria-label="Move part {active + 1} earlier"
				title="Move earlier"
				disabled={active === 0}
				onclick={() => move(-1)}
			>
				<ArrowUp class="size-4" aria-hidden="true" />
			</button>
			<button
				type="button"
				class="btn btn-ghost btn-icon"
				aria-label="Move part {active + 1} later"
				title="Move later"
				disabled={active >= parts.length - 1}
				onclick={() => move(1)}
			>
				<ArrowDown class="size-4" aria-hidden="true" />
			</button>
			{#if view}
				<a
					href={view}
					target="_blank"
					rel="noopener"
					class="btn btn-ghost btn-icon"
					aria-label="View part {active + 1} on the site"
					title="View this part"
				>
					<ExternalLink class="size-4" aria-hidden="true" />
				</a>
			{/if}
			<button
				type="button"
				class="btn btn-ghost btn-icon"
				aria-label="Delete part {active + 1}"
				title={parts.length <= 1 ? 'A post needs at least one part' : 'Delete part'}
				disabled={parts.length <= 1}
				onclick={() => (confirmDelete = true)}
			>
				<Trash2 class="size-4" aria-hidden="true" />
			</button>
		</div>
	</div>
</section>

<ConfirmDialog
	bind:open={confirmDelete}
	title="Delete part {active + 1}?"
	message="“{label(current, active)}” and its text are removed from this post. The change is kept only when you save."
	confirmLabel="Delete part"
	danger
	onconfirm={remove}
/>

<style>
	.part-tab {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		min-height: 2.75rem;
		padding: 0 0.75rem;
		border-radius: 8px;
		font-size: 0.875rem;
		transition: background-color 160ms ease-out;
	}
	.part-tab:hover {
		background: var(--paper);
	}
	.part-tab:focus-visible {
		outline: 2px solid var(--ink);
		outline-offset: 2px;
	}
	.part-tab[aria-current='true'] {
		background: var(--spot-select);
		color: var(--on-spot);
		font-weight: 600;
	}
</style>
